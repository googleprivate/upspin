// Package testuser implements a non-persistent, memory-resident user service.
package testuser

import (
	"errors"
	"fmt"
	"sync"

	"upspin.googlesource.com/upspin.git/bind"
	"upspin.googlesource.com/upspin.git/path"
	"upspin.googlesource.com/upspin.git/upspin"
)

// Service maps user names to potential machines holding root of the user's tree.
// It implements the upspin.User interface.
type Service struct {
	endpoint upspin.Endpoint

	// mu protects the fields below.
	mu       sync.RWMutex
	root     map[upspin.UserName][]upspin.Endpoint
	keystore map[upspin.UserName][]upspin.PublicKey
}

var _ upspin.User = (*Service)(nil)

// Lookup reports the set of locations the user's directory might be,
// with the earlier entries being the best choice; later entries are
// fallbacks and the user's public keys, if known.
func (s *Service) Lookup(name upspin.UserName) ([]upspin.Endpoint, []upspin.PublicKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Return copies so the caller can't modify our data structures.
	locs := make([]upspin.Endpoint, len(s.root[name]))
	copy(locs, s.root[name])
	keys := make([]upspin.PublicKey, len(s.keystore[name]))
	copy(keys, s.keystore[name])
	return locs, keys, nil
}

// SetPublicKeys sets a slice of public keys to the keystore for a
// given user name. Previously-known keys for that user are
// forgotten. To add keys to the existing set, Lookup and append to
// the slice. If keys is nil, the user is forgotten.
func (s *Service) SetPublicKeys(name upspin.UserName, keys []upspin.PublicKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if keys == nil {
		delete(s.keystore, name)
	} else {
		s.keystore[name] = keys
	}
}

// ListUsers returns a slice of all known users with at least one public key.
func (s *Service) ListUsers() []upspin.UserName {
	s.mu.RLock()
	defer s.mu.RUnlock()
	users := make([]upspin.UserName, 0, len(s.keystore))
	for u := range s.keystore {
		users = append(users, u)
	}
	return users
}

// validateUserName returns a parsed path if the username is valid.
func validateUserName(name upspin.UserName) (*path.Parsed, error) {
	parsed, err := path.Parse(upspin.PathName(name))
	if err != nil {
		return nil, err
	}
	if !parsed.IsRoot() {
		return nil, fmt.Errorf("testuser: %q not a user name", name)
	}
	return &parsed, nil
}

// Install installs a user and its root in the provided Directory
// service. For a real User service, this would be done by some offline
// administrative procedure. For this test version, we just provide a
// simple hook for testing.
func (s *Service) Install(name upspin.UserName, dir upspin.Directory) error {
	// Verify that it is a valid name.
	parsed, err := validateUserName(name)
	if err != nil {
		return err
	}
	loc, err := dir.MakeDirectory(upspin.PathName(parsed.User() + "/"))
	if err != nil {
		return err
	}
	s.innerAddRoot(parsed.User(), loc.Endpoint)
	return nil
}

// innerAddRoot adds a root for the user, which must be a parsed, valid Upspin user name.
func (s *Service) innerAddRoot(userName upspin.UserName, endpoint upspin.Endpoint) {
	s.mu.Lock()
	s.root[userName] = append(s.root[userName], endpoint)
	s.mu.Unlock()
}

// AddRoot adds an endpoint as the user's root endpoint.
func (s *Service) AddRoot(name upspin.UserName, endpoint upspin.Endpoint) error {
	// Verify that it is a valid name.
	parsed, err := validateUserName(name)
	if err != nil {
		return err
	}
	s.innerAddRoot(parsed.User(), endpoint)
	return nil
}

// Methods to implement upspin.Service

// Configure implements upspin.Service.
func (s *Service) Configure(options ...string) error {
	return nil
}
func (s *Service) Endpoint() upspin.Endpoint {
	return s.endpoint
}

// ServerUserName implements upspin.Service.
func (s *Service) ServerUserName() string {
	return "testuser"
}

// Dial always returns the same instance of the service. The Transport must be InProcess
// but the NetAddr is ignored.
func (s *Service) Dial(context *upspin.Context, e upspin.Endpoint) (upspin.Service, error) {
	if e.Transport != upspin.InProcess {
		return nil, errors.New("testuser: unrecognized transport")
	}
	s.endpoint = e
	return s, nil
}

func init() {
	s := &Service{
		root:     make(map[upspin.UserName][]upspin.Endpoint),
		keystore: make(map[upspin.UserName][]upspin.PublicKey),
	}
	bind.RegisterUser(upspin.InProcess, s)
}
