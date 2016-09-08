// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inprocess

import (
	"reflect"
	"testing"

	"upspin.io/context"
	"upspin.io/upspin"

	_ "upspin.io/dir/inprocess"
	_ "upspin.io/store/inprocess"
)

var (
	inProcessEndpoint = upspin.Endpoint{
		Transport: upspin.InProcess,
	}
	user = upspin.User{
		Name:      "joe@blow.com",
		Dirs:      []upspin.Endpoint{inProcessEndpoint},
		Stores:    []upspin.Endpoint{inProcessEndpoint},
		PublicKey: "this is a key",
	}
)

func setup(t *testing.T) upspin.KeyServer {
	c := context.New()
	c = context.SetUserName(c, user.Name)
	c = context.SetKeyEndpoint(c, inProcessEndpoint)
	c = context.SetStoreEndpoint(c, inProcessEndpoint)
	c = context.SetDirEndpoint(c, inProcessEndpoint)
	return New()
}

func TestInstallAndLookup(t *testing.T) {
	key := setup(t)
	if _, ok := key.(*server); !ok {
		t.Fatal("Not an inprocess KeyServer")
	}

	err := key.Put(&user)
	if err != nil {
		t.Fatal(err)
	}
	got, err := key.Lookup(user.Name)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, &user) {
		t.Errorf("Lookup: incorrect data returned: got %v; want %v", got, &user)
	}
}
