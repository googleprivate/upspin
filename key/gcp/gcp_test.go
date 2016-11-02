// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gcp

import (
	"encoding/json"
	"reflect"
	"testing"

	"upspin.io/cloud/storage/storagetest"
	"upspin.io/errors"
	"upspin.io/upspin"
)

const isAdmin = true

func TestLookupInvalidUser(t *testing.T) {
	userName := upspin.UserName("a")

	u := newDummyKeyServer()
	_, err := u.Lookup(userName)
	expectedErr := errors.E(errors.Invalid, userName)
	if !errors.Match(expectedErr, err) {
		t.Errorf("err = %s, want = %s", err, expectedErr)
	}
}

func TestLookup(t *testing.T) {
	const (
		myName    = "user@example.com"
		otherUser = "other@domain.org"
	)

	user := &upspin.User{
		Name: otherUser,
		Dirs: []upspin.Endpoint{
			{
				Transport: upspin.Remote,
				NetAddr:   upspin.NetAddr("there.co.uk"),
			},
		},
		Stores: []upspin.Endpoint{
			{
				Transport: upspin.Remote,
				NetAddr:   upspin.NetAddr("down-under.au"),
			},
		},
		PublicKey: upspin.PublicKey("my key"),
	}
	buf := marshalUser(t, user, !isAdmin)

	// Create a server authenticated with myName and with a pre-existing User entry for myName.
	u, _ := newKeyServerWithMocking(myName, otherUser, buf)

	retUser, err := u.Lookup(otherUser)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*retUser, *user) {
		t.Errorf("returned = %v, want = %v", retUser, user)
	}
}

func TestNotAdminPutOther(t *testing.T) {
	const (
		myName    = "cool@dude.com"
		otherUser = "uncool@buddy.com"
	)

	// Pre-existing user: myName, who *is not* an admin.
	user := &upspin.User{
		Name: myName,
	}
	buf := marshalUser(t, user, !isAdmin)

	// Create a server authenticated with myName and with a pre-existing User entry for myName.
	u, mockGCP := newKeyServerWithMocking(myName, myName, buf)

	// myName now attempts to write somebody else's information.
	otherU := &upspin.User{
		Name:      otherUser,
		PublicKey: upspin.PublicKey("going to change your key, haha"),
	}
	err := u.Put(otherU)
	expectedErr := errors.E(errors.Permission, upspin.UserName(myName), errors.Str("not an administrator"))
	if !errors.Match(expectedErr, err) {
		t.Errorf("err = %s, want = %s", err, expectedErr)
	}
	// Check that indeed we did not write to GCP.
	if len(mockGCP.PutRef) != 0 {
		t.Errorf("Expected no writes, got %d", len(mockGCP.PutRef))
	}
}

func TestIsAdminPutOther(t *testing.T) {
	const (
		myName    = "cool@dude.com"
		otherUser = "uncool@buddy.com"
	)

	// Pre-existing user: myName, who *is* an admin.
	user := &upspin.User{
		Name: myName,
	}
	buf := marshalUser(t, user, isAdmin)

	// Create a server authenticated with myName and with a pre-existing User entry for myName.
	u, mockGCP := newKeyServerWithMocking(myName, myName, buf)

	// myName now attempts to write somebody else's information.
	otherU := &upspin.User{
		Name:      otherUser,
		PublicKey: upspin.PublicKey("going to change your key, because I can"),
	}
	err := u.Put(otherU)
	if err != nil {
		t.Fatal(err)
	}
	// Check new user was written to GCP
	if len(mockGCP.PutRef) != 1 {
		t.Fatalf("Expected one write, got %d", len(mockGCP.PutRef))
	}
	if mockGCP.PutRef[0] != otherUser {
		t.Errorf("put = %s, want = %s", mockGCP.PutRef[0], otherUser)
	}
	savedUser, isAdmin := unmarshalUser(t, mockGCP.PutContents[0])
	if !reflect.DeepEqual(*savedUser, *otherU) {
		t.Errorf("saved = %v, want = %v", savedUser, otherU)
	}
	if isAdmin {
		t.Error("Expected user not to be an admin")
	}
}

func TestPutSelf(t *testing.T) {
	const myName = "cool@dude.com"

	// New server for myName.
	u, mockGCP := newKeyServerWithMocking(myName, "", nil)

	user := &upspin.User{
		Name: myName,
		Dirs: []upspin.Endpoint{
			{
				Transport: upspin.Remote,
				NetAddr:   upspin.NetAddr("there.co.uk"),
			},
		},
		Stores: []upspin.Endpoint{
			{
				Transport: upspin.Remote,
				NetAddr:   upspin.NetAddr("down-under.au"),
			},
		},
		PublicKey: upspin.PublicKey("my key"),
	}
	err := u.Put(user)
	if err != nil {
		t.Fatal(err)
	}

	// Verify that GCP received the Put.
	if len(mockGCP.PutRef) != 1 || len(mockGCP.PutContents) != 1 {
		t.Fatalf("num calls = %d, want = 1", len(mockGCP.PutRef))
	}
	if mockGCP.PutRef[0] != myName {
		t.Errorf("put = %s, want = %s", mockGCP.PutRef[0], myName)
	}
	savedUser, isAdmin := unmarshalUser(t, mockGCP.PutContents[0])
	if !reflect.DeepEqual(*savedUser, *user) {
		t.Errorf("saved = %v, want = %v", savedUser, user)
	}
	if isAdmin {
		t.Error("Expected user not to be an admin")
	}
}

func TestIsAdminPutExistingSelf(t *testing.T) {
	const myName = "cool@dude.com"

	user := &upspin.User{
		Name: myName,
		Stores: []upspin.Endpoint{
			{
				Transport: upspin.Remote,
				NetAddr:   upspin.NetAddr("some.place:443"),
			},
		},
		PublicKey: upspin.PublicKey("super secure"),
	}
	buf := marshalUser(t, user, isAdmin)

	// New server for myName.
	u, mockGCP := newKeyServerWithMocking(myName, myName, buf)

	// Changing my user info to include a root dir.
	user.Dirs = append(user.Dirs, upspin.Endpoint{
		Transport: upspin.Remote,
		NetAddr:   upspin.NetAddr("my-root-dir:443"),
	})
	// Change my information.
	err := u.Put(user)
	if err != nil {
		t.Fatal(err)
	}

	// Verify that GCP received the Put.
	if len(mockGCP.PutRef) != 1 || len(mockGCP.PutContents) != 1 {
		t.Fatalf("num calls = %d, want = 1", len(mockGCP.PutRef))
	}
	if mockGCP.PutRef[0] != myName {
		t.Errorf("put = %s, want = %s", mockGCP.PutRef[0], myName)
	}
	savedUser, isAdmin := unmarshalUser(t, mockGCP.PutContents[0])
	if !reflect.DeepEqual(*savedUser, *user) {
		t.Errorf("saved = %v, want = %v", savedUser, user)
	}
	if !isAdmin {
		t.Error("Expected user to be an admin")
	}
}

func TestNotAdminPutSuffixedSelf(t *testing.T) {
	const (
		myName    = "cool@dude.com"
		otherUser = "cool+suffix@dude.com"
	)

	// Pre-existing user: myName, who is not an admin.
	user := &upspin.User{
		Name: myName,
	}
	buf := marshalUser(t, user, !isAdmin)

	// Create a server authenticated with myName and with a pre-existing User entry for myName.
	u, mockGCP := newKeyServerWithMocking(myName, myName, buf)

	// myName now attempts to write somebody else's information.
	otherU := &upspin.User{
		Name:      otherUser,
		PublicKey: upspin.PublicKey("new key"),
	}
	err := u.Put(otherU)
	if err != nil {
		t.Fatal(err)
	}
	// Check new user was written to GCP
	if len(mockGCP.PutRef) != 1 {
		t.Fatalf("num calls = %d, want = 1", len(mockGCP.PutRef))
	}
	if mockGCP.PutRef[0] != otherUser {
		t.Errorf("put = %s, want = %s", mockGCP.PutRef[0], otherUser)
	}
	savedUser, isAdmin := unmarshalUser(t, mockGCP.PutContents[0])
	if !reflect.DeepEqual(*savedUser, *otherU) {
		t.Errorf("saved = %v, want = %v", savedUser, user)
	}
	if isAdmin {
		t.Error("Expected user not to be an admin")
	}
}

func TestPutWildcardUser(t *testing.T) {
	const myName = "*@mydomain.com"
	user := &upspin.User{
		Name: myName,
	}
	buf := marshalUser(t, user, isAdmin)

	// New server for myName.
	u, _ := newKeyServerWithMocking(myName, myName, buf)

	// Change my information.
	err := u.Put(user)
	expectedErr := errors.E(errors.Invalid, upspin.UserName(myName))
	if !errors.Match(expectedErr, err) {
		t.Fatalf("err = %s, want = %s", err, expectedErr)
	}
}

// marshalUser marshals the user struct and whether the user is an admin into JSON bytes.
func marshalUser(t *testing.T, user *upspin.User, isAdmin bool) []byte {
	ue := userEntry{
		User:    *user,
		IsAdmin: isAdmin,
	}
	buf, err := json.Marshal(ue)
	if err != nil {
		t.Fatal(err)
	}
	return buf
}

// unmarshalUser unmarshals JSON bytes into the user struct, along with whether the user is an admin.
func unmarshalUser(t *testing.T, buf []byte) (*upspin.User, bool) {
	var ue userEntry
	err := json.Unmarshal(buf, &ue)
	if err != nil {
		t.Fatalf("Wrote invalid bytes: %q: %v", buf, err)
	}
	return &ue.User, ue.IsAdmin
}

// newDummyKeyServer creates a new keyserver.
func newDummyKeyServer() *server {
	return &server{storage: &storagetest.DummyStorage{}}
}

// newKeyServerWithMocking sets up a mock GCP client for a user and expects a
// single lookup of user mockKey and it will reply with the preset
// data. It returns the user server, the mock GCP client for further
// verification.
func newKeyServerWithMocking(user upspin.UserName, ref string, data []byte) (*server, *storagetest.ExpectDownloadCapturePut) {
	mockGCP := &storagetest.ExpectDownloadCapturePut{
		Ref:         []string{ref},
		Data:        [][]byte{data},
		PutContents: make([][]byte, 0, 1),
		PutRef:      make([]string, 0, 1),
	}
	s := &server{
		storage: mockGCP,
		user:    user,
	}
	return s, mockGCP
}
