// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package keyloader loads public and private keys from the user's home directory.
package keyloader

import (
	"bytes"
	"os"
	"path/filepath"

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
	zeroPrivKey    string
	zeroPubKey     upspin.PublicKey
)

// Load reads a key pair from the user's .ssh directory and loads
// them into the context.
func Load(context upspin.Context) error {
	if context == nil {
		return errors.E(Load, errors.Invalid, errors.Str("nil context"))
	}
	pub, priv, err := privateKey("Load")
	if err != nil {
		return err
	}
	var f upspin.Factotum
	f, err = factotum.New(pub, priv)
	if err != nil {
		return err
	}
	context.SetFactotum(f)
	return nil
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
		return zeroPubKey, errors.Errorf(keyloaderErr, err)
	}
	return upspin.PublicKey(string(buf[:n])), nil
}

// privateKey returns the private key of the current user by reading from $HOME/.ssh/.
func privateKey(op string) (upspin.PublicKey, string, error) {
	f, err := os.Open(filepath.Join(sshdir(), "secret.upspinkey"))
	if err != nil {
		return zeroPubKey, zeroPrivKey, errors.E(op, errors.NotExist, errNoKeysFound)
	}
	defer f.Close()
	buf := make([]byte, 200) // enough for p521
	n, err := f.Read(buf)
	if err != nil {
		return zeroPubKey, zeroPrivKey, errors.Errorf(keyloaderErr, err)
	}
	buf = bytes.TrimSpace(buf[:n])
	pubkey, err := publicKey(op)
	if err != nil {
		return zeroPubKey, zeroPrivKey, err
	}
	return pubkey, string(buf), nil
	// TODO sanity check that Private is consistent with Public
}

func sshdir() string {
	home := os.Getenv("HOME")
	if len(home) == 0 {
		panic("no home directory")
	}
	return filepath.Join(home, ".ssh")
}
