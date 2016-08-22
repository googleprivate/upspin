// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

// This file deals with loading Access files and checking access permissions.

// TODO: add a cache and a negative cache for the parsed Access files.

import (
	"upspin.io/access"
	"upspin.io/client/clientutil"
	"upspin.io/errors"
	"upspin.io/log"
	"upspin.io/path"
	"upspin.io/upspin"
)

// whichAccessNoCache implements DirServer.WhichAccess without doing any
// caching of Access files.
// userLock must be held for p.User().
func (s *server) whichAccessNoCache(p path.Parsed) (*upspin.DirEntry, error) {
	const op = "dir/server.whichAccessNoCache"
	tree, err := s.loadTreeFor(p.User())
	if err != nil {
		return nil, errors.E(op, err)
	}
	// Do tree lookups until we find an Access file. Lookups start at the
	// root and go forward till the named path. If no Access file is there,
	// pop up one level. This is so we can find the closest Acesss file,
	// while being aware of links in the way.
	for {
		accPath := path.Join(p.Path(), "Access")
		log.Debug.Printf("%s: Looking up %s", op, accPath)
		entry, _, err := tree.Lookup(accPath)
		if err == upspin.ErrFollowLink {
			// If we got ErrFollowLink in the first iteration of the
			// look, it's because we're trying to look up a link,
			// say ".../link/Access". In this case, we need to go
			// one level up since the access file for the link
			// in in an ancestor of of the path.
			if entry.Name != p.Path() {
				// The link is not what we're looking up.
				return entry, upspin.ErrFollowLink
			}
			// Drop and continue (it's safe to drop because roots
			// are never links, so we're definitely not at the root.
			p = p.Drop(1)
			continue
		}
		if errors.Match(errNotExist, err) {
			if p.IsRoot() {
				// Already at the root, nothing found.
				return nil, nil
			}
			p = p.Drop(1)
			continue
		}
		if err != nil {
			return nil, errors.E(op, err)
		}
		// Found the Access file.
		return entry, nil
	}
}

// whichAccess returns the DirEntry of the ruling Access file on a path or a
// DirEntry of the link if ErrFollowLink is returned.
// userLock must be held for p.User().
func (s *server) whichAccess(p path.Parsed) (*upspin.DirEntry, error) {
	const op = "dir/server.whichAccess"
	// TODO: check the cache and negcache for an access dir entry for this path.

	entry, err := s.whichAccessNoCache(p)
	if err == upspin.ErrFollowLink {
		return entry, err
	}
	if err != nil {
		// TODO: if not found, record that fact in a negative cache.
		return nil, errors.E(op, err)
	}

	// TODO: add acc to a cache.
	return entry, nil
}

// loadAccess loads and processes an Access file from its DirEntry.
func (s *server) loadAccess(entry *upspin.DirEntry) (*access.Access, error) {
	log.Debug.Printf("loadAccess: entry: %v", entry)
	buf, err := clientutil.ReadAll(s.serverContext, entry)
	if err != nil {
		return nil, err
	}
	return access.Parse(entry.Name, buf)
}

// loadPath loads a name from the Store, if its entry can be resolved by this
// DirServer. Intended for use with access.Can. The userLock for the current
// user must be held.
func (s *server) loadPath(name upspin.PathName) ([]byte, error) {
	const op = "dir/server.loadPath"
	p, err := path.Parse(name)
	if err != nil {
		return nil, errors.E(op, err)
	}
	// TODO: relax this constraint, possibly using Client in a goroutine.
	// https://github.com/googleprivate/upspin/issues/37
	if p.User() != s.userName {
		return nil, errors.E(op, name, errors.Str("can't fetch other user's Access/Group files"))
	}
	tree, err := s.loadTreeFor(p.User())
	if err != nil {
		return nil, errors.E(op, err)
	}
	entry, _, err := tree.Lookup(name)
	if err != nil {
		return nil, errors.E(op, err)
	}
	return clientutil.ReadAll(s.serverContext, entry)
}

// hasRight reports whether the current user has the given right on the path. If
// ErrFollowLink is returned, the DirEntry will be that of the link.
// userLock must be held for p.User().
func (s *server) hasRight(right access.Right, p path.Parsed) (bool, *upspin.DirEntry, error) {
	const op = "dir/server.hasRight"
	entry, err := s.whichAccess(p)
	if err == upspin.ErrFollowLink {
		return false, entry, upspin.ErrFollowLink
	}
	if err != nil {
		return false, nil, errors.E(op, err)
	}
	// TODO: look up in accessCache.
	var acc *access.Access
	if entry != nil {
		acc, err = s.loadAccess(entry)
		if err != nil {
			return false, nil, errors.E(op, err)
		}
	} else {
		acc = s.defaultAccess
	}
	can, err := acc.Can(s.userName, right, p.Path(), s.loadPath)
	if err != nil {
		return false, nil, errors.E(op, err)
	}
	return can, nil, nil
}
