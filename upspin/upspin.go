package upspin

import (
	"crypto/elliptic"
	"math/big"
)

// A UserName is just a string representing a user.
// It is given a unique type so the API is clear.
// Example: gopher@google.com
type UserName string

// A PathName is just a string representing a full path name.
// It is given a unique type so the API is clear.
// Example: gopher@google.com/burrow/hoard
type PathName string

// Transport identifies the type of access required to reach the data, that is, the
// realm in which the network address within a Location is to be interpreted.
type Transport uint8

const (
	// InProcess indicates that contents are located in the current process,
	// typically in memory.
	InProcess Transport = iota

	// GCP indicates a Google Cloud Store instance.
	GCP

	// Remote indicates a connection to a remote server through RPC.
	// The Endpoint's NetAddr contains the HTTP address of the remote server.
	Remote
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

// A Reference is the string identifying an item in a Store.
type Reference string

// Signature is an ECDSA signature.
type Signature struct {
	R, S *big.Int
}

// Factotum implements an agent, potentially remote, to handle private key operations.
// Think of this as a replacement for *PrivateKey, or as an ssh-agent.
// Implementations typically provide NewFactotum() to set the key.
type Factotum interface {
	// FileSign ECDSA-signs p|n|t|dkey|hash, as required for EEp256Pack and similar.
	FileSign(p Packing, n PathName, t Time, dkey, hash []byte) (Signature, error)

	// ScalarMult is the bare private key operator, used in unwrapping packed data.
	// Each call needs security review to ensure it cannot be abused as a signing
	// oracle. Read https://en.wikipedia.org/wiki/Confused_deputy_problem.
	ScalarMult(c elliptic.Curve, x, y *big.Int) (sx, sy *big.Int)

	// UserSign assists in authenticating to Upspin servers.
	UserSign(hash []byte) (Signature, error)

	// PackingString returns the Packing.String() value associated with the key.
	PackingString() string
}

// Packdata stores the encoded information used to pack the data in an
// item, such decryption keys. The first byte identifies the Packing
// used to store the information; the rest of the slice is the data
// itself.
type Packdata []byte

// A Packing identifies the technique for turning the data pointed to by
// a key into the user's data. This may involve checksum verification,
// decrypting, signature checking, or nothing at all.
// Secondary data such as encryption keys may be required to implement
// the packing. That data appears in the API as arguments and struct fields
// called packdata.
type Packing uint8

// Packer provides the implementation of a Packing. The pack package binds
// Packing values to the concrete implementations of this interface.
type Packer interface {
	// Packing returns the integer identifier of this Packing algorithm.
	Packing() Packing

	// String returns the name of this packer.
	String() string

	// Pack takes cleartext data and encodes it
	// into the ciphertext slice. The ciphertext and cleartext slices
	// must not overlap. Pack might update the entry's Metadata, which
	// must not be nil but might have a nil Packdata field. If the
	// Packdata has length>0, the first byte must be the correct value
	// of Packing. Upon return the Packdata will be updated with the
	// correct information to unpack the ciphertext.
	// The ciphertext slice must be large enough to hold  the result;
	// the PackLen method may be used to find a suitable size to
	// allocate.
	// The returned count is the written length of the ciphertext.
	Pack(context *Context, ciphertext, cleartext []byte, entry *DirEntry) (int, error)

	// Unpack takes ciphertext data and stores the cleartext version
	// in the cleartext slice, which must be large enough, using the
	// Packdata field of the Metadata in the DirEntry to recover
	// keys and other necessary information.
	// If appropriate, the result is verified as correct according
	// to items such as the path name and time stamp in the Metadata.
	// The ciphertext and cleartext slices must not overlap.
	// Unpack might update the Metadata field of the DirEntry using
	// data recovered from the Packdata. The incoming Packdata must
	// must have the correct Packing value already present in its
	// first byte.
	// Unpack returns the number of bytes written to the slice.
	Unpack(context *Context, cleartext, ciphertext []byte, entry *DirEntry) (int, error)

	// PackLen returns an upper bound on the number of bytes required
	// to store the cleartext after packing.
	// PackLen might update the entry's Metadata.Packdata field, which
	// must not be nil but might have a nil Packdata field. If it has
	// length greather than 0,  the first byte must be the correct
	// value of Packing.
	// PackLen returns -1 if there is an error.
	PackLen(context *Context, cleartext []byte, entry *DirEntry) int

	// UnpackLen returns an upper bound on the number of bytes
	// required to store the unpacked cleartext.  UnpackLen might
	// update the entry's Metadata, which must have the correct Packing
	// value already present in Packdata[0].
	// UnpackLen eturns -1 if there is an error.
	UnpackLen(context *Context, ciphertext []byte, entry *DirEntry) int

	// Share updates each packdata element to enable all the readers,
	// and only those readers, to be able to decrypt the associated ciphertext,
	// which is held separate from this call. It is invoked in response to
	// Directory updates returning information about entries that need updating due to
	// changes in the set of users with permissions to read the associated items.
	// In case of error, Share skips processing for that reader or packdata.
	// If packdata[i] is nil on return, it was skipped.
	// Share trusts the caller to check the arguments are not malicious.
	Share(context *Context, readers []PublicKey, packdata []*[]byte)

	// Name updates the DirEntry to refer to a new path. If the new
	// path is in a different directory, the wrapped keys are reduced to
	// only that of the Upspin user invoking the method. The Packdata
	// in entry must contain a wrapped key for that user.
	Name(context *Context, entry *DirEntry, path PathName) error
}

const (
	// PlainPack is the trivial, no-op packing. Bytes are copied untouched.
	// It is the default packing but is, of course, insecure.
	PlainPack Packing = 0

	// Packings from 1 through 16 are not for production use. This region
	// is reserved for debugging and other temporary packing implementations.

	// DebugPack is available for use in tests for any purpose.
	// It is never used in production.
	DebugPack Packing = 1

	// Packings from 16 and above (as well as PlainPack=0) are fixed in
	// value and semantics and may be used in production.

	// EEp256Pack and EEp521Pack store AES-encrypted data, with metadata
	// including an ECDSA signature and ECDH-wrapped keys.
	// See NIST SP 800-57 Pt.1 Rev.4 section 5.6.1
	// These use: pathname, cleartext, context.Factotum, context.Packing,
	// Metadata.Time, public keys of readers.  They generate a per-file
	// symmetric encryption key, which they encode in Packdata.
	// EEp256Pack uses AES-128, SHA-256, and curve P256; strength 128.
	EEp256Pack Packing = 16
	// EEp384Pack uses AES-256, SHA-512, and curve P384; strength 192.
	EEp384Pack Packing = 18
	// EEp521Pack uses AES-256, SHA-512, and curve P521; strength 256.
	EEp521Pack Packing = 17
	// Ed25519Pack is a TODO packer
	Ed25519Pack Packing = 19 // TODO(ehg) x/crytpo/curve25519, github.com/agl/ed25519
)

// User service.

// The User interface provides access to public information about users.
type User interface {
	Dialer
	Service

	// Lookup returns a list (slice) of Endpoints of Directory
	// services that may hold the root directory for the named
	// user and a list (slice) of public keys for that user. Those
	// earlier in the lists are better places to look.
	Lookup(userName UserName) ([]Endpoint, []PublicKey, error)
}

// A PublicKey can be given to anyone and used for authenticating a User.
type PublicKey string

// A PrivateKey is paired with PublicKey but never leaves the Client.
// (This is largely being replaced by Factotum;  avoid new uses.)
type PrivateKey string

// A KeyPair is used when exchanging data with other users. It
// always contains both the public and private keys.
// (This is largely being replaced by Factotum;  avoid new uses.)
type KeyPair struct {
	Public  PublicKey
	Private PrivateKey
}

// Directory service.

// The Directory service manages the name space for one or more users.
type Directory interface {
	Dialer
	Service

	// Lookup returns the directory entry for the named file.
	Lookup(name PathName) (*DirEntry, error)

	// Put has the directory service record that the specified DirEntry
	// describes data stored in a Store service at the Location recorded
	// in the DirEntry, and can thereafter be recovered using the PathName
	// specified in the DirEntry.
	//
	// Before calling Put, the data must be packed using the same
	// Metadata object, which the Packer might update. That is,
	// after calling Pack, the Metadata should not be modified
	// before calling Put.
	//
	// Within the Metadata, several fields have special properties.
	// Size represents the size of the original, unpacked data as
	// seen by the client. It is advisory only and is unchecked.
	// Time represents a timestamp for the item. It is advisory only
	// but is included in the packing signature and so should usually
	// be set to a non-zero value.
	// Sequence represents a sequence number that is incremented
	// after each Put. If it is non-zero, the Directory service will
	// reject the Put operation unless Sequence is the same as that
	// stored in the metadata for the existing item with the same
	// path name.
	//
	// All but the last element of the path name must already exist
	// and be directories. The final element, if it exists, must not
	// be a directory. If something is already stored under the path,
	// the new location and packdata replace the old.
	Put(entry *DirEntry) error

	// MakeDirectory creates a directory with the given name, which
	// must not already exist. All but the last element of the path
	// name must already exist and be directories.
	// TODO: Make multiple elems?
	MakeDirectory(dirName PathName) (Location, error)

	// Glob matches the pattern against the file names of the full
	// rooted tree. That is, the pattern must look like a full path
	// name, but elements of the path may contain metacharacters.
	// Matching is done using Go's path.Match elementwise. The user
	// name must be present in the pattern and is treated as a literal
	// even if it contains metacharacters.
	// If the caller has no read permission for the items named in the
	// DirEntries, the returned Locations and Packdata fields are cleared.
	Glob(pattern string) ([]*DirEntry, error)

	// Delete deletes the DirEntry for a name from the directory service.
	// It does not delete the data it references; use Store.Delete for that.
	Delete(name PathName) error

	// WhichAccess returns the path name of the Access file that is
	// responsible for the access rights defined for the named item.
	// If there is no such file, that is, there are no Access files that
	// apply, it returns the empty string.
	WhichAccess(name PathName) (PathName, error)
}

// Time represents a timestamp in units of seconds since
// the Unix epoch, Jan 1 1970 0:00 UTC.
type Time int64

// DirEntry represents the directory information for a file.
type DirEntry struct {
	Name     PathName // The full path name of the file.
	Location Location // The location of the file.
	Metadata Metadata
}

// FileAttributes define the attributes for a DirEntry.
type FileAttributes byte

const (
	AttrNone      = FileAttributes(0)
	AttrDirectory = FileAttributes(1 << 0)
	AttrRedirect  = FileAttributes(1 << 1)
)

// Metadata stores (among other things) the keys that enable the
// file to be decrypted by the appropriate recipient.
type Metadata struct {
	Attr     FileAttributes // File attributes.
	Sequence int64          // The sequence (version) number of the item.
	Size     uint64         // Length of file in bytes.
	Time     Time           // Time associated with file; might be when it was last written.
	Packdata []byte         // Packing-specific metadata stored in directory.
}

// Store service.

// The Store service saves and retrieves data without interpretation.
type Store interface {
	Dialer
	Service

	// Get attempts to retrieve the data identified by the reference.
	// Three things might happen:
	// 1. The data is in this Store. It is returned. The Location slice
	// and error are nil.
	// 2. The data is not in this Store, but may be in one or more
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
	// same key will fail. If a key is not found, an error is
	// returned.
	Delete(ref Reference) error
}

// Client API.

// The Client interface provides a higher-level API suitable for applications
// that wish to access Upspin's name space.
type Client interface {
	// Get returns the clear, decrypted data stored under the given name.
	// It is intended only for special purposes, since it will allocate memory
	// for the entire "blob" to return. Most access will use the file-like
	// API below.
	Get(name PathName) ([]byte, error)

	// Put stores the data at the given name. If something is already
	// stored with that name, it is replaced with the new data.
	// Like Get, it is not the usual access method. The file-like API
	// is preferred.
	Put(name PathName, data []byte) (Location, error)

	// MakeDirectory creates a directory with the given name, which
	// must not already exist. All but the last element of the path name
	// must already exist and be directories.
	// TODO: Make multiple elems?
	MakeDirectory(dirName PathName) (Location, error)

	// Glob matches the pattern against the file names of the full rooted tree.
	// That is, the pattern must look like a full path name, but elements of the
	// path may contain metacharacters. Matching is done using Go's path.Match
	// elementwise. The user name must be present in the pattern and is treated as
	// a literal even if it contains metacharacters.
	// The Metadata contains no key information.
	Glob(pattern string) ([]*DirEntry, error)

	// File-like methods similar to Go's os.File API.
	// The name, however, is a fully-qualified upspin PathName.
	// TODO: Should there be a concept of current directory and
	// local names?
	Create(name PathName) (File, error)
	Open(name PathName) (File, error)

	// Directory returns an error or a reachable bound Directory for the user.
	Directory(name PathName) (Directory, error)

	// PublicKeys returns an error or a slice of public keys for the user.
	PublicKeys(name PathName) ([]PublicKey, error)

	// Link creates a new name for the reference referred to by the old name.
	// The old name is still a valid name for the reference.
	Link(oldName, newName PathName) (*DirEntry, error)

	// Rename renames oldName to newName. The old name is no longer valid.
	Rename(oldName, newName PathName) error
}

// The File interface has semantics and API that parallels a subset
// of Go's os.File's. The main semantic difference, besides the limited
// method set, is that a Read will only return once the entire contents
// have been decrypted and verified.
type File interface {
	// Close releases the resources. For a writable file, it also
	// writes the accumulated data in a Store server. After a
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
// preferred User, Directory, and Store servers.
type Context struct {
	// The name of the user requesting access.
	UserName UserName

	// KeyPair holds the user's private cryptographic keys.
	KeyPair  KeyPair
	Factotum Factotum // TODO Factotum will replace KeyPair.Private

	// Packing is the default Packing to use when creating new data items.
	// It may be overridden by circumstances such as preferences related
	// to the directory.
	Packing Packing

	// User is the User service to contact when evaluating names.
	User User

	// Directory is the Directory in which to place new data items,
	// usually the location of the user's root.
	Directory Directory

	// Store is the Store in which to place new data items.
	Store Store
}

// Dialer defines how to connect and authenticate to a server. Each
// service type (User, Directory, Store) implements the methods of
// the Dialer interface. These methods are not used directly by
// clients. Instead, clients should use the methods of
// the Upspin "bind" package to connect to services.
type Dialer interface {
	// Dial connects to the service and performs any needed authentication.
	Dial(*Context, Endpoint) (Service, error)
}

// Service is the general interface returned by a dialer. It includes
// methods to configure the service and report its setup.
type Service interface {
	// Configure configures a service once it has been dialed.
	// The details of the configuration are implementation-defined.
	Configure(options ...string) error

	// Endpoint returns the network endpoint of the server.
	Endpoint() Endpoint

	// ServerUserName returns the authenticated user name of the server.
	// If there is no authenticated name an empty string is returned.
	// TODO(p): Should I distinguish a server which didn't pass authentication
	// from one which has no user name?
	ServerUserName() string
}
