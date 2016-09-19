// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"strings"
	"testing"

	"upspin.io/path"
	"upspin.io/upspin"
)

const (
	userFirstName = "bambi"
	domain        = "forest.earth"
	canonicalUser = userFirstName + "@" + domain
	snapshotUser  = userFirstName + "+snapshot@" + domain

	isDir = true
)

var defaultEnt = upspin.DirEntry{
	Attr:     upspin.AttrNone,
	Writer:   canonicalUser,
	Sequence: upspin.SeqNotExist,
	Packing:  upspin.PlainPack,
}

func TestSnapshot(t *testing.T) {
	s := newDirServerForTesting(t, canonicalUser)
	create(t, s, canonicalUser+"/", isDir)
	create(t, s, canonicalUser+"/dir", isDir)
	create(t, s, canonicalUser+"/file.pdf", !isDir)

	snap := newDirServerForTesting(t, snapshotUser)
	create(t, snap, snapshotUser+"/", isDir)

	// Nothing exists under snapshotUser yet.
	ents, err := snap.Glob(snapshotUser + "/*")
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) > 0 {
		t.Fatalf("got = %d entries, want = 0", len(ents))
	}

	// Force a snapshot for all users who have a +snapshot tree.
	err = snap.snapshotAll()
	if err != nil {
		t.Fatal(err)
	}

	// Verify there are items under the snapshot user now.
	ents, err = snap.Glob(snapshotUser + "/*/*/*")
	if err != nil {
		t.Fatal(err)
	}
	// And one entry was created.
	if len(ents) != 1 {
		t.Fatalf("got = %d entries, want = 1", len(ents))
	}
	// Inside the snapshot directory, there's the entire root of userName.
	// Check that everything is there.
	ents, err = snap.Glob(snapshotUser + "/*/*/*/*")
	if err != nil {
		t.Fatal(err)
	}
	expected := []struct {
		prefix string
		suffix string
	}{
		{snapshotUser + "/", "/dir"},
		{snapshotUser + "/", "/file.pdf"},
	}
	if got, want := len(ents), len(expected); got != want {
		t.Fatalf("len(ents) = %d, want = %d", got, want)
	}
	for i, e := range ents {
		if !strings.HasPrefix(string(e.Name), expected[i].prefix) {
			t.Errorf("%d: e.Name = %q, want prefix %q", i, e.Name, expected[i].prefix)
		}
		if !strings.HasSuffix(string(e.Name), expected[i].suffix) {
			t.Errorf("%d: e.Name = %q, want suffix %q", i, e.Name, expected[i].suffix)
		}
	}

	// Snapshot again and nothing happens, because the previous snapshot is
	// recent enough.
	err = snap.snapshotAll()
	if err != nil {
		t.Fatal(err)
	}
	// Only one entry still.
	ents, err = snap.Glob(snapshotUser + "/*/*/*")
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 {
		t.Fatalf("got = %d entries, want = 1", len(ents))
	}
}

func TestForceSnapshotVersioning(t *testing.T) {
	s := newDirServerForTesting(t, snapshotUser)

	ents, err := s.Glob(snapshotUser + "/*/*/*")
	if err != nil {
		t.Fatal(err)
	}
	// A pre-existing entry exists.
	if len(ents) != 1 {
		t.Fatalf("got = %d entries, want = 1", len(ents))
	}
	// Re-use the same destination name, so we force the creation of a new
	// version.
	dstDir := ents[0].Name
	dstPath, err := path.Parse(dstDir)
	if err != nil {
		t.Fatal(err)
	}

	// Force a snapshot.
	err = s.takeSnapshot(dstPath, canonicalUser+"/")
	if err != nil {
		t.Fatal(err)
	}

	// The tree now contains two snapshotted versions.
	ents, err = s.Glob(snapshotUser + "/*/*/*")
	if err != nil {
		t.Fatal(err)
	}
	// One new entry was created.
	if len(ents) != 2 {
		t.Fatalf("got = %d entries, want = 2", len(ents))
	}
	// Verify the last element of the second entry contains a ".0" appended
	// to it.
	if !strings.HasSuffix(string(ents[1].Name), ".1") {
		t.Errorf("got %q, want suffix '.1'", ents[1].Name)
	}
}

func create(t *testing.T, s *server, name upspin.PathName, isDir bool) {
	var err error
	if isDir {
		var p path.Parsed
		p, err = path.Parse(name)
		if err != nil {
			t.Fatal(err)
		}
		mu := userLock(p.User())
		mu.Lock()
		_, err = s.makeDirectory("", p)
		mu.Unlock()
	} else {
		entry := defaultEnt
		entry.Name = name
		entry.SignedName = name
		_, err = s.Put(&entry)
	}
	if err != nil {
		t.Fatal(err)
	}
}
