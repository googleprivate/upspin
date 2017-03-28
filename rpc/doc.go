// Copyright 2017 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package rpc provides a framework for implementing RPC servers and clients.

RPC wire protocol

The protocol for a particular method is to send an HTTP request with the appropriate
request message to an "api" URL with the server type and method name; the response
will then be returned. Both request and response are sent as binary protocol
buffers, as declared by upspin.io/upspin/proto.  The encoding procedure is described
in more detail below.

As an example, to call the Put method of the Store server running at store.example.com,
send to the URL,

     https://store.example.com/api/Store/Put

a POST request with, as payload, an encoded StorePutRequest. The response will be
a StorePutResponse. There is such a pair of protocol buffer messages for each
method.

For streaming RPC methods, the requests are the same, but the response is sent as
the bytes "OK" followed by a series of encoded protocol buffers.  Each encoded
message is preceded by a four byte, big-endian-encoded int32 that describes the
length of the following encoded protocol buffer.  The stream is considered closed
when the HTTP response stream ends.

If an error occurs while processing a request, the server returns a 500 Internal
Server Error status code and the response body contains the error string.

Encoding

The arguments to each method, and the returned values, are encoded as protocol
buffers at the top level. These protocol buffers are declared in the package
upspin.io/upspin/proto and its .proto definition file,
upspin.io/upspin/proto/upspin.proto.

Unlike some other systems that use protocol buffers, Upspin does not use the types
defined in upspin.proto internally. The protocol buffer types are used for transport
only, and so the internal types must be transcoded for use on the wire.

Some of the transcoding is straightforward. The scalar types Transport, NetAddr,
Reference, UserName, PathName, and so on are transmitted as their underlying scalar
types, int or string.  (Strings are considered scalars here as they are basic types
in both Go and protocl buffers.) The structured types Endpoint, Location, Refdata
and User themselves contain only scalar types and so are converted field by field.
There are helper functions in the upspin.io/upspin/proto package to automate the
conversion of these types. For instance, the function proto.UpspinLocations converts
from a slice of protocol buffer Location structs to a slice of the internal type,
upspin.Location.

The DirEntry type and its component DirBlock are treated a little differently.  In
the upspin.io/upspin package, which defines these types, there are Marshal and
Unmarshal methods that encode and decode these types to byte slices.  (The details
of these encodings are listed below.) Thus within the protocol buffer types, these
appear as proto type "bytes".

The errors.Error type is also handled specially so its full functionality can be
accessed across the wire. Methods defined in the errors package do the marshaling
and unmarshaling of Error values.

How to Marshal

The fields of a structured type that marshals to a byte slice (one of DirBlock,
DirEntry, or Error) are encoded as follows using the methods of the Go package
encoding/binary to marshal their underlying type:

	int:
		varint
	string:
		varint count n followed by n bytes
	byte slice:
		varint count n followed by n bytes

Encoding of DirBlock

The fields of a DirBlock are encoded using the rules above in the following order:

	Location.Endpoint.Transport: 1 byte
	Location.Endpoint.NetAddr: as string
	Location.Reference: as string
	Offset: as int
	Size: as int
	Packdata: as bytes

Encoding of DirEntry

The fields of a DirEntry are encoded using the rules above in the following order:

	SignedName: as string
	Packing: 1 byte
	Time: as int
	Blocks:
		len(Blocks): as int
		Blocks: n DirBlocks as described above.
	Packdata: as bytes
	Link: as string
	Writer: as string
	Name: if different from SignedName, as string
		Otherwise, 1 byte with value 0.
	Attr: 1 byte
	Sequence: as int

Encoding of Error

The fields of an Errror are encoded using the rules above in the following order.

	Path: as string
	User: as string
	Op: as string
	Kind: as int
	Error:
		If of type Error, as described here.
		Otherwise the value of Error.Error() as string.

The Error value is always marshaled by address (*Error); if the pointer is nil,
nothing is marshaled.  Similarly, for a nil Error.Err field, nothing is marshaled.

*/
package rpc
