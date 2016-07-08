// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inprocess

import (
	"testing"

	"upspin.io/bind"
	"upspin.io/context"
	"upspin.io/upspin"

	_ "upspin.io/dir/inprocess"
	_ "upspin.io/pack/debug"
	_ "upspin.io/store/inprocess"
)

var (
	userName = upspin.UserName("joe@blow.com")
)

func setup(t *testing.T) (upspin.KeyServer, upspin.Context) {
	c := context.New().SetUserName(userName).SetPacking(upspin.DebugPack)
	e := upspin.Endpoint{
		Transport: upspin.InProcess,
		NetAddr:   "", // ignored
	}
	u, err := bind.KeyServer(c, e)
	if err != nil {
		t.Fatal(err)
	}
	c.SetKeyEndpoint(e)
	c.SetStoreEndpoint(e)
	c.SetDirEndpoint(e)
	return u, c
}

func TestInstallAndLookup(t *testing.T) {
	u, ctxt := setup(t)
	testKey, ok := u.(*Service)
	if !ok {
		t.Fatal("Not an inprocess KeyServer")
	}

	dir, err := bind.DirServer(ctxt, ctxt.DirEndpoint())
	if err != nil {
		t.Fatal(err)
	}
	err = testKey.Install(userName, dir)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	eRecv, keys, err := u.Lookup(userName)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("Expected no keys for user %v, got %d", userName, len(keys))
	}
	if len(eRecv) != 1 {
		t.Fatalf("Expected 1 endpoint, got %d", len(eRecv))
	}
	if eRecv[0].Transport != upspin.InProcess {
		t.Errorf("Expected endpoint to be %d, but instead it was %d", upspin.InProcess, eRecv[0].Transport)
	}
}

func TestPublicKeysAndUsers(t *testing.T) {
	u, _ := setup(t)
	testKey, ok := u.(*Service)
	if !ok {
		t.Fatal("Not an inprocess KeyServer")
	}
	const testKeyStr = "pub key1"
	testKey.SetPublicKeys(userName, []upspin.PublicKey{
		upspin.PublicKey(testKeyStr),
	})

	_, keys, err := u.Lookup(userName)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("Expected 1 key for user %v, got %d", userName, len(keys))
	}
	if string(keys[0]) != testKeyStr {
		t.Errorf("Expected key %s, got %s", testKeyStr, keys[0])
	}

	users := testKey.ListUsers()
	if len(users) != 1 {
		t.Fatalf("Expected 1 user, got %d", len(users))
	}
	if users[0] != userName {
		t.Errorf("Expected user %s, got %v", userName, users[0])
	}

	// Delete keys for user
	testKey.SetPublicKeys(userName, nil)

	users = testKey.ListUsers()
	if len(users) != 0 {
		t.Fatalf("Expected 0 users, got %d", len(users))
	}
}

func TestSafety(t *testing.T) {
	// Make sure the answers from Lookup are not aliases for the Service maps.
	u, _ := setup(t)
	testKey, ok := u.(*Service)
	if !ok {
		t.Fatal("Not an inprocess KeyServer")
	}
	const testKeyStr = "pub key2"
	testKey.SetPublicKeys(userName, []upspin.PublicKey{
		upspin.PublicKey(testKeyStr),
	})

	locs, keys, err := u.Lookup(userName)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(locs) != 1 || len(keys) != 1 {
		t.Fatal("Extra locs or keys")
	}

	// Save and then modify the two.
	loc0 := locs[0]
	locs[0].Transport++
	key0 := keys[0]
	keys[0] += "gotcha"

	// Fetch again, expect the original results.
	locs1, keys1, err := u.Lookup(userName)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(locs1) != 1 || len(keys1) != 1 {
		t.Fatal("Extra locs or keys (1)")
	}
	if locs1[0] != loc0 {
		t.Error("loc was modified")
	}
	if keys1[0] != key0 {
		t.Error("key was modified")
	}
}
