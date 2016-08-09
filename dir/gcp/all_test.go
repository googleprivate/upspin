// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gcp

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"upspin.io/access"
	"upspin.io/cloud/storage"
	"upspin.io/cloud/storage/storagetest"
	"upspin.io/errors"
	"upspin.io/factotum"
	"upspin.io/metric"
	"upspin.io/upspin"
)

const (
	userName       = "test@foo.com"
	rootPath       = userName + "/"
	parentPathName = userName + "/mydir"
	pathName       = parentPathName + "/myfile.txt"
	rootAccessFile = userName + "/Access"
)

var (
	timeFunc = func() upspin.Time { return upspin.Time(17) }
	dir      = upspin.DirEntry{
		Name:     upspin.PathName(pathName),
		Attr:     upspin.AttrNone,
		Time:     upspin.Now(),
		Packdata: []byte("12345"),
		Blocks: []upspin.DirBlock{
			{
				Size: 32,
				Location: upspin.Location{
					Reference: upspin.Reference("1234"),
					Endpoint: upspin.Endpoint{
						Transport: upspin.GCP,
						NetAddr:   "https://store-server.com",
					},
				},
			},
		},
	}
	serviceEndpoint = upspin.Endpoint{
		Transport: upspin.GCP,
		NetAddr:   upspin.NetAddr("This service's location"),
	}
	dirParent = upspin.DirEntry{
		Name: upspin.PathName(parentPathName),
		Attr: upspin.AttrDirectory,
	}
	defaultAccess, _ = access.New(rootAccessFile)
	userRoot         = root{
		dirEntry: upspin.DirEntry{
			Name: rootPath,
			Attr: upspin.AttrDirectory,
			Time: 1234,
			Blocks: []upspin.DirBlock{{
				Location: upspin.Location{
					// Reference is empty for the root.
					Endpoint: serviceEndpoint,
				},
			}},
		},
		accessFiles: accessFileDB{rootAccessFile: defaultAccess},
	}
)

// equal reports whether two slices of DirEntries are equal. If they are not, it logs the first that differ.
func equal(t *testing.T, d1, d2 []*upspin.DirEntry) bool {
	if len(d1) != len(d2) {
		t.Errorf("slices of directory entries differ in length: %d %d", len(d1), len(d2))
		return false
	}
	for i := range d1 {
		if !reflect.DeepEqual(d1[i], d2[i]) {
			t.Errorf("directory entries differ:\n%v\n%v", d1[i], d2[i])
			return false
		}
	}
	return true
}

func TestPutErrorParseRoot(t *testing.T) {
	// No path given
	expectErr := errors.E("Put", errors.E("Parse", errors.Str("no user name in path")))
	ds := newTestDirServer(t, &storagetest.DummyStorage{})
	err := ds.Put(&upspin.DirEntry{})
	if !errors.Match(expectErr, err) {
		t.Fatalf("Put: error mismatch: got %v; want %v", err, expectErr)
	}
}

func TestPutErrorParseUser(t *testing.T) {
	dir := upspin.DirEntry{
		Name: upspin.PathName("a@x/myroot/myfile"),
	}
	expectErr := errors.E("Put", errors.E("Parse", errors.Str("no user name in path")))
	ds := newTestDirServer(t, &storagetest.DummyStorage{})
	err := ds.Put(&dir)
	if !errors.Match(expectErr, err) {
		t.Fatalf("Put: error mismatch: got %v; want %v", err, expectErr)
	}
}

func TestPutErrorInvalidSequenceNumber(t *testing.T) {
	dir := upspin.DirEntry{
		Name:     upspin.PathName("fred@bob.com/myroot/myfile"),
		Attr:     upspin.AttrDirectory,
		Sequence: upspin.SeqNotExist - 1,
	}
	expectErr := errors.E("Put", errors.Invalid, errors.Str("invalid sequence number"))
	ds := newTestDirServer(t, &storagetest.DummyStorage{})
	err := ds.Put(&dir)
	if !errors.Match(expectErr, err) {
		t.Fatalf("Put: error mismatch: got %v; want %v", err, expectErr)
	}
}

func TestLookupPathError(t *testing.T) {
	ds := newTestDirServer(t, &storagetest.DummyStorage{})
	expectErr := errors.E("Lookup", errors.E("Parse", errors.Str("no user name in path")))
	_, err := ds.Lookup("")
	if !errors.Match(expectErr, err) {
		t.Fatalf("Lookup: error mismatch: got %v; want %v", err, expectErr)
	}
}

func TestGlobMissingPattern(t *testing.T) {
	ds := newTestDirServer(t, &storagetest.DummyStorage{})
	expectErr := errors.E("Glob", errors.E("Parse", errors.Str("no user name in path")))
	_, err := ds.Glob("")
	if !errors.Match(expectErr, err) {
		t.Fatalf("Glob: error mismatch: got %v; want %v", err, expectErr)
	}
}

func TestGlobBadPath(t *testing.T) {
	ds := newTestDirServer(t, &storagetest.DummyStorage{})
	expectErr := errors.E("Glob", errors.E("Parse", errors.Str("bad user name in path")))
	_, err := ds.Glob("missing/email/dir/file")
	if !errors.Match(expectErr, err) {
		t.Fatalf("Glob: error mismatch: got %v; want %v", err, expectErr)
	}
}

func TestPutErrorFileNoParentDir(t *testing.T) {
	dir := upspin.DirEntry{
		Name: upspin.PathName("test@foo.com/myroot/myfile"),
		Attr: upspin.AttrDirectory,
	}
	rootJSON := toRootJSON(t, &userRoot)
	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName, "something that does not match"},
		Data: [][]byte{rootJSON, []byte("")},
	}

	expectErr := errors.E("Put", upspin.PathName("test@foo.com/myroot/myfile"), errors.NotExist, errors.Str("parent path not found"))
	ds := newTestDirServer(t, egcp)
	err := ds.Put(&dir)
	if !errors.Match(expectErr, err) {
		t.Fatalf("Put: error mismatch: got %v; want %v", err, expectErr)
	}
}

func TestLookupPathNotFound(t *testing.T) {
	rootJSON := toRootJSON(t, &userRoot)
	expectErr := errors.E("Lookup", errors.NotExist)

	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName, "something that does not match"},
		Data: [][]byte{rootJSON, []byte("")},
	}

	ds := newTestDirServer(t, egcp)
	_, err := ds.Lookup("test@foo.com/invalid/invalid/invalid")
	if !errors.Match(expectErr, err) {
		t.Fatalf("Lookup: error mismatch: got %v; want %v", err, expectErr)
	}
}

// Regression test to catch that we don't panic (by going past the root).
func TestLookupRoot(t *testing.T) {
	// The root converted to JSON.
	rootJSON := toRootJSON(t, &userRoot)

	expect := &upspin.DirEntry{
		Name:     upspin.PathName("test@foo.com/"),
		Attr:     upspin.AttrDirectory,
		Sequence: 0,
		Time:     1234,
		Blocks: []upspin.DirBlock{{
			Size: 0,
			Location: upspin.Location{
				Endpoint:  serviceEndpoint,
				Reference: "",
			},
		}},
	}
	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName},
		Data: [][]byte{rootJSON},
	}

	ds := newTestDirServer(t, egcp)
	de, err := ds.Lookup(upspin.PathName(userName + "/"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(expect, de) {
		t.Fatalf("entries differ: got %v; want %v", de, expect)
	}
}

func TestLookupWithoutReadRights(t *testing.T) {
	// The root converted to JSON.
	newRoot := userRoot
	newRoot.accessFiles = make(accessFileDB)
	// lister-dude@me.com can only List, not read. Therefore the Lookup operation clears out Location.
	newRoot.accessFiles[rootAccessFile] = makeAccess(t, rootAccessFile, "l:lister-dude@me.com")
	rootJSON := toRootJSON(t, &newRoot)

	dirJSON := toJSON(t, dir)

	// Default, zero Location is the expected answer.
	expect := dir                                                 // copy
	expect.Blocks = append([]upspin.DirBlock(nil), dir.Blocks...) // copy
	expect.Blocks[0].Location = upspin.Location{}                 // Zero location
	expect.Packdata = nil                                         // No pack data either

	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName, pathName},
		Data: [][]byte{rootJSON, dirJSON},
	}

	ds := newTestDirServer(t, egcp)
	ds.context.SetUserName("lister-dude@me.com")
	de, err := ds.Lookup(pathName)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(&expect, de) {
		t.Fatalf("entries differ: got %v; want %v", de, expect)
	}
}

func TestGlobComplex(t *testing.T) {
	// Create dir entries for files that match that will be looked up after Globbing.
	dir1 := upspin.DirEntry{
		Name: "f@b.co/subdir/a.pdf",
	}
	dir1JSON := toJSON(t, dir1)
	dir2 := upspin.DirEntry{
		Name: "f@b.co/subdir2/b.pdf",
	}
	dir2JSON := toJSON(t, dir2)
	// dir3 is a match, but is not readable to user.
	dir3 := upspin.DirEntry{
		Name: "f@b.co/subdir3/c.pdf",
	}
	dir3JSON := toJSON(t, dir3)

	// Seed the root with two pre-parsed Access files granting read permission to userName on subdir1 and subdir2.
	acc1 := makeAccess(t, "f@b.co/Access", "l: "+userName) // current user has list access
	acc2 := makeAccess(t, "f@b.co/subdir3/Access", "")     // No one has access
	root := &root{
		dirEntry: upspin.DirEntry{
			Name: "f@b.co/",
		},
		accessFiles: map[upspin.PathName]*access.Access{"f@b.co/Access": acc1, "f@b.co/subdir3/Access": acc2},
	}
	rootJSON := toRootJSON(t, root)

	// Order of events:
	// 1) List all files in the prefix.
	// 2) Lookup the first one. Discover its path.
	// 3) Fetch root to find all Access files to see if one matches the first returned file. The root Access file grants permission.
	// 4) Lookup the second one. Discover its path. Root is in cache. Apply check. It passes for same reason.
	// 5) Lookup the the third one. Discover its path. Root is in cache. Discover Access file that rules it. It fails.
	// 5) Return files to user.
	lgcp := &listGCP{
		ExpectDownloadCapturePut: storagetest.ExpectDownloadCapturePut{
			Ref:  []string{"f@b.co/subdir/a.pdf", "f@b.co", "f@b.co/subdir2/b.pdf", "f@b.co/subdir3/c.pdf"},
			Data: [][]byte{dir1JSON, rootJSON, dir2JSON, dir3JSON},
		},
		prefix: "f@b.co/",
		fileNames: []string{"f@b.co/subdir/a.pdf", "f@b.co/otherdir/b.pdf", "f@b.co/subfile",
			"f@b.co/subdir/notpdf", "f@b.co/subdir2/b.pdf", "f@b.co/subdir3/c.pdf"},
	}

	expect := []*upspin.DirEntry{&dir1, &dir2} // dir3 is NOT returned to user (no access)
	ds := newTestDirServer(t, lgcp)
	de, err := ds.Glob("f@b.co/sub*/*.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if !equal(t, expect, de) {
		t.Fatal("Glob returned wrong entries")
	}

	if lgcp.listDirCalled {
		t.Error("Call to ListDir unexpected")
	}
	if !lgcp.listPrefixCalled {
		t.Error("Expected call to ListPrefix")
	}
}

func TestGlobSimple(t *testing.T) {
	// Create dir entries for files that match that will be looked up after Globbing.
	// All files belong to the owner (userName) and hence no special Access files are needed, just the default one
	// at the root.
	dir1 := upspin.DirEntry{
		Name:     userName + "/subdir/a.pdf",
		Packdata: []byte("blah"),
		Blocks: []upspin.DirBlock{{
			Location: upspin.Location{
				Reference: upspin.Reference("xxxx"),
			},
		}},
	}
	dir1JSON := toJSON(t, dir1)
	dir2 := upspin.DirEntry{
		Name:     userName + "/subdir/b.pdf",
		Packdata: []byte("bleh"),
		Blocks: []upspin.DirBlock{{
			Location: upspin.Location{
				Reference: upspin.Reference("yyyy"),
			},
		}},
	}
	dir2JSON := toJSON(t, dir2)

	newRoot := userRoot
	newRoot.accessFiles = make(accessFileDB)
	// Access file gives the owner full list and read rights, but listerdude can only list, but not see the Location.
	newRoot.accessFiles[rootAccessFile] = makeAccess(t, rootAccessFile, "r,l: test@foo.com\n l: listerdude@me.com")
	rootJSON := toRootJSON(t, &newRoot)

	// Order of events:
	// 1) List all files in the prefix.
	// 2) Lookup the first one. Discover its path.
	// 3) Fetch root to find all Access files to see if one matches the first returned file. It does (implicitly).
	// 4) Lookup the second one. Discover its path. Root is in cache. Apply check. It passes (implicitly again).
	// 5) Return files to user.
	lgcp := &listGCP{
		ExpectDownloadCapturePut: storagetest.ExpectDownloadCapturePut{
			Ref:  []string{userName + "/subdir/a.pdf", userName, userName + "/subdir/b.pdf"},
			Data: [][]byte{dir1JSON, rootJSON, dir2JSON},
		},
		prefix: userName + "/subdir/",
		fileNames: []string{userName + "/subdir/a.pdf", userName + "/subdir/bpdf", userName + "/subdir/foo",
			userName + "/subdir/notpdf", userName + "/subdir/b.pdf"},
	}

	expect := []*upspin.DirEntry{&dir1, &dir2}

	ds := newTestDirServer(t, lgcp)
	de, err := ds.Glob(userName + "/subdir/*.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if !equal(t, expect, de) {
		t.Fatal("Glob returned wrong entries")
	}

	if !lgcp.listDirCalled {
		t.Error("Expected call to ListDir")
	}
	if lgcp.listPrefixCalled {
		t.Error("Unexpected call to ListPrefix")
	}

	// Now check that another user who doesn't have read permission, but does have list permission would get the
	// same list, but without the location in them.
	ds.context.SetUserName("listerdude@me.com")
	// Location and Packdata are anonymized.
	dir1.Blocks[0].Location = upspin.Location{}
	dir2.Blocks[0].Location = upspin.Location{}
	dir1.Packdata = nil
	dir2.Packdata = nil
	expect = []*upspin.DirEntry{&dir1, &dir2} // new expected response does not have Location.

	de, err = ds.Glob(userName + "/subdir/*.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if !equal(t, expect, de) {
		t.Fatal("Glob returned wrong entries")
	}
}

func TestPutParentNotDir(t *testing.T) {
	// The DirEntry of the parent, converted to JSON.
	notDirParent := dirParent
	notDirParent.Attr = upspin.AttrNone // Parent is not dir!
	dirParentJSON := toJSON(t, notDirParent)

	rootJSON := toRootJSON(t, &userRoot)

	expectErr := errors.E("Put", errors.NotDir)

	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName, parentPathName},
		Data: [][]byte{rootJSON, dirParentJSON},
	}

	ds := newTestDirServer(t, egcp)
	err := ds.Put(&dir)
	if !errors.Match(expectErr, err) {
		t.Fatalf("Put: error mismatch: got %v; want %v", err, expectErr)
	}
}

func TestPutFileOverwritesDir(t *testing.T) {
	// The DirEntry of the parent, converted to JSON.
	dirParentJSON := toJSON(t, dirParent)

	// The dir entry we're trying to add already exists as a directory.
	existingDirEntry := dir
	existingDirEntry.SetDir()
	existingDirEntryJSON := toJSON(t, existingDirEntry)

	rootJSON := toRootJSON(t, &userRoot)

	expectErr := errors.E("Put", errors.Exist)

	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName, pathName, parentPathName},
		Data: [][]byte{rootJSON, existingDirEntryJSON, dirParentJSON},
	}

	ds := newTestDirServer(t, egcp)
	err := ds.Put(&dir)
	if !errors.Match(expectErr, err) {
		t.Fatalf("Put: error mismatch: got %v; want %v", err, expectErr)
	}
}

func TestPutDirOverwritesFile(t *testing.T) {
	// The DirEntry of the parent, converted to JSON.
	dirParentJSON := toJSON(t, dirParent)

	// The dir entry we're trying to add already exists as a file.
	existingDirEntry := dir
	existingDirEntryJSON := toJSON(t, existingDirEntry)

	rootJSON := toRootJSON(t, &userRoot)

	expectErr := errors.E("MakeDirectory", errors.NotDir)

	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName, pathName, parentPathName},
		Data: [][]byte{rootJSON, existingDirEntryJSON, dirParentJSON},
	}

	ds := newTestDirServer(t, egcp)
	_, err := ds.MakeDirectory(dir.Name)
	if !errors.Match(expectErr, err) {
		t.Fatalf("MakeDirectory: error mismatch: got %v; want %v", err, expectErr)
	}
}

func TestPutPermissionDenied(t *testing.T) {
	newRoot := userRoot
	newRoot.accessFiles = make(accessFileDB)
	newRoot.accessFiles[rootAccessFile] = makeAccess(t, rootAccessFile, "") // No one can write, including owner.
	rootJSON := toRootJSON(t, &newRoot)

	expectErr := errors.E("Put", errors.Permission)

	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName},
		Data: [][]byte{rootJSON},
	}

	ds := newTestDirServer(t, egcp)
	err := ds.Put(&dir)
	if !errors.Match(expectErr, err) {
		t.Fatalf("Put: error mismatch: got %v; want %v", err, expectErr)
	}
}

func TestPut(t *testing.T) {
	// The DirEntry of the parent, converted to JSON.
	dirParentJSON := toJSON(t, dirParent)

	rootJSON := toRootJSON(t, &userRoot)

	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName, "test@foo.com/mydir"},
		Data: [][]byte{rootJSON, dirParentJSON},
	}

	ds := newTestDirServer(t, egcp)
	err := ds.Put(&dir)
	if err != nil {
		t.Fatal(err)
	}

	// Check that the parent Sequence number was updated...
	updatedParent := dirParent
	updatedParent.Sequence++
	updatedParentJSON := toJSON(t, updatedParent)

	updatedDir := dir
	updatedDirJSON := toJSON(t, updatedDir)

	// Verify what was actually put
	if len(egcp.PutContents) != 2 {
		t.Fatalf("Expected put to write 2 dir entries, got %d", len(egcp.PutContents))
	}
	if egcp.PutRef[0] != string(dirParent.Name) {
		t.Errorf("Expected put to write to %s, wrote to %s", dirParent.Name, egcp.PutRef[0])
	}
	if !bytes.Equal(updatedParentJSON, egcp.PutContents[0]) {
		t.Errorf("Expected put to write %s, wrote %s", updatedParentJSON, egcp.PutContents[0])
	}
	if egcp.PutRef[1] != string(dir.Name) {
		t.Errorf("Expected put to write to %s, wrote to %s", dir.Name, egcp.PutRef)
	}
	if !bytes.Equal(updatedDirJSON, egcp.PutContents[1]) {
		t.Errorf("Expected put to write %s, wrote %s", updatedDirJSON, egcp.PutContents[1])
	}

	// Check that a second put with SeqNotExist fails.
	ndir := dir
	ndir.Sequence = upspin.SeqNotExist
	err = ds.Put(&ndir)
	if err == nil {
		t.Fatal("Put with SeqNotExist should have failed")
	}
	if !strings.Contains(err.Error(), "file already exists") {
		t.Errorf("Put with SeqNotExist failed with %s", err)
	}
}

func TestMakeRoot(t *testing.T) {
	// rootJSON is what the server puts to GCP.
	userRootSavedNow := userRoot                // copy
	userRootSavedNow.dirEntry.Time = timeFunc() // time of creation is now.
	rootJSON := toRootJSON(t, &userRootSavedNow)

	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref: []string{"does not exist"},
	}

	ds := newTestDirServer(t, egcp)
	de, err := ds.MakeDirectory(userRoot.dirEntry.Name)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(&userRootSavedNow.dirEntry, de) {
		t.Fatalf("entries differ: got %v; want %v", de, &userRootSavedNow.dirEntry)
	}

	if len(egcp.PutContents) != 1 {
		t.Fatalf("Expected put to write 1 dir entry, got %d", len(egcp.PutContents))
	}
	if egcp.PutRef[0] != userName {
		t.Errorf("Expected put to write to %s, wrote to %s", userName, egcp.PutRef)
	}
	if !bytes.Equal(rootJSON, egcp.PutContents[0]) {
		t.Errorf("Expected put to write %s, wrote %s", rootJSON, egcp.PutContents[0])
	}
}

func TestMakeRootPermissionDenied(t *testing.T) {
	expectErr := errors.E("MakeDirectory", errors.Permission)

	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref: []string{"does not exist"},
	}

	ds := newTestDirServer(t, egcp)

	// The session is for a user other than the expected root owner.
	ds.context.SetUserName("bozo@theclown.org")
	_, err := ds.MakeDirectory(userRoot.dirEntry.Name)
	if !errors.Match(expectErr, err) {
		t.Fatalf("MakeDirectory: error mismatch: got %v; want %v", err, expectErr)
	}

	if len(egcp.PutContents) != 0 {
		t.Fatalf("Expected put to write 0 dir entries, got %d", len(egcp.PutContents))
	}
}

func TestPutAccessFile(t *testing.T) {
	var (
		parentDir      = userName + "/subdir"
		accessPath     = parentDir + "/Access"
		accessContents = "r: mom@me.com\nl: bro@me.com"
	)

	// The DirEntry we're trying to Put, converted to JSON.
	dir := upspin.DirEntry{
		Name: upspin.PathName(accessPath),
		Blocks: []upspin.DirBlock{{
			Location: upspin.Location{
				Reference: "1234",
				Endpoint: upspin.Endpoint{
					Transport: upspin.GCP,
					NetAddr:   upspin.NetAddr("https://store-server.upspin.io"),
				},
			},
		}},
	}

	// The DirEntry of the root.
	rootJSON := toRootJSON(t, &userRoot)

	// The DirEntry of the parent
	dirParent := upspin.DirEntry{
		Name: upspin.PathName(parentDir),
		Attr: upspin.AttrDirectory,
	}
	dirParentJSON := toJSON(t, dirParent)

	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName, parentDir},
		Data: [][]byte{rootJSON, dirParentJSON},
	}

	// Setup the directory's store client to return the contents of the access file.
	f, err := factotum.New(repo("key/testdata/gcp"))
	if err != nil {
		t.Fatal(err)
	}

	ds := newDirectory(egcp, f,
		func(e upspin.Endpoint) (upspin.StoreServer, error) {
			return &dummyStore{
				ref:      upspin.Reference("1234"),
				contents: []byte(accessContents),
			}, nil
		}, timeFunc)
	ds.context.SetUserName(userName) // the default user for the default session.
	ds.endpoint = serviceEndpoint
	err = ds.Put(&dir)
	if err != nil {
		t.Fatal(err)
	}

	// And the server Put a new root to GCP, the Access file and incremented the parent's sequence.
	if len(egcp.PutRef) != 3 {
		t.Fatalf("Expected one Put, got %d", len(egcp.PutRef))
	}
	// First, update the parent.
	if egcp.PutRef[0] != parentDir {
		t.Errorf("Expected put to %s, got %s", parentDir, egcp.PutRef[0])
	}
	// Then store the Access file.
	if egcp.PutRef[1] != accessPath {
		t.Errorf("Expected put to %s, got %s", accessPath, egcp.PutRef[1])
	}
	// Finally update the root.
	if egcp.PutRef[2] != userName {
		t.Errorf("Expected put to %s, got %s", userName, egcp.PutRef[2])
	}
	// Check that the root was updated with the new Access file.
	acc := makeAccess(t, upspin.PathName(accessPath), accessContents)
	expectedRoot := userRoot // Shallow copy
	expectedRoot.accessFiles = make(accessFileDB)
	// Copy map instead of modifying a test global (that will be re-used later)
	for k, v := range userRoot.accessFiles {
		expectedRoot.accessFiles[k] = v
	}
	expectedRoot.accessFiles[upspin.PathName(accessPath)] = acc
	expectedRootJSON := toRootJSON(t, &expectedRoot)
	if !bytes.Equal(egcp.PutContents[2], expectedRootJSON) {
		t.Errorf("Expected new root %s, got %s", expectedRootJSON, egcp.PutContents[2])
	}
}

func TestGroupAccessFile(t *testing.T) {
	// There's an access file that gives rights to a Group called family, which contains one user.
	const broUserName = "bro@family.com"
	newRoot := userRoot
	newRoot.accessFiles = make(accessFileDB)
	newRoot.accessFiles[rootAccessFile] = makeAccess(t, rootAccessFile, "r,l,w,c: family, "+userName)
	rootJSON := toRootJSON(t, &newRoot)

	refOfGroupFile := "sha-256 of Group/family"
	groupDir := upspin.DirEntry{
		Name: upspin.PathName(userName + "/Group/family"),
		Blocks: []upspin.DirBlock{{
			Location: upspin.Location{
				Reference: upspin.Reference(refOfGroupFile),
				Endpoint:  dir.Blocks[0].Location.Endpoint, // Same endpoint as the dir entry itself.
			},
		}},
	}
	groupDirJSON := toJSON(t, groupDir)

	groupParentDir := upspin.DirEntry{
		Name: upspin.PathName(userName + "/Group"),
		Attr: upspin.AttrDirectory,
	}
	groupParentDirJSON := toJSON(t, &groupParentDir)

	// newGroupDir is where the new group file will go when the user puts it. Just the reference changes.
	newRefOfGroupFile := "new sha-256 of newly-put Group/family"
	newGroupDir := groupDir
	newGroupDir.Blocks = append([]upspin.DirBlock(nil), groupDir.Blocks...)
	newGroupDir.Blocks[0].Location.Reference = upspin.Reference(newRefOfGroupFile)
	newGroupDirJSON := toJSON(t, &newGroupDir)

	contentsOfFamilyGroup := broUserName
	newContentsOfFamilyGroup := "sister@family.com" // bro@family.com is dropped!

	// We'll now attempt to have broUserName read a file under userName's tree.
	dirJSON := toJSON(t, dir) // dir is the dirEntry of the file that broUserName will attempt to read

	// Internally, we look up the root, the Group file and finally the pathName requested. Later, a new group file is
	// put so we lookup its parent and finally we retrieve the new group entry.
	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName, userName + "/Group/family", pathName, userName + "/Group", userName + "/Group/family"},
		Data: [][]byte{rootJSON, groupDirJSON, dirJSON, groupParentDirJSON, newGroupDirJSON},
	}

	// Setup the directory's store client to return the contents of the Group file.
	d1 := &dummyStore{
		ref:      upspin.Reference(refOfGroupFile),
		contents: []byte(contentsOfFamilyGroup),
	}
	d2 := &dummyStore{
		ref:      upspin.Reference(newRefOfGroupFile),
		contents: []byte(newContentsOfFamilyGroup),
	}
	f, err := factotum.New(repo("key/testdata/gcp"))
	if err != nil {
		t.Fatal(err)
	}
	// Create a store factory that returns d1 then d2.
	count := 0
	ds := newDirectory(egcp, f, func(e upspin.Endpoint) (upspin.StoreServer, error) {
		count++
		switch count {
		case 1:
			return d1, nil
		case 2:
			return d2, nil
		}
		return nil, errors.Str("invalid")
	}, timeFunc)
	// Create a session for broUserName
	ds.context.SetUserName(broUserName)
	de, err := ds.Lookup(pathName)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(&dir, de) {
		t.Fatalf("entries differ: got %v; want %v", de, &dir)
	}

	// Now Put a new Group with new contents that does not include broUserName and check that if we fetch the file
	// again with access will be denied, because the new definition got picked up (after first being invalidated).

	ds.context.SetUserName(userName)
	err = ds.Put(&newGroupDir) // This is the owner of the file putting the new group file.
	if err != nil {
		t.Fatal(err)
	}

	// Go back to bro accessing.
	ds.context.SetUserName(broUserName)
	// Expected permission denied this time.
	expectErr := errors.E("Lookup", errors.Permission)
	_, err = ds.Lookup(pathName)
	if !errors.Match(expectErr, err) {
		t.Fatalf("Lookup: error mismatch: got %v; want %v", err, expectErr)
	}
}

func TestMarshalRoot(t *testing.T) {
	const (
		dirRoot          = upspin.PathName("me@here.com/")
		fileRoot         = dirRoot + "foo.txt"
		dirRestricted    = dirRoot + "restricted"
		accessRoot       = dirRoot + "Access"
		accessRestricted = dirRestricted + "/Access"
		reader           = upspin.UserName("bob@foo.com")
		writer           = upspin.UserName("marie@curie.fr")
		lister           = upspin.UserName("gandh@pace.in")
	)
	acc1 := makeAccess(t, accessRoot, string("r: "+reader+"\nw: "+writer))
	acc2 := makeAccess(t, accessRestricted, string("l: "+lister))
	r := &root{
		dirEntry: upspin.DirEntry{
			Name: upspin.PathName("me@here.com/"),
			Attr: upspin.AttrDirectory,
		},
		accessFiles: accessFileDB{accessRoot: acc1, accessRestricted: acc2},
	}

	// Round trip to/from JSON.
	b := toRootJSON(t, r)
	var err error
	r, err = unmarshalRoot(b)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := len(r.accessFiles), 2; got != want {
		t.Fatalf("got %v access files, want %v", got, want)
	}

	// Test that the saved access file has the expected permissions.
	cases := []struct {
		access upspin.PathName
		user   upspin.UserName
		right  access.Right
		path   upspin.PathName
		want   bool
	}{
		{accessRoot, reader, access.Read, fileRoot, true},
		{accessRoot, reader, access.Write, fileRoot, false},
		{accessRoot, writer, access.Write, dirRoot, true},
		{accessRoot, writer, access.Read, fileRoot, false},
		{accessRestricted, lister, access.List, dirRestricted, true},
		{accessRestricted, reader, access.Read, dirRestricted, false},
	}
	for _, c := range cases {
		acc, ok := r.accessFiles[c.access]
		if !ok {
			t.Fatalf("could not find %q in accessFiles", c.access)
		}
		got, err := acc.Can(c.user, c.right, c.path, nil)
		if err != nil {
			t.Errorf("Can(%v, %v, %v) returned error: %v",
				c.user, c.right, c.path, err)
			continue
		}
		if got != c.want {
			t.Errorf("Can(%v, %v, %v) = %v, want %v",
				c.user, c.right, c.path, got, c.want)
		}
	}
}

func TestGCPCorruptsData(t *testing.T) {
	rootJSON := toRootJSON(t, &userRoot)

	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName, parentPathName},
		Data: [][]byte{rootJSON, []byte("really bad JSON structure that does not parse")},
	}

	expectErr := errors.E(errors.IO, errors.Str("json unmarshal failed retrieving metadata: invalid character 'r' looking for beginning of value"))

	ds := newTestDirServer(t, egcp)
	err := ds.Put(&dir)
	if !errors.Match(expectErr, err) {
		t.Fatalf("Put: error mismatch: got %v; want %v", err, expectErr)
	}
}

func TestLookup(t *testing.T) {
	dirEntryJSON := toJSON(t, dir)
	rootJSON := toRootJSON(t, &userRoot)

	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName, pathName},
		Data: [][]byte{rootJSON, dirEntryJSON},
	}

	ds := newTestDirServer(t, egcp)
	de, err := ds.Lookup(pathName)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(&dir, de) {
		t.Fatalf("entries differ: got %v; want %v", de, &dir)
	}
}

func TestLookupPermissionDenied(t *testing.T) {
	rootJSON := toRootJSON(t, &userRoot)

	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName},
		Data: [][]byte{rootJSON},
	}

	expectErr := errors.E("Lookup", errors.Permission)
	ds := newTestDirServer(t, egcp)

	ds.context.SetUserName("sloppyjoe@unauthorized.com")
	_, err := ds.Lookup(pathName)
	if !errors.Match(expectErr, err) {
		t.Fatalf("Lookup: error mismatch: got %v; want %v", err, expectErr)
	}
}

func TestDelete(t *testing.T) {
	rootJSON := toRootJSON(t, &userRoot)
	dirEntryJSON := toJSON(t, dir)

	lgcp := &listGCP{
		ExpectDownloadCapturePut: storagetest.ExpectDownloadCapturePut{
			Ref:  []string{userName, pathName},
			Data: [][]byte{rootJSON, dirEntryJSON},
		},
		deletePathExpected: pathName,
	}

	ds := newTestDirServer(t, lgcp)

	err := ds.Delete(pathName)
	if err != nil {
		t.Fatal(err)
	}

	if lgcp.listDirCalled {
		t.Errorf("ListDir should not have been called as pathName is not a directory")
	}
	if !lgcp.deleteCalled {
		t.Errorf("Delete should have been called")
	}
}

func TestDeleteDirNotEmpty(t *testing.T) {
	rootJSON := toRootJSON(t, &userRoot)
	parentPathJSON := toJSON(t, dirParent)

	lgcp := &listGCP{
		ExpectDownloadCapturePut: storagetest.ExpectDownloadCapturePut{
			Ref:  []string{userName, parentPathName},
			Data: [][]byte{rootJSON, parentPathJSON},
		},
		prefix:    parentPathName + "/",
		fileNames: []string{pathName}, // pathName is inside parentPathName.
	}

	expectErr := errors.E("Delete", errors.NotEmpty)

	ds := newTestDirServer(t, lgcp)

	err := ds.Delete(parentPathName)
	if !errors.Match(expectErr, err) {
		t.Fatalf("Delete: error mismatch: got %v; want %v", err, expectErr)
	}

	if !lgcp.listDirCalled {
		t.Errorf("ListDir should have been called as pathName is a directory")
	}
	if lgcp.deleteCalled {
		t.Errorf("Delete should not have been called")
	}
}

func TestDeleteDirPermissionDenied(t *testing.T) {
	rootJSON := toRootJSON(t, &userRoot)

	lgcp := &listGCP{
		ExpectDownloadCapturePut: storagetest.ExpectDownloadCapturePut{
			Ref:  []string{userName}, // only the root is looked up.
			Data: [][]byte{rootJSON},
		},
	}

	expectErr := errors.E("Delete", errors.Permission)

	ds := newTestDirServer(t, lgcp)

	ds.context.SetUserName("some-random-dude@bozo.com")
	err := ds.Delete(pathName)
	if !errors.Match(expectErr, err) {
		t.Fatalf("Delete: error mismatch: got %v; want %v", err, expectErr)
	}

	if lgcp.listDirCalled {
		t.Errorf("ListDir should not have been called as pathName is not a directory")
	}
	if lgcp.deleteCalled {
		t.Errorf("Delete should not have been called")
	}
}

func TestDeleteAccessFile(t *testing.T) {
	accessDir := upspin.DirEntry{
		Name: rootAccessFile,
		Blocks: []upspin.DirBlock{{
			Location: upspin.Location{
				Reference: "some place in store", // We don't need this, but just for completion.
			},
		}},
	}
	accessDirJSON := toJSON(t, accessDir)
	// Let's pretend we had a non-default Access file for the root dir.
	accessFile := makeAccess(t, rootAccessFile, "r,w,c: somefolks@domain.com")
	newRoot := userRoot
	newRoot.accessFiles = accessFileDB{rootAccessFile: accessFile}

	rootJSON := toRootJSON(t, &newRoot)

	lgcp := &listGCP{
		ExpectDownloadCapturePut: storagetest.ExpectDownloadCapturePut{
			Ref:  []string{userName, rootAccessFile},
			Data: [][]byte{rootJSON, accessDirJSON},
		},
		deletePathExpected: rootAccessFile,
	}

	ds := newTestDirServer(t, lgcp)

	err := ds.Delete(rootAccessFile)
	if err != nil {
		t.Fatal(err)
	}

	// Verify we put a new root with a plain vanilla Access file.
	if len(lgcp.PutRef) != 1 {
		t.Fatalf("Expected one Put, got %d", len(lgcp.PutRef))
	}
	if lgcp.PutRef[0] != userName {
		t.Errorf("Expected a write to the root (%s/), wrote to %s instead", userName, lgcp.PutRef[0])
	}
	savedRoot := lgcp.PutContents[0]
	expectedRoot := toRootJSON(t, &userRoot)
	if !bytes.Equal(savedRoot, expectedRoot) {
		t.Errorf("Expected to save root contents %s, saved contents %s instead", expectedRoot, savedRoot)
	}
	// Verify we deleted the Access file
	if !lgcp.deleteCalled {
		t.Fatal("Delete on GCP was not called")
	}
}

func TestDeleteGroupFile(t *testing.T) {
	// There's an access file that gives rights to a Group called family, which contains one user.
	const broUserName = "bro@family.com"
	newRoot := userRoot
	newRoot.accessFiles = make(accessFileDB)
	newRoot.accessFiles[rootAccessFile] = makeAccess(t, rootAccessFile, "r,l,w,c: family, "+userName)
	rootJSON := toRootJSON(t, &newRoot)

	dirJSON := toJSON(t, dir)

	groupPathName := upspin.PathName(userName + "/Group/family")
	access.AddGroup(groupPathName, []byte(broUserName))

	refOfGroupFile := "sha-256 of Group/family"
	groupDir := upspin.DirEntry{
		Name: groupPathName,
		Blocks: []upspin.DirBlock{{
			Location: upspin.Location{
				Reference: upspin.Reference(refOfGroupFile),
				Endpoint:  dir.Blocks[0].Location.Endpoint, // Same endpoint as the dir entry itself.
			},
		}},
	}
	groupDirJSON := toJSON(t, groupDir)

	lgcp := &listGCP{
		ExpectDownloadCapturePut: storagetest.ExpectDownloadCapturePut{
			Ref:  []string{userName, pathName, string(groupPathName)},
			Data: [][]byte{rootJSON, dirJSON, groupDirJSON},
		},
		deletePathExpected: string(groupPathName),
	}

	// Verify that bro@family.com has access.
	ds := newTestDirServer(t, lgcp)

	ds.context.SetUserName(broUserName)
	de, err := ds.Lookup(pathName)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(&dir, de) {
		t.Fatalf("entries differ: got %v; want %v", de, &dir)
	}

	// Now the owner deletes the group file.
	ds.context.SetUserName(userName)
	err = ds.Delete(groupPathName)
	if err != nil {
		t.Fatal(err)
	}

	if !lgcp.deleteCalled {
		t.Errorf("Expected delete to be called on %s", groupPathName)
	}

	// And now the session for bro can no longer read it.
	expectErr := errors.E("Lookup", errors.NotExist)
	ds.context.SetUserName(broUserName)
	_, err = ds.Lookup(pathName)
	if !errors.Match(expectErr, err) {
		t.Fatalf("Lookup: error mismatch: got %v; want %v", err, expectErr)
	}
}

func TestWhichAccessImplicitAtRoot(t *testing.T) {
	rootJSON := toRootJSON(t, &userRoot)

	// The Access file at the root really exists.
	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName},
		Data: [][]byte{rootJSON},
	}

	ds := newTestDirServer(t, egcp)
	accessPath, err := ds.WhichAccess(pathName)
	if err != nil {
		t.Fatal(err)
	}
	if accessPath != "" {
		t.Errorf("Expected implicit path, got %q", accessPath)
	}
}

func TestWhichAccess(t *testing.T) {
	rootJSON := toRootJSON(t, &userRoot)
	accessJSON := toJSON(t, upspin.DirEntry{
		Name: rootAccessFile,
	})

	// The Access file at the root really exists.
	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName, rootAccessFile},
		Data: [][]byte{rootJSON, accessJSON},
	}

	ds := newTestDirServer(t, egcp)
	accessPath, err := ds.WhichAccess(pathName)
	if err != nil {
		t.Fatal(err)
	}
	if accessPath != rootAccessFile {
		t.Errorf("Expected %q, got %q", rootAccessFile, accessPath)
	}
}

func TestWhichAccessPermissionDenied(t *testing.T) {
	rootJSON := toRootJSON(t, &userRoot)

	expectErr := errors.E("WhichAccess", errors.NotExist)

	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName},
		Data: [][]byte{rootJSON},
	}

	ds := newTestDirServer(t, egcp)
	ds.context.SetUserName("somerandomguy@a.co")
	_, err := ds.WhichAccess(pathName)
	if !errors.Match(expectErr, err) {
		t.Fatalf("WhichAccess: error mismatch: got %v; want %v", err, expectErr)
	}
}

func toJSON(t *testing.T, data interface{}) []byte {
	ret, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return ret
}

func toRootJSON(t *testing.T, root *root) []byte {
	json, err := marshalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	return json
}

func makeAccess(t *testing.T, path upspin.PathName, accessFileContents string) *access.Access {
	acc, err := access.Parse(path, []byte(accessFileContents))
	if err != nil {
		t.Fatal(err)
	}
	return acc
}

func newTestDirServer(t *testing.T, gcp storage.Storage) *directory {
	f, err := factotum.New(repo("key/testdata/gcp"))
	if err != nil {
		t.Fatal(err)
	}
	ds := newDirectory(gcp, f, nil, timeFunc)
	ds.context.SetUserName(userName) // the default user for the default session.
	ds.endpoint = serviceEndpoint
	return ds
}

// listGCP is an ExpectDownloadCapturePutGCP that returns a slice of fileNames
// if a call to ListPrefix or ListDir matches the expected prefix or dir.
type listGCP struct {
	storagetest.ExpectDownloadCapturePut
	prefix             string
	fileNames          []string
	listPrefixCalled   bool
	listDirCalled      bool
	deletePathExpected string
	deleteCalled       bool
}

func (l *listGCP) ListPrefix(prefix string, depth int) ([]string, error) {
	l.listPrefixCalled = true
	if l.prefix == prefix {
		return l.fileNames, nil
	}
	return []string{}, errors.Str("Not found")
}

func (l *listGCP) ListDir(dir string) ([]string, error) {
	l.listDirCalled = true
	if l.prefix == dir {
		return l.fileNames, nil
	}
	return []string{}, errors.Str("Not found")
}

func (l *listGCP) Delete(path string) error {
	l.deleteCalled = true
	if path == l.deletePathExpected {
		return nil
	}
	return errors.E("Delete", errors.NotExist)
}

type dummyStore struct {
	ref      upspin.Reference
	contents []byte
}

var _ upspin.StoreServer = (*dummyStore)(nil)

func (d *dummyStore) Get(ref upspin.Reference) ([]byte, []upspin.Location, error) {
	if ref == d.ref {
		return d.contents, nil, nil
	}
	return nil, nil, errors.Str("not found")
}
func (d *dummyStore) Put(data []byte) (upspin.Reference, error) {
	panic("unimplemented")
}
func (d *dummyStore) Dial(cc upspin.Context, e upspin.Endpoint) (upspin.Service, error) {
	panic("unimplemented")
}
func (d *dummyStore) Ping() bool {
	return true
}
func (d *dummyStore) Endpoint() upspin.Endpoint {
	panic("unimplemented")
}
func (d *dummyStore) Configure(options ...string) error {
	panic("unimplemented")
}
func (d *dummyStore) Delete(ref upspin.Reference) error {
	panic("unimplemented")
}
func (d *dummyStore) Close() {
}
func (d *dummyStore) Authenticate(upspin.Context) error {
	return nil
}

type sinkSaver struct {
}

func (s *sinkSaver) Register(ch chan *metric.Metric) {
	go func() {
		for {
			<-ch
			// Drop it on the floor
		}
	}()
}

var _ metric.Saver = (*sinkSaver)(nil)

func init() {
	// So we don't see a ton of "Metric channel is full" messages
	metric.RegisterSaver(&sinkSaver{})
}
