// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package keyserver is a wrapper for an upspin.KeyServer implementation
// that presents it as an authenticated GRPC service.
package keyserver

import (
	"fmt"

	gContext "golang.org/x/net/context"

	"upspin.io/auth/grpcauth"
	"upspin.io/context"
	"upspin.io/errors"
	"upspin.io/log"
	"upspin.io/upspin"
	"upspin.io/upspin/proto"
	"upspin.io/user"
)

// server is a SecureServer that talks to a KeyServer interface and serves GRPC requests.
type server struct {
	context upspin.Context

	// What this server reports itself as through its Endpoint method.
	endpoint upspin.Endpoint

	// The underlying keyserver implementation.
	key upspin.KeyServer

	// Automatically handles authentication by implementing the Authenticate server method.
	grpcauth.SecureServer
}

func New(ctx upspin.Context, key upspin.KeyServer, ss grpcauth.SecureServer, addr upspin.NetAddr) proto.KeyServer {
	return &server{
		context: ctx,
		endpoint: upspin.Endpoint{
			Transport: upspin.Remote,
			NetAddr:   addr,
		},
		key:          key,
		SecureServer: ss,
	}
}

// keyFor returns a KeyServer bound to the user specified in the context.
func (s *server) keyFor(ctx gContext.Context) (upspin.KeyServer, error) {
	// Validate that we have a session. If not, it's an auth error.
	session, err := s.GetSessionFromContext(ctx)
	if err != nil {
		return nil, err
	}
	userName, err := user.Clean(session.User())
	if err != nil {
		return nil, err
	}
	svc, err := s.key.Dial(context.SetUserName(s.context, userName), s.key.Endpoint())
	if err != nil {
		return nil, err
	}
	return svc.(upspin.KeyServer), nil
}

// Lookup implements proto.KeyServer, and does not do any authentication.
func (s *server) Lookup(ctx gContext.Context, req *proto.KeyLookupRequest) (*proto.KeyLookupResponse, error) {
	op := logf("Lookup %q", req.UserName)

	user, err := s.key.Lookup(upspin.UserName(req.UserName))
	if err != nil {
		op.log(err)
		return &proto.KeyLookupResponse{Error: errors.MarshalError(err)}, nil
	}
	return &proto.KeyLookupResponse{User: proto.UserProto(user)}, nil
}

// Put implements proto.KeyServer.
func (s *server) Put(ctx gContext.Context, req *proto.KeyPutRequest) (*proto.KeyPutResponse, error) {
	op := logf("Put %v", req)

	key, err := s.keyFor(ctx)
	if err != nil {
		op.log(err)
		return putError(err), nil
	}

	user := proto.UpspinUser(req.User)
	err = key.Put(user)
	if err != nil {
		op.log(err)
		return putError(err), nil
	}
	return &proto.KeyPutResponse{}, nil
}

func putError(err error) *proto.KeyPutResponse {
	return &proto.KeyPutResponse{Error: errors.MarshalError(err)}
}

// Endpoint implements proto.KeyServer.
func (s *server) Endpoint(ctx gContext.Context, req *proto.EndpointRequest) (*proto.EndpointResponse, error) {
	return &proto.EndpointResponse{
		Endpoint: &proto.Endpoint{
			Transport: int32(s.endpoint.Transport),
			NetAddr:   string(s.endpoint.NetAddr),
		},
	}, nil
}

func logf(format string, args ...interface{}) operation {
	s := fmt.Sprintf(format, args...)
	log.Info.Print("grpc/keyserver: " + s)
	return operation(s)
}

type operation string

func (op operation) log(err error) {
	logf("%v failed: %v", op, err)
}
