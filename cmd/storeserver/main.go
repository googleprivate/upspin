// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Storeserver is a wrapper for a store implementation that presents it as an
// HTTP interface.
package main

import (
	"net/http"

	"upspin.io/cloud/https"
	"upspin.io/config"
	"upspin.io/errors"
	"upspin.io/flags"
	"upspin.io/log"
	"upspin.io/metric"
	"upspin.io/serverutil/perm"
	"upspin.io/store/filesystem"
	"upspin.io/store/gcp"
	"upspin.io/store/inprocess"
	"upspin.io/transport/storeserver"
	"upspin.io/upspin"

	// We need the directory transports to fetch write permissions.
	_ "upspin.io/transports"

	// Load packers for reading Access and Group files.
	_ "upspin.io/pack/eeintegrity"
	_ "upspin.io/pack/plain"
)

const serverName = "storeserver"

func main() {
	flags.Parse("addr", "config", "https", "kind", "letscache", "log", "project", "serverconfig", "tls")

	if flags.Project != "" {
		log.Connect(flags.Project, serverName)
		svr, err := metric.NewGCPSaver(flags.Project, "serverName", serverName)
		if err != nil {
			log.Fatalf("Can't start a metric saver for GCP project %q: %s", flags.Project, err)
		} else {
			metric.RegisterSaver(svr)
		}
	}

	// Load configuration and keys for this server. It needs a real upspin username and keys.
	ctx, err := config.FromFile(flags.Config)
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
		store, err = gcp.New(flags.ServerConfig...)
	case "filesystem":
		store, err = filesystem.New(ctx, flags.ServerConfig...)
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

	httpStore := storeserver.New(ctx, store, upspin.NetAddr(flags.NetAddr))
	http.Handle("/api/Store/", httpStore)
	https.ListenAndServeFromFlags(ready, serverName)
}
