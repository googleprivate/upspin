// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package perm

import (
	"fmt"
	"testing"
	"time"

	"upspin.io/test/testenv"
	"upspin.io/upspin"
)

const (
	owner  = "aly@example.com" // aly has keys in key/testdata/aly
	writer = "bob@uncle.com"   // bob has keys in key/testdata/bob

	accessFile    = owner + "/Access"
	accessContent = "r,l: " + testenv.TestServerName + "\n*: " + owner

	groupDir     = owner + "/Group"
	writersGroup = groupDir + "/" + WritersGroupFile
)

// setupEnv sets up a test environment, used by the tests in this package.
func setupEnv(t *testing.T) *testenv.Env {
	var err error
	env, err := testenv.New(&testenv.Setup{
		OwnerName: owner,
		Packing:   upspin.PlainPack,
		Kind:      "server", // Must implement Watch API.
	})
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func newWithEnv(t *testing.T, env *testenv.Env) (perm *Perm, wait func()) {
	wait, onUpdate, onRetry := newStubs(t)
	cfg := env.Config
	dir := env.DirServer
	perm = newPerm("newWithEnv", cfg, readyNow, cfg.UserName(), dir.Lookup, dir.Watch, onUpdate, onRetry)
	return
}

func newWithConfig(t *testing.T, cfg upspin.Config) (perm *Perm, wait func()) {
	wait, onUpdate, onRetry := newStubs(t)
	perm = newPerm("newWithConfig", cfg, readyNow, cfg.UserName(), nil, nil, onUpdate, onRetry)
	return
}

// The wait func, when called, blocks until onUpdate fires or a timeout occurs.
func newStubs(t *testing.T) (wait, onUpdate, onRetry func()) {
	update := make(chan bool)
	n := 0
	wait = func() {
		n++
		select {
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for update %d", n)
		case update <- true:
			// OK.
		}
	}
	onUpdate = func() { <-update }
	onRetry = func() { time.Sleep(100 * time.Millisecond) }
	return
}

// readyNow is closed at init time and should be passed no New, WrapStore, or
// WrapDir to indicate that it should poll immediately.
var readyNow chan struct{}

func init() {
	readyNow = make(chan struct{})
	close(readyNow)
}

func TestCantFindFileAllowsAll(t *testing.T) {
	env := setupEnv(t)
	defer env.Exit()

	perm, wait := newWithEnv(t, env)
	wait()

	// Everyone is allowed, since we can't read the owner file.
	for _, user := range []upspin.UserName{
		owner,
		writer,
		"foo@bar.com",
		"nobody@nobody.org",
	} {
		if !perm.IsWriter(user) {
			t.Errorf("IsWriter(%q)=false, want true", user)
		}
	}
}

func TestNoFileAllowsAll(t *testing.T) {
	env := setupEnv(t)
	defer env.Exit()

	// Put a permissive Access file, now server knows the file is not there.
	r := testenv.NewRunner()
	r.AddUser(env.Config)
	r.As(owner)
	r.Put(accessFile, accessContent) // So server can lookup the file.
	if r.Failed() {
		t.Fatal(r.Diag())
	}

	perm, wait := newWithEnv(t, env)
	wait()

	// Everyone is allowed.
	for _, user := range []upspin.UserName{
		owner,
		writer,
		"foo@bar.com",
		"nobody@nobody.org",
	} {
		if !perm.IsWriter(user) {
			t.Errorf("user %q is not allowed; expected allowed", user)
		}
	}
}

func TestAllowsOnlyOwner(t *testing.T) {
	env := setupEnv(t)
	defer env.Exit()

	r := testenv.NewRunner()
	r.AddUser(env.Config)

	r.As(owner)
	r.Put(accessFile, accessContent) // So server can lookup the file.
	r.MakeDirectory(groupDir)
	r.Put(writersGroup, owner) // Only owner can write.
	if r.Failed() {
		t.Fatal(r.Diag())
	}

	perm, wait := newWithEnv(t, env)
	wait()

	// Owner is allowed.
	if !perm.IsWriter(owner) {
		t.Errorf("Owner is not allowed, expected allowed")
	}

	// No one else is allowed.
	for _, user := range []upspin.UserName{
		writer,
		"foo@bar.com",
		"nobody@nobody.org",
	} {
		if perm.IsWriter(user) {
			t.Errorf("user %q is allowed; expected not allowed", user)
		}
	}
}

func TestAllowsOthersAndWildcard(t *testing.T) {
	env := setupEnv(t)
	defer env.Exit()

	r := testenv.NewRunner()
	r.AddUser(env.Config)

	r.As(owner)
	r.Put(accessFile, accessContent) // So server can lookup the file.
	r.MakeDirectory(groupDir)
	r.Put(writersGroup, owner+" "+writer+" *@superusers.com")
	if r.Failed() {
		t.Fatal(r.Diag())
	}

	perm, wait := newWithEnv(t, env)
	wait() // Update call
	wait() // Watch event

	// Owner, writer and a wildcard user are allowed.
	for _, user := range []upspin.UserName{
		owner,
		writer,
		"master@superusers.com",
	} {
		if !perm.IsWriter(user) {
			t.Errorf("%s is not allowed, expected allowed", user)
		}
	}

	// No one else is allowed.
	for _, user := range []upspin.UserName{
		"foo@bar.com",
		"nobody@nobody.org",
	} {
		if perm.IsWriter(user) {
			t.Errorf("user %q is allowed; expected not allowed", user)
		}
	}

	// Remove everyone but owner.
	// Update should happen quickly through the Watch API.
	r.Put(writersGroup, owner)
	if r.Failed() {
		t.Fatal(r.Diag())
	}
	wait()

	for _, user := range []upspin.UserName{
		writer,
		"master@superusers.com",
		"foo@bar.com",
		"nobody@nobody.org",
	} {
		if perm.IsWriter(user) {
			t.Errorf("%s is allowed; expected not allowed", user)
		}
	}
}

// Regression test for issue #317.
func TestSequentialErrorsOK(t *testing.T) {
	env := setupEnv(t)
	defer env.Exit()

	wait, onUpdate, onRetry := newStubs(t)
	cfg := env.Config
	newPerm("TestSequentialErrorsOK", cfg, readyNow, owner, env.DirServer.Lookup, errorReturningWatch, onUpdate, onRetry)
	wait()

	// No crash, no problem.
}

// Issue #125
func TestOrderOfPuts(t *testing.T) {
	env := setupEnv(t)
	defer env.Exit()

	r := testenv.NewRunner()
	r.AddUser(env.Config)

	r.As(owner)
	r.MakeDirectory(groupDir)
	r.Put(writersGroup, owner+" "+writer)
	if r.Failed() {
		t.Fatal(r.Diag())
	}

	perm, wait := newWithEnv(t, env)
	wait() // Update call.

	r.Put(accessFile, accessContent) // So server can lookup Writers.
	if r.Failed() {
		t.Fatal(r.Diag())
	}
	wait() // New watch event.

	// Owner and writer are allowed.
	for _, user := range []upspin.UserName{
		owner,
		writer,
	} {
		if !perm.IsWriter(user) {
			t.Errorf("%s is not allowed, expected allowed", user)
		}
	}
}

func errorReturningWatch(_ upspin.PathName, _ int64, done <-chan struct{}) (<-chan upspin.Event, error) {
	c := make(chan upspin.Event)
	go func() {
		var i int
		for {
			err := upspin.Event{Error: fmt.Errorf("error %d", i)}
			select {
			case c <- err:
				i++
			case <-done:
				return
			}

		}
	}()
	return c, nil
}
