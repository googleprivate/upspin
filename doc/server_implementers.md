# Upspin server implementors guide

This document describes how to implement an Upspin Store and Directory server
in Go. It explains the core concepts and interfaces, the available support
packages, and deployment concerns.

Throughout the document we use the demoserver as an example.
You can install it with `go get`:

```
$ go get upspin.io/exp/cmd/demoserver
```

Most of the types discussed in this document are declared in package `upspin`.
You should consult
[the thorough documentation](https://godoc.org/upspin.io/upspin/)
when implementing Upspin servers.

## Config

## Endpoints

An [Endpoint](https://godoc.org/upspin.io/upspin/#Endpoint)
identifies a means for connecting to a Service.

```
type Endpoint struct {
	Transport Transport
	NetAddr   NetAddr
}
```

The Transport field specifies how to connect to the service,
which in almost all cases is over the network (upspin.Remote).
The NetAddr field sepecifies the network address (such as "localhost:9999").

Each Service must have an Endpoint.
Typically the NetAddr component of a Service's Endpoint is provided as the
command-line argument -addr, available to your program as flags.NetAddr.

```
addr := upspin.NetAddr(flags.NetAddr)
ep := upspin.Endpoint{
	Transport: upspin.Remote,
	NetAddr:   addr,
}
```

## Services

The three core Upspin services (KeyServer, StoreServer, and DirServer) each
implement the [Service](https://godoc.org/upspin.io/upspin/#Service) interface.

```
type Service interface {
	Endpoint() Endpoint
	Ping() bool
	Close()
}
```

The implementation of these methods is trivial.

## The Dialer interface

The core Upspin services also implement the
[Dialer](https://godoc.org/upspin.io/upspin/#Dialer) interface.

```
type Dialer interface {
	Dial(Config, Endpoint) (Service, error)
}
```

The Dial method returns an instance of the Service for the given Config's User.

If your service doesn't perform any kind of User-based access controls
then its implementation could be as simple as this:

```
func (s *service) Dial(upspin.Config, upspin.Endpoint) (upspin.Service, error) {
	return s, nil
}
```

More often, a Dial method will return a copy of the Service that serves
the given user:

```
func (s *service) Dial(cfg upspin.Config, _ upspin.Endpoint) (upspin.Service, error) {
	ss := *s
	s.user = cfg.UserName()
	return &ss, nil
}
```

Other methods of the Service may then use its `user` field for access control
(authenticating and/or validating reads, writes, etc).

## The StoreServer interface

An Upspin [StoreServer](https://godoc.org/upspin.io/upspin/#StoreServer)
stores blobs of data.

```
type StoreServer interface {
	Dialer
	Service
	Get(ref Reference) ([]byte, *Refdata, []Location, error)
	Put(data []byte) (*Refdata, error)
	Delete(ref Reference) error
}
```

```
type Refdata struct {
	Reference Reference
	Volatile  bool
	Duration  time.Duration
}
```


## The DirServer interface

[DirServer](https://godoc.org/upspin.io/upspin/#StoreServer)

```
type DirServer interface {
	Dialer
	Service
	Lookup(name PathName) (*DirEntry, error)
	Put(entry *DirEntry) (*DirEntry, error)
	Delete(name PathName) (*DirEntry, error)
	WhichAccess(name PathName) (*DirEntry, error)
	Watch(name PathName, order int64, done <-chan struct{}) (<-chan Event, error)
}
```


## Serving RPC methods

Upspin uses a simple RPC implementation that sends [Protocol Buffers](TODO) over HTTP.
The `upspin.io/rpc` package provides both the server and client implementations.

Once you have implemented an StoreServer or DirServer, you may use the wrappers
The `upspin.io/rpc/storeserver` and `upspin.io/rpc/dirserver` provide functions
that take a `StoreServer` or `DirServer` (repsectively) and return it as an
`http.Handler`, which may then be served by the `net/http` package's HTTP
server.

```
var (
	myStore upspin.StoreServer
	myDir   upspin.DirServer
	cfg     upspin.Config
)
http.Handle("/api/Store/", storeserver.New(cfg, myStore, addr))
http.Handle("/api/Dir/", dirserver.New(cfg, myDir, addr))
```

## Testing with `upbox`

