// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package plain is the no-op Packing that passes the data untouched.
// Metadata is not affected. The path name is not stored in the packed data.
package plain

import (
	"upspin.io/errors"
	"upspin.io/pack"
	"upspin.io/pack/internal"
	"upspin.io/path"
	"upspin.io/upspin"
)

type plainPack struct{}

var _ upspin.Packer = plainPack{}

func init() {
	pack.Register(plainPack{})
}

var errTooShort = errors.Str("destination slice too short")

func (plainPack) Packing() upspin.Packing {
	return upspin.PlainPack
}

func (plainPack) String() string {
	return "plain"
}

func (plainPack) ReaderHashes(packdata []byte) ([][]byte, error) {
	return nil, nil
}

func (plainPack) Share(cfg upspin.Config, readers []upspin.PublicKey, packdata []*[]byte) {
	// Nothing to do.
}

func (p plainPack) Pack(cfg upspin.Config, d *upspin.DirEntry) (upspin.BlockPacker, error) {
	const op = "pack/plain.Pack"
	if err := pack.CheckPacking(p, d); err != nil {
		return nil, errors.E(op, errors.Invalid, d.Name, err)
	}
	return &blockPacker{
		cfg:   cfg,
		entry: d,
	}, nil
}

type blockPacker struct {
	cfg   upspin.Config
	entry *upspin.DirEntry
}

func (bp *blockPacker) Pack(cleartext []byte) (ciphertext []byte, err error) {
	const op = "pack/plain.blockPacker.Pack"
	if err := internal.CheckLocationSet(bp.entry); err != nil {
		return nil, err
	}

	ciphertext = cleartext

	size := int64(len(ciphertext))
	offs, err := bp.entry.Size()
	if err != nil {
		return nil, errors.E(op, errors.Invalid, err)
	}

	block := upspin.DirBlock{
		Size:   size,
		Offset: offs,
	}
	bp.entry.Blocks = append(bp.entry.Blocks, block)

	return
}

func (bp *blockPacker) SetLocation(l upspin.Location) {
	bs := bp.entry.Blocks
	bs[len(bs)-1].Location = l
}

func (bp *blockPacker) Close() error {
	return internal.CheckLocationSet(bp.entry)
}

func (p plainPack) Unpack(cfg upspin.Config, d *upspin.DirEntry) (upspin.BlockUnpacker, error) {
	const op = "pack/plain.Unpack"
	if err := pack.CheckPacking(p, d); err != nil {
		return nil, errors.E(op, errors.Invalid, d.Name, err)
	}
	// Call Size to check that the block Offsets and Sizes are consistent.
	if _, err := d.Size(); err != nil {
		return nil, errors.E(op, d.Name, err)
	}
	return &blockUnpacker{
		cfg:          cfg,
		entry:        d,
		BlockTracker: internal.NewBlockTracker(d.Blocks),
	}, nil
}

type blockUnpacker struct {
	cfg                   upspin.Config
	entry                 *upspin.DirEntry
	internal.BlockTracker // provides NextBlock method and Block field
}

func (bp *blockUnpacker) Unpack(ciphertext []byte) (cleartext []byte, err error) {
	cleartext = ciphertext
	return
}

func (bp *blockUnpacker) Close() error {
	return nil
}

// Name implements upspin.Name.
func (p plainPack) Name(cfg upspin.Config, dirEntry *upspin.DirEntry, newName upspin.PathName) error {
	const op = "pack/plain.Name"
	if dirEntry.IsDir() {
		return errors.E(op, errors.IsDir, dirEntry.Name, "cannot rename directory")
	}
	parsed, err := path.Parse(newName)
	if err != nil {
		return errors.E(op, err)
	}
	dirEntry.Name = parsed.Path()
	dirEntry.SignedName = dirEntry.Name
	return nil
}

func (p plainPack) PackLen(cfg upspin.Config, cleartext []byte, entry *upspin.DirEntry) int {
	if err := pack.CheckPacking(p, entry); err != nil {
		return -1
	}
	return len(cleartext)
}

func (p plainPack) UnpackLen(cfg upspin.Config, ciphertext []byte, entry *upspin.DirEntry) int {
	if err := pack.CheckPacking(p, entry); err != nil {
		return -1
	}
	return len(ciphertext)
}
