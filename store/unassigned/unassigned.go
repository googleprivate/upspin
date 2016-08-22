// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package unassigned implements a store server that errors out all its requests.
package unassigned

import (
	"upspin.io/bind"
	"upspin.io/errors"
	"upspin.io/upspin"
)

// unassigned implements upspin.StoreServer.
type unassigned struct {
	endpoint upspin.Endpoint
}

var _ upspin.StoreServer = (*unassigned)(nil)

var unassignedErr = errors.Str("request to unassigned service")

// Get implements upspin.StoreServer.Get.
func (*unassigned) Get(ref upspin.Reference) ([]byte, []upspin.Location, error) {
	const op = "store/unassigned.Get"
	return nil, nil, errors.E(op, errors.Invalid, unassignedErr)
}

// Put implements upspin.StoreServer.Put.
func (*unassigned) Put(data []byte) (upspin.Reference, error) {
	const op = "store/unassigned.Put"
	return "", errors.E(op, errors.Invalid, unassignedErr)
}

// Delete implements upspin.StoreServer.Delete.
func (*unassigned) Delete(ref upspin.Reference) error {
	const op = "store/unassigned.Delete"
	return errors.E(op, errors.Invalid, unassignedErr)
}

// Endpoint implements upspin.Service.
func (u *unassigned) Endpoint() upspin.Endpoint {
	return u.endpoint
}

// Configure implements upspin.Service.
func (*unassigned) Configure(options ...string) error {
	const op = "store/unassigned.Configure"
	return errors.E(op, errors.Invalid, unassignedErr)
}

// Close implements upspin.Service.
func (*unassigned) Close() {
}

// Authenticate implements upspin.Service.
func (*unassigned) Authenticate(context upspin.Context) error {
	const op = "store/unassigned.Authenticate"
	return errors.E(op, errors.Invalid, unassignedErr)
}

// Ping implements upspin.Service.
func (*unassigned) Ping() bool {
	return true
}

// Dial implements upspin.Service.
func (u *unassigned) Dial(context upspin.Context, e upspin.Endpoint) (upspin.Service, error) {
	const op = "store/unassigned.Dial"
	if e.Transport != upspin.Unassigned {
		return nil, errors.E(op, errors.Invalid, errors.Str("unrecognized transport"))
	}

	return &unassigned{e}, nil
}

const transport = upspin.Unassigned

func init() {
	bind.RegisterStoreServer(transport, &unassigned{})
}
