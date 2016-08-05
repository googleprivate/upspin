// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"upspin.io/access"
	"upspin.io/cloud/storage"
	"upspin.io/cloud/storage/storagetest"
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
		Blocks: []upspin.DirBlock{{
			Size: 32,
			Location: upspin.Location{
				Reference: upspin.Reference("1234"),
				Endpoint: upspin.Endpoint{
					Transport: upspin.GCP,
					NetAddr:   "https://store-server.com",
				},
			},
		}},
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
	// These are not real keys. Just *valid* keys so authClient does not complain.
	serverPublic  = upspin.PublicKey("p256\n104278369061367353805983276707664349405797936579880352274235000127123465616334\n26941412685198548642075210264642864401950753555952207894712845271039438170192\n")
	serverPrivate = "82201047360680847258309465671292633303992565667422607675215625927005262185934"
)

func assertError(t *testing.T, expectedError string, err error) {
	if err == nil {
		t.Fatal("Expected error")
	}
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("Expected error %q, got %q", expectedError, err)
	}
}

func assertDirEntries(t *testing.T, exectedDirEntries []*upspin.DirEntry, de []*upspin.DirEntry, err error) {
	if err != nil {
		t.Fatal(err)
	}
	if len(exectedDirEntries) != len(de) {
		t.Errorf("Expected %d dir entries, got %d: (%v vs %v)", len(exectedDirEntries), len(de), exectedDirEntries, de)
	}
	for i, dir := range exectedDirEntries {
		if !reflect.DeepEqual(*dir, *de[i]) {
			t.Errorf("Expected entry %d: %v, got %v", i, dir, de[i])
		}
	}
}

func assertDirEntry(t *testing.T, exectedDirEntry *upspin.DirEntry, de *upspin.DirEntry, err error) {
	assertDirEntries(t, []*upspin.DirEntry{exectedDirEntry}, []*upspin.DirEntry{de}, err)
}

func Put(t *testing.T, ds *directory, dirEntry *upspin.DirEntry, expectedError string) {
	err := ds.Put(dirEntry)
	assertError(t, expectedError, err)
}

func TestPutErrorParseRoot(t *testing.T) {
	// No path given
	Put(t, newTestDirServer(t, &storagetest.DummyStorage{}), &upspin.DirEntry{}, "no user name in path")
}

func TestPutErrorParseUser(t *testing.T) {
	dir := upspin.DirEntry{
		Name: upspin.PathName("a@x/myroot/myfile"),
	}
	Put(t, newTestDirServer(t, &storagetest.DummyStorage{}), &dir, "no user name in path")
}

func TestPutErrorInvalidSequenceNumber(t *testing.T) {
	dir := upspin.DirEntry{
		Name:     upspin.PathName("fred@bob.com/myroot/myfile"),
		Attr:     upspin.AttrDirectory,
		Sequence: upspin.SeqNotExist - 1,
	}
	Put(t, newTestDirServer(t, &storagetest.DummyStorage{}), &dir,
		"fred@bob.com/myroot/myfile: Put: invalid operation: invalid sequence number")
}

func TestLookupPathError(t *testing.T) {
	expectedError := "no user name in path"
	ds := newTestDirServer(t, &storagetest.DummyStorage{})
	_, err := ds.Lookup("")
	assertError(t, expectedError, err)
}

func TestGlobMissingPattern(t *testing.T) {
	expectedError := "no user name in path"
	ds := newTestDirServer(t, &storagetest.DummyStorage{})
	_, err := ds.Glob("")
	assertError(t, expectedError, err)
}

func TestGlobBadPath(t *testing.T) {
	expectedError := "bad user name in path"
	ds := newTestDirServer(t, &storagetest.DummyStorage{})
	_, err := ds.Glob("missing/email/dir/file")
	assertError(t, expectedError, err)
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

	ds := newTestDirServer(t, egcp)
	Put(t, ds, &dir, "test@foo.com/myroot/myfile: Put: item does not exist: parent path not found")
}

func TestLookupPathNotFound(t *testing.T) {
	rootJSON := toRootJSON(t, &userRoot)
	expectedError := "Lookup: item does not exist:\n\ttest@foo.com/invalid/invalid/invalid: Download: not found"

	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName, "something that does not match"},
		Data: [][]byte{rootJSON, []byte("")},
	}

	ds := newTestDirServer(t, egcp)
	_, err := ds.Lookup("test@foo.com/invalid/invalid/invalid")
	assertError(t, expectedError, err)
}

// Regression test to catch that we don't panic (by going past the root).
func TestLookupRoot(t *testing.T) {
	// The root converted to JSON.
	rootJSON := toRootJSON(t, &userRoot)

	expectedDirEntry := upspin.DirEntry{
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
	assertDirEntry(t, &expectedDirEntry, de, err)
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
	expectedDirEntry := dir                                                 // copy
	expectedDirEntry.Blocks = append([]upspin.DirBlock(nil), dir.Blocks...) // copy
	expectedDirEntry.Blocks[0].Location = upspin.Location{}                 // Zero location
	expectedDirEntry.Packdata = nil                                         // No pack data either

	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName, pathName},
		Data: [][]byte{rootJSON, dirJSON},
	}

	ds := newTestDirServer(t, egcp)
	ds.context.SetUserName("lister-dude@me.com")
	de, err := ds.Lookup(pathName)
	assertDirEntry(t, &expectedDirEntry, de, err)
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

	expectedDirEntries := []*upspin.DirEntry{&dir1, &dir2} // dir3 is NOT returned to user (no access)
	ds := newTestDirServer(t, lgcp)
	de, err := ds.Glob("f@b.co/sub*/*.pdf")
	assertDirEntries(t, expectedDirEntries, de, err)

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

	expectedDirEntries := []*upspin.DirEntry{&dir1, &dir2}

	ds := newTestDirServer(t, lgcp)
	de, err := ds.Glob(userName + "/subdir/*.pdf")
	assertDirEntries(t, expectedDirEntries, de, err)

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
	expectedDirEntries = []*upspin.DirEntry{&dir1, &dir2} // new expected response does not have Location.

	de, err = ds.Glob(userName + "/subdir/*.pdf")
	assertDirEntries(t, expectedDirEntries, de, err)
}

func TestPutParentNotDir(t *testing.T) {
	// The DirEntry of the parent, converted to JSON.
	notDirParent := dirParent
	notDirParent.Attr = upspin.AttrNone // Parent is not dir!
	dirParentJSON := toJSON(t, notDirParent)

	rootJSON := toRootJSON(t, &userRoot)

	expectedError := "test@foo.com/mydir/myfile.txt: Put: item is not a directory: parent is not a directory"

	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName, parentPathName},
		Data: [][]byte{rootJSON, dirParentJSON},
	}

	ds := newTestDirServer(t, egcp)
	err := ds.Put(&dir)
	assertError(t, expectedError, err)
}

func TestPutFileOverwritesDir(t *testing.T) {
	// The DirEntry of the parent, converted to JSON.
	dirParentJSON := toJSON(t, dirParent)

	// The dir entry we're trying to add already exists as a directory.
	existingDirEntry := dir
	existingDirEntry.SetDir()
	existingDirEntryJSON := toJSON(t, existingDirEntry)

	rootJSON := toRootJSON(t, &userRoot)

	expectedError := "test@foo.com/mydir/myfile.txt: Put: item already exists: directory already exists"

	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName, pathName, parentPathName},
		Data: [][]byte{rootJSON, existingDirEntryJSON, dirParentJSON},
	}

	ds := newTestDirServer(t, egcp)
	err := ds.Put(&dir)
	assertError(t, expectedError, err)
}

func TestPutDirOverwritesFile(t *testing.T) {
	// The DirEntry of the parent, converted to JSON.
	dirParentJSON := toJSON(t, dirParent)

	// The dir entry we're trying to add already exists as a file.
	existingDirEntry := dir
	existingDirEntryJSON := toJSON(t, existingDirEntry)

	rootJSON := toRootJSON(t, &userRoot)

	expectedError := "test@foo.com/mydir/myfile.txt: MakeDirectory: item is not a directory: overwriting file with directory"

	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName, pathName, parentPathName},
		Data: [][]byte{rootJSON, existingDirEntryJSON, dirParentJSON},
	}

	ds := newTestDirServer(t, egcp)
	_, err := ds.MakeDirectory(dir.Name)
	assertError(t, expectedError, err)
}

func TestPutPermissionDenied(t *testing.T) {
	newRoot := userRoot
	newRoot.accessFiles = make(accessFileDB)
	newRoot.accessFiles[rootAccessFile] = makeAccess(t, rootAccessFile, "") // No one can write, including owner.
	rootJSON := toRootJSON(t, &newRoot)

	expectedError := "test@foo.com/mydir/myfile.txt: Put: permission denied"

	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName},
		Data: [][]byte{rootJSON},
	}

	ds := newTestDirServer(t, egcp)
	err := ds.Put(&dir)
	assertError(t, expectedError, err)
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
	assertDirEntry(t, &userRootSavedNow.dirEntry, de, err)

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
	expectedError := "test@foo.com/, user bozo@theclown.org: MakeDirectory: permission denied"

	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref: []string{"does not exist"},
	}

	ds := newTestDirServer(t, egcp)

	// The session is for a user other than the expected root owner.
	ds.context.SetUserName("bozo@theclown.org")
	_, err := ds.MakeDirectory(userRoot.dirEntry.Name)
	assertError(t, expectedError, err)

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
	f, err := factotum.DeprecatedNew(serverPublic, serverPrivate)
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
	f, err := factotum.DeprecatedNew(serverPublic, serverPrivate)
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
		return nil, errors.New("invalid")
	}, timeFunc)
	// Create a session for broUserName
	ds.context.SetUserName(broUserName)
	de, err := ds.Lookup(pathName)
	assertDirEntry(t, &dir, de, err)

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
	expectedError := "test@foo.com/mydir/myfile.txt: Lookup: permission denied"
	_, err = ds.Lookup(pathName)
	assertError(t, expectedError, err)
}

func TestMarshalRoot(t *testing.T) {
	var (
		fileInRoot       = upspin.PathName("me@here.com/foo.txt")
		dirRestricted    = upspin.PathName("me@here.com/restricted")
		accessRoot       = upspin.PathName("me@here.com/Access")
		accessRestricted = upspin.PathName("me@here.com/restricted/Access")
	)
	acc1 := makeAccess(t, accessRoot, "r: bob@foo.com\nw: marie@curie.fr")
	acc2 := makeAccess(t, accessRestricted, "l: gandhi@peace.in")
	root := &root{
		dirEntry: upspin.DirEntry{
			Name: upspin.PathName("me@here.com/"),
			Attr: upspin.AttrDirectory,
		},
		accessFiles: accessFileDB{accessRoot: acc1, accessRestricted: acc2},
	}
	buf := toRootJSON(t, root)
	root2, err := unmarshalRoot(buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(root2.accessFiles) != 2 {
		t.Fatalf("Expected two Access files, got %d", len(root2.accessFiles))
	}
	acc1saved, ok := root2.accessFiles[accessRoot]
	if !ok {
		t.Fatalf("Expected %s to exist in DB.", accessRoot)
	}
	if !acc1.Equal(acc1saved) {
		t.Fatalf("files differ; want %q got %q\n", acc1, acc1saved)
	}
	can, err := acc1saved.Can(upspin.UserName("bob@foo.com"), access.Read, fileInRoot, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !can {
		t.Errorf("Expected bob@foo.com to have Read access to %s", fileInRoot)
	}
	acc2saved, ok := root2.accessFiles[accessRestricted]
	can, err = acc2saved.Can(upspin.UserName("gandhi@peace.in"), access.List, dirRestricted, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !can {
		t.Errorf("Expected gandhi@peace.in to have List access to %s", dirRestricted)
	}
}

func TestGCPCorruptsData(t *testing.T) {
	rootJSON := toRootJSON(t, &userRoot)

	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName, parentPathName},
		Data: [][]byte{rootJSON, []byte("really bad JSON structure that does not parse")},
	}

	expectedError := "test@foo.com/mydir: I/O error: json unmarshal failed retrieving metadata: invalid character 'r' looking for beginning of value"

	ds := newTestDirServer(t, egcp)
	err := ds.Put(&dir)
	assertError(t, expectedError, err)
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
	assertDirEntry(t, &dir, de, err)
}

func TestLookupPermissionDenied(t *testing.T) {
	rootJSON := toRootJSON(t, &userRoot)

	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName},
		Data: [][]byte{rootJSON},
	}

	expectedError := "test@foo.com/mydir/myfile.txt: Lookup: permission denied"
	ds := newTestDirServer(t, egcp)

	ds.context.SetUserName("sloppyjoe@unauthorized.com")
	_, err := ds.Lookup(pathName)
	assertError(t, expectedError, err)
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

	expectedError := "Delete: directory not empty:\n\ttest@foo.com/mydir"

	ds := newTestDirServer(t, lgcp)

	err := ds.Delete(parentPathName)
	assertError(t, expectedError, err)

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

	expectedError := "test@foo.com/mydir/myfile.txt: Delete: permission denied"

	ds := newTestDirServer(t, lgcp)

	ds.context.SetUserName("some-random-dude@bozo.com")
	err := ds.Delete(pathName)
	assertError(t, expectedError, err)

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
	assertDirEntry(t, &dir, de, err)

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
	expectedError := "Lookup: item does not exist:\n\ttest@foo.com/Group/family"
	ds.context.SetUserName(broUserName)
	_, err = ds.Lookup(pathName)
	assertError(t, expectedError, err)
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

	expectedError := "test@foo.com/mydir/myfile.txt: WhichAccess: item does not exist"

	egcp := &storagetest.ExpectDownloadCapturePut{
		Ref:  []string{userName},
		Data: [][]byte{rootJSON},
	}

	ds := newTestDirServer(t, egcp)
	ds.context.SetUserName("somerandomguy@a.co")
	_, err := ds.WhichAccess(pathName)
	assertError(t, expectedError, err)
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
	f, err := factotum.DeprecatedNew(serverPublic, serverPrivate)
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
	return []string{}, errors.New("Not found")
}

func (l *listGCP) ListDir(dir string) ([]string, error) {
	l.listDirCalled = true
	if l.prefix == dir {
		return l.fileNames, nil
	}
	return []string{}, errors.New("Not found")
}

func (l *listGCP) Delete(path string) error {
	l.deleteCalled = true
	if path == l.deletePathExpected {
		return nil
	}
	return errors.New("Not found")
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
	return nil, nil, errors.New("not found")
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
