// Package client implements a simple client service talking to services
// running anywhere (GCP, InProcess, etc).
package client

import (
	"fmt"

	"upspin.googlesource.com/upspin.git/access"
	"upspin.googlesource.com/upspin.git/bind"
	"upspin.googlesource.com/upspin.git/client/common/file"
	"upspin.googlesource.com/upspin.git/pack"
	"upspin.googlesource.com/upspin.git/path"
	"upspin.googlesource.com/upspin.git/upspin"

	// Plain packer used when encoding an Access file.
	_ "upspin.googlesource.com/upspin.git/pack/plain"
)

// Client implements upspin.Client.
type Client struct {
	context *upspin.Context
}

var _ upspin.Client = (*Client)(nil)

var (
	zeroLoc upspin.Location
)

// New creates a Client. The client finds the servers according to the given Context.
func New(context *upspin.Context) upspin.Client {
	return &Client{
		context: context,
	}
}

// Put implements upspin.Client.
func (c *Client) Put(name upspin.PathName, data []byte) (upspin.Location, error) {
	dir, err := c.Directory(name)
	if err != nil {
		return zeroLoc, err
	}

	_, err = path.Parse(name)
	if err != nil {
		return zeroLoc, err
	}

	var packer upspin.Packer
	if access.IsAccessFile(name) || access.IsGroupFile(name) {
		packer = pack.Lookup(upspin.PlainPack)
	} else {
		// Encrypt data according to the preferred packer
		// TODO: Do a Lookup in the parent directory to find the overriding packer.
		packer = pack.Lookup(c.context.Packing)
		if packer == nil {
			return zeroLoc, fmt.Errorf("unrecognized Packing %d for %q", c.context.Packing, name)
		}
	}

	de := &upspin.DirEntry{
		Name: name,
		Metadata: upspin.Metadata{
			Time:     upspin.Now(),
			Sequence: 0, // Don't care for now.
			Size:     uint64(len(data)),
		},
	}

	var cipher []byte

	// Get a buffer big enough for this data
	cipherLen := packer.PackLen(c.context, data, de)
	if cipherLen < 0 {
		return zeroLoc, fmt.Errorf("PackLen failed for %q", name)
	}
	cipher = make([]byte, cipherLen)
	n, err := packer.Pack(c.context, cipher, data, de)
	if err != nil {
		return zeroLoc, err
	}
	cipher = cipher[:n]

	// Store it.
	ref, err := c.context.Store.Put(cipher)
	if err != nil {
		return zeroLoc, err
	}
	de.Location = upspin.Location{
		Endpoint:  c.context.Store.Endpoint(),
		Reference: ref,
	}
	// Record it.
	readers, paths, err := dir.Put(de)

	// Update sharing information as requested.
	if len(readers) > 0 {
		// TODO(ehg) if not Access change, warn if path!=de?
		readerkeys := make([]upspin.PublicKey, len(readers))
		for i, r := range readers {
			_, pubkeys, err := c.context.User.Lookup(r)
			if err != nil {
				// really bad if we're given a bogus reader name!
				return de.Location, err
			}
			readerkeys[i] = pubkeys[0]
		}
		direntries := make([]*upspin.DirEntry, len(paths))
		packdata := make([]*[]byte, len(paths))
		for i, p := range paths {
			direntries[i], err = dir.Lookup(p)
			if err != nil {
				// really bad if we're given a bogus path name!
				return de.Location, err
			}
			packdata[i] = &direntries[i].Metadata.Packdata
		}
		packer.Share(c.context, readerkeys, packdata)
		for i, pd := range packdata {
			if pd == nil {
				continue
			}
			direntries[i].Metadata.Packdata = *pd
			_, _, err = dir.Put(direntries[i])
			if err != nil {
				break
			}
		}
	}

	return de.Location, err
}

// MakeDirectory implements upspin.Client.
func (c *Client) MakeDirectory(dirName upspin.PathName) (upspin.Location, error) {
	dir, err := c.Directory(dirName)
	if err != nil {
		return zeroLoc, err
	}
	return dir.MakeDirectory(dirName)
}

// Get implements upspin.Client.
func (c *Client) Get(name upspin.PathName) ([]byte, error) {
	dir, err := c.Directory(name)
	if err != nil {
		return nil, err
	}
	entry, err := dir.Lookup(name)
	if err != nil {
		return nil, err
	}

	// firstError remembers the first error we saw. If we fail completely we return it.
	var firstError error
	// isError reports whether err is non-nil and remembers it if it is.
	isError := func(err error) bool {
		if err == nil {
			return false
		}
		if firstError == nil {
			firstError = err
		}
		return true
	}

	// where is the list of locations to examine. It is updated in the loop.
	where := []upspin.Location{entry.Location}
	for i := 0; i < len(where); i++ { // Not range loop - where changes as we run.
		loc := where[i]
		store, err := bind.Store(c.context, loc.Endpoint)
		if isError(err) {
			continue
		}
		cipher, locs, err := store.Get(loc.Reference)
		if isError(err) {
			continue // locs guaranteed to be nil.
		}
		if locs == nil && err == nil {
			// Encrypted data was found. Need to unpack it.
			// TODO(p,edpin): change when GCP makes the indirected reference
			// have the correct packing info.
			packer := pack.Lookup(entry.Metadata.Packing())
			if packer == nil {
				return nil, fmt.Errorf("client: unrecognized Packing %d for %q", entry.Metadata.Packing(), name)
			}
			clearLen := packer.UnpackLen(c.context, cipher, entry)
			if clearLen < 0 {
				return nil, fmt.Errorf("client: UnpackLen failed for %q", name)
			}
			cleartext := make([]byte, clearLen)
			n, err := packer.Unpack(c.context, cleartext, cipher, entry)
			if err != nil {
				return nil, err // Showstopper.
			}
			return cleartext[:n], nil
		}
		// Add new locs to the list. Skip ones already there - they've been processed. TODO: n^2.
	outer:
		for _, newLoc := range locs {
			for _, oldLoc := range where {
				if oldLoc == newLoc {
					continue outer
				}
			}
			where = append(where, newLoc)
		}
	}
	// TODO: custom error types.
	if firstError != nil {
		return nil, firstError
	}
	return nil, fmt.Errorf("client: %q not found on any store server", name)
}

// Glob implements upspin.Client.
func (c *Client) Glob(pattern string) ([]*upspin.DirEntry, error) {
	dir, err := c.Directory(upspin.PathName(pattern))
	if err != nil {
		return nil, err
	}
	return dir.Glob(pattern)
}

// Create implements upspin.Client.
func (c *Client) Create(name upspin.PathName) (upspin.File, error) {
	// TODO: Make sure directory exists?
	return file.Writable(c, name), nil
}

// Open implements upspin.Client.
func (c *Client) Open(name upspin.PathName) (upspin.File, error) {
	data, err := c.Get(name)
	if err != nil {
		return nil, err
	}
	return file.Readable(c, name, data), nil
}

// Directory implements upspin.Client.
func (c *Client) Directory(name upspin.PathName) (upspin.Directory, error) {
	parsed, err := path.Parse(name)
	if err != nil {
		return nil, err
	}
	var endpoints []upspin.Endpoint
	if parsed.User == c.context.UserName {
		endpoints = append(endpoints, c.context.Directory.Endpoint())
	}
	if eps, _, err := c.context.User.Lookup(parsed.User); err == nil {
		endpoints = append(endpoints, eps...)
	}
	var dir upspin.Directory
	for _, e := range endpoints {
		dir, err = bind.Directory(c.context, e)
		if dir != nil {
			return dir, nil
		}
	}
	if err == nil {
		err = fmt.Errorf("client: no endpoint for user %q", parsed.User)
	}
	return nil, err
}

// PublicKeys implements upspin.PublicKeys.
func (c *Client) PublicKeys(name upspin.PathName) ([]upspin.PublicKey, error) {
	parsed, err := path.Parse(name)
	if err != nil {
		return nil, err
	}
	var pubKeys []upspin.PublicKey
	if parsed.User == c.context.UserName {
		pubKeys = append(pubKeys, c.context.KeyPair.Public)
	}
	if _, pks, err := c.context.User.Lookup(parsed.User); err == nil {
		pubKeys = append(pubKeys, pks...)
	}
	if len(pubKeys) == 0 {
		return nil, fmt.Errorf("client: no public keys for user %q", parsed.User)
	}
	return pubKeys, nil
}
