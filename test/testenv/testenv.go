// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package testenv provides a declarative environment for creating a complete Upspin test tree.
// See testenv_test.go for an example on how to use it.
package testenv

import (
	"log"
	"strings"

	"upspin.io/bind"
	"upspin.io/client"
	"upspin.io/context"
	"upspin.io/errors"
	"upspin.io/factotum"
	"upspin.io/key/inprocess"
	"upspin.io/pack/ee"
	"upspin.io/path"
	"upspin.io/upspin"
)

// Entry is an entry in the Upspin namespace.
type Entry struct {
	// P is a path name in string format. Directories must end in "/".
	P string

	// C is the contents of P. C must be empty for directories.
	C string
}

// KeyPair holds the public and private string form of a user key.  Ideally
// this would not be used, since we want only factotum to have the private key.
// But it helps the test setup for now.
type KeyPair struct {
	Public  upspin.PublicKey
	Private string
}

// Setup is a configuration structure that contains a directory tree and other optional flags.
type Setup struct {
	// OwnerName is the name of the directory tree owner.
	OwnerName upspin.UserName

	// Transport is what kind of servers to use, InProcess or GCP. Mixed usage is not supported.
	// TODO support mixing.
	Transport upspin.Transport

	// Packing is the desired packing for the tree.
	Packing upspin.Packing

	// KeyKind is the desired key type for the tree, e.g. "p256".
	KeyKind string

	// Keys holds all keys for the owner. Leave empty to be assigned randomly-created new keys.
	Keys KeyPair

	// Tree is the directory tree desired at the start of the test environment.
	Tree Tree

	// Some configuration options follow

	// Verbose indicates whether we should print verbose debug messages.
	Verbose bool

	// IgnoreExistingDirectories does not report an error if the directories already exist from a previous run.
	IgnoreExistingDirectories bool

	// Cleanup, if present, is run at Exit to clean up any test state necessary.
	// It may return an error, which is returned by Exit.
	Cleanup func(e *Env) error
}

// Tree is a full directory tree with path names and their contents.
type Tree []Entry

// Env is the test environment. It contains a client which is the main piece that tests should use.
type Env struct {
	// Client is the client tests should use for reaching the newly-created Tree.
	Client upspin.Client

	// Context is the context used when creating the client.
	Context upspin.Context

	// Setup contains the original setup options.
	Setup *Setup

	exitCalled bool
}

// New creates a new Env for testing.
func New(setup *Setup) (*Env, error) {
	client, context, err := innerNewUser("New", setup.OwnerName, &setup.Keys, setup)
	if err != nil {
		return nil, err
	}
	err = makeRoot(context)
	if err != nil {
		return nil, err
	}
	env := &Env{
		Client:  client,
		Context: context,
		Setup:   setup,
	}
	// Generate the dir tree using the client.
	for _, e := range setup.Tree {
		if strings.HasSuffix(e.P, "/") {
			if len(e.C) > 0 {
				return nil, errors.E("New", errors.NotEmpty, errors.Str("directory entry must not have contents"))
			}
			dir := path.Join(upspin.PathName(setup.OwnerName), e.P)
			entry, err := client.MakeDirectory(dir)
			if err != nil {
				if !setup.IgnoreExistingDirectories {
					log.Printf("Tree: Error creating directory %s: %s", dir, err)
					return nil, err
				}
			}
			if setup.Verbose {
				log.Printf("Tree: Created directory %#v", entry)
			}
		} else {
			name := path.Join(upspin.PathName(setup.OwnerName), e.P)
			entry, err := client.Put(name, []byte(e.C))
			if err != nil {
				log.Printf("Error creating file %s: %s", name, err)
				return nil, errors.E("New", err)
			}
			if setup.Verbose {
				log.Printf("Tree: Created file %#v", entry)
			}
		}
	}
	if setup.Verbose {
		log.Printf("Tree: All entries created.")
	}
	return env, nil
}

// E (short for Entry) is a helper function to return a new Entry.
func E(pathName string, contents string) Entry {
	return Entry{
		P: pathName,
		C: contents,
	}
}

// Exit indicates the end of the test environment. It must only be called once. If Setup.Cleanup exists it is called.
func (e *Env) Exit() error {
	if e.exitCalled {
		return errors.E("Exit", errors.Invalid, errors.Str("exit already called"))
	}
	e.exitCalled = true
	if e.Setup.Cleanup != nil {
		err := e.Setup.Cleanup(e)
		if err != nil {
			return errors.E("Exit", err)
		}
	}
	return nil
}

func innerNewUser(op string, userName upspin.UserName, keyPair *KeyPair, setup *Setup) (upspin.Client, upspin.Context, error) {
	var context upspin.Context
	var err error
	if keyPair == nil || *keyPair == (KeyPair{}) {
		context, err = newContextForUser(userName, setup)
	} else {
		context, err = newContextForUserWithKey(userName, keyPair, setup)
	}
	if err != nil {
		return nil, nil, errors.E(op, err)
	}
	var client upspin.Client
	switch setup.Transport {
	case upspin.GCP:
		client, err = gcpClient(context)
	case upspin.InProcess:
		client, err = inProcessClient(context)
	default:
		return nil, nil, errors.E(op, errors.Invalid, errors.Str("invalid transport"))
	}
	if err != nil {
		return nil, nil, errors.E(op, err)
	}
	err = installUserRoot(context)
	if err != nil {
		return nil, nil, errors.E(op, err)
	}
	return client, context, nil
}

// NewUser creates a new client for a user, generating new keys of the right
// packing type if the provided keys are nil or empty. The new user will not
// have a root created. Callers should use the client to MakeDirectory if
// necessary.
func (e *Env) NewUser(userName upspin.UserName, keyPair *KeyPair) (upspin.Client, upspin.Context, error) {
	return innerNewUser("NewUser", userName, keyPair, e.Setup)
}

// gcpClient returns a Client pointing to the GCP test instances on upspin.io given a Context partially initialized
// with a user and keys.
func gcpClient(context upspin.Context) (upspin.Client, error) {
	// Use a test GCP StoreServer...
	endpointStore := upspin.Endpoint{
		Transport: upspin.Remote,
		NetAddr:   "store.test.upspin.io:443", // Test store server.
	}
	// ... and a test GCP directory ...
	endpointDir := upspin.Endpoint{
		Transport: upspin.Remote,
		NetAddr:   "dir.test.upspin.io:443", // Test dir server.
	}
	// and an in-process test user.
	endpointKey := upspin.Endpoint{
		Transport: upspin.InProcess,
		NetAddr:   "", // ignored
	}
	setContextEndpoints(context, endpointStore, endpointDir, endpointKey)
	client := client.New(context)
	return client, nil
}

// inProcessClient returns a Client pointing to in-process instances given a Context partially initialized
// with a user and keys.
func inProcessClient(context upspin.Context) (upspin.Client, error) {
	// Use an in-process StoreServer...
	endpointStore := upspin.Endpoint{
		Transport: upspin.InProcess,
		NetAddr:   "",
	}
	// ... and an in-process directory ...
	endpointDir := upspin.Endpoint{
		Transport: upspin.InProcess,
		NetAddr:   "",
	}
	// and an in-process test user.
	endpointKey := upspin.Endpoint{
		Transport: upspin.InProcess,
		NetAddr:   "",
	}
	setContextEndpoints(context, endpointStore, endpointDir, endpointKey)
	client := client.New(context)
	return client, nil
}

// newContextForUser adds a new user to the inprocess user service, creates a
// new key for the user based on the setup's packing type and returns a
// partially filled Context.
func newContextForUser(userName upspin.UserName, setup *Setup) (upspin.Context, error) {
	kind := "p256" // Default is p256.
	if k := setup.KeyKind; setup.Packing == upspin.EEPack && (k == "p386" || k == "p521") {
		kind = k
	}

	entropy := make([]byte, 32) // Enough for p521
	ee.GenEntropy(entropy)

	var keyPair KeyPair
	var err error
	keyPair.Public, keyPair.Private, err = ee.CreateKeys(kind, entropy)
	if err != nil {
		return nil, err
	}
	return newContextForUserWithKey(userName, &keyPair, setup)
}

// newContextForUserWithKey adds a new user to the inprocess user service and
// returns a Context partially filled with specified user and key.
func newContextForUserWithKey(userName upspin.UserName, keyPair *KeyPair, setup *Setup) (upspin.Context, error) {
	context := context.New().SetUserName(userName).SetPacking(setup.Packing)

	endpointInProcess := upspin.Endpoint{
		Transport: upspin.InProcess,
		NetAddr:   "",
	}
	user, err := bind.KeyServer(context, endpointInProcess)
	if err != nil {
		return nil, err
	}
	testKey, ok := user.(*inprocess.Service)
	if !ok {
		return nil, errors.Str("key service must be the in-process instance")
	}
	// Set the public key for the registered user.
	testKey.SetPublicKeys(userName, []upspin.PublicKey{keyPair.Public})
	f, err := factotum.DeprecatedNew(keyPair.Public, keyPair.Private)
	if err != nil {
		panic("DeprecatedNewFactotum failed")
	}
	context.SetFactotum(f)
	return context, nil
}

// installUserRoot installs a root dir for the user in the context, but does not create the root dir.
func installUserRoot(context upspin.Context) error {
	user, err := bind.KeyServer(context, context.KeyEndpoint())
	if err != nil {
		return err
	}
	testKey, ok := user.(*inprocess.Service)
	if !ok {
		return errors.Str("user service must be the in-process instance")
	}
	testKey.AddRoot(context.UserName(), context.DirEndpoint())
	return nil
}

func makeRoot(context upspin.Context) error {
	// Make the root to be sure it's there.
	directory, err := bind.DirServer(context, context.DirEndpoint())
	if err != nil {
		return err
	}
	_, err = directory.MakeDirectory(upspin.PathName(context.UserName() + "/"))
	if err != nil && !strings.Contains(err.Error(), "already ") {
		return err
	}
	return nil
}

func setContextEndpoints(context upspin.Context, store, dir, user upspin.Endpoint) {
	context.SetStoreEndpoint(store)
	context.SetDirEndpoint(dir)
	context.SetKeyEndpoint(user)
}
