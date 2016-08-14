// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tree

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"upspin.io/context"
	"upspin.io/errors"
	"upspin.io/factotum"
	"upspin.io/upspin"

	_ "upspin.io/key/inprocess"
	_ "upspin.io/pack/ee"
	_ "upspin.io/store/inprocess"
)

const (
	userName   = "user@domain.com"
	serverName = "tree@server.com"
	isDir      = true
)

// This test checks the tree for log consistency by exercising the life-cycle of a tree,
// from creating a new tree from scratch, adding new nodes, flushing it to Store then
// adding more nodes to a new tree and having to load it from the Store.
func TestPutNodes(t *testing.T) {
	cfg := newConfigForTesting(t)
	tree := New(userName, cfg)

	dir1 := newDirEntry("/", isDir, cfg)
	err := tree.Put(dir1)
	if err != nil {
		t.Fatal(err)
	}
	dir2 := newDirEntry("/dir", isDir, cfg)
	err = tree.Put(dir2)
	if err != nil {
		t.Fatal(err)
	}
	dir3 := newDirEntry("/dir/doc.pdf", !isDir, cfg)
	err = tree.Put(dir3)
	if err != nil {
		t.Fatal(err)
	}

	// Verify three log entries were written.
	if got, want := cfg.Log.LastOffset(), int64(3); got != want {
		t.Fatalf("LastIndex = %d, want %d", got, want)
	}
	entries, _, err := cfg.Log.ReadAt(3, int64(0))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(entries), 3; got != want {
		t.Errorf("len(entries) = %d, want = %d", got, want)
	}
	if !reflect.DeepEqual(&entries[0].Entry, dir1) {
		t.Errorf("dir1 = %v, want %v", entries[0], dir1)
	}
	if !reflect.DeepEqual(&entries[1].Entry, dir2) {
		t.Errorf("dir2 = %v, want %v", entries[1], dir2)
	}
	if !reflect.DeepEqual(&entries[2].Entry, dir3) {
		t.Errorf("dir3 = %v, want %v", entries[2], dir3)
	}

	// Lookup path.
	de, dirty, err := tree.Lookup(userName + "/dir/doc.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if !dirty {
		t.Errorf("dirty = %v, want %v", dirty, true)
	}
	if !reflect.DeepEqual(de, dir3) {
		t.Errorf("de = %v, want %v", de, dir3)
	}

	// Flush to later build a new tree and verify new is equivalent to old.
	err = tree.Flush()
	if err != nil {
		t.Fatal(err)
	}

	// New log index shows we're now at the end of the log.
	got, err := cfg.LogIndex.ReadOffset()
	if err != nil {
		t.Fatal(err)
	}
	if want := cfg.Log.LastOffset(); got != want {
		t.Fatalf("cfg.Log.LastIndex() = %d, want %d", got, want)
	}

	// Lookup now returns !dirty.
	de, dirty, err = tree.Lookup(userName + "/dir/doc.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if dirty {
		t.Errorf("dirty = %v, want %v", dirty, false)
	}
	if got, want := de.Name, dir3.Name; got != want {
		t.Errorf("de.Name = %v, want %v", de.Name, want)
	}

	// Now start a new tree from scratch and confirm it is loaded from the Store.
	tree2 := New(userName, cfg)

	t.Logf("== Tree:\n%s\n", tree2.String())
	dir4 := newDirEntry("/dir/img.jpg", !isDir, cfg)
	err = tree2.Put(dir4)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.Log.LastOffset(), int64(4); got != want {
		t.Fatalf("cfg.Log.LastIndex() = %d, want %d", cfg.Log.LastOffset(), want)
	}

	t.Logf("== Tree:\n%s\n", tree2.String())

	// Delete dir4.
	err = tree2.Delete(userName + "/dir/img.jpg")
	if err != nil {
		t.Fatal(err)
	}
	// Lookup won't return it.
	_, _, err = tree2.Lookup(userName + "/dir/img.jpg")
	expectedErr := errors.E("Delete", errors.NotExist, upspin.PathName(userName+"/dir/img.jpg"))
	if errors.Match(expectedErr, err) {
		t.Fatalf("err = %s, want = %s", err, expectedErr)
	}
	// One new entry was written to the log (an updated dir2).
	if got, want := cfg.Log.LastOffset(), int64(5); got != want {
		t.Fatalf("cfg.Log.LastIndex() = %d, want %d", cfg.Log.LastOffset(), want)
	}
	// Verify logged entry is a new dir2
	entries, _, err = cfg.Log.ReadAt(1, int64(4))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want = 1", len(entries))
	}
	if got, want := entries[0].Entry.Name, upspin.PathName(userName+"/dir/img.jpg"); got != want {
		t.Errorf("entries[0].Name = %s, want = %s", got, want)
	}
	if got, want := entries[0].Op, Delete; got != want {
		t.Errorf("entries[0].Op = %v, want = %v", got, want)
	}
}

func TestAddKidToEmptyNonDirtyDir(t *testing.T) {
	cfg := newConfigForTesting(t)
	tree := New(userName, cfg)

	de := newDirEntry("/", isDir, cfg)
	err := tree.Put(de)
	if err != nil {
		t.Fatal(err)
	}
	de = newDirEntry("/dir", isDir, cfg)
	err = tree.Put(de)
	if err != nil {
		t.Fatal(err)
	}
	err = tree.Flush()
	if err != nil {
		t.Fatal(err)
	}
	de = newDirEntry("/dir/subdir", isDir, cfg)
	err = tree.Put(de)
	if err != nil {
		t.Fatal(err)
	}
}

// Test that an empty root can be saved and retrieved.
// Roots are handled differently than other directory entries.
func TestPutEmptyRoot(t *testing.T) {
	cfg := newConfigForTesting(t)
	tree := New(userName, cfg)

	dir1 := newDirEntry("/", isDir, cfg)
	err := tree.Put(dir1)
	if err != nil {
		t.Fatal(err)
	}

	err = tree.Flush()
	if err != nil {
		t.Fatal(err)
	}

	// Now start a new tree from scratch and confirm it is loaded from the Store.
	tree2 := New(userName, cfg)

	dir2 := newDirEntry("/dir", isDir, cfg)
	err = tree2.Put(dir2)
	if err != nil {
		t.Fatal(err)
	}

	// Try to put a file under an non-existent dir
	dir3 := newDirEntry("/invaliddir/myfile", !isDir, cfg)
	err = tree2.Put(dir3)
	if err == nil {
		t.Fatal("Expected error, got none")
	}
	expectedErr := errors.E(errors.NotExist)
	if !errors.Match(expectedErr, err) {
		t.Errorf("err = %s, want %s", err, expectedErr)
	}
}

// TestRebuildFromLog creates a tree and simulates a crash while there are
// entries that were not flushed to the Store. It tests that the new tree
// recovers from the log and is fully functional.
func TestRebuildFromLog(t *testing.T) {
	cfg := newConfigForTesting(t)
	tree := New(userName, cfg)

	tests := []struct {
		name  upspin.PathName
		isDir bool
	}{
		{"/", isDir},
		{"/file1.txt", !isDir},
		{"/dir0", isDir},
		{"/dir0/file_in_dir.txt", !isDir},
	}
	for _, test := range tests {
		de := newDirEntry(test.name, test.isDir, cfg)
		err := tree.Put(de)
		if err != nil {
			t.Fatalf("Creating %q, isDir %v: %s", test.name, test.isDir, err)
		}
	}

	// Flush to Store.
	err := tree.Flush()
	if err != nil {
		t.Fatal(err)
	}

	// Write more stuff after flush.
	tests = []struct {
		name  upspin.PathName
		isDir bool
	}{
		{"/file2.txt", !isDir},
		{"/dir1", isDir},
		{"/dir1/file_in_dir.txt", !isDir},
	}
	for _, test := range tests {
		de := newDirEntry(test.name, test.isDir, cfg)
		err := tree.Put(de)
		if err != nil {
			t.Fatalf("Creating %q, isDir %v: %s", test.name, test.isDir, err)
		}
	}
	// And delete some others.
	err = tree.Delete(userName + "/file1.txt")
	if err != nil {
		t.Fatal(err)
	}
	err = tree.Delete(userName + "/dir0/file_in_dir.txt")
	if err != nil {
		t.Fatal(err)
	}

	// Now we crash and restart.
	// file2 and file_in_dir must exist after recovery and file1 must not.
	tree = New(userName, cfg)
	_, dirty, err := tree.Lookup(userName + "/file2.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !dirty {
		t.Errorf("dirty = %v, want = true", dirty)
	}
	_, dirty, err = tree.Lookup(userName + "/dir1/file_in_dir.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !dirty {
		t.Errorf("dirty = %v, want = true", dirty)
	}

	_, _, err = tree.Lookup(userName + "/file1.txt")
	expectedErr := errors.E(errors.NotExist)
	if !errors.Match(expectedErr, err) {
		t.Fatalf("err = %s, want %s", err, expectedErr)
	}
	_, _, err = tree.Lookup(userName + "/dir0/file_in_dir.txt")
	if !errors.Match(expectedErr, err) {
		t.Fatalf("err = %s, want %s", err, expectedErr)
	}

	// Furthermore, we can create entries in an existing directory.
	de := newDirEntry("/dir0/will_not_fail", !isDir, cfg)
	err = tree.Put(de)
	if err != nil {
		t.Fatal(err)
	}
}

// TODO: TestPutLargeNode: test that a huge DirEntry (>blockSize) gets split into multiple ones.
// TODO: Run all tests in loop using Plain and Debug packs as well.
// TODO: test more error cases.

// newDirEntry returns a dir entry for a path name filled with the mandatory
// arguments. It is used to make tests more concise.
func newDirEntry(name upspin.PathName, isDir bool, cfg *Config) *upspin.DirEntry {
	var writer upspin.UserName
	var attr upspin.Attribute
	if isDir {
		writer = serverName
		attr = upspin.AttrDirectory
	} else {
		writer = userName
		attr = upspin.AttrNone
	}
	return &upspin.DirEntry{
		Name:    userName + name,
		Attr:    attr,
		Packing: cfg.Context.Packing(),
		Writer:  writer,
	}
}

// newConfigForTesting creates a config with mocks, fakes, inprocess and otherwise testing
// versions of the Tree's dependencies.
func newConfigForTesting(t *testing.T) *Config {
	factotum, err := factotum.New(repo("key/testdata/upspin-test"))
	if err != nil {
		t.Fatal(err)
	}
	endpointInProcess := upspin.Endpoint{
		Transport: upspin.InProcess,
		NetAddr:   "",
	}
	context := context.New().
		SetFactotum(factotum).
		SetUserName(serverName).
		SetStoreEndpoint(endpointInProcess).
		SetKeyEndpoint(endpointInProcess).
		SetPacking(upspin.EEPack)
	key := context.KeyServer()
	// Set the public key for the tree, since it must do Auth against the Store.
	user := &upspin.User{
		Name:      serverName,
		Dirs:      []upspin.Endpoint{context.DirEndpoint()},
		Stores:    []upspin.Endpoint{context.StoreEndpoint()},
		PublicKey: factotum.PublicKey(),
	}
	err = key.Put(user)
	if err != nil {
		panic(err)
	}

	// Set the public key for the user, since EE Pack requires the dir owner to have a wrapped key.
	// TODO: re-think this for directories, but probably correct as-is because if the dir server goes
	// rogue or fails, the user can always run a dir server locally as himself and retrieve dir blocks.
	user = &upspin.User{
		Name:      userName,
		Dirs:      []upspin.Endpoint{context.DirEndpoint()},
		Stores:    []upspin.Endpoint{context.StoreEndpoint()},
		PublicKey: factotum.PublicKey(),
	}
	err = key.Put(user)
	if err != nil {
		panic(err)
	}

	return &Config{
		Context: context,
		Log: &fakeLog{
			user: userName,
		},
		LogIndex: &fakeLogIndex{
			user: userName,
		},
	}
}

// fakeLog implements a simple, in-memory Log for testing.
type fakeLog struct {
	user       upspin.UserName
	logEntries []LogEntry
}

var _ Log = (*fakeLog)(nil)

// fakeLogIndex implements a simple, in-memory LogIndex for testing.
type fakeLogIndex struct {
	user       upspin.UserName
	root       upspin.DirEntry
	lastOffset int64
	hasRoot    bool
}

var _ LogIndex = (*fakeLogIndex)(nil)

// User returns the user name for whom this log logs.
func (l *fakeLog) User() upspin.UserName {
	return l.user
}

// Append appends a DirEntry at the end of the log.
func (l *fakeLog) Append(le *LogEntry) error {
	l.logEntries = append(l.logEntries, *le)
	return nil
}

// Read reads at most n entries from the log starting at index.
func (l *fakeLog) ReadAt(n int, offset int64) ([]LogEntry, int64, error) {
	if int(offset)+n > len(l.logEntries) {
		n = len(l.logEntries) - int(offset)
	}
	return l.logEntries[int(offset) : int(offset)+n], offset + int64(n), nil
}

// LastOffset returns the offset after the most-recently-appended entry.
func (l *fakeLog) LastOffset() int64 {
	return int64(len(l.logEntries))
}

// Root returns the location of the user's root.
func (l *fakeLogIndex) Root() (*upspin.DirEntry, error) {
	if l.hasRoot {
		return &l.root, nil
	}
	return nil, errors.E(errors.NotExist)
}

// SaveRoot saves the user's root.
func (l *fakeLogIndex) SaveRoot(r *upspin.DirEntry) error {
	l.root = *r
	l.hasRoot = true
	return nil
}

// User returns the user name who owns the root of the tree that this
// log index represents.
func (l *fakeLogIndex) User() upspin.UserName {
	return l.user
}

// ReadOffset reads from stable storage the offset saved by SaveOffset.
func (l *fakeLogIndex) ReadOffset() (int64, error) {
	return l.lastOffset, nil
}

// SaveOffset saves to stable storage the last offset processed.
func (l *fakeLogIndex) SaveOffset(offs int64) error {
	l.lastOffset = offs
	return nil
}

// repo returns the local pathname of a file in the upspin repository.
func repo(dir string) string {
	gopath := os.Getenv("GOPATH")
	if len(gopath) == 0 {
		panic("no GOPATH")
	}
	return filepath.Join(gopath, "src/upspin.io/"+dir)
}
