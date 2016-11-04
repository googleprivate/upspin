// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"fmt"
	"time"

	"upspin.io/dir/server/tree"
	"upspin.io/errors"
	"upspin.io/log"
	"upspin.io/path"
	"upspin.io/upspin"
	"upspin.io/user"
)

const (
	snapshotSuffix            = "snapshot"
	snapshotGlob              = "*+" + snapshotSuffix + "@*"
	snapshotControlFile       = "TakeSnapshot"
	snapshotDefaultDateFormat = "2006/01/02"
	snapshotDefaultInterval   = 12 * time.Hour
	snapshotWorkerInterval    = 2 * time.Hour
)

// snapshotConfig holds the configuration for a snapshot. Users may have
// multiple such configurations.
type snapshotConfig struct {
	srcDir     upspin.PathName
	dstDir     upspin.PathName
	dateFormat string // must be formattable by time.Format.
	interval   time.Duration
}

// getSnapshotConfig retrieves all configured snapshots for a user and domain
// pair, as returned by user.Parse.
func (s *server) getSnapshotConfig(userName upspin.UserName) (*snapshotConfig, error) {
	uname, suffix, domain, err := user.Parse(userName)
	if err != nil {
		return nil, err
	}
	if suffix != snapshotSuffix {
		return nil, errors.E(errors.Internal, userName,
			errors.Errorf("invalid snapshot suffix: %q", suffix))
	}

	// Strip the suffix from the username.
	uname = uname[:len(uname)-len(snapshotSuffix)-1]

	return &snapshotConfig{
		srcDir:     upspin.PathName(uname + "@" + domain + "/"),
		dstDir:     upspin.PathName(userName),
		dateFormat: snapshotDefaultDateFormat,
		interval:   snapshotDefaultInterval,
	}, nil
}

func (s *server) startSnapshotLoop() {
	if s.snapshotControl != nil {
		log.Error.Printf("dir/server: Attempting to restart snapshot worker")
		return
	}
	s.snapshotControl = make(chan upspin.UserName)
	go s.snapshotLoop()
}

func (s *server) stopSnapshotLoop() {
	if s.snapshotControl != nil {
		close(s.snapshotControl)
	}
}

// snapshotLoop runs in a goroutine and performs periodic snapshots.
func (s *server) snapshotLoop() {
	// Run once upon starting.
	s.snapshotAll() // returned error is already logged.

	// Run periodically.
	ticker := time.NewTicker(snapshotWorkerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.snapshotAll() // returned error is already logged.
		case userName := <-s.snapshotControl:
			if userName == "" {
				// Closing the channel.
				return
			}
			s.takeSnapshotFor(userName)
		}
	}
}

// snapshotAll scans all roots that have a +snapshot suffix, determines whether
// it's time to perform a new snapshot for them and if so snapshots them.
func (s *server) snapshotAll() error {
	const op = "dir/server.snapshotAll"
	users, err := tree.ListUsers(snapshotGlob, s.logDir)
	if err != nil {
		log.Error.Printf("%s: error listing snapshot users: %s", op, err)
		return err
	}
	var firstErr error
	check := func(err error) error {
		if firstErr == nil {
			firstErr = err
		}
		return err
	}
	for _, userName := range users {
		cfg, err := s.getSnapshotConfig(userName)
		if check(err) != nil {
			log.Error.Printf("%s: can't get config for user %q", op, userName)
			continue
		}
		ok, dstPath, err := s.shouldSnapshot(cfg)
		if check(err) != nil {
			log.Error.Printf("%s: error checking whether to snapshot: %s", op, err)
			continue
		}
		if !ok {
			continue
		}
		err = s.takeSnapshot(dstPath, cfg.srcDir)
		if check(err) != nil {
			log.Error.Printf("%s: error snapshotting: %s", op, err)
		}
	}
	return firstErr
}

// snapshotDir returns the destination path for a snapshot given its
// configuration.
func (s *server) snapshotDir(cfg *snapshotConfig) (path.Parsed, error) {
	date := s.now().Go().UTC().Format(cfg.dateFormat)
	dstDir := path.Join(cfg.dstDir, date)

	p, err := path.Parse(dstDir)
	if err != nil {
		return path.Parsed{}, err
	}
	return p, nil
}

// shouldSnapshot reports whether it's time to snapshot the given configuration.
// It also returns the parsed path of where the snapshot will be made.
func (s *server) shouldSnapshot(cfg *snapshotConfig) (bool, path.Parsed, error) {
	const op = "dir/server.shouldSnapshot"

	p, err := s.snapshotDir(cfg)
	if err != nil {
		return false, path.Parsed{}, errors.E(op, err)
	}

	entry, err := s.lookup(op, p, !entryMustBeClean)
	if err != nil {
		if err == upspin.ErrFollowLink {
			// We need to get the real entry and we cannot resolve links on our own.
			return false, path.Parsed{}, errors.E(op, errors.Internal, p.Path(), errors.Str("cannot follow a link to snapshot"))
		}
		if !errors.Match(errNotExist, err) {
			// Some other error. Abort.
			return false, path.Parsed{}, errors.E(op, err)
		}
		// Ok, proceed.
	} else {
		// Is entry so old that a new snapshot is now warranted?
		if entry.Time.Go().Add(cfg.interval).After(s.now().Go()) {
			// Not time yet. Nothing to do.
			return false, p, nil
		}
		// Ok, proceed.
	}
	return true, p, nil
}

// takeSnapshotFor takes a snapshot for a user.
func (s *server) takeSnapshotFor(user upspin.UserName) error {
	cfg, err := s.getSnapshotConfig(user)
	if err != nil {
		return err
	}
	dstDir, err := s.snapshotDir(cfg)
	if err != nil {
		return err
	}
	return s.takeSnapshot(dstDir, cfg.srcDir)
}

// takeSnapshot takes a snapshot to dstDir from srcDir.
func (s *server) takeSnapshot(dstDir path.Parsed, srcDir upspin.PathName) error {
	srcParsed, err := path.Parse(srcDir)
	if err != nil {
		return err
	}
	entry, err := s.lookup("takeSnapshot", srcParsed, entryMustBeClean)
	if err != nil {
		return err
	}

	tree, err := s.loadTreeFor(dstDir.User())
	if err != nil {
		return err
	}

	dstDir, err = nextDirectoryVersion(tree, dstDir)
	if err != nil {
		return err
	}
	err = s.makeSnapshotPath(dstDir.Path())
	if err != nil {
		return err
	}

	// Update time so we know when the snapshot was taken.
	entry.Time = s.now()
	snapEntry, err := tree.PutDir(dstDir, entry)
	if err != nil {
		return err
	}

	log.Printf("dir/server: Snapshotted %q into %q", entry.SignedName, snapEntry.Name)
	return nil
}

// nextDirectoryVersion examines the tree and finds the next suitable name to
// create, by adding a monotonically-increasing version number to the original
// name. For example, if dir represents path "/2016/09/18" and under directory
// "/2016/09" there exists a "18" entry, it then would return "18.1". And if
// that exists it would return "18.2", and so on.
func nextDirectoryVersion(tree *tree.Tree, dir path.Parsed) (path.Parsed, error) {
	next := dir
	for i := 1; i < 1000; i++ {
		_, _, err := tree.Lookup(next)
		if errors.Match(errNotExist, err) {
			return next, nil
		}
		if err != nil {
			return path.Parsed{}, err
		}
		next, err = path.Parse(upspin.PathName(fmt.Sprintf("%s.%d", dir, i)))
		if err != nil {
			return path.Parsed{}, err
		}
	}
	return path.Parsed{}, errors.E(errors.Internal, errors.Str("too many attempts at creating snapshot directory"))
}

// makeSnapshotPath makes the full path name, creating any necessary
// subdirectories.
func (s *server) makeSnapshotPath(name upspin.PathName) error {
	p, err := path.Parse(name)
	if err != nil {
		return err
	}
	// Traverse the path one element of a time making each subdir. We start
	// from 1 as we don't try to make the root.
	for i := 1; i <= p.NElem(); i++ {
		err = s.mkDirIfNotExist(p.First(i))
		if err != nil {
			return err
		}
	}
	return nil
}

// mkDirIfNotExist makes a directory if it does not yet exist.
func (s *server) mkDirIfNotExist(name path.Parsed) error {
	// Create a new dir entry for this new dir.
	de := &upspin.DirEntry{
		Name:       name.Path(),
		SignedName: name.Path(),
		Attr:       upspin.AttrDirectory,
		Writer:     name.User(),
		Packing:    s.serverContext.Packing(),
		Time:       upspin.Now(),
		Sequence:   0, // Tree will increment when flushed.
	}

	tree, err := s.loadTreeFor(name.User())
	if err != nil {
		return err
	}
	_, _, err = tree.Lookup(name)
	if err == upspin.ErrFollowLink {
		return errors.E(errors.Internal, errors.Str("cannot mkdir through a link"))
	}
	if err != nil && !errors.Match(errNotExist, err) {
		// Real error. Abort.
		return err
	}
	if err == nil {
		// Directory exists. We're done.
		return nil
	}
	// Attempt to put this new dir entry.
	_, err = tree.Put(name, de)
	return err
}

// TODO: isSnapshotUser and isSnapshotOwner should be combined and simplified to
// avoid calling parse every time.

// isSnapshotUser reports whether the userName contains the snapshot suffix.
func isSnapshotUser(userName upspin.UserName) bool {
	_, suffix, _, err := user.Parse(userName)
	if err != nil {
		log.Error.Printf("isSnapshotUser: error parsing user name %q: %s", userName, err)
		return false
	}
	return suffix == snapshotSuffix
}

// isSnapshotOwner reports whether username is the base user name (without the
// "+snapshot" suffix) of snapshotUser or the snapshotUser itself.
func isSnapshotOwner(userName upspin.UserName, snapshotUser upspin.UserName) bool {
	u, suffix, domain, err := user.Parse(userName)
	if err != nil {
		// This should not happen. Log the error.
		log.Error.Printf("isSnapshotOwner: error parsing %q: %s", userName, err)
		return false
	}
	if suffix != "" && suffix != snapshotSuffix {
		// Some other suffix. Definitely not the base user nor the
		// snapshotUser.
		return false
	}
	if suffix == snapshotSuffix {
		// userName is snapshotUser or it's another snapshot user.
		return snapshotUser == userName
	}
	// userName is the owner if and only if adding the snapshot suffix makes
	// it the snapshotUser.
	return u+"+"+snapshotSuffix+"@"+domain == string(snapshotUser)
}

// isSnapshotControlFile reports whether the path name is for an entry in the
// root named snapshotControlFile.
func isSnapshotControlFile(p path.Parsed) bool {
	return p.NElem() == 1 && p.Elem(0) == snapshotControlFile
}

// isValidSnapshotControlEntry reports whether an entry correctly represents the
// control entry we expect in order to start a new snapshot.
func isValidSnapshotControlEntry(entry *upspin.DirEntry) error {
	if len(entry.Blocks) != 0 || entry.Packing != upspin.PlainPack || entry.IsLink() || entry.IsDir() {
		return errors.E(errors.Invalid, entry.Name, errors.Str("snapshot control entry must be an empty plain-packed file"))
	}
	return nil
}
