// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build !windows

// A FUSE driver for Upspin.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"upspin.io/context"
	"upspin.io/flags"
	"upspin.io/log"

	_ "upspin.io/pack/ee"
	_ "upspin.io/pack/plain"

	"upspin.io/transports"
)

var (
	cacheFlag = flag.String("cache", defaultCacheDir(), "`directory` for file cache")
)

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s <mountpoint>\n", os.Args[0])
	flag.PrintDefaults()
	os.Exit(2)
}

func main() {
	flag.Usage = usage
	flags.Parse("context", "log")

	if flag.NArg() != 1 {
		usage()
	}

	// Normal setup, get context from file and push user cache onto context.
	ctx, err := context.FromFile(flags.Context)
	if err != nil {
		log.Debug.Fatal(err)
	}
	transports.Init(ctx)

	// Mount the file system and start serving.
	mountpoint, err := filepath.Abs(flag.Arg(0))
	if err != nil {
		log.Fatalf("can't determine absolute path to mount point %s: %s", flag.Arg(0), err)
	}
	done := do(ctx, mountpoint, *cacheFlag)
	<-done
}

func defaultCacheDir() string {
	homeDir := os.Getenv("HOME")
	if len(homeDir) == 0 {
		homeDir = "/etc"
	}
	return homeDir + "/upspin"
}
