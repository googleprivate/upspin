// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Keyserver is a wrapper for a key implementation that presents it as an HTTP
// interface.
package main

import (
	"flag"
	"net"
	"net/http"

	"upspin.io/cloud/https"
	"upspin.io/config"
	"upspin.io/errors"
	"upspin.io/factotum"
	"upspin.io/flags"
	"upspin.io/key/gcp"
	"upspin.io/key/inprocess"
	"upspin.io/log"
	"upspin.io/metric"
	"upspin.io/transport/keyserver"
	"upspin.io/upspin"

	// Load required transports
	_ "upspin.io/key/transports"
)

// The upspin username for this server.
const serverName = "keyserver"

var (
	testUser    = flag.String("test_user", "", "initialize a test `user` (localhost, inprocess only)")
	testSecrets = flag.String("test_secrets", "", "initialize test user with the secrets in this `directory`")
	// The format of the email config file must be lines: api key, incoming email provider user name and password.
	mailConfigFile = flag.String("mail_config", "", "config file name for incoming email signups")
)

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

	// All we need in the config is some user name. It does not need to be registered as a "real" user.
	cfg := config.SetUserName(config.New(), serverName)

	// Create a new key implementation.
	var key upspin.KeyServer
	var err error
	switch flags.ServerKind {
	case "inprocess":
		key = inprocess.New()
	case "gcp":
		key, err = gcp.New(flags.ServerConfig...)
	default:
		err = errors.Errorf("bad -kind %q", flags.ServerKind)

	}
	if err != nil {
		log.Fatalf("Setting up KeyServer: %v", err)
	}

	// Special hack for bootstrapping the inprocess key server.
	setupTestUser(key)

	httpStore := keyserver.New(cfg, key, upspin.NetAddr(flags.NetAddr))
	http.Handle("/api/Key/", httpStore)

	if *mailConfigFile != "" {
		mailHandler, err := newMailHandler(key, *mailConfigFile)
		if err != nil {
			log.Fatal(err)
		}
		http.Handle("/mail", mailHandler)
	}

	https.ListenAndServeFromFlags(nil, "keyserver")
}

// isLocal returns true if the name only resolves to loopback addresses.
func isLocal(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return false
	}
	for _, ip := range ips {
		if !ip.IsLoopback() {
			return false
		}
	}
	return true
}

func setupTestUser(key upspin.KeyServer) {
	if *testUser == "" {
		return
	}
	if *testSecrets == "" {
		log.Fatalf("cannot set up a test user without specifying -test_secrets")
	}

	// Sanity checks to make sure we're not doing this in production.
	if key.Endpoint().Transport != upspin.InProcess {
		log.Fatalf("cannot use testuser for endpoint %q", key.Endpoint())
	}
	if !isLocal(flags.HTTPSAddr) {
		log.Fatal("cannot use -testuser flag except on localhost:port")
	}

	f, err := factotum.NewFromDir(*testSecrets)
	if err != nil {
		log.Fatalf("unable to initialize factotum for %q: %v", *testUser, err)
	}
	userStruct := &upspin.User{
		Name:      upspin.UserName(*testUser),
		PublicKey: f.PublicKey(),
	}
	err = key.Put(userStruct)
	if err != nil {
		log.Fatalf("Put %q failed: %v", *testUser, err)
	}
}
