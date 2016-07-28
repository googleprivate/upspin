// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package packtest provides common functionality used by packer tests.
package packtest

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	mRand "math/rand"
	"testing"

	"upspin.io/upspin"
)

type fakeStore map[upspin.Reference][]byte

func TestMultiBlockRoundTrip(t *testing.T, ctx upspin.Context, packer upspin.Packer, userName upspin.UserName) {
	pathName := upspin.PathName(userName + "/file")

	// Work with 1MB of random data.
	data := make([]byte, 1<<20)
	n, err := rand.Read(data)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(data) {
		t.Fatalf("read %v bytes, want %v", n, len(data))
	}

	de := &upspin.DirEntry{
		Name:    pathName,
		Writer:  userName,
		Packing: packer.Packing(),
	}

	store := make(fakeStore)

	if err := packEntry(ctx, store, packer, de, bytes.NewReader(data)); err != nil {
		t.Fatal("packEntry:", err)
	}

	t.Logf("packed %v bytes into %v blocks", len(data), len(de.Blocks))

	var out bytes.Buffer
	if err := unpackEntry(ctx, store, packer, de, &out); err != nil {
		t.Fatal("unpackEntry:", err)
	}

	t.Logf("unpacked %v bytes", out.Len())

	if !bytes.Equal(data, out.Bytes()) {
		t.Fatal("output did not match input")
	}
}

func packEntry(ctx upspin.Context, store fakeStore, packer upspin.Packer, de *upspin.DirEntry, r io.Reader) error {
	bp, err := packer.Pack(ctx, de)
	if err != nil {
		return err
	}

	rand := mRand.New(mRand.NewSource(1))

	// Store and pack data in 1KB increments.
	clear := make([]byte, 1<<10)
loop:
	for {
		// Pick a pseudo-random block size.
		clear = clear[:rand.Intn(cap(clear)-1)+1]

		n, err := io.ReadFull(r, clear)
		switch err {
		case nil, io.ErrUnexpectedEOF:
			// OK
		case io.EOF:
			break loop
		default:
			// Handle read error.
			return err
		}

		cipher, err := bp.Pack(clear[:n])
		if err != nil {
			return err
		}

		// Store the ciphertext, creating a pseudo-ref.
		sum := sha256.Sum256(cipher)
		ref := upspin.Reference(fmt.Sprintf("%x", sum))
		store[ref] = append([]byte(nil), cipher...)

		bp.SetLocation(upspin.Location{Reference: ref})
	}

	return bp.Close()
}

func unpackEntry(ctx upspin.Context, store fakeStore, packer upspin.Packer, de *upspin.DirEntry, w io.Writer) error {
	bp, err := packer.Unpack(ctx, de)
	if err != nil {
		return err
	}

	for {
		b, ok := bp.NextBlock()
		if !ok {
			return nil
		}

		ref := b.Location.Reference
		cipher, ok := store[ref]
		if !ok {
			return fmt.Errorf("could not find reference %q in store", ref)
		}

		clear, err := bp.Unpack(cipher)
		if err != nil {
			return err
		}

		if _, err := w.Write(clear); err != nil {
			return err
		}
	}
}
