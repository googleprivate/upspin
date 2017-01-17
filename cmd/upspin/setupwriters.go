// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"upspin.io/client"
	"upspin.io/config"
	"upspin.io/errors"
	"upspin.io/flags"
	"upspin.io/upspin"
	"upspin.io/user"
)

func (s *State) setupwriters(args ...string) {
	const help = `
Setupwriters creates or updates the Writers file for the given project.
The file lists the names of users granted access to write to the project's
store server and to create their own root on the directory server.

A wildcard permits access to all users of a domain ("*@example.com").

The user name of the project's directory server is automatically included in
the list, so the directory server can use the store for its own data storage.

This command is designed to operate on projects created by setupdomain.
`
	fs := flag.NewFlagSet("setupwriters", flag.ExitOnError)
	where := fs.String("where", filepath.Join(os.Getenv("HOME"), "upspin", "deploy"), "`directory` containing private configuration files")
	s.parseFlags(fs, args, help, "[-project=<gcp_project_name>] setupwriters [-where=$HOME/upspin/deploy] <user names>")

	var users []upspin.UserName
	for _, arg := range fs.Args() {
		u, err := user.Clean(upspin.UserName(arg))
		if err != nil {
			s.exit(err)
		}
		users = append(users, u)
	}

	cfgDir := filepath.Join(*where, flags.Project)
	if fi, err := os.Stat(cfgDir); err != nil {
		s.exitf("error reading configuration directory: %v", err)
	} else if !fi.IsDir() {
		s.exitf("specified location is not a directory: %v", cfgDir)
	}

	storeCfg, err := config.FromFile(filepath.Join(cfgDir, "storeserver/rc"))
	if err != nil {
		s.exit(err)
	}
	dirCfg, err := config.FromFile(filepath.Join(cfgDir, "dirserver/rc"))
	if err != nil {
		s.exit(err)
	}

	storeUser := storeCfg.UserName()
	dirUser := dirCfg.UserName()

	// Act as the store user.
	c := client.New(storeCfg)

	// Make the store root.
	_, err = c.MakeDirectory(upspin.PathName(storeUser) + "/")
	if err != nil && !errors.Match(errors.E(errors.Exist), err) {
		s.exit(err)
	}
	// Make the Group directory.
	_, err = c.MakeDirectory(upspin.PathName(storeUser) + "/Group")
	if err != nil && !errors.Match(errors.E(errors.Exist), err) {
		s.exit(err)
	}

	// Prepare Access file and put it to the server.
	buf := new(bytes.Buffer)
	fmt.Fprintf(buf, "*:%v\n", storeUser)
	fmt.Fprintf(buf, "l,r:%v\n", dirUser)
	_, err = c.Put(upspin.PathName(storeUser)+"/Access", buf.Bytes())
	if err != nil {
		s.exit(err)
	}

	// Prepare Writers file and put it to the server.
	buf.Reset()
	fmt.Fprintln(buf, dirUser) // Always include the directory server's user.
	for _, u := range users {
		fmt.Fprintln(buf, u)
	}
	_, err = c.Put(upspin.PathName(storeUser)+"/Group/Writers", buf.Bytes())
	if err != nil {
		s.exit(err)
	}
}
