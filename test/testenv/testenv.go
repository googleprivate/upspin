// Package testenv provides a declarative environment for creating a complete Upspin test tree.
// See testenv_test.go for an example on how to use it.
package testenv

import (
	"errors"
	"log"
	"strings"

	"upspin.googlesource.com/upspin.git/bind"
	"upspin.googlesource.com/upspin.git/client"
	"upspin.googlesource.com/upspin.git/pack/ee"
	"upspin.googlesource.com/upspin.git/path"
	"upspin.googlesource.com/upspin.git/upspin"
	"upspin.googlesource.com/upspin.git/user/testuser"
)

// Entry is an entry in the Upspin namespace.
type Entry struct {
	// P is a path name in string format. Directories must end in "/".
	P string

	// C is the contents of P. C must be empty for directories.
	C string
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

	// Keys holds all keys for the owner. Leave empty to be assigned randomly-created new keys.
	Keys upspin.KeyPair

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
	Context *upspin.Context

	// Setup contains the original setup options.
	Setup *Setup

	exitCalled bool
}

var (
	zeroKey upspin.KeyPair
)

// New creates a new Env for testing.
func New(setup *Setup) (*Env, error) {
	context, client, err := innerNewUser(setup.OwnerName, &setup.Keys, setup.Packing, setup.Transport)
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
				return nil, errors.New("directory entry must not have contents")
			}
			dir := path.Join(upspin.PathName(setup.OwnerName), e.P)
			loc, err := client.MakeDirectory(dir)
			if err != nil {
				if !setup.IgnoreExistingDirectories {
					log.Printf("Tree: Error creating directory %s: %s", dir, err)
					return nil, err
				}
			}
			if setup.Verbose {
				log.Printf("Tree: Created dir %s at %v", dir, loc)
			}
		} else {
			name := path.Join(upspin.PathName(setup.OwnerName), e.P)
			loc, err := client.Put(name, []byte(e.C))
			if err != nil {
				log.Printf("Error creating file %s: %s", name, err)
				return nil, err
			}
			if setup.Verbose {
				log.Printf("Tree: Created file %s at %v", name, loc)
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
		return errors.New("exit already called")
	}
	e.exitCalled = true
	if e.Setup.Cleanup != nil {
		return e.Setup.Cleanup(e)
	}
	return nil
}

func innerNewUser(userName upspin.UserName, keyPair *upspin.KeyPair, packing upspin.Packing, transport upspin.Transport) (*upspin.Context, upspin.Client, error) {
	var context *upspin.Context
	var err error
	if keyPair == nil || *keyPair == zeroKey {
		context, err = newContextForUser(userName, packing)
	} else {
		context, err = newContextForUserWithKey(userName, keyPair, packing)
	}
	if err != nil {
		return nil, nil, err
	}
	var client upspin.Client
	switch transport {
	case upspin.GCP:
		client, err = gcpClient(context)
	case upspin.InProcess:
		client, err = inProcessClient(context)
	default:
		return nil, nil, errors.New("invalid transport")
	}
	if err != nil {
		return nil, nil, err
	}
	err = installUserRoot(context)
	if err != nil {
		return nil, nil, err
	}
	return context, client, err
}

// NewUser creates a new client for a user, generating new keys of the right packing type if the provided
// keys are nil or empty. The new user will not have a root created. Callers should use the client to
// MakeDirectory if necessary.
func (e *Env) NewUser(userName upspin.UserName, keyPair *upspin.KeyPair) (upspin.Client, error) {
	_, client, err := innerNewUser(userName, keyPair, e.Setup.Packing, e.Setup.Transport)
	return client, err
}

// gcpClient returns a Client pointing to the GCP test instances on upspin.io given a Context partially initialized
// with a user and keys.
func gcpClient(context *upspin.Context) (upspin.Client, error) {
	// Use a test GCP Store...
	endpointStore := upspin.Endpoint{
		Transport: upspin.GCP,
		NetAddr:   "https://upspin.io:9980", // Test store server.
	}
	// ... and a test GCP directory ...
	endpointDir := upspin.Endpoint{
		Transport: upspin.GCP,
		NetAddr:   "https://upspin.io:9981", // Test dir server.
	}
	// and an in-process test user.
	endpointUser := upspin.Endpoint{
		Transport: upspin.InProcess,
		NetAddr:   "", // ignored.
	}
	err := bindEndpoints(context, endpointStore, endpointDir, endpointUser)
	if err != nil {
		return nil, err
	}
	client := client.New(context)
	return client, nil
}

// inProcessClient returns a Client pointing to in-process instances given a Context partially initialized
// with a user and keys.
func inProcessClient(context *upspin.Context) (upspin.Client, error) {
	// Use an in-process Store...
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
	endpointUser := upspin.Endpoint{
		Transport: upspin.InProcess,
		NetAddr:   "",
	}
	err := bindEndpoints(context, endpointStore, endpointDir, endpointUser)
	if err != nil {
		return nil, err
	}
	client := client.New(context)
	return client, nil
}

// newContextForUser adds a new user to the testuser service, creates a new key for the user based on
// the chosen packing type and returns a partially filled Context.
func newContextForUser(userName upspin.UserName, packing upspin.Packing) (*upspin.Context, error) {
	entropy := make([]byte, 32) // Enough for p521
	ee.GenEntropy(entropy)
	var keyPair *upspin.KeyPair
	var err error
	switch packing {
	case upspin.EEp256Pack, upspin.EEp384Pack, upspin.EEp521Pack:
		keyPair, err = ee.CreateKeys(packing, entropy)
	default:
		// For non-EE packing, a p256 key will do.
		keyPair, err = ee.CreateKeys(upspin.EEp256Pack, entropy)
	}
	if err != nil {
		return nil, err
	}
	return newContextForUserWithKey(userName, keyPair, packing)
}

// newContextForUserWithKey adds a new user to the testuser service and returns a Context partially filled with user,
// key and packing type as given.
func newContextForUserWithKey(userName upspin.UserName, keyPair *upspin.KeyPair, packing upspin.Packing) (*upspin.Context, error) {
	context := &upspin.Context{
		UserName: userName,
		Packing:  packing,
		KeyPair:  *keyPair,
	}

	endpointInProcess := upspin.Endpoint{
		Transport: upspin.InProcess,
		NetAddr:   "",
	}
	user, err := bind.User(context, endpointInProcess)
	if err != nil {
		return nil, err
	}
	testUser, ok := user.(*testuser.Service)
	if !ok {
		return nil, errors.New("user service must be the in-process instance")
	}
	// Set the public key for the registered user.
	testUser.SetPublicKeys(userName, []upspin.PublicKey{keyPair.Public})
	return context, nil
}

// installUserRoot installs a root dir for the user in the context, but does not create the root dir.
func installUserRoot(context *upspin.Context) error {
	testUser, ok := context.User.(*testuser.Service)
	if !ok {
		return errors.New("installUserRoot: user service must be the in-process instance")
	}
	testUser.AddRoot(context.UserName, context.Directory.Endpoint())
	return nil
}

func makeRoot(context *upspin.Context) error {
	// Make the root to be sure it's there.
	_, err := context.Directory.MakeDirectory(upspin.PathName(context.UserName + "/"))
	if err != nil && !strings.Contains(err.Error(), "already ") {
		return err
	}
	return nil
}

func bindEndpoints(context *upspin.Context, store, dir, user upspin.Endpoint) error {
	var err error
	context.Store, err = bind.Store(context, store)
	if err != nil {
		return err
	}
	context.Directory, err = bind.Directory(context, dir)
	if err != nil {
		return err
	}
	context.User, err = bind.User(context, user)
	if err != nil {
		return err
	}
	return nil
}
