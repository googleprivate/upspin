// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tree

import (
	"reflect"
	"testing"

	"upspin.io/context"
	"upspin.io/factotum"
	"upspin.io/key/inprocess"
	"upspin.io/upspin"

	"upspin.io/errors"
	_ "upspin.io/pack/ee"
	_ "upspin.io/store/inprocess"
)

const (
	userName   = "user@domain.com"
	serverName = "tree@server.com"
)

// This test checks the tree for log consistency by exercising the life-cycle of a tree,
// from creating a new tree from scratch, adding new nodes, flushing it to Store to then
// adding more nodes to a new tree and having to load it from the Store.
func TestPutNodes(t *testing.T) {
	cfg := newConfigWithFakes(t)
	tree := New(userName, cfg)

	dir1 := upspin.DirEntry{
		Name:    userName + "/",
		Attr:    upspin.AttrDirectory,
		Packing: cfg.Context.Packing(),
		Writer:  serverName,
	}
	err := tree.Put(&dir1)
	if err != nil {
		t.Fatal(err)
	}
	dir2 := upspin.DirEntry{
		Name:    userName + "/dir",
		Attr:    upspin.AttrDirectory,
		Packing: cfg.Context.Packing(),
		Writer:  serverName,
	}
	err = tree.Put(&dir2)
	if err != nil {
		t.Fatal(err)
	}
	dir3 := upspin.DirEntry{
		Name:    userName + "/dir/doc.pdf",
		Attr:    upspin.AttrNone,
		Packing: cfg.Context.Packing(),
		Writer:  serverName,
	}
	err = tree.Put(&dir3)
	if err != nil {
		t.Fatal(err)
	}

	// Verify three log entries were written.
	if got, want := cfg.Log.LastIndex(), 2; got != want {
		t.Fatalf("LastIndex = %d, want %d", got, want)
	}
	entries, err := cfg.Log.Read(0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(entries[0], dir1) {
		t.Errorf("dir1 = %v, want %v", entries[0], dir1)
	}
	if !reflect.DeepEqual(entries[1], dir2) {
		t.Errorf("dir2 = %v, want %v", entries[1], dir2)
	}
	if !reflect.DeepEqual(entries[2], dir3) {
		t.Errorf("dir3 = %v, want %v", entries[2], dir3)
	}

	// Flush and very new tree is equivalent.
	err = tree.Flush()
	if err != nil {
		t.Fatal(err)
	}

	// Log is empty now.
	if got, want := cfg.Log.LastIndex(), -1; got != want {
		t.Fatalf("cfg.Log.LastIndex() = %d, want %d", cfg.Log.LastIndex(), want)
	}

	t.Logf("Root: %v", tree.Root())

	// Now start a new tree from scratch and confirm it is loaded from the Store just the same.
	tree2 := New(userName, cfg)

	dir4 := &upspin.DirEntry{
		Name:    userName + "/dir/img.jpg",
		Attr:    upspin.AttrNone,
		Packing: cfg.Context.Packing(),
		Writer:  userName, // This was written by the user, the server is just packing it in a dir block.
	}
	err = tree2.Put(dir4)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := cfg.Log.LastIndex(), 0; got != want {
		t.Fatalf("cfg.Log.LastIndex() = %d, want %d", cfg.Log.LastIndex(), want)
	}

	// TODO: a Lookup here will confirm dir4 is fully integrated in the tree now.
}

// Test that an empty root can be saved and retrieved.
// Roots are handled differently than other directory entries.
func TestPutEmptyRoot(t *testing.T) {
	cfg := newConfigWithFakes(t)
	tree := New(userName, cfg)

	dir1 := &upspin.DirEntry{
		Name:    userName + "/",
		Attr:    upspin.AttrDirectory,
		Packing: cfg.Context.Packing(),
		Writer:  serverName,
	}
	err := tree.Put(dir1)
	if err != nil {
		t.Fatal(err)
	}

	err = tree.Flush()
	if err != nil {
		t.Fatal(err)
	}

	// Now start a new tree from scratch and confirm it is loaded from the Store just the same.
	tree2 := New(userName, cfg)

	dir2 := &upspin.DirEntry{
		Name:    userName + "/dir",
		Attr:    upspin.AttrDirectory,
		Packing: cfg.Context.Packing(),
		Writer:  serverName,
	}
	err = tree2.Put(dir2)
	if err != nil {
		t.Fatal(err)
	}

	// Try to put a file under an non-existent dir
	dir3 := &upspin.DirEntry{
		Name:    userName + "/invaliddir/myfile.txt",
		Attr:    upspin.AttrNone,
		Packing: cfg.Context.Packing(),
		Writer:  serverName,
	}
	err = tree2.Put(dir3)
	if err == nil {
		t.Fatal("Expected error, got none")
	}
	expectedErr := errors.E(errors.NotExist)
	if !errors.Match(expectedErr, err) {
		t.Errorf("err = %s, want %s", err, expectedErr)
	}
}

// TODO: TestPutLargeNode: test that a huge DirEntry (>blockSize) gets split into multiple ones.
// TODO: Run all tests in loop using Plain and Debug packs as well.
// TODO: test more error cases.

func newConfigWithFakes(t *testing.T) *Config {
	pubKey := upspin.PublicKey("p256\n104278369061367353805983276707664349405797936579880352274235000127123465616334\n26941412685198548642075210264642864401950753555952207894712845271039438170192\n")
	// TODO: rename factotum.DeprecatedNew to NewWithKeys or NewForTesting.
	factotum, err := factotum.DeprecatedNew(
		pubKey,
		"82201047360680847258309465671292633303992565667422607675215625927005262185934")
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
	testKey, ok := key.(*inprocess.Service)
	if !ok {
		t.Fatal(err)
	}
	// Set the public key for the tree, since it must do Auth against the Store.
	testKey.SetPublicKeys(serverName, []upspin.PublicKey{pubKey})

	// Set the public key for the user, since EE Pack requires the dir owner to have a wrapped key.
	// TODO: re-think this for directories, but probably correct as-is because if the dir server goes
	// rogue or fails, the user can always run a dir server locally as himself and retrieve dir blocks.
	testKey.SetPublicKeys(userName, []upspin.PublicKey{pubKey})

	return &Config{
		Context: context,
		Log:     new(fakeLog),
	}
}

// fakeLog implements a simple, in-memory Log for testing.
type fakeLog struct {
	user       upspin.UserName
	dirEntries []upspin.DirEntry
	root       *upspin.DirEntry
}

var _ Log = (*fakeLog)(nil)

// User returns the user name for whom this log logs.
func (l *fakeLog) User() upspin.UserName {
	return l.user
}

// Append appends a DirEntry at the end of the log.
func (l *fakeLog) Append(de *upspin.DirEntry) error {
	l.dirEntries = append(l.dirEntries, *de)
	return nil
}

// Read reads at most n entries from the log starting at index.
func (l *fakeLog) Read(index, n int) ([]upspin.DirEntry, error) {
	return l.dirEntries[index : index+n], nil // No error checking.
}

// LastIndex returns the index of the most-recently-appended entry.
func (l *fakeLog) LastIndex() int {
	return len(l.dirEntries) - 1
}

// Drop deletes the entries up to the index.
func (l *fakeLog) Drop(index int) error {
	l.dirEntries = l.dirEntries[index+1:]
	return nil
}

// Root returns the location of the user's root.
func (l *fakeLog) Root() *upspin.DirEntry {
	return l.root
}

// SetRoot sets the user's root.
func (l *fakeLog) SetRoot(r *upspin.DirEntry) error {
	l.root = r
	return nil
}
