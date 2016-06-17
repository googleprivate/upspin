// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package keyloader loads public and private keys from the user's home directory.
package keyloader

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"upspin.io/errors"
	"upspin.io/factotum"
	"upspin.io/upspin"
)

const (
	keyloaderErr = "keyloader: %v"
)

var (
	errNoKeysFound = errors.Str("no keys found")
	errNilContext  = errors.Str("nil context")
	zeroPrivKey    upspin.KeyPair
	zeroPubKey     upspin.PublicKey
)

// Load reads a key pair from the user's .ssh directory and loads
// them into the context.
func Load(context *upspin.Context) error {
	if context == nil {
		return errors.E(Load, errors.Invalid, errors.Str("nil context"))
	}
	k, err := privateKey("Load")
	if err != nil {
		return err
	}
	context.Factotum, err = factotum.New(k)
	return err
}

// publicKey returns the public key of the current user by reading from $HOME/.ssh/.
func publicKey(op string) (upspin.PublicKey, error) {
	f, err := os.Open(filepath.Join(sshdir(), "public.upspinkey"))
	if err != nil {
		return zeroPubKey, errors.E(op, errors.NotExist, errNoKeysFound)
	}
	defer f.Close()
	buf := make([]byte, 400) // enough for p521
	n, err := f.Read(buf)
	if err != nil {
		return zeroPubKey, fmt.Errorf(keyloaderErr, err)
	}
	return upspin.PublicKey(string(buf[:n])), nil
}

// privateKey returns the private key of the current user by reading from $HOME/.ssh/.
func privateKey(op string) (upspin.KeyPair, error) {
	f, err := os.Open(filepath.Join(sshdir(), "secret.upspinkey"))
	if err != nil {
		return zeroPrivKey, errors.E(op, errors.NotExist, errNoKeysFound)
	}
	defer f.Close()
	buf := make([]byte, 200) // enough for p521
	n, err := f.Read(buf)
	if err != nil {
		return zeroPrivKey, fmt.Errorf(keyloaderErr, err)
	}
	buf = buf[:n]
	buf = []byte(strings.TrimSpace(string(buf)))
	pubkey, err := publicKey(op)
	if err != nil {
		return zeroPrivKey, err
	}
	return upspin.KeyPair{
		Public:  pubkey,
		Private: upspin.PrivateKey(string(buf)),
	}, nil
	// TODO sanity check that Private is consistent with Public
}

func sshdir() string {
	home := os.Getenv("HOME")
	if len(home) == 0 {
		panic("no home directory")
	}
	return filepath.Join(home, ".ssh")
}
