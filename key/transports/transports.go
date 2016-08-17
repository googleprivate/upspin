// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package transports is a helper package that aggregates the user imports.
// It has no functionality itself; it is meant to be imported, using an "underscore"
// import, as a convenient way to link with all the transport implementations.
package transports

import (
	_ "upspin.io/key/inprocess"
	_ "upspin.io/key/remote"
	_ "upspin.io/key/unassigned"
)
