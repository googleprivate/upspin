// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"

	"upspin.io/bind"
	"upspin.io/context"
	"upspin.io/upspin"

	_ "upspin.io/dir/inprocess"
	_ "upspin.io/key/inprocess"
	_ "upspin.io/pack/debug"
	_ "upspin.io/store/inprocess"
)

// TODO: Copied from directory/inprocess/all_test.go. Make this publicly available.

func newContext(name upspin.UserName) upspin.Context {
	inProcess := upspin.Endpoint{
		Transport: upspin.InProcess,
		NetAddr:   "", // ignored
	}
	context := context.New().SetUserName(name).SetPacking(upspin.DebugPack).SetKeyEndpoint(inProcess).SetDirEndpoint(inProcess).SetStoreEndpoint(inProcess)
	return context
}

func checkTransport(s upspin.Service) {
	if t := s.Endpoint().Transport; t != upspin.InProcess {
		panic(fmt.Sprintf("bad transport %v, want inprocess", t))
	}
}

func setup(userName upspin.UserName, publicKey upspin.PublicKey) upspin.Context {
	context := newContext(userName)
	key, err := bind.KeyServer(context, context.KeyEndpoint())
	if err != nil {
		panic(err)
	}
	checkTransport(key)
	dir, err := bind.DirServer(context, context.DirEndpoint())
	if err != nil {
		panic(err)
	}
	checkTransport(dir)
	if publicKey == "" {
		publicKey = upspin.PublicKey(fmt.Sprintf("key for %s", userName))
	}
	user := &upspin.User{
		Name:      upspin.UserName(userName),
		Dirs:      []upspin.Endpoint{context.DirEndpoint()},
		Stores:    []upspin.Endpoint{context.StoreEndpoint()},
		PublicKey: publicKey,
	}
	err = key.Put(user)
	if err != nil {
		panic(err)
	}
	_, err = dir.MakeDirectory(upspin.PathName(userName))
	if err != nil {
		panic(err)
	}
	return context
}

// TODO: End of copied code.

func TestPutGetTopLevelFile(t *testing.T) {
	const (
		user = "user1@google.com"
		root = user + "/"
	)
	client := New(setup(user, ""))
	const (
		fileName = root + "file"
		text     = "hello sailor"
	)
	_, err := client.Put(fileName, []byte(text))
	if err != nil {
		t.Fatal("put file:", err)
	}
	data, err := client.Get(fileName)
	if err != nil {
		t.Fatal("get file:", err)
	}
	if string(data) != text {
		t.Fatalf("get of %q has text %q; should be %q", fileName, data, text)
	}
}

const (
	Max = 100 * 1000 // Must be > 100.
)

func setupFileIO(user upspin.UserName, fileName upspin.PathName, max int, t *testing.T) (upspin.Client, upspin.File, []byte) {
	client := New(setup(user, ""))
	f, err := client.Create(fileName)
	if err != nil {
		t.Fatal("create file:", err)
	}

	// Create a data set with each byte equal to its offset.
	data := make([]byte, max)
	for i := range data {
		data[i] = uint8(i)
	}
	return client, f, data
}

func TestFileSequentialAccess(t *testing.T) {
	const (
		user     = "user3@google.com"
		root     = user + "/"
		fileName = user + "/" + "file"
	)
	client, f, data := setupFileIO(user, fileName, Max, t)

	// Write the file in randomly sized chunks until it's full.
	for offset, length := 0, 0; offset < Max; offset += length {
		// Pick a random length.
		length = rand.Intn(Max / 100)
		if offset+length > Max {
			length = Max - offset
		}
		n, err := f.Write(data[offset : offset+length])
		if err != nil {
			t.Fatalf("Write(offset %d length %d): %v", offset, length, err)
		}
		if n != length {
			t.Fatalf("Write length failed: offset %d expected %d got %d", offset, length, n)
		}
	}
	err := f.Close()
	if err != nil {
		t.Fatal(err)
	}

	// Now read it back with a similar scan.
	f, err = client.Open(fileName)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, Max)
	for offset, length := 0, 0; offset < Max; offset += length {
		length = rand.Intn(Max / 100)
		if offset+length > Max {
			length = Max - offset
		}
		n, err := f.Read(buf[offset : offset+length])
		if err != nil {
			t.Fatalf("Read(offset %d length %d): %v", offset, length, err)
		}
		if n != length {
			t.Fatalf("Read length failed: offset %d expected %d got %d", offset, length, n)
		}
		for i := offset; i < offset+length; i++ {
			if buf[i] != data[i] {
				t.Fatalf("Read at %d (%#x): expected %#.2x got %#.2x", i, i, data[i], buf[i])
			}
		}
	}
}

func TestFileRandomAccess(t *testing.T) {
	const (
		user     = "user4@google.com"
		root     = user + "/"
		fileName = root + "file"
	)
	client, f, data := setupFileIO(user, fileName, Max, t)

	// Use WriteAt at random offsets and random sizes to create file.
	// Start with a map of bools (easy) saying the byte has been written.
	// Loop until its length is the file size, meaning every byte has been written.
	written := make(map[int]bool)
	for len(written) != Max {
		// Pick a random offset and length.
		offset := rand.Intn(Max)
		// Don't bother starting at a known location - speeds up the coverage.
		for written[offset] {
			offset = rand.Intn(Max)
		}
		length := rand.Intn(Max / 100)
		if offset+length > Max {
			length = Max - offset
		}
		n, err := f.WriteAt(data[offset:offset+length], int64(offset))
		if err != nil {
			t.Fatalf("WriteAt(offset %d length %d): %v", offset, length, err)
		}
		if n != length {
			t.Fatalf("WriteAt length failed: offset %d expected %d got %d", offset, length, n)
		}
		for i := offset; i < offset+length; i++ {
			written[i] = true
		}
	}
	err := f.Close()
	if err != nil {
		t.Fatal(err)
	}

	// Read file back all at once, for simple verification.
	f, err = client.Open(fileName)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	result := make([]byte, Max)
	n, err := f.Read(result)
	if err != nil {
		t.Fatal(err)
	}
	if n != Max {
		t.Fatalf("Read: expected %d got %d", Max, n)
	}
	if !bytes.Equal(data, result) {
		for i, c := range data {
			if result[i] != c {
				t.Fatalf("byte at offset %d should be %#.2x is %#.2x", i, c, result[i])
			}
		}
	}

	// Now use a similar algorithm to WriteAt but with ReadAt to check random access.
	read := make(map[int]bool)
	buf := make([]byte, Max)
	for len(read) != Max {
		// Pick a random offset and length.
		offset := rand.Intn(Max)
		// Don't bother starting at a known location - speeds up the coverage.
		for read[offset] {
			offset = rand.Intn(Max)
		}
		length := rand.Intn(Max / 100)
		if offset+length > Max {
			length = Max - offset
		}
		n, err := f.ReadAt(buf[offset:offset+length], int64(offset))
		if err != nil {
			t.Fatalf("ReadAt(offset %d length %d): %v", offset, length, err)
		}
		if n != length {
			t.Fatalf("ReadAt length failed: offset %d expected %d got %d", offset, length, n)
		}
		for i := offset; i < offset+length; i++ {
			if buf[i] != data[i] {
				t.Fatalf("ReadAt at %d (%#x): expected %#.2x got %#.2x", i, i, data[i], buf[i])
			}
		}
		for i := offset; i < offset+length; i++ {
			read[i] = true
		}
	}
}

func TestFileZeroFill(t *testing.T) {
	const (
		user     = "zerofill@google.com"
		fileName = user + "/" + "file"
	)
	client, f, _ := setupFileIO(user, fileName, 0, t)
	// Create and write one byte 100 bytes out.
	f, err := client.Create(fileName)
	if err != nil {
		t.Fatal("create file:", err)
	}
	const N = 100
	n64, err := f.Seek(N, 0)
	if err != nil {
		t.Fatal("seek file:", err)
	}
	if n64 != N {
		t.Fatalf("seek file: expected %d got %d", N, n64)
	}
	n, err := f.Write([]byte{'x'})
	if err != nil {
		t.Fatal("write file:", err)
	}
	if n != 1 {
		t.Fatalf("write file: expected %d got %d", 1, n)
	}
	f.Close()
	// Read it back.
	f, err = client.Open(fileName)
	if err != nil {
		t.Fatal("open file:", err)
	}
	defer f.Close()
	buf := make([]byte, 2*N) // Much more than was written.
	// Make it all non-zero.
	for i := range buf {
		buf[i] = 'y'
	}
	n, err = f.Read(buf)
	if err != nil {
		t.Fatal("read file:", err)
	}
	if n != N+1 {
		t.Fatalf("read file: expected %d got %d", N+1, n)
	}
	for i := 0; i < N; i++ {
		if buf[i] != 0 {
			t.Errorf("byte %d should be 0 is %#.2x", i, buf[i])
		}
	}
	if buf[N] != 'x' {
		t.Errorf("byte %d should be 'x' is %#.2x", N, buf[N])
	}
}

func TestGlob(t *testing.T) {
	const user = "multiuser@a.co"
	client := New(setup(user, ""))
	var err error
	var paths []*upspin.DirEntry
	checkPaths := func(expPaths ...string) {
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if len(paths) != len(expPaths) {
			t.Fatalf("Expected %d paths, got %d", len(expPaths), len(paths))
		}
		for i, p := range expPaths {
			if string(paths[i].Name) != p {
				t.Errorf("Expected path %d %q, got %q", i, p, paths[i].Name)
			}
		}
	}

	for _, fno := range []int{0, 1, 7, 17} {
		fileName := fmt.Sprintf("%s/testfile%d.txt", user, fno)
		text := fmt.Sprintf("Contents of file %s", fileName)
		_, err = client.Put(upspin.PathName(fileName), []byte(text))
		if err != nil {
			t.Fatal("put file:", err)
		}
	}

	paths, err = client.Glob("multiuser@a.co/testfile*.txt")
	checkPaths("multiuser@a.co/testfile0.txt", "multiuser@a.co/testfile1.txt", "multiuser@a.co/testfile17.txt", "multiuser@a.co/testfile7.txt")

	paths, err = client.Glob("multiuser@a.co/*7.txt")
	checkPaths("multiuser@a.co/testfile17.txt", "multiuser@a.co/testfile7.txt")

	paths, err = client.Glob("multiuser@a.co/*1*.txt")
	checkPaths("multiuser@a.co/testfile1.txt", "multiuser@a.co/testfile17.txt")
}

func TestPutDuplicateAndRename(t *testing.T) {
	const user = "link@a.com"
	client := New(setup(user, ""))
	original := upspin.PathName(fmt.Sprintf("%s/original", user))
	text := "the rain in spain"
	if _, err := client.Put(original, []byte(text)); err != nil {
		t.Fatal("put file:", err)
	}

	// Duplicate: create a new name for the same reference.
	dup := upspin.PathName(fmt.Sprintf("%s/dup", user))
	entry, err := client.PutDuplicate(original, dup)
	if err != nil {
		t.Fatal("duplicate file:", err)
	}
	if entry == nil {
		t.Fatal("nil entry returned by PutDuplicate")
	}
	if entry.Name != dup {
		t.Fatal("duped directory entry has wrong name")
	}
	in, err := client.Get(original)
	if err != nil {
		t.Fatal("get file:", err)
	}
	if string(in) != text {
		t.Fatal(fmt.Sprintf("contents of %q corrupted", original))
	}
	in, err = client.Get(dup)
	if err != nil {
		t.Fatal("get file:", err)
	}
	if string(in) != text {
		t.Fatal(fmt.Sprintf("contents of %q and %q don't match", original, dup))
	}

	// Rename the new file.
	renamed := upspin.PathName(fmt.Sprintf("%s/renamed", user))
	if err := client.Rename(dup, renamed); err != nil {
		t.Fatal("link file:", err)
	}
	if _, err := client.Get(dup); err == nil {
		t.Fatal("renamed file still exists")
	}
	in, err = client.Get(renamed)
	if err != nil {
		t.Fatal("get file:", err)
	}
	if string(in) != text {
		t.Fatal(fmt.Sprintf("contents of %q and %q don't match", renamed, original))
	}
}

func TestSimpleLinks(t *testing.T) {
	const (
		user     = "linker@google.com"
		root     = user + "/"
		dirName  = root + "/dir"
		fileName = dirName + "/file"
		linkName = root + "/link"
		text     = "hello sailor"
		linkText = "what a lovely lovely day"
	)
	client := New(setup(user, ""))
	// Install and check file.
	_, err := client.MakeDirectory(dirName)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Put(fileName, []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	data, err := client.Get(fileName)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != text {
		t.Fatalf("get of %q has text %q; should be %q", fileName, data, text)
	}
	// Make a link.
	entry, err := client.PutLink(fileName, linkName)
	if err != nil {
		t.Fatal(err)
	}
	if entry == nil {
		t.Fatal("empty entry from PutLink")
	}
	// Get through the link.
	data, err = client.Get(linkName)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != text {
		t.Fatalf("get of %q has text %q; should be %q", fileName, data, text)
	}
	// Put through the link.
	_, err = client.Put(fileName, []byte(linkText))
	if err != nil {
		t.Fatal(err)
	}
	// Get through the file.
	data, err = client.Get(fileName)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != linkText {
		t.Fatalf("get of %q has text %q; should be %q", fileName, data, linkText)
	}
	// Get through the link.
	data, err = client.Get(linkName)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != linkText {
		t.Fatalf("get of %q has text %q; should be %q", fileName, data, linkText)
	}
	// Delete the link.
	err = client.Delete(linkName)
	if err != nil {
		t.Fatal(err)
	}
	// Get through the file, which should still be there.
	_, err = client.Get(fileName)
	if err != nil {
		t.Fatal(err)
	}
}
