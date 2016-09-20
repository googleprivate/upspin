// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package upspin

import (
	"crypto/elliptic"
	"crypto/x509"
	"errors"
	"math/big"
)

// A UserName is just an e-mail address representing a user.
// It is given a unique type so the API is clear.
// The user part may contain an optional suffix after a plus sign.
// Examples: gopher@google.com, me+you@forever.com
type UserName string

// A PathName is just a string representing a full path name.
// It is given a unique type so the API is clear.
// Example: gopher@google.com/burrow/hoard
type PathName string

// Transport identifies the type of access required to reach the data, that is, the
// realm in which the network address within a Location is to be interpreted.
type Transport uint8

const (
	// Unassigned indicates a connection to a service that returns an error
	// from every method call. It is useful when a component wants to
	// guarantee it does not access another service.
	// It is also the zero value for Transport.
	Unassigned Transport = iota

	// InProcess indicates that contents are located in the current process,
	// typically in memory.
	InProcess

	// Remote indicates a connection to a remote server through RPC.
	// The Endpoint's NetAddr contains the HTTP address of the remote server.
	Remote

	// HTTPS indicates that contents are stored at regular web services that
	// speak the HTTPS protocol.
	HTTPS
)

// A Location identifies where a piece of data is stored and how to retrieve it.
type Location struct {
	// Endpoint identifies the machine or service where the data resides.
	Endpoint Endpoint

	// Reference is the key that will retrieve the data from the endpoint.
	Reference Reference
}

// An Endpoint identifies an instance of a service, encompassing an address
// such as a domain name and information (the Transport) about how to interpret
// that address.
type Endpoint struct {
	// Transport specifies how the network address is to be interpreted,
	// for instance that it is the URL of an HTTP service.
	Transport Transport

	// NetAddr returns the (typically) network address of the data.
	NetAddr NetAddr
}

// A NetAddr is the network address of service. It is interpreted by Dialer's
// Dial method to connect to the service.
type NetAddr string

// A Reference is the string identifying an item in a StoreServer.
type Reference string

// Signature is an ECDSA signature.
type Signature struct {
	R, S *big.Int
}

// Factotum implements an agent, potentially remote, to handle private key operations.
// Implementations typically provide NewFactotum() to set the key.
type Factotum interface {
	// FileSign ECDSA-signs p|n|t|dkey|hash, as required for EEPack and similar.
	FileSign(n PathName, t Time, dkey, hash []byte) (Signature, error)

	// ScalarMult is the bare private key operator, used in unwrapping packed data.
	// Each call needs security review to ensure it cannot be abused as a signing
	// oracle. Read https://en.wikipedia.org/wiki/Confused_deputy_problem.
	// Returns error "no such key" if factotum doesn't hold the necessary private key.
	ScalarMult(keyHash []byte, c elliptic.Curve, x, y *big.Int) (sx, sy *big.Int, err error)

	// UserSign assists in authenticating to Upspin servers.
	UserSign(hash []byte) (Signature, error)

	// Pop derives a Factotum that defaults to the previous key.
	Pop() Factotum

	// PublicKey returns the user's public key in canonical string format.
	PublicKey() PublicKey

	// PublicKeyFromHash returns the matching public key or an error.
	PublicKeyFromHash(keyHash []byte) (PublicKey, error)
}

// A Packing identifies the technique for turning the data pointed to by
// a key into the user's data. This may involve checksum verification,
// decrypting, signature checking, or nothing at all.
// Secondary data such as encryption keys may be required to implement
// the packing. That data appears in the API as arguments and struct fields
// called packdata.
type Packing uint8

// BlockPacker operates on a DirEntry, packing and signing DirBlocks.
type BlockPacker interface {
	// Pack takes cleartext data and returns the packed ciphertext,
	// appending a DirBlock to the DirEntry.
	//
	// The ciphertext slice remains valid until the next call to Pack.
	Pack(cleartext []byte) (ciphertext []byte, err error)

	// SetLocation updates the Location field of the last-Packed DirBlock.
	SetLocation(Location)

	// Close updates the Signature for the DirEntry.
	Close() error
}

// BlockUnpacker operates on a DirEntry, unpacking and verifying its DirBlocks.
type BlockUnpacker interface {
	// NextBlock returns the next DirBlock in the sequence.
	NextBlock() (DirBlock, bool)

	// Unpack takes ciphertext returns the cleartext. If appropriate, the
	// result is verified as correct according to the block's Packdata.
	//
	// The cleartext slice remains valid until the next call to Unpack.
	Unpack(ciphertext []byte) (cleartext []byte, err error)
}

// Packer provides the implementation of a Packing. The pack package binds
// Packing values to the concrete implementations of this interface.
type Packer interface {
	// Packing returns the integer identifier of this Packing algorithm.
	Packing() Packing

	// String returns the name of this packer.
	String() string

	// Pack returns a BlockPacker that packs blocks
	// into the given DirEntry.
	Pack(Context, *DirEntry) (BlockPacker, error)

	// Unpack returns a BlockUnpacker that unpacks blocks
	// from the given DirEntry.
	Unpack(Context, *DirEntry) (BlockUnpacker, error)

	// PackLen returns an upper bound on the number of bytes required
	// to store the cleartext after packing.
	// PackLen might update the entry's Packdata field.
	// PackLen returns -1 if there is an error.
	PackLen(context Context, cleartext []byte, entry *DirEntry) int

	// UnpackLen returns an upper bound on the number of bytes
	// required to store the unpacked cleartext.
	// UnpackLen might update the entry's Packdata field.
	// UnpackLen returns -1 if there is an error.
	UnpackLen(context Context, ciphertext []byte, entry *DirEntry) int

	// ReaderHashes returns SHA-256 hashes of the public keys able to decrypt the
	// associated ciphertext.
	ReaderHashes(packdata []byte) ([][]byte, error)

	// Share updates each packdata element to enable all the readers,
	// and only those readers, to be able to decrypt the associated ciphertext,
	// which is held separate from this call. It is invoked in response to
	// DirServer updates returning information about entries that need updating due to
	// changes in the set of users with permissions to read the associated items.
	// (TODO: DirServer updates not yet implemented.)
	// In case of error, Share skips processing for that reader or packdata.
	// If packdata[i] is nil on return, it was skipped.
	// Share trusts the caller to check the arguments are not malicious.
	Share(context Context, readers []PublicKey, packdata []*[]byte)

	// Name updates the DirEntry to refer to a new path. If the new
	// path is in a different directory, the wrapped keys are reduced to
	// only that of the Upspin user invoking the method. The Packdata
	// in entry must contain a wrapped key for that user.
	Name(context Context, entry *DirEntry, path PathName) error
}

const (
	// PlainPack is the trivial, no-op packing. Bytes are copied untouched.
	// It is the default packing but is, of course, insecure.
	PlainPack Packing = 0

	// Packings from 1 through 19 are not for production use. This region
	// is reserved for debugging and other temporary packing implementations.

	// DebugPack is available for use in tests for any purpose.
	// It is never used in production.
	DebugPack Packing = 1

	// Packings from 20 and above (as well as PlainPack=0) are fixed in
	// value and semantics and may be used in production.

	// EEPack stores AES-encrypted data, with metadata
	// including an ECDSA signature and ECDH-wrapped keys.
	// (NIST SP 800-57 Pt.1 Rev.4 section 5.6.1)
	// A signature and a per-file symmetric encryption key, wrapped
	// for each reader, are encoded in Packdata.
	// User keys specify a curve:
	// "p256": AES-256, SHA-256, and curve P256; strength 128.
	// "p384": AES-256, SHA-512, and curve P384; strength 192.
	// "p521": AES-256, SHA-512, and curve P521; strength 256.
	// "25519": TODO(ehg) x/crypto/curve25519, github.com/agl/ed25519
	EEPack Packing = 20
)

// User represents all the public information about an Upspin user as returned by KeyServer.
type User struct {
	// Name represents the user's name as an e-mail address, such as joe@smith.com.
	Name UserName

	// Dirs is a slice of DirServer endpoints where the user's root directory may be located.
	// TODO: Provide full documentation.
	Dirs []Endpoint

	// Stores is a slice of StoreServer endpoints where the user's data is primarily written to.
	// TODO: Provide full documentation.
	Stores []Endpoint

	// PublicKey is the user's current public key.
	PublicKey PublicKey
}

// The KeyServer interface provides access to public information about users.
type KeyServer interface {
	Dialer
	Service

	// Lookup returns all public information about a user.
	Lookup(userName UserName) (*User, error)

	// Put sets or updates information about a user. The user's name must
	// match the authenticated user.
	// TODO: Provide full documentation.
	Put(user *User) error
}

// A PublicKey can be given to anyone and used for authenticating a user.
type PublicKey string

// ErrFollowLink indicates that all or part of a path name has evaluated
// to a DirEntry that is a link. In that case, the returned DirEntry
// will be that of the link, and its Name field is guaranteed to be an
// element-wise prefix of the argument path name. The caller should
// retry the operation, substituting that prefix (which may be the
// entire name) with the contents of the Link field of the returned
// DirEntry.
var ErrFollowLink = errors.New("action incomplete: must follow link")

// MaxLinkHops is the maximum number of links that will be followed
// when evaluating a single path name.
const MaxLinkHops = 20

// DirServer manages the name space for one or more users.
type DirServer interface {
	Dialer
	Service

	// Lookup returns the directory entry for the named file.
	//
	// If the returned error is ErrFollowLink, the caller should
	// retry the operation as outlined in the description for
	// ErrFollowLink. Otherwise in the case of error the
	// returned DirEntry will be nil.
	Lookup(name PathName) (*DirEntry, error)

	// Put stores the DirEntry in the directory server. The entry
	// may be a plain file, a link, or a directory. (Only one of
	// these attributes may be set.)
	// In practice the data for the file should be stored in
	// a StoreServer as specified by the blocks in the entry,
	// all of which should be stored with the same packing.
	//
	// Within the DirEntry, several fields have special properties.
	// Time represents a timestamp for the item. It is advisory only
	// but is included in the packing signature and so should usually
	// be set to a non-zero value.
	// Sequence represents a sequence number that is incremented
	// after each Put. If it is neither 0 nor -1, the DirServer will
	// reject the Put operation unless Sequence is the same as that
	// stored in the metadata for the existing item with the same
	// path name. If it is -1, Put will fail if there is already an item
	// with that name.
	//
	// The Name field of the DirEntry identifies where in the directory
	// tree the entry belongs. The SignedName field, which usually has the
	// same value, is the name used to sign the DirEntry to guarantee its
	// security. They may differ if an entry appears in multiple locations,
	// such as in its original location plus within a second tree holding
	// a snapshot of the original tree but starting from a different root.
	//
	// Most software will concern itself only with the Name field unless
	// generating or validating the entry's signature.
	//
	// All but the last element of the path name must already exist
	// and be directories or links. The final element, if it exists,
	// must not be a directory. If something is already stored under
	// the path, the new location and packdata replace the old.
	//
	// If the returned error is ErrFollowLink, the caller should
	// retry the operation as outlined in the description for
	// ErrFollowLink (with the added step of updating the
	// Name field of the argument DirEntry). Otherwise, the
	// returned DirEntry will be nil whether the operation
	// succeeded or not.
	Put(entry *DirEntry) (*DirEntry, error)

	// Glob matches the pattern against the file names of the full
	// rooted tree. That is, the pattern must look like a full path
	// name, but elements of the path may contain metacharacters.
	// Matching is done using Go's path.Match elementwise. The user
	// name must be present in the pattern and is treated as a literal
	// even if it contains metacharacters.
	// If the caller has no read permission for the items named in the
	// DirEntries, the returned Location and Packdata fields are cleared.
	//
	// If the returned error is ErrFollowLink, one or more of the
	// returned DirEntries is a link (the others are completely
	// evaluated). The caller should retry the operation for those
	// DirEntries as outlined in the description for ErrFollowLink,
	// updating the pattern as appropriate. Note that any returned
	// links may only partially match the original argument pattern.
	Glob(pattern string) ([]*DirEntry, error)

	// Delete deletes the DirEntry for a name from the directory service.
	// It does not delete the data it references; use StoreServer.Delete
	// for that. If the name identifies a link, Delete will delete the
	// link itself, not its target.
	//
	// If the returned error is ErrFollowLink, the caller should
	// retry the operation as outlined in the description for
	// ErrFollowLink. (And in that case, the DirEntry will never
	// represent the full path name of the argument.) Otherwise, the
	// returned DirEntry will be nil whether the operation succeeded
	// or not.
	Delete(name PathName) (*DirEntry, error)

	// WhichAccess returns the DirEntry of the Access file that is
	// responsible for the access rights defined for the named item.
	// WhichAccess requires that the calling user have at least one access
	// right granted for the argument name. If not, WhichAccess will return
	// a "does not exist" error, even if the item and/or the Access file
	// exist.
	//
	// If the returned error is ErrFollowLink, the caller should
	// retry the operation as outlined in the description for
	// ErrFollowLink. Otherwise, in the case of error the returned
	// DirEntry will be nil.
	WhichAccess(name PathName) (*DirEntry, error)
}

// Time represents a timestamp in units of seconds since
// the Unix epoch, Jan 1 1970 0:00 UTC.
type Time int64

// DirEntry represents the directory information for a file.
// The blocks of a file represent contiguous data. There are no
// holes and no overlaps and the first block always has offset 0.
// Name and SignedName must not be empty. See comments in DirServer.Put.
type DirEntry struct {
	// Fields contributing to the signature.
	SignedName PathName   // The full path name of the file used for signing.
	Packing    Packing    // Packing used for every block in file.
	Time       Time       // Time associated with file; might be when it was last written.
	Blocks     []DirBlock // Descriptors for each block. A nil or empty slice represents an empty file.
	Packdata   []byte     // Information maintained by the packing algorithm.
	Link       PathName   // The link target, iff the DirEntry has Attr=AttrLink.

	// Field determining the key used for the signature, hence also tamper-resistant.
	Writer UserName // Writer of the file, often the same as owner.

	// Fields not included in the signature.
	Name     PathName  // The full path name of the file. Only the last element can be a link.
	Attr     Attribute // Attributes for the DirEntry.
	Sequence int64     // The sequence (version) number of the item.
}

// DirBlock describes a block of data representing a contiguous section of a file.
// The block may be of any size, including zero bytes.
type DirBlock struct {
	Location Location // Location of data in store.
	Offset   int64    // Byte offset of start of block's data in file.
	Size     int64    // Length of block data in bytes.
	Packdata []byte   // Information maintained by the packing algorithm.
}

// Attribute defines the attributes for a DirEntry.
type Attribute byte

// Supported Attributes.
const (
	// AttrNone is the default attribute, identifying a plain data object.
	AttrNone = Attribute(0)
	// AttrDirectory identifies a directory. It must be the only attribute.
	AttrDirectory = Attribute(1 << 0)
	// AttrLink identifies a link. It must be the only attribute.
	// A link is a path name whose DirEntry identifies another
	// "target" item in the tree, similar to a Unix symbolic link.
	// The target of a link may be another link.
	// The target path is stored in the Link field of the DirEntry.
	// A link DirEntry holds zero DirBlocks.
	AttrLink = Attribute(1 << 1)
)

// Special Sequence numbers.
const (
	SeqNotExist = -1 // Put will fail if item exists.
	SeqIgnore   = 0  // Put will not check sequence number, but will update it.
	SeqBase     = 1  // Base at which valid sequence numbers start.
)

// The StoreServer saves and retrieves data without interpretation.
type StoreServer interface {
	Dialer
	Service

	// Get attempts to retrieve the data identified by the reference.
	// Three things might happen:
	// 1. The data is in this StoreServer. It is returned. The Location slice
	// and error are nil.
	// 2. The data is not in this StoreServer, but may be in one or more
	// other locations known to the store. The slice of Locations
	// is returned. The data slice and error are nil.
	// 3. An error occurs. The data and Location slices are nil
	// and the error describes the problem.
	Get(ref Reference) ([]byte, []Location, error)

	// Put puts the data into the store and returns the reference
	// to be used to retrieve it.
	Put(data []byte) (Reference, error)

	// Delete permanently removes all storage space associated
	// with the reference. After a successful Delete, calls to Get with the
	// same reference will fail. If the reference is not found, an error is
	// returned.
	Delete(ref Reference) error
}

// Client API.

// The Client interface provides a higher-level API suitable for applications
// that wish to access Upspin's name space. When Client evaluates a path
// name and encounters a link, it evaluates the link, iteratively if necessary,
// until it reaches an item that is not a link.
// (The DirServer interface does not evaluate links.)
//
// In methods where a name is evaluated and a DirEntry returned,
// if links were evaluated in processing the operation, the Name field
// of the DirEntry will be different from the argument path name and
// will hold the link-free path to item.
type Client interface {
	// Get returns the clear, decrypted data stored under the given name.
	// It is intended only for special purposes, since it will allocate memory
	// for the entire "blob" to return. Most access will use the file-like
	// API below.
	Get(name PathName) ([]byte, error)

	// Lookup returns the directory entry for the named file. The
	// boolean determines whether, if the final path element is a link,
	// to return the DirEntry for the link (false) or for the target of
	// the link (true).
	Lookup(name PathName, followFinal bool) (*DirEntry, error)

	// Put stores the data at the given name. If something is already
	// stored with that name, it will no longer be available using the
	// name, although it may still exist in the storage server. (See
	// the documentation for Delete.) Like Get, it is not the usual
	// access method. The file-like API is preferred.
	Put(name PathName, data []byte) (*DirEntry, error)

	// PutLink creates a link from the new name to the old name. The
	// new name must not look like the path to an Access or Group file.
	// If something is already stored with the new name, it is first
	// deleted from the directory but its storage is not deleted from
	// the Store. (See the documentation for Delete.) The old name is
	// not evaluated, that is, the resulting link will hold the
	// argument to PutLink even if it refers to a path that itself
	// contains links. The name is canonicalized, however (see
	// path.Clean).
	PutLink(oldName, newName PathName) (*DirEntry, error)

	// PutDuplicate creates a new name for the references referred to
	// by the old name. Subsequent Puts to either name do not effect
	// the contents referred to by the other. There must be no existing
	// item with the new name.
	PutDuplicate(oldName, newName PathName) (*DirEntry, error)

	// MakeDirectory creates a directory with the given name, which
	// must not already exist. All but the last element of the path
	// name must already exist and be directories.
	// TODO: Make multiple elems?
	MakeDirectory(dirName PathName) (*DirEntry, error)

	// Rename renames oldName to newName. The old name is no longer valid.
	Rename(oldName, newName PathName) error

	// Delete deletes the DirEntry associated with the name. The
	// storage referenced by the DirEntry is not explicitly deleted,
	// although the storage server may garbage collect unreferenced
	// data independently. If the final element of the path name is a
	// link, Delete will delete the link itself, not the link target.
	Delete(name PathName) error

	// Glob matches the pattern against the file names of the full
	// rooted tree. That is, the pattern must look like a full path
	// name, but elements of the path may contain metacharacters.
	// Matching is done using Go's path.Match elementwise. The user
	// name must be present in the pattern and is treated as a literal
	// even if it contains metacharacters. Note that if links are
	// evaulated while executing Glob, the Name fields of the returned
	// DirEntries might not match the original argument pattern.
	Glob(pattern string) ([]*DirEntry, error)

	// File-like methods similar to Go's os.File API.
	// The name, however, is a fully-qualified upspin PathName.
	Create(name PathName) (File, error)
	Open(name PathName) (File, error)

	// DirServer returns an error or a reachable bound DirServer for the user.
	DirServer(name PathName) (DirServer, error)
}

// The File interface has semantics and API that parallels a subset
// of Go's os.File's. The main semantic difference, besides the limited
// method set, is that a Read will only return once the entire contents
// have been decrypted and verified.
type File interface {
	// Close releases the resources. For a writable file, it also
	// writes the accumulated data in a StoreServer. After a
	// Close, successful or not, all methods of File except Name
	// will fail.
	Close() error

	// Name returns the full path name of the File.
	Name() PathName

	// Read, ReadAt, Write, WriteAt and Seek implement
	// the standard Go interfaces io.Reader, etc.
	// Because of the nature of upsin storage, the entire
	// item might need to be read into memory by the
	// implementation before Read can return any data.
	// Similarly, Write might accumulate all data and only
	// flush to storage once Close is called.
	Read(b []byte) (n int, err error)
	ReadAt(b []byte, off int64) (n int, err error)
	Write(b []byte) (n int, err error)
	WriteAt(b []byte, off int64) (n int, err error)
	Seek(offset int64, whence int) (ret int64, err error)
}

// Context contains client information such as the user's keys and
// preferred KeyServer, DirServer, and StoreServer endpoints.
type Context interface {
	// The name of the user requesting access.
	UserName() UserName

	// Factotum holds the user's cryptographic keys and encapsulates crypto operations.
	Factotum() Factotum

	// Packing is the default Packing to use when creating new data items.
	// It may be overridden by circumstances such as preferences related
	// to the directory.
	Packing() Packing

	// KeyEndpoint is the endpoint of the KeyServer to contact to retrieve keys.
	KeyEndpoint() Endpoint

	// DirEndpoint is the endpoint of the DirServer in which to place new data items.  It is
	// usually the location of the user's root.
	DirEndpoint() Endpoint

	// StoreEndpoint is the endpoint of the StoreServer in which to place new data items.
	StoreEndpoint() Endpoint

	// StoreCacheEndpoint is the endpoint of the StoreServer in which to place new data items.
	StoreCacheEndpoint() Endpoint

	// CertPool returns the x509 certificate pool
	// used to validate client TLS connections.
	CertPool() *x509.CertPool
}

// Dialer defines how to connect and authenticate to a server. Each
// service type (KeyServer, DirServer, StoreServer) implements the methods of
// the Dialer interface. These methods are not used directly by
// clients. Instead, clients should use the methods of
// the Upspin "bind" package to connect to services.
type Dialer interface {
	// Dial connects to the service and performs any needed authentication.
	Dial(Context, Endpoint) (Service, error)
}

// Service is the general interface returned by a dialer. It includes
// methods to configure the service and report its setup.
type Service interface {
	// Configure configures a service once it has been dialed.
	// The details of the configuration are implementation-defined.
	Configure(options ...string) (UserName, error)

	// Endpoint returns the network endpoint of the server.
	Endpoint() Endpoint

	// Ping reports whether the Service is reachable.
	Ping() bool

	// Close closes the connection to the service and releases all resources used.
	// A Service may not be re-used after close.
	Close()
}
