// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package context creates a client context from various sources.
package context

import (
	"bufio"
	"io"
	"os"
	ospath "path"
	"strings"

	"upspin.io/bind"
	"upspin.io/errors"
	"upspin.io/key/keyloader"
	"upspin.io/log"
	"upspin.io/pack"
	"upspin.io/path"
	"upspin.io/upspin"
)

type contextImpl struct {
	userName      upspin.UserName
	factotum      upspin.Factotum
	packing       upspin.Packing
	keyEndpoint   upspin.Endpoint
	dirEndpoint   upspin.Endpoint
	storeEndpoint upspin.Endpoint
}

// New returns a context with all fields set as defaults.
func New() upspin.Context {
	return &contextImpl{
		userName: "noone@nowhere.org",
		packing:  upspin.PlainPack,
	}
}

// InitContext returns a context generated from a configuration file and/or
// environment variables.
//
// The default configuration file location is $HOME/upspin/rc.
// If passed a non-nil io.Reader, that is used instead of the default file.
// The upserpinusername, upspinpacking, upspinkeyserver, upspindirserver and upspinstoreserver
// environment variables override the user name, packing, and key, directory, and store servers specified
// in the provided reader or default rc file.
//
// Any endpoints not set in the data for the context will be set to the
// "unassigned" transport and an empty network address.
//
// A configuration file should be of the format
//   # lines that begin with a hash are ignored
//   key = value
// where key may be one of user, keyserver, dirserver, storeserver, or packing.
//
func InitContext(r io.Reader) (upspin.Context, error) {
	const op = "InitContext"
	vals := map[string]string{
		"username":    "noone@nowhere.org",
		"keyserver":   "",
		"dirserver":   "",
		"storeserver": "",
		"packing":     "plain"}

	if r == nil {
		home := os.Getenv("HOME")
		if len(home) == 0 {
			log.Fatal("no home directory")
		}
		if f, err := os.Open(ospath.Join(home, "upspin/rc")); err == nil {
			r = f
			defer f.Close()
		}
	}

	// First source of truth is the RC file.
	if r != nil {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			// Remove comments.
			if sharp := strings.IndexByte(line, '#'); sharp >= 0 {
				line = line[:sharp]
			}
			line = strings.TrimSpace(line)
			tokens := strings.SplitN(line, "=", 2)
			if len(tokens) != 2 {
				continue
			}
			val := strings.TrimSpace(tokens[1])
			attr := strings.TrimSpace(tokens[0])
			if _, ok := vals[attr]; ok {
				vals[attr] = val
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}

	// Environment variables trump the RC file.
	for k := range vals {
		if v := os.Getenv("upspin" + k); len(v) != 0 {
			vals[k] = v
		}
	}

	context := new(contextImpl)
	context.userName = upspin.UserName(vals["username"])
	packer := pack.LookupByName(vals["packing"])
	if packer == nil {
		return nil, errors.Errorf("unknown packing %s", vals["packing"])
	}
	context.packing = packer.Packing()

	// Implicitly load the user's keys from $HOME/.ssh.
	// This must be done before bind so that keys are ready for authenticating to servers.
	// TODO(edpin): fix this by re-checking keys when they're needed.
	// TODO(ehg): remove loading of private key
	var err error
	err = keyloader.Load(context)
	if err != nil {
		log.Error.Print(err)
		return nil, err
	}

	context.keyEndpoint = parseEndpoint(op, vals, "keyserver", &err)
	context.storeEndpoint = parseEndpoint(op, vals, "storeserver", &err)
	context.dirEndpoint = parseEndpoint(op, vals, "dirserver", &err)
	return context, err
}

var ep0 upspin.Endpoint // Will have upspin.Unassigned as transport.

func parseEndpoint(op string, vals map[string]string, key string, errorp *error) upspin.Endpoint {
	text, ok := vals[key]
	if !ok || text == "" {
		// No setting for this value, so set to 'unassigned'.
		return ep0
	}
	ep, err := upspin.ParseEndpoint(text)
	if err != nil {
		log.Error.Printf("%s: cannot parse %q service: %s", op, text, err)
		if *errorp == nil {
			*errorp = err
		}
		return ep0
	}
	return *ep
}

// KeyServer implements upspin.Context.
func (ctx *contextImpl) KeyServer() upspin.KeyServer {
	u, err := bind.KeyServer(ctx, ctx.keyEndpoint)
	if err != nil {
		u, _ = bind.KeyServer(ctx, ep0)
	}
	return u
}

// DirServer implements upspin.Context.
func (ctx *contextImpl) DirServer(name upspin.PathName) upspin.DirServer {
	if len(name) == 0 {
		// If name is empty, just return the directory at
		// ctx.directoryEndpoint.
		d, err := bind.DirServer(ctx, ctx.dirEndpoint)
		if err != nil {
			d, _ = bind.DirServer(ctx, ep0)
		}
		return d
	}
	parsed, err := path.Parse(name)
	if err != nil {
		d, _ := bind.DirServer(ctx, ep0)
		return d
	}
	var endpoints []upspin.Endpoint
	if parsed.User() == ctx.userName {
		endpoints = append(endpoints, ctx.dirEndpoint)
	}
	if u, err := ctx.KeyServer().Lookup(parsed.User()); err == nil {
		endpoints = append(endpoints, u.Dirs...)
	}
	for _, e := range endpoints {
		d, _ := bind.DirServer(ctx, e)
		if d != nil {
			return d
		}
	}
	d, _ := bind.DirServer(ctx, ep0)
	return d
}

// StoreServer implements upspin.Context.
func (ctx *contextImpl) StoreServer() upspin.StoreServer {
	u, err := bind.StoreServer(ctx, ctx.storeEndpoint)
	if err != nil {
		u, _ = bind.StoreServer(ctx, ep0)
	}
	return u
}

// UserName implements upspin.Context.
func (ctx *contextImpl) UserName() upspin.UserName {
	return ctx.userName
}

// SetUserName implements upspin.Context.
func (ctx *contextImpl) SetUserName(u upspin.UserName) upspin.Context {
	ctx.userName = u
	return ctx
}

// Factotum implements upspin.Context.
func (ctx *contextImpl) Factotum() upspin.Factotum {
	return ctx.factotum
}

// SetFactotum implements upspin.Context.
func (ctx *contextImpl) SetFactotum(f upspin.Factotum) upspin.Context {
	ctx.factotum = f
	return ctx
}

// Packing implements upspin.Context.
func (ctx *contextImpl) Packing() upspin.Packing {
	return ctx.packing
}

// SetPacking implements upspin.Context.
func (ctx *contextImpl) SetPacking(p upspin.Packing) upspin.Context {
	ctx.packing = p
	return ctx
}

// KeyEndpoint implements upspin.Context.
func (ctx *contextImpl) KeyEndpoint() upspin.Endpoint {
	return ctx.keyEndpoint
}

// SetKeyEndpoint implements upspin.Context.
func (ctx *contextImpl) SetKeyEndpoint(e upspin.Endpoint) upspin.Context {
	ctx.keyEndpoint = e
	return ctx
}

// DirEndpoint implements upspin.Context.
func (ctx *contextImpl) DirEndpoint() upspin.Endpoint {
	return ctx.dirEndpoint
}

// SetDirEndpoint implements upspin.Context.
func (ctx *contextImpl) SetDirEndpoint(e upspin.Endpoint) upspin.Context {
	ctx.dirEndpoint = e
	return ctx
}

// StoreEndpoint implements upspin.Context.
func (ctx *contextImpl) StoreEndpoint() upspin.Endpoint {
	return ctx.storeEndpoint
}

// SetStoreEndpoint implements upspin.Context.
func (ctx *contextImpl) SetStoreEndpoint(e upspin.Endpoint) upspin.Context {
	ctx.storeEndpoint = e
	return ctx
}

// Copy implements upspin.Context.
func (ctx *contextImpl) Copy() upspin.Context {
	c := *ctx
	return &c
}
