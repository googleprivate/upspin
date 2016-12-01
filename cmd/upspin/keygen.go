// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

// This file contains the implementation of the keygen command.

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"upspin.io/errors"
	"upspin.io/key/proquint"
	"upspin.io/pack/ee"
)

func (s *State) keygen(args ...string) {
	const help = `
Keygen creates a new Upspin key pair and stores the pair in local
files secret.upspinkey and public.upspinkey in $HOME/.ssh. Existing
key pairs are appended to $HOME/.ssh/secret2.upspinkey. Keygen does
not update the information in the key server; use the user -put
command for that.

New users should instead use the signup command to create their
first key. Keygen can be used to create new keys.

See the description for rotate for information about updating keys.
`
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	curveName := fs.String("curve", "p256", "cryptographic curve `name`: p256, p384, or p521")
	secret := fs.String("secretseed", "", "128 bit secret `seed` in proquint format")
	where := fs.String("where", filepath.Join(os.Getenv("HOME"), ".ssh"), "`directory` to store keys")
	s.parseFlags(fs, args, help, "keygen [-curve=256] [-secret=seed] [-where=$HOME/.ssh]")
	if fs.NArg() != 0 {
		fs.Usage()
	}
	switch *curveName {
	case "p256":
	case "p384":
	case "p521":
		// ok
	default:
		log.Printf("no such curve %q", *curveName)
		fs.Usage()
	}

	public, private, proquintStr, err := createKeys(*curveName, *secret)
	if err != nil {
		s.exitf("creating keys: %v", err)
	}

	if *where == "" {
		s.exitf("-where must not be empty")
	}
	err = saveKeys(*where)
	if err != nil {
		s.exitf("saving previous keys failed(%v); keys not generated", err)
	}
	err = writeKeys(*where, public, private, proquintStr)
	if err != nil {
		s.exitf("writing keys: %v", err)
	}
}

func createKeys(curveName, secret string) (public string, private, proquintStr string, err error) {
	// Pick secret 128 bits.
	// TODO(ehg)  Consider whether we are willing to ask users to write long seeds for P521.
	b := make([]byte, 16)
	if len(secret) > 0 {
		if len((secret)) != 47 || (secret)[5] != '-' {
			log.Printf("expected secret like\n lusab-babad-gutih-tugad.gutuk-bisog-mudof-sakat\n"+
				"not\n %s\nkey not generated", secret)
			return "", "", "", errors.E("keygen", errors.Invalid, errors.Str("bad format for secret"))
		}
		for i := 0; i < 8; i++ {
			binary.BigEndian.PutUint16(b[2*i:2*i+2], proquint.Decode([]byte((secret)[6*i:6*i+5])))
		}
	} else {
		ee.GenEntropy(b)
		proquints := make([]interface{}, 8)
		for i := 0; i < 8; i++ {
			proquints[i] = proquint.Encode(binary.BigEndian.Uint16(b[2*i : 2*i+2]))
		}
		proquintStr = fmt.Sprintf("%s-%s-%s-%s.%s-%s-%s-%s", proquints...)
		// Ignore punctuation on input;  this format is just to help the user keep their place.
	}

	pub, priv, err := ee.CreateKeys(curveName, b)
	if err != nil {
		return "", "", "", err
	}
	return string(pub), priv, proquintStr, nil
}

func writeKeys(where, publicKey, privateKey, proquintStr string) error {
	// Save the keys to files.
	fdPrivate, err := os.Create(filepath.Join(where, "secret.upspinkey"))
	if err != nil {
		return err
	}
	err = fdPrivate.Chmod(0600)
	if err != nil {
		return err
	}
	fdPublic, err := os.Create(filepath.Join(where, "public.upspinkey"))
	if err != nil {
		return err
	}

	fdPrivate.WriteString(privateKey)
	fdPublic.WriteString(string(publicKey))

	err = fdPrivate.Close()
	if err != nil {
		return err
	}
	err = fdPublic.Close()
	if err != nil {
		return err
	}
	if proquintStr != "" {
		fmt.Printf("Keys generated. To recover them if lost, run:\n\tupspin keygen -secretseed %s\n", proquintStr)
	}
	return nil
}

func saveKeys(where string) error {
	private, err := os.Open(filepath.Join(where, "secret.upspinkey"))
	if os.IsNotExist(err) {
		return nil // There is nothing we need to save.
	}
	priv, err := ioutil.ReadAll(private)
	if err != nil {
		return err
	}
	public, err := os.Open(filepath.Join(where, "public.upspinkey"))
	if err != nil {
		return err // Halt. Existing files are corrupted and need manual attention.
	}
	pub, err := ioutil.ReadAll(public)
	if err != nil {
		return err
	}
	archive, err := os.OpenFile(filepath.Join(where, "secret2.upspinkey"),
		os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if err != nil {
		return err // We don't have permission to archive old keys?
	}
	_, err = archive.Write([]byte("# EE \n")) // TODO(ehg) add file date
	if err != nil {
		return err
	}
	_, err = archive.Write(pub)
	if err != nil {
		return err
	}
	_, err = archive.Write(priv)
	if err != nil {
		return err
	}
	return nil
}
