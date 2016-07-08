// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// A FUSE driver for Upspin.
package main

import (
	"flag"
	"fmt"
	"os"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"

	"upspin.io/bind"
	"upspin.io/context"
	"upspin.io/log"
	"upspin.io/upspin"
	"upspin.io/user/inprocess"
	"upspin.io/user/usercache"

	_ "upspin.io/directory/transports"
	_ "upspin.io/pack/ee"
	_ "upspin.io/pack/plain"
	_ "upspin.io/store/transports"
	_ "upspin.io/user/transports"
)

var (
	testFlag  = flag.String("test", "", "set up test context with specified user")
	debugFlag = flag.Bool("d", false, "turn on debugging")
)

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s <mountpoint>\n", os.Args[0])
	flag.PrintDefaults()
	os.Exit(2)
}

func debug(msg interface{}) {
	log.Debug.Printf("FUSE %v", msg)
}

func main() {
	flag.Usage = usage
	flag.Parse()

	if *debugFlag {
		fuse.Debug = debug
		log.SetLevel(log.Ldebug)
	}

	if flag.NArg() != 1 {
		usage()
	}
	mountpoint := flag.Arg(0)

	context, err := context.InitContext(nil)
	if err != nil {
		log.Debug.Fatal(err)
	}

	// dfuse does not do user lookups, so it does not need a usercache (other layers will use it).

	// Hack for testing
	if *testFlag != "" {
		key, err := bind.KeyServer(context, context.KeyEndpoint())
		if err != nil {
			log.Debug.Fatal(err)
		}
		testKey, ok := key.(*inprocess.Service)
		if !ok {
			log.Debug.Fatal("key server not a inprocess.Service")
		}

		dir, err := bind.DirServer(context, context.DirEndpoint())
		if err != nil {
			log.Debug.Fatal(err)
		}
		if err := testKey.Install(upspin.UserName(*testFlag), dir); err != nil {
			log.Debug.Print(err)
		}
	}

	context = usercache.Global(context)
	f := newUpspinFS(context)

	c, err := fuse.Mount(
		mountpoint,
		fuse.FSName("upspin"),
		fuse.Subtype("fs"),
		fuse.LocalVolume(),
		fuse.VolumeName(string(f.context.UserName())),
		fuse.DaemonTimeout("240"),
		//fuse.OSXDebugFuseKernel(),
		fuse.NoAppleDouble(),
		fuse.NoAppleXattr(),
	)
	if err != nil {
		log.Debug.Fatal(err)
	}
	defer c.Close()

	err = fs.Serve(c, f)
	if err != nil {
		log.Debug.Fatal(err)
	}

	// check if the mount process has an error to report
	<-c.Ready
	if err := c.MountError; err != nil {
		log.Debug.Fatal(err)
	}
}
