// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inprocess

// This test uses an in-process StoreServer for the underlying
// storage.
import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"upspin.io/bind"
	"upspin.io/context"
	"upspin.io/errors"
	"upspin.io/pack"
	"upspin.io/path"
	"upspin.io/upspin"

	_ "upspin.io/pack/debug"

	keyserver "upspin.io/key/inprocess"
	storeserver "upspin.io/store/inprocess"
)

func init() {
	bind.RegisterKeyServer(upspin.InProcess, keyserver.New())
	bind.RegisterStoreServer(upspin.InProcess, storeserver.New())
}

var (
	userNumber int32 // Updated atomically
)

func nextUser() upspin.UserName {
	atomic.AddInt32(&userNumber, 1)
	return upspin.UserName(fmt.Sprintf("user%d@google.com", userNumber))
}

func newContextAndServices(name upspin.UserName) (ctx upspin.Context, key upspin.KeyServer, dir upspin.DirServer, store upspin.StoreServer) {
	endpoint := upspin.Endpoint{
		Transport: upspin.InProcess,
		NetAddr:   "", // ignored
	}
	ctx = context.New()
	ctx = context.SetUserName(ctx, name)
	ctx = context.SetPacking(ctx, upspin.DebugPack)
	ctx = context.SetKeyEndpoint(ctx, endpoint)
	ctx = context.SetStoreEndpoint(ctx, endpoint)
	ctx = context.SetDirEndpoint(ctx, endpoint)

	key, _ = bind.KeyServer(ctx, ctx.KeyEndpoint())
	store, _ = bind.StoreServer(ctx, ctx.KeyEndpoint())
	dir = New(ctx)
	return
}

func setup() (upspin.Context, upspin.DirServer) {
	userName := nextUser()
	context, key, dir, _ := newContextAndServices(userName)
	publicKey := upspin.PublicKey(fmt.Sprintf("key for %s", userName))
	user := &upspin.User{
		Name:      upspin.UserName(userName),
		Dirs:      []upspin.Endpoint{context.DirEndpoint()},
		Stores:    []upspin.Endpoint{context.StoreEndpoint()},
		PublicKey: publicKey,
	}
	err := key.Put(user)
	if err != nil {
		panic(err)
	}
	_, err = dir.MakeDirectory(upspin.PathName(userName))
	if err != nil {
		panic(err)
	}
	return context, dir
}

func storeData(t *testing.T, context upspin.Context, data []byte, name upspin.PathName) *upspin.DirEntry {
	return storeDataHelper(t, context, data, name, context.Packing())
}

func storePlainData(t *testing.T, context upspin.Context, data []byte, name upspin.PathName) *upspin.DirEntry {
	return storeDataHelper(t, context, data, name, upspin.PlainPack)
}

func storeDataHelper(t *testing.T, context upspin.Context, data []byte, name upspin.PathName, packing upspin.Packing) *upspin.DirEntry {
	if path.Clean(name) != name {
		t.Fatalf("%q is not a clean path name", name)
	}
	entry, err := newDirEntry(context, packing, name, data, upspin.AttrNone, "", upspin.SeqIgnore)
	if err != nil {
		t.Fatal(err)
	}
	// Our implementation stores a block for a zero-length file and newDirEntry sets that up,
	// but dirServer.put does not allow that, so clear out the blocks here for an empty file.
	if len(data) == 0 {
		entry.Blocks = nil
	}
	return entry
}

// readAll retrieves the data for the entry. It is a test-only version of Service.readAll.
func readAll(context upspin.Context, entry *upspin.DirEntry) ([]byte, error) {
	packer := pack.Lookup(entry.Packing)
	if packer == nil {
		return nil, errors.Errorf("no packing %#x registered", entry.Packing)
	}
	u, err := packer.Unpack(context, entry)
	if err != nil {
		return nil, err
	}
	var data []byte
	for {
		block, ok := u.NextBlock()
		if !ok {
			break
		}
		store, err := bind.StoreServer(context, context.StoreEndpoint())
		if err != nil {
			return nil, err
		}
		ciphertext, locs, err := store.Get(block.Location.Reference)
		if err != nil {
			return nil, err
		}
		if locs != nil { // TODO
			return nil, errors.Str("dir/inprocess: redirection not implemented")
		}
		cleartext, err := u.Unpack(ciphertext)
		if err != nil {
			return nil, err
		}
		data = append(data, cleartext...)
	}
	return data, nil
}

func TestPutTopLevelFileUsingDirectory(t *testing.T) {
	context, directory := setup()
	user := context.UserName()
	root := upspin.PathName(user + "/")
	fileName := root + "file"
	const text = "hello sailor"

	entry1 := storeData(t, context, []byte(text), fileName)
	if len(entry1.Blocks) != 1 {
		t.Fatalf("internal error: %v: expected one block, found %d", fileName, len(entry1.Blocks))
	}
	_, err := directory.Put(entry1)
	if err != nil {
		t.Fatal("put file:", err)
	}

	// Test that Lookup returns the same location.
	entry2, err := directory.Lookup(fileName)
	if err != nil {
		t.Fatalf("lookup %s: %s", fileName, err)
	}
	if len(entry2.Blocks) != 1 {
		t.Fatalf("lookup %s: expected one block, found %d", fileName, len(entry2.Blocks))
	}
	if entry1.Blocks[0].Location != entry2.Blocks[0].Location {
		t.Errorf("Lookup's location does not match Put's location:\t%v\n\t%v", entry1.Blocks[0].Location, entry2.Blocks[0].Location)
	}

	// Fetch the data back and inspect it.
	clear, err := readAll(context, entry1)
	if err != nil {
		t.Fatal("unpack:", err)
	}
	str := string(clear)
	if str != text {
		t.Fatalf("get of %q has text %q; should be %q", fileName, str, text)
	}
}

const nFile = 100

func TestPutHundredTopLevelFilesUsingDirectory(t *testing.T) {
	context, directory := setup()
	user := context.UserName()
	// Create a hundred files.
	locs := make([]upspin.Location, nFile)
	for i := 0; i < nFile; i++ {
		text := "X" + strings.Repeat(fmt.Sprint(i), i) // Need a non-empty file so we have a Location.
		fileName := upspin.PathName(fmt.Sprintf("%s/file.%d", user, i))
		entry := storeData(t, context, []byte(text), fileName)
		_, err := directory.Put(entry)
		if err != nil {
			t.Fatal("put file:", err)
		}
		locs[i] = entry.Blocks[0].Location
	}
	// Read them all back in funny order.
	for i := 0; i < nFile; i++ {
		j := 7 * i % nFile
		text := "X" + strings.Repeat(fmt.Sprint(j), j)
		fileName := upspin.PathName(fmt.Sprintf("%s/file.%d", user, j))
		// Fetch the data back and inspect it.
		entry, err := directory.Lookup(fileName)
		if err != nil {
			t.Fatalf("lookup %s: %s", fileName, err)
		}
		clear, err := readAll(context, entry)
		if err != nil {
			t.Fatal("unpack:", err)
		}
		str := string(clear)
		if str != text {
			t.Fatalf("get of %q has text %q; should be %q", fileName, str, text)
		}
	}
}

func TestGetHundredTopLevelFilesUsingDirectory(t *testing.T) {
	context, directory := setup()
	user := context.UserName()
	// Create a hundred files.
	href := make([]upspin.Location, nFile)
	for i := 0; i < nFile; i++ {
		text := "Y" + strings.Repeat(fmt.Sprint(i), i) // Need a non-empty file so we have a Location.
		fileName := upspin.PathName(fmt.Sprintf("%s/file.%d", user, i))
		entry := storeData(t, context, []byte(text), fileName)
		_, err := directory.Put(entry)
		if err != nil {
			t.Fatal("put file:", err)
		}
		href[i] = entry.Blocks[0].Location
	}
	// Get them all back in funny order.
	for i := 0; i < nFile; i++ {
		j := 7 * i % nFile
		text := "Y" + strings.Repeat(fmt.Sprint(j), j)
		fileName := upspin.PathName(fmt.Sprintf("%s/file.%d", user, j))
		// Fetch the data back and inspect it.
		entry, err := directory.Lookup(fileName)
		if err != nil {
			t.Fatalf("lookup %s: %s", fileName, err)
		}
		clear, err := readAll(context, entry)
		if err != nil {
			t.Fatalf("%q: unpack file: %v", fileName, err)
		}
		str := string(clear)
		if str != text {
			t.Fatalf("get of %q has text %q; should be %q", fileName, str, text)
		}
	}
}

func TestCreateDirectoriesAndAFile(t *testing.T) {
	context, directory := setup()
	user := context.UserName()
	dirName := upspin.PathName(fmt.Sprintf("%s/foo", user))
	entry, err := directory.MakeDirectory(dirName)
	if err != nil {
		t.Fatal(err)
	}
	if entry == nil {
		t.Fatal("nil entry from MakeDirectory")
	}
	if entry.Name != dirName {
		t.Fatalf("entry from MakeDirectory(%q) is named %q", dirName, entry.Name)
	}
	if entry.Attr != upspin.AttrDirectory {
		t.Fatalf("entry from MakeDirectory(%q) has attr %v", dirName, entry.Attr)
	}
	_, err = directory.MakeDirectory(upspin.PathName(fmt.Sprintf("%s/foo/bar", user)))
	if err != nil {
		t.Fatal(err)
	}
	_, err = directory.MakeDirectory(upspin.PathName(fmt.Sprintf("%s/foo/bar/asdf", user)))
	if err != nil {
		t.Fatal(err)
	}
	_, err = directory.MakeDirectory(upspin.PathName(fmt.Sprintf("%s/foo/bar/asdf/zot", user)))
	if err != nil {
		t.Fatal(err)
	}
	fileName := upspin.PathName(fmt.Sprintf("%s/foo/bar/asdf/zot/file", user))
	text := "hello world"
	entry = storeData(t, context, []byte(text), fileName)
	e, err := directory.Put(entry)
	if err != nil {
		t.Fatal(err)
	}
	if e != nil {
		t.Fatal("non-nil entry from Put")
	}
	// Read it back.
	entry, err = directory.Lookup(fileName)
	data, err := readAll(context, entry)
	if err != nil {
		t.Fatalf("%q: unpack file: %v", fileName, err)
	}
	str := string(data)
	if str != text {
		t.Fatalf("expected %q; got %q", text, str)
	}
	// Now overwrite it.
	text = "goodnight mother"
	entry = storeData(t, context, []byte(text), fileName)
	_, err = directory.Put(entry)
	if err != nil {
		t.Fatal(err)
	}
	// Read it back.
	entry, err = directory.Lookup(fileName)
	data, err = readAll(context, entry)
	if err != nil {
		t.Fatalf("%q: second unpack file: %v", fileName, err)
	}
	str = string(data)
	if str != text {
		t.Fatalf("after overwrite expected %q; got %q", text, str)
	}
}

/*
	Tree:

		user@google.com/
			ten
				eleven (file)
				twelve
					thirteen (file)
			twenty
				twentyone (file)
				twentytwo (file)
			thirty (dir)
*/

type globTest struct {
	// Strings all miss the leading "user@google.com" for brevity.
	pattern string
	files   []string
	err     error
}

var globTests = []globTest{
	{"", []string{""}, nil},
	{"*", []string{"ten", "twenty", "thirty"}, nil},
	{"ten/eleven/thirteen", []string{}, errors.E(errors.NotExist)},
	{"ten/twelve/thirteen", []string{"ten/twelve/thirteen"}, nil},
	{"ten/*", []string{"ten/twelve", "ten/eleven"}, nil},
	{"ten/twelve/*", []string{"ten/twelve/thirteen"}, nil},
	{"twenty/tw*", []string{"twenty/twentyone", "twenty/twentytwo"}, nil},
	{"*/*", []string{"ten/twelve", "ten/eleven", "twenty/twentyone", "twenty/twentytwo"}, nil},
}

func TestGlob(t *testing.T) {
	context, directory := setup()
	user := context.UserName()
	// Build the tree.
	dirs := []string{
		"ten",
		"ten/twelve",
		"twenty",
		"thirty",
	}
	files := []string{
		"ten/eleven",
		"ten/twelve/thirteen",
		"twenty/twentyone",
		"twenty/twentytwo",
	}
	for _, dir := range dirs {
		name := upspin.PathName(fmt.Sprintf("%s/%s", user, dir))
		_, err := directory.MakeDirectory(name)
		if err != nil {
			t.Fatalf("make directory: %s: %v", name, err)
		}
	}
	for _, file := range files {
		name := upspin.PathName(fmt.Sprintf("%s/%s", user, file))
		entry := storeData(t, context, []byte(name), name)
		_, err := directory.Put(entry)
		if err != nil {
			t.Fatalf("make file: %s: %v", name, err)
		}
	}
	// Now do the test proper.
	for i, test := range globTests {
		t.Logf("%d: pattern %q expect %q", i, test.pattern, test.files)
		name := fmt.Sprintf("%s/%s", user, test.pattern)
		entries, err := directory.Glob(name)
		if test.err != nil {
			if !errors.Match(test.err, err) {
				t.Errorf("%s: got error %q, want %q", name, err, test.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v\n", test.pattern, err)
			continue
		}
		for i, f := range test.files {
			test.files[i] = fmt.Sprintf("%s/%s", user, f)
		}
		if len(test.files) != len(entries) {
			t.Errorf("%s: expected %d results; got %d:", test.pattern, len(test.files), len(entries))
			for _, e := range entries {
				t.Errorf("\t%q", e.Name)
			}
			continue
		}
		// Sort so they match the output of Glob.
		sort.Strings(test.files)
		for i, f := range test.files {
			entry := entries[i]
			if string(entry.Name) != f {
				t.Errorf("%s: expected %q; got %q", test.pattern, f, entry.Name)
				continue
			}
		}
	}
}

func TestGlobSyntaxError(t *testing.T) {
	context, directory := setup()
	// We need to create a file so the Glob test processes the whole pattern.
	user := context.UserName()
	root := upspin.PathName(user + "/")
	fileName := root + "file"
	entry := storeData(t, context, []byte("hello"), fileName)
	_, err := directory.Put(entry)
	if err != nil {
		t.Fatal(err)
	}
	expectErr := errors.E("dir/inprocess.Glob", errors.Syntax)
	_, err = directory.Glob(string(context.UserName()) + "/[]")
	if !errors.Match(expectErr, err) {
		t.Fatalf("err = %v; expected %v", err, expectErr)
	}
}

func TestSequencing(t *testing.T) {
	context, directory := setup()
	user := context.UserName()
	fileName := upspin.PathName(user + "/file")
	// Validate sequence increases after write.
	seq := int64(-1)
	for i := 0; i < 10; i++ {
		// Create a file.
		text := fmt.Sprintln("version", i)
		entry := storeData(t, context, []byte(text), fileName)
		_, err := directory.Put(entry)
		if err != nil {
			t.Fatalf("put file %d: %v", i, err)
		}
		entry, err = directory.Lookup(fileName)
		if err != nil {
			t.Fatalf("lookup file %d: %v", i, err)
		}
		if entry.Sequence <= seq {
			t.Fatalf("sequence file %d did not increase: old seq %d; new seq %d", i, seq, entry.Sequence)
		}
		seq = entry.Sequence
	}
	// Now check it updates if we set the sequence correctly.
	// Ditto for the directory.
	entry, err := directory.Lookup(upspin.PathName(user))
	if err != nil {
		t.Fatalf("lookup root: %v", err)
	}
	dirSeq := entry.Sequence
	entry = storeData(t, context, []byte("first seq version"), fileName)
	entry.Sequence = seq
	_, err = directory.Put(entry)
	if err != nil {
		t.Fatal(err)
	}
	entry, err = directory.Lookup(fileName)
	if err != nil {
		t.Fatalf("lookup file: %v", err)
	}
	if entry.Sequence != seq+1 {
		t.Fatalf("wrong sequence for file: expected %d got %d", seq+1, entry.Sequence)
	}
	entry, err = directory.Lookup(upspin.PathName(user))
	if err != nil {
		t.Fatalf("lookup root: %v", err)
	}
	if entry.Sequence != dirSeq+1 {
		t.Fatalf("wrong sequence for directory: expected %d got %d", dirSeq+1, entry.Sequence)
	}
	// Now check it fails if we don't.
	entry = storeData(t, context, []byte("second seq version"), fileName)
	entry.Sequence = seq
	_, err = directory.Put(entry)
	if err == nil {
		t.Fatal("expected error, got none")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "sequence mismatch") {
		t.Fatalf("expected sequence error, got %v", err)
	}
}

func TestRootDirectorySequencing(t *testing.T) {
	context, directory := setup()
	user := context.UserName()
	fileName := upspin.PathName(user + "/file")
	// Validate sequence increases after write.
	seq := int64(-1)
	for i := 0; i < 10; i++ {
		// Create a file.
		text := fmt.Sprintln("version", i)
		entry := storeData(t, context, []byte(text), fileName)
		_, err := directory.Put(entry)
		if err != nil {
			t.Fatalf("put file %d: %v", i, err)
		}
		entry, err = directory.Lookup(fileName)
		if err != nil {
			t.Fatalf("lookup dir %d: %v", i, err)
		}
		if entry.Sequence <= seq {
			t.Fatalf("sequence on dir %d did not increase: old seq %d; new seq %d", i, seq, entry.Sequence)
		}
		seq = entry.Sequence
	}
}

func TestSeqNotExist(t *testing.T) {
	context, directory := setup()
	user := context.UserName()
	fileName := upspin.PathName(user + "/file")
	entry := storeData(t, context, []byte("hello"), fileName)
	// First write with SeqNotExist should succeed.
	entry.Sequence = upspin.SeqNotExist
	_, err := directory.Put(entry)
	if err != nil {
		t.Fatalf("put file: %v", err)
	}
	// Second should fail.
	_, err = directory.Put(entry)
	if err == nil {
		t.Fatalf("put file succeeded; should have failed")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("put file expected 'already exists' error; got %v", err)
	}
}

func TestDelete(t *testing.T) {
	context, dir := setup()
	user := context.UserName()
	fileName := upspin.PathName(user + "/file")
	entry := storeData(t, context, []byte("hello"), fileName)
	_, err := dir.Put(entry)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dir.Lookup(fileName)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dir.Delete(fileName)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dir.Lookup(fileName)
	if err == nil {
		t.Fatal("file still exists after deletion")
	}
	// Another Delete should fail.
	_, err = dir.Delete(fileName)
	if err == nil {
		t.Fatal("second Delete succeeds")
	}
	const expect = "item does not exist"
	if !strings.Contains(err.Error(), expect) {
		t.Fatalf("second delete gives wrong error: %q; expected %q", err, expect)
	}
}

func TestDeleteDirectory(t *testing.T) {
	context, dir := setup()
	user := context.UserName()
	dirName := upspin.PathName(user + "/dir")
	fileName := dirName + "/file"
	_, err := dir.MakeDirectory(dirName)
	if err != nil {
		t.Fatal(err)
	}
	entry := storeData(t, context, []byte("hello"), fileName)
	_, err = dir.Put(entry)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dir.Lookup(fileName)
	if err != nil {
		t.Fatal(err)
	}
	// File exists. First attempt to delete directory should fail.
	_, err = dir.Delete(dirName)
	if err == nil {
		t.Fatal("deleted non-empty directory")
	}
	if err == upspin.ErrFollowLink {
		t.Fatal("unexpected link")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Fatalf("deleting non-empty directory succeeded with wrong error: %v", err)
	}
	// Now delete the file.
	_, err = dir.Delete(fileName)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dir.Lookup(fileName)
	if err == nil {
		t.Fatal("file still exists after deletion")
	}
	// Now try again to delete the directory.
	_, err = dir.Delete(dirName)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dir.Lookup(dirName)
	if err == nil {
		t.Fatal("directory still exists after deletion")
	}
}

func TestWhichAccess(t *testing.T) {
	context, dir := setup()
	user := context.UserName()
	dir1Name := upspin.PathName(user + "/dir1")
	dir2Name := dir1Name + "/dir2"
	fileName := dir2Name + "/file"
	accessFileName := dir1Name + "/Access"
	_, err := dir.MakeDirectory(dir1Name)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dir.MakeDirectory(dir2Name)
	if err != nil {
		t.Fatal(err)
	}
	entry := storeData(t, context, []byte("hello"), fileName)
	_, err = dir.Put(entry)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dir.Lookup(fileName)
	if err != nil {
		t.Fatal(err)
	}
	// No Access file exists. Should get root.
	accessEntry, err := dir.WhichAccess(fileName)
	if err != nil {
		t.Fatal(err)
	}
	if accessEntry != nil {
		t.Errorf("expected no Access file, got %q", accessEntry.Name)
	}
	// Add an Access file to dir1.
	entry = storePlainData(t, context, []byte("r:*@google.com\n"), accessFileName)
	_, err = dir.Put(entry)
	if err != nil {
		t.Fatal(err)
	}
	accessEntry, err = dir.WhichAccess(fileName)
	if err != nil {
		t.Fatal(err)
	}
	if accessEntry == nil || accessEntry.Name != accessFileName {
		t.Errorf("expected %q, got %q", accessFileName, accessEntry.Name)
	}
	// Remove Access file from dir1.
	_, err = dir.Delete(entry.Name)
	if err != nil {
		t.Fatal(err)
	}
	// No access file exists (again). Should get root.
	accessEntry, err = dir.WhichAccess(fileName)
	if err != nil {
		t.Fatal(err)
	}
	if accessEntry != nil {
		t.Errorf("expected no Access file, got %q", accessEntry.Name)
	}
}

func TestLinkToFile(t *testing.T) {
	context, dir := setup()
	user := context.UserName()
	dirName := upspin.PathName(user + "/dir")
	fileName := dirName + "/file"
	linkName := upspin.PathName(user + "/link")
	dirLinkName := upspin.PathName(user + "/dirlink")
	e, err := dir.MakeDirectory(dirName)
	if err != nil {
		t.Fatal(err)
	}
	if e == nil {
		t.Fatal("nil entry from MakeDirectory")
	}
	entry := storeData(t, context, []byte("hello"), fileName)
	e, err = dir.Put(entry)
	if err != nil {
		t.Fatal(err)
	}
	if e != nil {
		t.Fatal("non-nil entry from Put")
	}
	_, err = dir.Lookup(fileName)
	if err != nil {
		t.Fatal(err)
	}
	// File exists. Now create a link to it in the root.
	linkEntry, err := newDirEntry(context, upspin.PlainPack, linkName, nil, upspin.AttrLink, fileName, upspin.SeqIgnore)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dir.Put(linkEntry)
	if err != nil {
		t.Fatal(err)
	}

	// Lookup the link, should get ErrFollow link with the right path.
	lookupEntry, err := dir.Lookup(linkName)
	if err != upspin.ErrFollowLink {
		t.Fatalf("err = %v; expected %v", err, upspin.ErrFollowLink)
	}
	if !equal(linkEntry, lookupEntry) {
		t.Fatalf("lookup: expected\n\t%#v\ngot\n\t%#v", linkEntry, lookupEntry)
	}

	// Put through the link, should get ErrFollow link with the right path.
	putEntry, err := newDirEntry(context, upspin.PlainPack, linkName, []byte("hello"), upspin.AttrNone, "", upspin.SeqIgnore)
	if err != nil {
		t.Fatal(err)
	}
	e, err = dir.Put(putEntry)
	if err != upspin.ErrFollowLink {
		t.Fatalf("err = %v; expected %v", err, upspin.ErrFollowLink)
	}
	if !equal(linkEntry, lookupEntry) {
		t.Fatalf("lookup: expected %#v\ngot\n%#v", linkEntry, lookupEntry)
	}

	// Make a link to the directory.
	dirLinkEntry, err := newDirEntry(context, upspin.PlainPack, dirLinkName, nil, upspin.AttrLink, dirName, upspin.SeqIgnore)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dir.Put(dirLinkEntry)
	if err != nil {
		t.Fatal(err)
	}
	// Try to make a directory through the link, should get ErrFollowLink.
	e, err = dir.MakeDirectory(dirLinkName + "/subdir")
	if err != upspin.ErrFollowLink {
		t.Fatalf("err = %v; expected %v", err, upspin.ErrFollowLink)
	}
	if e.Link != dirName {
		t.Fatalf("link = %q; expected %q", e.Link, dirName)
	}

	// Test Glob("*/*"). We should get ErrFollowLink due to the evaluation of dirlink/*.
	entries, err := dir.Glob(string(user + "/*/*"))
	if err != upspin.ErrFollowLink {
		t.Fatalf("err = %v; expected %v", err, upspin.ErrFollowLink)
	}
	if !equalNames(t, user, entries, []upspin.PathName{"dir/file", "dirlink", "link"}) {
		t.Fatal(`wrong names from Glob("*")`)
	}

	// Test Glob("*"). It should not error out, but instead include the links.
	entries, err = dir.Glob(string(user + "/*"))
	if err != nil {
		t.Fatalf("err = %v; expected none", err)
	}
	if !equalNames(t, user, entries, []upspin.PathName{"dir", "dirlink", "link"}) {
		t.Fatal(`wrong names from Glob("*")`)
	}

	// Now try to delete the file link, should succeed but leave the original intact.
	_, err = dir.Delete(linkName)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dir.Lookup(fileName)
	if err != nil {
		t.Fatal(err)
	}
}

func equalNames(t *testing.T, user upspin.UserName, entries []*upspin.DirEntry, expectNames []upspin.PathName) bool {
	if len(entries) != len(expectNames) {
		t.Errorf("got %d entries, expected %d", len(entries), len(expectNames))
		return false
	}
	// The results are known to be sorted.
	for i, name := range expectNames {
		got := entries[i].Name
		expect := upspin.PathName(user) + "/" + name
		if got != upspin.PathName(expect) {
			t.Errorf("%d: name = %q; want = %q", i, got, expect)
			return false
		}
	}
	return true
}

func TestWhichAccessLink(t *testing.T) {
	context, dir := setup()
	user := context.UserName()
	// This is more elaborate than we need, but it's clear.
	// We construct a tree with a private directory and a public one, with
	// suitable access controls. (We just one user; it's all we need.)
	// The test verifies that a link in the public directory to a private
	// is controlled by the private Access file.
	publicDirName := upspin.PathName(user + "/public")
	privateDirName := upspin.PathName(user + "/private")
	privateFileName := privateDirName + "/file"
	publicLinkName := publicDirName + "/link" // Will point to the _private_file
	privateAccessFileName := upspin.PathName(user + "/private/Access")
	publicAccessFileName := upspin.PathName(user + "/public/Access")
	_, err := dir.MakeDirectory(publicDirName)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dir.MakeDirectory(privateDirName)
	if err != nil {
		t.Fatal(err)
	}
	entry := storeData(t, context, []byte("hello"), privateFileName)
	_, err = dir.Put(entry)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dir.Lookup(privateFileName)
	if err != nil {
		t.Fatal(err)
	}
	// Private file exists. Now create a link to it in the public directory.
	linkEntry, err := newDirEntry(context, upspin.PlainPack, publicLinkName, nil, upspin.AttrLink, privateFileName, upspin.SeqIgnore)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dir.Put(linkEntry)
	if err != nil {
		t.Fatal(err)
	}
	// Lookup the link, should get ErrFollow link with the right path.
	lookupEntry, err := dir.Lookup(publicLinkName)
	if err != upspin.ErrFollowLink {
		t.Fatalf("err = %v; expected %v", err, upspin.ErrFollowLink)
	}
	if !equal(linkEntry, lookupEntry) {
		t.Fatalf("lookup: expected %#v\ngot\n%#v", linkEntry, lookupEntry)
	}
	// All is well. Now create two access files, a public one and a private one.
	// The contents don't really matter, since DirServer doesn't evaluate links, but be thorough.
	entry = storePlainData(t, context, []byte(""), privateAccessFileName) // Empty file means the owner (only) can read or list.
	_, err = dir.Put(entry)
	if err != nil {
		t.Fatal(err)
	}
	allRights := fmt.Sprintf("*:%s\n", user)
	entry = storePlainData(t, context, []byte(allRights), publicAccessFileName) // Empty file means the owner (only) can read or list.
	_, err = dir.Put(entry)
	if err != nil {
		t.Fatal(err)
	}
	// WhichAccess should not show the private Access file, but instead present the link.
	entry, err = dir.WhichAccess(publicLinkName)
	if err != upspin.ErrFollowLink {
		t.Fatal(err)
	}
	if entry.Link != privateFileName {
		t.Fatalf("got %q for link; expected %q", entry.Link, privateFileName)
	}
}

// reflect.DeepEqual is too fussy, worrying about nil vs. empty. This is a lazy way to
// compare their equivalence.
func equal(d0, d1 *upspin.DirEntry) bool {
	b0, err := d0.Marshal()
	if err != nil {
		panic(err)
	}
	b1, err := d1.Marshal()
	if err != nil {
		panic(err)
	}
	return bytes.Equal(b0, b1)
}
