// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package test

import (
	"fmt"
	"testing"

	"upspin.io/access"
	"upspin.io/test/testenv"
	"upspin.io/upspin"

	_ "upspin.io/dir/unassigned"
)

func testReadAccess(t *testing.T, r *testenv.Runner) {
	const (
		user              = readerName
		owner             = ownerName
		base              = owner + "/"
		groupDir          = base + "Group"
		publicDir         = base + "public"
		privateDir        = base + "private"
		publicFile        = publicDir + "/public.txt"
		privateFile       = privateDir + "/private.txt"
		contentsOfPublic  = "public file"
		contentsOfPrivate = "private file"
	)

	// Build test tree.
	r.As(owner)
	r.MakeDirectory(groupDir)
	r.MakeDirectory(publicDir)
	r.Put(publicFile, contentsOfPublic)
	r.MakeDirectory(privateDir)
	r.Put(privateFile, contentsOfPrivate)
	if r.Failed() {
		t.Fatal(r.Diag())
	}

	// With no access files, every item is readable by owner.
	r.Get(privateFile)
	if r.Failed() {
		t.Fatal(r.Diag())
	}
	if r.Data != contentsOfPrivate {
		t.Errorf("data = %q, want = %q", r.Data, contentsOfPrivate)
	}
	r.Get(publicFile)
	if r.Failed() {
		t.Fatal(r.Diag())
	}
	if r.Data != contentsOfPublic {
		t.Errorf("data = %q, want = %q", r.Data, contentsOfPublic)
	}

	// With no access files, no item is visible to user.
	r.As(user)
	r.DirLookup(base)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.DirLookup(privateDir)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.Get(privateFile)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.DirLookup(publicDir)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.Get(publicFile)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}

	// Add /public/Access, granting Read to user and write to owner.
	const accessFile = publicDir + "/Access"
	var (
		accessText = fmt.Sprintf("r:%s\nw:%s", user, owner)
	)
	r.As(owner)
	r.Put(accessFile, accessText)
	r.Put(publicFile, contentsOfPublic) // Put again to ensure re-wrapping of keys. TODO: fix.

	// With Access file, every item is still readable by owner.
	r.Get(privateFile)
	r.Get(publicFile)
	if r.Failed() {
		t.Fatal(r.Diag())
	}

	// With Access file, only public items are visible to user.
	r.As(user)
	r.DirLookup(base)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.DirLookup(privateDir)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.Get(privateFile)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.DirLookup(publicDir)
	if r.Failed() {
		t.Fatal(r.Diag())
	}
	r.Get(publicFile)
	if r.Failed() {
		t.Fatal(r.Diag())
	}
	if r.Data != contentsOfPublic {
		t.Errorf("data = %q, want = %q", r.Data, contentsOfPublic)
	}

	// Change Access file to disable again.
	const (
		noUserAccessText = "r: someoneElse@test.com\n"
	)
	r.As(owner)
	r.Put(accessFile, noUserAccessText)
	if r.Failed() {
		t.Fatal(r.Diag())
	}

	r.As(user)
	r.DirLookup(base)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.DirLookup(privateDir)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.Get(privateFile)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.DirLookup(publicDir)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.Get(publicFile)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.Put(publicFile, "will not succeed")
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}

	// Now create a group and put user in it and make owner a writer.
	const groupFile = groupDir + "/mygroup"
	var (
		groupAccessText = string("r: mygroup\nw:" + owner)
		groupText       = fmt.Sprintf("%s\n", user)
	)

	r.As(owner)
	r.Put(groupFile, groupText)
	r.Put(accessFile, groupAccessText)
	r.Put(publicFile, contentsOfPublic) // Put file again to trigger sharing.
	if r.Failed() {
		t.Fatal(r.Diag())
	}

	r.As(user)
	r.DirLookup(base)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.DirLookup(privateDir)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.Get(privateFile)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.DirLookup(publicDir)
	r.Get(publicFile)
	if r.Failed() {
		t.Fatal(r.Diag())
	}
	if r.Data != contentsOfPublic {
		t.Errorf("data = %q, want = %q", r.Data, contentsOfPublic)
	}

	// Remove Group file and check user lost all access now.
	r.As(owner)
	r.Delete(groupFile)

	r.As(user)
	r.DirLookup(publicDir)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.Get(publicFile)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}

	// Put group file back, but take user out of the group.
	const (
		noUserGroupText = "someoneElse@test.com\n"
	)

	r.As(owner)
	r.Put(groupFile, noUserGroupText)

	r.As(user)
	r.DirLookup(base)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.DirLookup(privateDir)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.Get(privateFile)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.DirLookup(publicDir)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.Get(publicFile)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}

	// Remove group file and dir, so tests are hermetic.
	r.As(owner)
	r.Delete(groupFile)
	r.Delete(groupDir)
}

func testWhichAccess(t *testing.T, r *testenv.Runner) {
	const (
		user              = readerName
		owner             = ownerName
		base              = owner + "/which-access"
		publicDir         = base + "/public"
		privateDir        = base + "/private"
		publicFile        = publicDir + "/public.txt"
		privateFile       = privateDir + "/private.txt"
		contentsOfPublic  = "public file"
		contentsOfPrivate = "private file"
	)
	r.As(owner)
	r.MakeDirectory(base)
	r.MakeDirectory(publicDir)
	r.Put(publicFile, contentsOfPublic)
	r.MakeDirectory(privateDir)
	r.Put(privateFile, contentsOfPrivate)
	if r.Failed() {
		t.Fatal(r.Diag())
	}

	// With no access files, every item is seen by owner.
	r.DirWhichAccess(base)
	if r.Entry != nil {
		t.Errorf("entry.Name = %q, want = nil", r.Entry.Name)
	}
	r.DirWhichAccess(privateDir)
	if r.Entry != nil {
		t.Errorf("entry.Name = %q, want = nil", r.Entry.Name)
	}
	r.DirWhichAccess(privateFile)
	if r.Entry != nil {
		t.Errorf("entry.Name = %q, want = nil", r.Entry.Name)
	}
	r.DirWhichAccess(publicDir)
	if r.Entry != nil {
		t.Errorf("entry.Name = %q, want = nil", r.Entry.Name)
	}
	r.DirWhichAccess(publicFile)
	if r.Entry != nil {
		t.Errorf("entry.Name = %q, want = nil", r.Entry.Name)
	}
	if r.Failed() {
		t.Fatal(r.Diag())
	}

	// With no access files, no item is seen by user.
	r.As(user)
	r.DirWhichAccess(base)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.DirWhichAccess(privateDir)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.DirWhichAccess(privateFile)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.DirWhichAccess(publicDir)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.DirWhichAccess(publicFile)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}

	// Add /public/Access, granting List to user.
	var (
		accessFile = upspin.PathName(publicDir + "/Access")
		accessText = fmt.Sprintf("list:%s\nw:%s", user, owner)
	)
	r.As(owner)
	r.Put(accessFile, accessText)
	if r.Failed() {
		t.Fatal(r.Diag())
	}

	// With Access file, every item is still seen by owner.
	r.DirWhichAccess(base)
	if r.Entry != nil {
		t.Errorf("entry.Name = %q, want = nil", r.Entry.Name)
	}
	r.DirWhichAccess(privateDir)
	if r.Entry != nil {
		t.Errorf("entry.Name = %q, want = nil", r.Entry.Name)
	}
	r.DirWhichAccess(privateFile)
	if r.Entry != nil {
		t.Errorf("entry.Name = %q, want = nil", r.Entry.Name)
	}
	r.DirWhichAccess(publicDir)
	if r.Failed() {
		t.Fatal(r.Diag())
	}
	if got, want := r.Entry.Name, accessFile; got != want {
		t.Errorf("entry.Name = %q, want = %q", got, want)
	}
	r.DirWhichAccess(publicFile)
	if r.Failed() {
		t.Fatal(r.Diag())
	}
	if got, want := r.Entry.Name, accessFile; got != want {
		t.Errorf("entry.Name = %q, want = %q", got, want)
	}

	// With Access file, only public items are seen by user.
	r.As(user)
	r.DirWhichAccess(base)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.DirWhichAccess(privateDir)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.DirWhichAccess(privateFile)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}
	r.DirWhichAccess(publicDir)
	if r.Failed() {
		t.Fatal(r.Diag())
	}
	if got, want := r.Entry.Name, accessFile; got != want {
		t.Errorf("entry.Name = %q, want = %q", got, want)
	}
	r.DirWhichAccess(publicFile)
	if r.Failed() {
		t.Fatal(r.Diag())
	}
	if got, want := r.Entry.Name, accessFile; got != want {
		t.Errorf("entry.Name = %q, want = %q", got, want)
	}
}

func testGroupAccess(t *testing.T, r *testenv.Runner) {
	const (
		base           = ownerName + "/group-access"
		accessFile     = base + "/Access"
		accessContents = "l,r: friends,family\n*: " + ownerName
		groupDir       = ownerName + "/Group"
		groupFriends   = groupDir + "/friends"
		groupFamily    = groupDir + "/family"
		familyMembers  = "uncle@domain.com,cousin@foo.com"
		groupContents  = familyMembers + "," + readerName
	)
	r.As(ownerName)
	r.MakeDirectory(base)
	r.MakeDirectory(groupDir)
	r.Put(groupFriends, readerName)
	r.Put(groupFamily, groupContents)
	r.Put(accessFile, accessContents)
	if r.Failed() {
		t.Fatal(r.Diag())
	}

	// Reader has List and Read access via both Group files.
	r.As(readerName)
	r.Glob(base + "/*")
	if !r.GotEntries(true, accessFile) {
		t.Fatal(r.Diag())
	}

	// Drop reader from one of the Group files.
	r.As(ownerName)
	r.Put(groupFamily, familyMembers)

	// Still got read Access.
	r.As(readerName)
	r.Glob(base + "/*")
	if !r.GotEntries(true, accessFile) {
		t.Fatal(r.Diag())
	}

	// Now drop from remaining Group file.
	r.As(ownerName)
	r.Put(groupFriends, "# Just kidding. This is empty.")

	// Can't see it anymore.
	r.As(readerName)
	r.DirLookup(base)
	if !r.Match(errNotExist) {
		t.Fatal(r.Diag())
	}

	// Give the reader list permissions by adding them
	// to the friends group, and adding the friends group
	// to the family group.
	r.As(ownerName)
	r.Put(groupFriends, "someone@else.com,family")
	r.Put(groupFamily, groupContents)
	r.Put(accessFile, "l:friends\n") // only list rights.
	if r.Failed() {
		t.Fatal(r.Diag())
	}

	// Can Glob but not see the contents (can't see Blocks).
	r.As(readerName)
	r.Glob(base + "/*")
	if !r.GotEntries(false, accessFile) {
		t.Fatal(r.Diag())
	}

	// Create a Group in the reader's tree that contains
	// both the reader and the owner, and then use that
	// Group in the owner's Access file.
	const (
		readerGroupDir  = readerName + "/Group"
		readerGroupFile = readerGroupDir + "/team"
	)

	// Create a tree for reader.
	r.As(readerName)
	r.MakeDirectory(readerName + "/")
	r.MakeDirectory(readerGroupDir)
	r.Put(readerGroupFile, ownerName+","+readerName)

	// Use only readerGroupFile in Access file.
	r.As(ownerName)
	r.Put(accessFile, "*:"+readerGroupFile)

	// Now reader can Lookup owner's directory.
	r.As(readerName)
	r.Glob(base + "/*")
	if !r.GotEntries(true, accessFile) {
		t.Fatal(r.Diag())
	}

	// Give the reader create rights in their Group,
	// and test that it works across trees.
	const newDir = base + "/newdir"
	r.As(ownerName)
	r.Put(accessFile, "c:"+readerGroupFile)
	r.MakeDirectory(newDir)
	if r.Failed() {
		t.Fatal(r.Diag())
	}
	r.Delete(newDir)
	if !r.Match(access.ErrPermissionDenied) {
		t.Fatal(r.Diag())
	}

	// Also test the delete right.
	r.Put(accessFile, "d:"+readerGroupFile)
	r.Delete(newDir)
	if r.Failed() {
		t.Fatal(r.Diag())
	}

	// List and Read via Group files are already tested in testReadAccess.

	// Clean up the reader's tree, as the integration test's cleanup
	// process doesn't know about it.
	r.As(readerName)
	r.Delete(readerGroupFile)
	r.Delete(readerGroupDir)
	r.Delete(readerName + "/")

	// Remove group file and dir, so tests are hermetic.
	r.As(ownerName)
	r.Delete(groupFriends)
	r.Delete(groupFamily)
	r.Delete(groupDir)
	if r.Failed() {
		t.Fatal(r.Diag())
	}
}

// TODO: cross DirServer support for Group files.
// Requires that DirServer implements it.
// Also, requires that testenv supports configuring multiple DirServers at
// once (it mostly does via serverMux, but requires that env.Setup passes a l
// ist of servers to work with).
