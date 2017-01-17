// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package plain_test

import (
	"crypto/rand"
	"testing"

	"upspin.io/config"
	"upspin.io/pack"
	"upspin.io/pack/internal/packtest"
	"upspin.io/upspin"
)

var (
	globalConfig = config.New()
)

func TestRegister(t *testing.T) {
	p := pack.Lookup(upspin.PlainPack)
	if p == nil {
		t.Fatal("Lookup failed")
	}
	if p.Packing() != upspin.PlainPack {
		t.Fatalf("expected plain pack got %q", p)
	}
}

func TestPack(t *testing.T) {
	const (
		name upspin.PathName = "user@google.com/file/of/user"
		text                 = "this is some text"
	)

	cipher, de := doPack(t, name, []byte(text))
	clear := doUnpack(t, cipher, de)

	if string(clear) != text {
		t.Errorf("text: expected %q; got %q", text, clear)
	}
}

// doPack packs the contents of data for name and returns the cipher and the dir entry.
func doPack(t testing.TB, name upspin.PathName, data []byte) ([]byte, *upspin.DirEntry) {
	packer := pack.Lookup(upspin.PlainPack)
	de := &upspin.DirEntry{
		Name: name,
	}
	bp, err := packer.Pack(globalConfig, de)
	if err != nil {
		t.Fatal("doPack:", err)
	}
	cipher, err := bp.Pack(data)
	if err != nil {
		t.Fatal("doPack:", err)
	}
	bp.SetLocation(upspin.Location{Reference: "dummy"})
	if err := bp.Close(); err != nil {
		t.Fatal("doPack:", err)
	}
	return cipher, de
}

// doUnpack unpacks cipher for a dir entry and returns the clear text.
func doUnpack(t testing.TB, cipher []byte, de *upspin.DirEntry) []byte {
	packer := pack.Lookup(upspin.PlainPack)
	bp, err := packer.Unpack(globalConfig, de)
	if err != nil {
		t.Fatal("doUnpack:", err)
	}
	if _, ok := bp.NextBlock(); !ok {
		t.Fatal("doUnpack: no next block")
	}
	text, err := bp.Unpack(cipher)
	if err != nil {
		t.Fatal("doUnpack:", err)
	}
	return text
}

func benchmarkPlainPack(b *testing.B, fileSize int) {
	data := make([]byte, fileSize)
	n, err := rand.Read(data)
	if err != nil {
		b.Fatal(err)
	}
	if n != fileSize {
		b.Fatalf("Not enough random bytes: got %d, expected %d", n, fileSize)
	}
	data = data[:n]
	for i := 0; i < b.N; i++ {
		doPack(b, upspin.PathName("bench@upspin.io/foo.txt"), data)
	}
}

func BenchmarkPlainPack_1byte(b *testing.B)  { benchmarkPlainPack(b, 1) }
func BenchmarkPlainPack_1kbyte(b *testing.B) { benchmarkPlainPack(b, 1024) }
func BenchmarkPlainPack_1Mbyte(b *testing.B) { benchmarkPlainPack(b, 1024*1024) }

func TestMultiBlockRoundTrip(t *testing.T) {
	p := pack.Lookup(upspin.PlainPack)
	if p == nil {
		t.Fatal("Lookup failed")
	}
	const userName = upspin.UserName("ken@google.com")
	packtest.TestMultiBlockRoundTrip(t, globalConfig, p, userName)
}
