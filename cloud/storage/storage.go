// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package storage implements a low-level interface for storing blobs in
// stable storage such as a database.
package storage

import (
	"strings"
	"time"

	"upspin.io/errors"
	"upspin.io/upspin"
)

// Storage is a low-level storage interface for services to store their data permanently.
type Storage interface {
	// PutLocalFile copies a local file to storage using ref as its
	// name. It may return a direct link for downloading the file
	// from the storage backend, or empty if the backend does not offer
	// direct links into it.
	PutLocalFile(srcLocalFilename string, ref string) (refLink string, error error)

	// Get returns a link for downloading ref from the storage backend,
	// if the ref is publicly readable and the backend offers direct links.
	Get(ref string) (link string, error error)

	// Download retrieves the bytes associated with a ref.
	Download(ref string) ([]byte, error)

	// Put stores the contents given as ref on the storage backend.
	// It may return a direct link for retrieving data directly from
	// the backend, if it provides direct links.
	Put(ref string, contents []byte) (refLink string, error error)

	// ListPrefix lists all files that match a given prefix, up to a
	// certain depth, counting from the prefix, not absolute
	ListPrefix(prefix string, depth int) ([]string, error)

	// ListDir lists the contents of a given directory. It's equivalent to
	// ListPrefix(dir, 1) but much more efficient.
	ListDir(dir string) ([]string, error)

	// Delete permanently removes all storage space associated
	// with a ref.
	Delete(ref string) error

	// Dial dials the storage backend with servers and options as given by opts.
	// It is called only once, but not directly, use storage.Dial instead.
	Dial(opts *StorageOpts) error

	// Close closes the the connection to the storage backend and releases all resources used.
	// It must be called only once and only after storage.Dial has succeeded.
	Close()
}

var registration = make(map[string]Storage)

// StorageOpts holds configuration options for the storage backend.
// It is meant to be used by implementations of Storage.
type StorageOpts struct {
	Opts    map[string]string // key-value pair
	Timeout time.Duration
	Addrs   []upspin.NetAddr
}

// DialOpts is a daisy-chaining mechanism for setting options to a backend during Dial.
type DialOpts func(*StorageOpts) error

// Register registers a new Storage under a name. It is typically used in init functions.
func Register(name string, storage Storage) error {
	if _, exists := registration[name]; exists {
		return errors.E("Register", errors.Exist)
	}
	registration[name] = storage
	return nil
}

// WithNetAddr sets a network host:port pair as the network address to dial.
// Multiple calls can be made to register a pool of servers.
func WithNetAddr(netAddr upspin.NetAddr) DialOpts {
	return func(o *StorageOpts) error {
		o.Addrs = append(o.Addrs, netAddr)
		return nil
	}
}

// WithTimeout sets a maximum duration for dialing.
func WithTimeout(timeout time.Duration) DialOpts {
	return func(o *StorageOpts) error {
		o.Timeout = timeout
		return nil
	}
}

// WithOptions parses a string in the format "key1=value1,key2=value2,..." where keys and values
// are specific to each storage backend. Neither key nor value may contain the characters "," or "=".
// Use WithKeyValue repeatedly if these characters need to be used.
func WithOptions(options string) DialOpts {
	return func(o *StorageOpts) error {
		pairs := strings.Split(options, ",")
		for _, p := range pairs {
			kv := strings.Split(p, "=")
			if len(kv) != 2 {
				return errors.E("WithOptions", errors.Syntax, errors.Errorf("error parsing option %s", kv))
			} else {
				o.Opts[kv[0]] = kv[1]
			}
		}
		return nil
	}
}

// WithKeyValue sets a key-value pair as option. If called multiple times with the same key, the last one wins.
func WithKeyValue(key, value string) DialOpts {
	return func(o *StorageOpts) error {
		o.Opts[key] = value
		return nil
	}
}

// Dial dials the named storage backend using the dial options opts.
func Dial(name string, opts ...DialOpts) (Storage, error) {
	s, found := registration[name]
	if !found {
		return nil, errors.E("Dial", errors.NotExist, errors.Str("storage backend type not registered"))
	}
	dOpts := &StorageOpts{
		Opts: make(map[string]string),
	}
	var err error
	for _, o := range opts {
		if o != nil {
			err = o(dOpts)
			if err != nil {
				return nil, err
			}
		}
	}
	return s, s.Dial(dOpts)
}
