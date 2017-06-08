// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Command upbox builds and runs Upspin servers as specified by a schema and
provides an upspin shell acting as the first user specified by the schema.

For information on defining a schema, see the documentation for package
upspin.io/exp/upbox.
*/
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"upspin.io/exp/upbox"
)

var (
	logLevel = flag.String("log", "info", "log `level`")
	basePort = flag.Int("port", 8000, "base `port` number for upspin servers")
	schema   = flag.String("schema", "", "schema `file` name")
)

func main() {
	flag.Parse()

	sc, err := upbox.SchemaFromFile(*schema, *basePort)
	if err != nil {
		fail(fmt.Errorf("parsing schema: %v", err))
	}
	sc.LogLevel = *logLevel

	if err := sc.Start(); err != nil {
		fail(err)
	}

	// Start a shell as the first user.
	args := []string{
		"-config=" + sc.Config(sc.Users[0].Name),
		"-log=" + *logLevel,
		"shell",
	}
	fmt.Fprintf(os.Stderr, "upbox: upspin %s\n", strings.Join(args, " "))
	shell := exec.Command("upspin", args...)
	shell.Stdin = os.Stdin
	shell.Stdout = os.Stdout
	shell.Stderr = os.Stderr
	err = shell.Run()
	err2 := sc.Stop()
	if err != nil {
		fail(err)
	}
	if err2 != nil {
		fail(err2)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "upbox:", err)
	os.Exit(1)
}
