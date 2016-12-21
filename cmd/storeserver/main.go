// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Storeserver is a wrapper for a store implementation that presents it as a
// GRPC interface.
package main

import (
	"net/http"

	"google.golang.org/grpc"

	"upspin.io/auth/grpcauth"
	"upspin.io/cloud/https"
	"upspin.io/context"
	"upspin.io/errors"
	"upspin.io/flags"
	"upspin.io/grpc/storeserver"
	"upspin.io/log"
	"upspin.io/metric"
	"upspin.io/serverutil/perm"
	"upspin.io/store/filesystem"
	"upspin.io/store/gcp"
	"upspin.io/store/inprocess"
	"upspin.io/upspin"
	"upspin.io/upspin/proto"

	// We need the directory transports to fetch write permissions.
	_ "upspin.io/transports"

	// Load packers for reading Access and Group files.
	_ "upspin.io/pack/eeintegrity"
	_ "upspin.io/pack/plain"
)

const serverName = "storeserver"

func main() {
	flags.Parse("addr", "config", "context", "https", "kind", "log", "project", "tls")

	if flags.Project != "" {
		log.Connect(flags.Project, serverName)
		svr, err := metric.NewGCPSaver(flags.Project, "serverName", serverName)
		if err != nil {
			log.Fatalf("Can't start a metric saver for GCP project %q: %s", flags.Project, err)
		} else {
			metric.RegisterSaver(svr)
		}
	}

	// Load context and keys for this server. It needs a real upspin username and keys.
	ctx, err := context.FromFile(flags.Context)
	if err != nil {
		log.Fatal(err)
	}

	// Create a new store implementation.
	var store upspin.StoreServer
	err = nil
	switch flags.ServerKind {
	case "inprocess":
		store = inprocess.New()
	case "gcp":
		store, err = gcp.New(flags.Config...)
	case "filesystem":
		store, err = filesystem.New(ctx, flags.Config...)
	default:
		err = errors.Errorf("bad -kind %q", flags.ServerKind)
	}
	if err != nil {
		log.Fatalf("Setting up StoreServer: %v", err)
	}

	// Wrap with permission checks.
	ready := make(chan struct{})
	store, err = perm.WrapStore(ctx, ready, store)
	if err != nil {
		log.Fatalf("Error wrapping store: %s", err)
	}

	authServer := grpcauth.NewServer(ctx, nil)
	s := storeserver.New(ctx, store, authServer, upspin.NetAddr(flags.NetAddr))

	grpcServer := grpc.NewServer()
	proto.RegisterStoreServer(grpcServer, s)
	http.Handle("/", grpcServer)

	https.ListenAndServe(ready, serverName, flags.HTTPSAddr, &https.Options{
		CertFile: flags.TLSCertFile,
		KeyFile:  flags.TLSKeyFile,
	})
}
