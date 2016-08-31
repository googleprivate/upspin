// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package test contains an integration test for all of Upspin.

package test

import (
	"fmt"
	"log"
	"testing"

	"upspin.io/access"
	"upspin.io/bind"
	"upspin.io/errors"
	"upspin.io/key/usercache"
	"upspin.io/path"
	"upspin.io/test/testenv"
	"upspin.io/upspin"

	_ "upspin.io/dir/transports"
	_ "upspin.io/key/transports"
	_ "upspin.io/pack/debug"
	_ "upspin.io/pack/ee"
	_ "upspin.io/pack/plain"
	_ "upspin.io/store/transports"
)

const (
	contentsOfFile1     = "contents of file 1"
	contentsOfFile2     = "contents of file 2..."
	contentsOfFile3     = "===PDF PDF PDF=="
	genericFileContents = "contents"
	hasLocation         = true
	ownerName           = "upspin-test@google.com"
	readerName          = "upspin-friend-test@google.com"
)

var (
	errNotExist   = errors.E(errors.NotExist)
	setupTemplate = testenv.Setup{
		Tree: testenv.Tree{
			testenv.E("/dir1/", ""),
			testenv.E("/dir2/", ""),
			testenv.E("/dir1/file1.txt", contentsOfFile1),
			testenv.E("/dir2/file2.txt", contentsOfFile2),
			testenv.E("/dir2/file3.pdf", contentsOfFile3),
		},
		OwnerName:                 ownerName,
		IgnoreExistingDirectories: false, // left-over Access files would be a problem.
		Cleanup:                   cleanup,
	}
	readerContext upspin.Context
)

func testNoReadersAllowed(t *testing.T, r *testRunner) {
	fileName := upspin.PathName(ownerName + "/dir1/file1.txt")

	r.As(readerName)
	r.Get(fileName)
	if !r.Match(access.ErrPermissionDenied) {
		t.Fatal(r.Diag())
	}

	// But the owner can still read it.
	r.As(ownerName)
	r.Get(fileName)
	if r.Failed() {
		t.Fatal(r.Diag())
	}
	if r.Data != contentsOfFile1 {
		t.Errorf("Expected contents %q, got %q", contentsOfFile1, r.Data)
	}
}

func testAllowListAccess(t *testing.T, r *testRunner) {
	r.As(ownerName)
	r.Put(ownerName+"/dir1/Access", "l:"+readerName)

	// Check that readerClient can list file1, but can't read and therefore the Location is zeroed out.
	file := ownerName + "/dir1/file1.txt"
	r.As(readerName)
	r.Glob(file)
	if r.Failed() {
		t.Fatal(r.Diag())
	}
	if len(r.Entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(r.Entries))
	}
	checkDirEntry(t, r.Entries[0], ownerName+"/dir1/file1.txt", !hasLocation, 0)

	// Ensure we can't read the data.
	r.As(readerName)
	r.Get(upspin.PathName(file))
	if !r.Match(access.ErrPermissionDenied) {
		t.Fatal(r.Diag())
	}
}

func testAllowReadAccess(t *testing.T, r *testRunner) {
	// Owner has no delete permission (assumption tested in testDelete).
	r.As(ownerName)
	r.Put(ownerName+"/dir1/Access",
		"l,r:"+readerName+"\nc,w,l,r:"+ownerName)
	// Put file back again so we force keys to be re-wrapped.
	r.Put(ownerName+"/dir1/file1.txt",
		contentsOfFile1)

	// Now try reading as the reader.
	r.As(readerName)
	r.Get(upspin.PathName(ownerName + "/dir1/file1.txt"))
	if r.Failed() {
		t.Fatal(r.Diag())
	}
	if r.Data != contentsOfFile1 {
		t.Errorf("Expected contents %q, got %q", contentsOfFile1, r.Data)
	}
}

func testCreateAndOpen(t *testing.T, r *testRunner) {
	filePath := upspin.PathName(path.Join(ownerName, "myotherfile.txt"))

	r.As(ownerName)
	r.Put(filePath, genericFileContents)
	r.Get(filePath)
	if r.Failed() {
		t.Fatal(r.Diag())
	}
	if r.Data != genericFileContents {
		t.Errorf("file content = %q, want %q", r.Data, genericFileContents)
	}
}

func testGlobWithLimitedAccess(t *testing.T, r *testRunner) {
	dir1Pat := ownerName + "/dir1/*.txt"
	dir2Pat := ownerName + "/dir2/*.txt"
	bothPat := ownerName + "/dir*/*.txt"

	checkDirs := func(context, pat string, want int) {
		if r.Failed() {
			t.Fatalf("%v globbing %v: %v", context, pat, r.Diag())
		}
		got := len(r.Entries)
		if got == want {
			return
		}
		for _, d := range r.Entries {
			t.Log("got:", d.Name)
		}
		t.Fatalf("%v globbing %v saw %v dirs, want %v", context, pat, got, want)
	}

	// Owner sees both files.
	r.As(ownerName)
	r.Glob(bothPat)
	checkDirs("owner", bothPat, 2)
	checkDirEntry(t, r.Entries[0], ownerName+"/dir1/file1.txt", hasLocation, len(contentsOfFile1))
	checkDirEntry(t, r.Entries[1], ownerName+"/dir2/file2.txt", hasLocation, len(contentsOfFile2))

	// readerClient should be able to see /dir1/
	r.As(readerName)
	r.Glob(dir1Pat)
	checkDirs("reader", dir1Pat, 1)
	checkDirEntry(t, r.Entries[0], ownerName+"/dir1/file1.txt", hasLocation, len(contentsOfFile1))

	// but not /dir2/
	r.Glob(dir2Pat)
	checkDirs("reader", dir2Pat, 0)

	// Without list access to the root, the reader can't glob /dir*.
	r.Glob(bothPat)
	checkDirs("reader", bothPat, 0)

	// Give the reader list access to the root.
	r.As(ownerName)
	r.Put(ownerName+"/Access",
		"l:"+readerName+"\n*:"+ownerName)
	// But don't give any access to /dir2/.
	r.Put(ownerName+"/dir2/Access", "*:"+ownerName)

	// Then try globbing the root again.
	r.As(readerName)
	r.Glob(bothPat)
	checkDirs("reader after access", bothPat, 1)
	checkDirEntry(t, r.Entries[0], ownerName+"/dir1/file1.txt", hasLocation, len(contentsOfFile1))
}

func testGlobWithPattern(t *testing.T, r *testRunner) {
	r.As(ownerName)
	for i := 0; i <= 10; i++ {
		r.MakeDirectory(upspin.PathName(fmt.Sprintf("%s/mydir%d", ownerName, i)))
	}
	r.Glob(ownerName + "/mydir[0-1]*")
	if r.Failed() {
		t.Fatal(r.Diag())
	}
	if len(r.Entries) != 3 {
		t.Fatalf("Expected 3 paths, got %d", len(r.Entries))
	}
	if string(r.Entries[0].Name) != ownerName+"/mydir0" {
		t.Errorf("Expected mydir0, got %s", r.Entries[0].Name)
	}
	if string(r.Entries[1].Name) != ownerName+"/mydir1" {
		t.Errorf("Expected mydir1, got %s", r.Entries[1].Name)
	}
	if string(r.Entries[2].Name) != ownerName+"/mydir10" {
		t.Errorf("Expected mydir10, got %s", r.Entries[2].Name)
	}
}

func testDelete(t *testing.T, r *testRunner) {
	pathName := upspin.PathName(ownerName + "/dir2/file3.pdf")

	r.As(ownerName)
	r.Delete(pathName)

	// Check it really deleted it (and is not being cached in memory).
	r.Get(pathName)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}

	// But I can't delete files in dir1, since I lack permission.
	pathName = upspin.PathName(ownerName + "/dir1/file1.txt")
	r.Delete(pathName)
	if !r.Match(access.ErrPermissionDenied) {
		t.Fatal(r.Diag())
	}

	// But we can always remove the Access file.
	r.Delete(upspin.PathName(ownerName + "/dir1/Access"))

	// Now delete file1.txt
	r.Delete(pathName)
	if r.Failed() {
		t.Fatal(r.Diag())
	}
}

// integrationTests list all tests and their names. Order is important.
var integrationTests = []struct {
	name string
	fn   func(*testing.T, *testRunner)
}{
	{"NoReadersAllowed", testNoReadersAllowed},
	{"AllowListAccess", testAllowListAccess},
	{"AllowReadAccess", testAllowReadAccess},
	{"CreateAndOpen", testCreateAndOpen},
	{"GlobWithLimitedAccess", testGlobWithLimitedAccess},
	{"GlobWithPattern", testGlobWithPattern},
	{"Delete", testDelete},
}

func testSelectedOnePacking(t *testing.T, setup testenv.Setup) {
	usercache.ResetGlobal()

	env, err := testenv.New(&setup)
	if err != nil {
		t.Fatal(err)
	}

	_, readerContext, err = env.NewUser(readerName)
	if err != nil {
		t.Fatal(err)
	}

	r := newRunner()
	r.AddUser(env.Context)
	r.AddUser(readerContext)

	// The ordering here is important as each test adds state to the tree.
	for _, test := range integrationTests {
		t.Run(test.name, func(t *testing.T) { test.fn(t, r) })
	}

	err = env.Exit()
	if err != nil {
		t.Fatal(err)
	}
}

var integrationTestKinds = []string{"inprocess"}

func TestIntegration(t *testing.T) {
	for _, kind := range integrationTestKinds {
		t.Run(fmt.Sprintf("kind=%v", kind), func(t *testing.T) {
			setup := setupTemplate
			setup.Kind = kind
			for _, p := range []struct {
				packing upspin.Packing
				curve   string
			}{
				{packing: upspin.PlainPack, curve: "p256"},
				{packing: upspin.DebugPack, curve: "p256"},
				{packing: upspin.EEPack, curve: "p256"},
				//{packing: upspin.EEPack, curve: "p521"}, // TODO: figure out if and how to test p521.
			} {
				setup.Packing = p.packing
				t.Run(fmt.Sprintf("packing=%v/curve=%v", p.packing, p.curve), func(t *testing.T) {
					testSelectedOnePacking(t, setup)
				})
			}
		})
	}
}

// checkDirEntry verifies a dir entry against expectations. size == 0 for don't check.
func checkDirEntry(t *testing.T, dirEntry *upspin.DirEntry, name string, hasLocation bool, size int) {
	if dirEntry.Name != upspin.PathName(name) {
		t.Errorf("Expected name %s, got %s", name, dirEntry.Name)
	}
	if loc := locationOf(dirEntry); loc == (upspin.Location{}) {
		if hasLocation {
			t.Errorf("%s has no location, expected one", name)
		}
	} else {
		if !hasLocation {
			t.Errorf("%s has location %v, want none", name, loc)
		}
	}
	dSize, err := dirEntry.Size()
	if err != nil {
		t.Errorf("Size error: %s: %v", name, err)
	}
	if got, want := int(dSize), size; got != want {
		t.Errorf("%s has size %d, want %d", name, got, want)
	}
}

func locationOf(entry *upspin.DirEntry) upspin.Location {
	if len(entry.Blocks) == 0 {
		return upspin.Location{}
	}
	return entry.Blocks[0].Location
}

func cleanup(env *testenv.Env) error {
	dir, err := bind.DirServer(env.Context, env.Context.DirEndpoint())
	if err != nil {
		return err
	}

	fileSet1, err := dir.Glob(ownerName + "/*/*")
	if err != nil {
		return err
	}
	fileSet2, err := dir.Glob(ownerName + "/*")
	if err != nil {
		return err
	}
	entries := append(fileSet1, fileSet2...)
	var firstErr error
	deleteNow := func(name upspin.PathName) {
		_, err = dir.Delete(name)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			log.Printf("cleanup: error deleting %q: %v", name, err)
		}
	}
	// First, delete all Access files,
	// so we don't lock ourselves out if our tests above remove delete rights.
	for _, entry := range entries {
		if access.IsAccessFile(entry.Name) {
			deleteNow(entry.Name)
		}
	}
	for _, entry := range entries {
		if !access.IsAccessFile(entry.Name) {
			deleteNow(entry.Name)
		}
	}
	return firstErr
}
