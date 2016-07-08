// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package gcp implements upspin.StoreServer using Google Cloud Platform as its storage.
package gcp

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"strings"
	"sync"

	"upspin.io/bind"
	"upspin.io/cloud/storage"
	"upspin.io/context"
	"upspin.io/errors"
	"upspin.io/key/sha256key"
	"upspin.io/log"
	"upspin.io/store/gcp/cache"
	"upspin.io/upspin"

	// We use GCS as the backing for our data.
	_ "upspin.io/cloud/storage/gcs"
)

// Configuration options for this package.
const (
	// ConfigTemporaryDir specifies which temporary directory to write files to before they're
	// uploaded to the destination bucket. If not present, one will be created in the
	// system's default location.
	ConfigTemporaryDir = "gcpTemporaryDir"
)

var (
	errNotConfigured = errors.E(errors.Invalid, errors.Str("GCP StoreServer not configured"))
)

// Server implements upspin.StoreServer.
type server struct {
	context  upspin.Context
	endpoint upspin.Endpoint
}

var _ upspin.StoreServer = (*server)(nil)

var (
	mu          sync.RWMutex // Protects fields below.
	refCount    uint64       // How many clones of us exist.
	cloudClient storage.Storage
	fileCache   *cache.FileCache
)

// New returns a new, unconfigured StoreServer bound to the user in the context.
func New(context upspin.Context) upspin.StoreServer {
	return &server{
		context: context.Copy(), // Make a copy to prevent user making further changes.
	}
}

// Put implements upspin.StoreServer.
func (s *server) Put(data []byte) (upspin.Reference, error) {
	const Put = "Put"
	reader := bytes.NewReader(data)
	// TODO: check that userName has permission to write to this store server.
	mu.RLock()
	if !s.isConfigured() {
		return "", errors.E(Put, errNotConfigured)
	}
	sha := sha256key.NewShaReader(reader)
	initialRef := fileCache.RandomRef()
	err := fileCache.Put(initialRef, sha)
	if err != nil {
		mu.RUnlock()
		return "", errors.E(Put, err)
	}
	// Figure out the appropriate reference for this blob.
	ref := sha.EncodedSum()

	// Rename it in the cache
	fileCache.Rename(ref, initialRef)

	// Now go store it in the cloud.
	go func() {
		if _, err := cloudClient.PutLocalFile(fileCache.GetFileLocation(ref), ref); err == nil {
			// Remove the locally-cached entry so we never
			// keep files locally, as we're a tiny server
			// compared with our much better-provisioned
			// storage backend.  This is safe to do
			// because FileCache is thread safe.
			fileCache.Purge(ref)
		}
		mu.RUnlock()
	}()
	return upspin.Reference(ref), nil
}

// Get implements upspin.StoreServer.
func (s *server) Get(ref upspin.Reference) ([]byte, []upspin.Location, error) {
	fmt.Printf("context is %v\n", s.context)
	file, loc, err := s.innerGet(s.context.UserName(), ref)
	if err != nil {
		return nil, nil, err
	}
	if file != nil {
		defer file.Close()
		bytes, err := ioutil.ReadAll(file)
		if err != nil {
			err = errors.E("Get", err)
		}
		return bytes, nil, err
	}
	return nil, []upspin.Location{loc}, nil
}

// innerGet gets a local file descriptor or a new location for the reference. It returns only one of the two return
// values or an error. file is non-nil when the ref is found locally; the file is open for read and the
// caller should close it. If location is non-zero ref is in the backend at that location.
func (s *server) innerGet(userName upspin.UserName, ref upspin.Reference) (file *os.File, location upspin.Location, err error) {
	const Get = "Get"
	mu.RLock()
	defer mu.RUnlock()
	if !s.isConfigured() {
		return nil, upspin.Location{}, errors.E(Get, errNotConfigured)
	}
	file, err = fileCache.OpenRefForRead(string(ref))
	if err == nil {
		// Ref is in the local cache. Send the file and be done.
		log.Debug.Printf("ref %s is in local cache. Returning it as file: %s", ref, file.Name())
		return
	}

	// File is not local, try to get it from our storage.
	var link string
	link, err = cloudClient.Get(string(ref))
	if err != nil {
		err = errors.E(Get, err)
		return
	}
	// GCP should return an http link
	if !strings.HasPrefix(link, "http") {
		err = errors.E(Get, errors.Errorf("invalid link returned from GCP: %s", link))
		log.Error.Println(err)
		return
	}

	url, err := url.Parse(link)
	if err != nil {
		err = errors.E(Get, errors.Errorf("can't parse url: %s: %s", link, err))
		log.Error.Print(err)
		return
	}
	location.Reference = upspin.Reference(link)
	// Go fetch using the provided link. NetAddr is important so we can both ping the server and also cache the
	// HTTPS transport client efficiently.
	location.Endpoint.Transport = upspin.HTTPS
	location.Endpoint.NetAddr = upspin.NetAddr(fmt.Sprintf("%s://%s", url.Scheme, url.Host))
	log.Debug.Printf("Ref %s returned as link: %s", ref, link)
	return
}

// Delete implements upspin.StoreServer.
func (s *server) Delete(ref upspin.Reference) error {
	const Delete = "Delete"
	mu.RLock()
	defer mu.RUnlock()
	if !s.isConfigured() {
		return errors.E(Delete, errNotConfigured)
	}
	// TODO: verify ownership and proper ACLs to delete blob
	err := cloudClient.Delete(string(ref))
	if err != nil {
		return errors.E(Delete, errors.Errorf("%s: %s", ref, err))
	}
	return nil
}

// Dial implements upspin.Service.
func (s *server) Dial(context upspin.Context, e upspin.Endpoint) (upspin.Service, error) {
	if e.Transport != upspin.GCP {
		return nil, errors.E("Dial", errors.Invalid, errors.Str("unrecognized transport"))
	}

	mu.Lock()
	defer mu.Unlock()
	refCount++

	this := *s                    // Clone ourselves.
	this.context = context.Copy() // Make a copy of the context, to prevent changes.
	this.endpoint = e
	return &this, nil
}

// Configure configures the connection to the backing store (namely, GCP) once the service
// has been dialed. The details of the configuration are explained at the package comments.
func (s *server) Configure(options ...string) error {
	const Configure = "Configure"
	tempDir := ""
	var dialOpts []storage.DialOpts
	for _, option := range options {
		// Parse all options we understand. What we don't understand we pass it down to the storage.
		switch {
		case strings.HasPrefix(option, ConfigTemporaryDir):
			tempDir = option[len(ConfigTemporaryDir)+1:] // skip 'ConfigTemporaryDir='
		default:
			dialOpts = append(dialOpts, storage.WithOptions(option))
		}
	}
	mu.Lock()
	defer mu.Unlock()

	var err error
	cloudClient, err = storage.Dial("GCS", dialOpts...)
	if err != nil {
		return errors.E(Configure, err)
	}
	fileCache = cache.NewFileCache(tempDir)
	if fileCache == nil {
		return errors.E(Configure, errors.Str("filecache failed to create temp directory"))
	}
	log.Debug.Printf("Configured GCP store: %v", options)
	return nil
}

// isConfigured returns whether this server is configured properly.
// It must be called with mu read locked.
func (s *server) isConfigured() bool {
	return cloudClient != nil && fileCache != nil
}

// Ping implements upspin.Service.
func (s *server) Ping() bool {
	return true
}

// Close implements upspin.Service.
func (s *server) Close() {
	mu.Lock()
	defer mu.Unlock()

	if refCount == 0 {
		log.Error.Printf("Closing non-dialed gcp store")
		return
	}
	refCount--

	if refCount == 0 {
		if cloudClient != nil {
			cloudClient.Close()
		}
		cloudClient = nil
		if fileCache != nil {
			fileCache.Delete()
		}
		fileCache = nil
		// Do other cleanups here.
	}
}

// Authenticate implements upspin.Service.
func (s *server) Authenticate(upspin.Context) error {
	// Authentication is not dealt here. It happens at other layers.
	return nil
}

// Endpoint implements upspin.Service.
func (s *server) Endpoint() upspin.Endpoint {
	return s.endpoint
}

func init() {
	bind.RegisterStoreServer(upspin.GCP, New(context.New()))
}
