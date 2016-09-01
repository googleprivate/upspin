// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package test

import (
	"testing"

	"upspin.io/errors"
	"upspin.io/upspin"
)

// testGetErrors checks that the client receives 'not exist' or
// 'permission' errors where appropriate. In particular, it
// makes sure that the DirServer implementations do not allow
// unauthorized users to probe the name space. The rule of thumb
// is that users should receive 'not exist' for any path
// to which they have no rights.
func testGetErrors(t *testing.T, r *testRunner) {
	// Create a simple tree.
	const (
		base    = ownerName + "/get-errors"
		dir     = base + "/dir"
		file    = dir + "/file"
		access  = dir + "/Access"
		content = "hello, gophers"
	)
	r.As(ownerName)
	r.MakeDirectory(base)
	r.MakeDirectory(dir)
	r.Put(file, content)
	r.Get(file)
	if r.Failed() {
		t.Fatal(r.Diag())
	}

	// We expect a "not exist" error for a reader that has no rights
	// to the file, as they cannot even know that it exists.
	r.As(readerName)
	r.Get(file)
	if !r.Match(errors.E(errors.NotExist)) {
		t.Fatal(r.Diag())
	}

	// Put in an access file for the owner only
	// and try the same reader check again.
	r.As(ownerName)
	r.Put(access, "*:"+ownerName)
	if r.Failed() {
		t.Fatal(r.Diag())
	}
	r.As(readerName)
	r.Get(file)
	if !r.Match(errors.E(errors.NotExist)) {
		t.Fatal(r.Diag())
	}

	// Give the reader the a specific (non-read) right and
	// it should see a permission error.
	for _, right := range []string{"list", "write", "create", "delete"} {
		r.As(ownerName)
		r.Put(access, right+":"+readerName)
		if r.Failed() {
			t.Fatal(r.Diag())
		}
		r.As(readerName)
		r.Get(file)
		if !r.Match(errors.E(errors.Permission)) {
			t.Fatalf("%s: %s", right, r.Diag())
		}
	}

	// Give the reader the "read" right and it works.
	r.As(ownerName)
	r.Put(access, "*:"+ownerName+"\nread:"+readerName)
	r.Put(file, content) // Put the file again to wrap the keys.
	r.As(readerName)
	r.Get(file)
	if r.Failed() {
		t.Fatal(r.Diag())
	}
}

// testGetLinkErrors is like testGetErrors but checks the
// behavior of Get when links are present.
func testGetLinkErrors(t *testing.T, r *testRunner) {
	// Create a simple tree.
	const (
		base      = ownerName + "/get-link-errors"
		srcDir    = base + "/src"
		dstDir    = base + "/dst"
		link      = srcDir + "/link"
		file      = dstDir + "/file"
		srcAccess = srcDir + "/Access"
		dstAccess = dstDir + "/Access"
		content   = "hello, gophers"
	)
	r.As(ownerName)
	r.MakeDirectory(base)
	r.MakeDirectory(srcDir)
	r.MakeDirectory(dstDir)
	r.Put(file, content)
	r.Get(file)
	r.PutLink(file, link)
	r.Get(link)
	if r.Failed() {
		t.Fatal(r.Diag())
	}
	if r.Data != content {
		t.Fatalf("link content = %q, want %q", r.Data, content)
	}

	// Make sure we get ErrFollowLink when looking up the link.
	r.DirLookup(link)
	if !r.Match(upspin.ErrFollowLink) {
		t.Fatal(r.Diag())
	}

	// As a user with no rights over the link,
	// we should get a 'not exist' error.
	r.As(readerName)
	r.Get(link)
	if !r.Match(errors.E(errors.NotExist)) {
		t.Fatal(r.Diag())
	}
	r.DirLookup(link)
	if !r.Match(errors.E(errors.NotExist)) {
		t.Fatal(r.Diag())
	}

	// Give the user rights over the link's target,
	// but still no rights over the link. The user should
	// be able to get the file, but still can't see the link.
	r.As(ownerName)
	r.Put(dstAccess, "*:"+ownerName+"\n*:"+readerName)
	r.Put(file, content) // Put the file again to wrap the keys.
	r.As(readerName)
	r.Get(file)
	if r.Failed() {
		t.Fatal(r.Diag())
	}
	r.Get(link)
	if !r.Match(errors.E(errors.NotExist, upspin.PathName(link))) {
		t.Fatal(r.Diag())
	}
	r.DirLookup(link)
	if !r.Match(errors.E(errors.NotExist, upspin.PathName(link))) {
		t.Fatal(r.Diag())
	}

	// Give the user one right over the link and Get should work
	// as the client will now be able to follow the link.
	r.As(ownerName)
	r.Put(srcAccess, "create:"+readerName)
	r.As(readerName)
	r.Get(link)
	if r.Failed() {
		t.Fatal(r.Diag())
	}
	r.DirLookup(link)
	if !r.Match(upspin.ErrFollowLink) {
		t.Fatal(r.Diag())
	}

	// Remove the user's rights to the link target,
	// now a get of the link should fail with 'not exist' for the target.
	r.As(ownerName)
	r.Delete(dstAccess)
	r.As(readerName)
	r.Get(link)
	if !r.Match(errors.E(errors.NotExist, errors.E(upspin.PathName(file)))) {
		t.Fatal(r.Diag())
	}

	// Add a non-read permission for the target,
	// now a get of the link should fail with 'permission' for the target.
	r.As(ownerName)
	r.Put(dstAccess, "list:"+readerName)
	r.As(readerName)
	r.Get(link)
	if !r.Match(errors.E(errors.Permission, errors.E(upspin.PathName(file)))) {
		t.Fatal(r.Diag())
	}
}
