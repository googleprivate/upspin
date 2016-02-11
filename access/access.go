// Package access contains the global AccessSwitch and its methods.
package access

import (
	"fmt"
	"sync"

	"upspin.googlesource.com/upspin.git/upspin"
)

// Switch is the global accessor for services.
var (
	mu           sync.Mutex
	userMap      = make(map[upspin.Transport]upspin.User)
	directoryMap = make(map[upspin.Transport]upspin.Directory)
	storeMap     = make(map[upspin.Transport]upspin.Store)
)

// RegisterUser registers a User interface for the transport.
func RegisterUser(transport upspin.Transport, user upspin.User) error {
	mu.Lock()
	defer mu.Unlock()
	_, ok := userMap[transport]
	if ok {
		return fmt.Errorf("cannot override User interface: %v", transport)
	}
	userMap[transport] = user
	return nil
}

// RegisterDirectory registers a Directory interface for the transport.
func RegisterDirectory(transport upspin.Transport, dir upspin.Directory) error {
	mu.Lock()
	defer mu.Unlock()
	_, ok := directoryMap[transport]
	if ok {
		return fmt.Errorf("cannot override Directory interface: %v", transport)
	}
	directoryMap[transport] = dir
	return nil
}

// RegisterStore registers a Store interface for the transport.
func RegisterStore(transport upspin.Transport, store upspin.Store) error {
	mu.Lock()
	defer mu.Unlock()
	_, ok := storeMap[transport]
	if ok {
		return fmt.Errorf("cannot override Store interface: %v", transport)
	}
	storeMap[transport] = store
	return nil
}

// BindUser returns a User interface bound to the endpoint.
func BindUser(cc upspin.ClientContext, e upspin.Endpoint) (upspin.User, error) {
	mu.Lock()
	u, ok := userMap[e.Transport]
	mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("User service with transport %q not registered", e.Transport)
	}
	x, err := u.Dial(cc, e)
	if err != nil {
		return nil, err
	}
	return x.(upspin.User), nil
}

// BindStore returns a Store interface bound to the endpoint.
func BindStore(cc upspin.ClientContext, e upspin.Endpoint) (upspin.Store, error) {
	mu.Lock()
	s, ok := storeMap[e.Transport]
	mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("Store service with transport %q not registered", e.Transport)
	}
	x, err := s.Dial(cc, e)
	if err != nil {
		return nil, err
	}
	return x.(upspin.Store), nil
}

// BindDirectory returns a Directory interface bound to the endpoint.
func BindDirectory(cc upspin.ClientContext, e upspin.Endpoint) (upspin.Directory, error) {
	mu.Lock()
	d, ok := directoryMap[e.Transport]
	mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("Directory service with transport %q not registered", e.Transport)
	}
	x, err := d.Dial(cc, e)
	if err != nil {
		return nil, err
	}
	return x.(upspin.Directory), nil
}
