// Copyright 2017 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Frontend provides a web server that serves documentation and meta
// tags to instruct "go get" where to find the Upspin source repository.
package main // import "upspin.io/cmd/frontend"

import (
	"upspin.io/cloud/https"
	"upspin.io/serverutil/frontend"
)

func main() {
	frontend.Main()
	https.ListenAndServeFromFlags(nil)
}
