// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package flags defines command-line flags to make them consistent between binaries.
// Not all flags make sense for all binaries.
package flags

import (
	"flag"
	"os"
	"path/filepath"

	"upspin.io/log"
)

// We define the flags in two steps so clients don't have to write *flags.Flag.
// It also makes the documentation easier to read.

var (
	// Config is a comma-separated list of configuration options (key=value) for this server.
	Config = ""

	// Context names the Upspin context file to use.
	Context = filepath.Join(os.Getenv("HOME"), "/upspin/rc")

	// Endpoint specifies the endpoint of remote service (applies to forwarding servers only).
	Endpoint = "inprocess"

	// HTTPSAddr is the network address on which to listen for incoming network connections.
	HTTPSAddr = "localhost:443"

	// LogFile names the log file on GCP; leave empty to disable GCP logging.
	LogFile = ""

	// LogLevel sets the level of logging.
	Log = logFlag("info")
)

type logFlag string

// String implements flag.Value.
func (l *logFlag) String() string {
	return log.Level()
}

// Set implements flag.Value.
func (l *logFlag) Set(level string) error {
	return log.SetLevel(level)
}

// Get implements flag.Getter.
func (l *logFlag) Get() interface{} {
	return log.Level()
}

func init() {
	flag.StringVar(&Config, "config", Config, "comma-separated list of configuration options (key=value) for this server")
	flag.StringVar(&Context, "context", Context, "context file")
	flag.StringVar(&Endpoint, "endpoint", Endpoint, "endpoint of remote service for forwarding servers")
	flag.StringVar(&HTTPSAddr, "https_addr", HTTPSAddr, "laddress for incoming network connections")
	flag.StringVar(&LogFile, "log_file", LogFile, "name of the log file on GCP (empty to disable GCP logging)")
	flag.Var(&Log, "log", "level of logging: debug, info, error, disabled")
}
