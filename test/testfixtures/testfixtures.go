// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package testfixtures implements dummies for StoreServers, DirServers and User services for tests.
package testfixtures

import "upspin.io/upspin"

// DummyUser is an implementation of upspin.User that does nothing.
type DummyUser struct {
	dummyDialer
	dummyService
}

var _ upspin.User = (*DummyUser)(nil)

// DummyStore is an implementation of upspin.StoreServer that does nothing.
type DummyStoreServer struct {
	dummyDialer
	dummyService
}

var _ upspin.StoreServer = (*DummyStoreServer)(nil)

// DummyDirServer is an implementation of upspin.DirServer that does nothing.
type DummyDirServer struct {
	dummyDialer
	dummyService
}

var _ upspin.DirServer = (*DummyDirServer)(nil)

// dummyService implements a no-op upspin.Service
type dummyService struct {
}

var _ upspin.Service = (*dummyService)(nil)

type dummyDialer struct {
}

var _ upspin.Dialer = (*dummyDialer)(nil)

// Dial implements upspin.Dialer.
func (d *dummyDialer) Dial(upspin.Context, upspin.Endpoint) (upspin.Service, error) {
	return nil, nil
}

// Endpoint implements upspin.Service.
func (d *dummyService) Endpoint() upspin.Endpoint {
	return upspin.Endpoint{}
}

// Configure implements upspin.Service.
func (d *dummyService) Configure(options ...string) error {
	return nil
}

// Authenticate implements upspin.Service.
func (d *dummyService) Authenticate(upspin.Context) error {
	return nil
}

// Close implements upspin.Service.
func (d *dummyService) Close() {
}

// Ping implements upspin.Service.
func (d *dummyService) Ping() bool {
	return true
}

// Lookup implements upspin.User.
func (d *DummyUser) Lookup(userName upspin.UserName) ([]upspin.Endpoint, []upspin.PublicKey, error) {
	return nil, nil, nil
}

// Get implements upspin.StoreServer.
func (d *DummyStoreServer) Get(ref upspin.Reference) ([]byte, []upspin.Location, error) {
	return nil, nil, nil
}

// Put implements upspin.StoreServer.
func (d *DummyStoreServer) Put(data []byte) (upspin.Reference, error) {
	return "", nil
}

// Delete implements upspin.StoreServer.
func (d *DummyStoreServer) Delete(ref upspin.Reference) error {
	return nil
}

// Lookup implements upspin.DirServer.
func (d *DummyDirServer) Lookup(name upspin.PathName) (*upspin.DirEntry, error) {
	return nil, nil
}

// Put implements upspin.DirServer.
func (d *DummyDirServer) Put(entry *upspin.DirEntry) error {
	return nil
}

// MakeDirectory implements upspin.DirServer.
func (d *DummyDirServer) MakeDirectory(dirName upspin.PathName) (upspin.Location, error) {
	return upspin.Location{}, nil
}

// Glob implements upspin.DirServer.
func (d *DummyDirServer) Glob(pattern string) ([]*upspin.DirEntry, error) {
	return nil, nil
}

// Delete implements upspin.DirServer.
func (d *DummyDirServer) Delete(name upspin.PathName) error {
	return nil
}

// WhichAccess implements upspin.DirServer.
func (d *DummyDirServer) WhichAccess(name upspin.PathName) (upspin.PathName, error) {
	return "", nil
}
