// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Dirserver is a wrapper for a directory implementation that presents it as a grpc interface.
package main

import (
	"flag"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"upspin.io/auth"
	"upspin.io/auth/grpcauth"
	"upspin.io/bind"
	"upspin.io/cloud/https"
	"upspin.io/context"
	"upspin.io/errors"
	"upspin.io/log"
	"upspin.io/metric"
	"upspin.io/upspin"
	"upspin.io/upspin/proto"

	gContext "golang.org/x/net/context"

	// TODO: Which of these are actually needed?

	// Load useful packers
	_ "upspin.io/pack/debug"
	_ "upspin.io/pack/ee"
	_ "upspin.io/pack/plain"

	// Load required transports
	_ "upspin.io/dir/transports"
	_ "upspin.io/key/transports"
	_ "upspin.io/store/transports"
)

var (
	httpsAddr    = flag.String("https_addr", "localhost:8000", "HTTPS listen address")
	ctxfile      = flag.String("context", filepath.Join(os.Getenv("HOME"), "/upspin/rc.dirserver"), "context file to use to configure server")
	endpointFlag = flag.String("endpoint", "inprocess", "endpoint of remote service")
	project      = flag.String("project", "", "The GCP project name, if any.")
	configFile   = flag.String("configfile", "", "Name of file with config parameters with one key=value per line")
)

// Server is a SecureServer that talks to a DirServer interface and serves gRPC requests.
type Server struct {
	context  upspin.Context
	endpoint upspin.Endpoint
	// Automatically handles authentication by implementing the Authenticate server method.
	grpcauth.SecureServer
}

const serverName = "dirserver"

func main() {
	flag.Parse()

	if *project != "" {
		log.Connect(*project, serverName)
		svr, err := metric.NewGCPSaver(*project, "serverName", serverName)
		if err != nil {
			log.Fatalf("Can't start a metric saver for GCP project %q: %s", *project, err)
		} else {
			metric.RegisterSaver(svr)
		}
	}

	// Load context and keys for this server. It needs a real upspin username and keys.
	ctxfd, err := os.Open(*ctxfile)
	if err != nil {
		log.Fatal(err)
	}
	defer ctxfd.Close()
	context, err := context.InitContext(ctxfd)
	if err != nil {
		log.Fatal(err)
	}

	endpoint, err := upspin.ParseEndpoint(*endpointFlag)
	if err != nil {
		log.Fatalf("endpoint parse error: %v", err)
	}

	// Get an instance so we can configure it and use it for authenticated connections.
	dir, err := bind.DirServer(context, *endpoint)
	if err != nil {
		log.Fatal(err)
	}

	// If there are configuration options, set them now.
	if *configFile != "" {
		opts := parseConfigFile(*configFile)
		// Configure it appropriately.
		log.Printf("Configuring server with options: %v", opts)
		err = dir.Configure(opts...)
		if err != nil {
			log.Fatal(err)
		}
		// Now this pre-configured DirServer is the one that will generate new instances.
		err = bind.ReregisterDirServer(endpoint.Transport, dir)
		if err != nil {
			log.Fatal(err)
		}
	}

	config := auth.Config{Lookup: auth.PublicUserKeyService(context)}
	grpcSecureServer, err := grpcauth.NewSecureServer(config)
	if err != nil {
		log.Fatal(err)
	}
	s := &Server{
		context:      context,
		SecureServer: grpcSecureServer,
		endpoint:     *endpoint,
	}
	proto.RegisterDirServer(grpcSecureServer.GRPCServer(), s)

	http.Handle("/", grpcSecureServer.GRPCServer())
	https.ListenAndServe(serverName, *httpsAddr, nil)
}

var (
	// Empty structs we can allocate just once.
	putResponse       proto.DirPutResponse
	deleteResponse    proto.DirDeleteResponse
	configureResponse proto.ConfigureResponse
)

// parseConfigFile reads fileName's contents and splits it in lines, removing
// empty lines and leading and trailing spaces on each line.
func parseConfigFile(fileName string) (out []string) {
	log.Debug.Printf("$HOME=%q", os.Getenv("HOME"))
	buf, err := ioutil.ReadFile(fileName)
	if err != nil {
		log.Error.Printf("Can't read config file %s", fileName)
		return nil
	}
	for _, l := range strings.Split(string(buf), "\n") {
		l = strings.TrimSpace(l)
		if len(l) == 0 {
			continue
		}
		out = append(out, l)
	}
	return out
}

// dirFor returns a DirServer instance bound to the user specified in the context.
func (s *Server) dirFor(ctx gContext.Context) (upspin.DirServer, error) {
	// Validate that we have a session. If not, it's an auth error.
	session, err := s.GetSessionFromContext(ctx)
	if err != nil {
		return nil, err
	}
	context := s.context.Copy().SetUserName(session.User())
	return bind.DirServer(context, s.endpoint)
}

// Lookup implements upspin.DirServer.
func (s *Server) Lookup(ctx gContext.Context, req *proto.DirLookupRequest) (*proto.DirLookupResponse, error) {
	log.Printf("Lookup %q", req.Name)

	dir, err := s.dirFor(ctx)
	if err != nil {
		return nil, err
	}
	entry, err := dir.Lookup(upspin.PathName(req.Name))
	if err != nil {
		log.Printf("Lookup %q failed: %v", req.Name, err)
		return &proto.DirLookupResponse{Error: errors.MarshalError(err)}, nil
	}
	b, err := entry.Marshal()
	if err != nil {
		return nil, err
	}

	resp := &proto.DirLookupResponse{
		Entry: b,
	}
	return resp, nil
}

// Put implements upspin.DirServer.
func (s *Server) Put(ctx gContext.Context, req *proto.DirPutRequest) (*proto.DirPutResponse, error) {
	log.Printf("Put")

	entry, err := proto.UpspinDirEntry(req.Entry)
	if err != nil {
		return &proto.DirPutResponse{Error: errors.MarshalError(err)}, nil
	}
	log.Printf("Put %q", entry.Name)
	dir, err := s.dirFor(ctx)
	if err != nil {
		return nil, err
	}
	err = dir.Put(entry)
	if err != nil {
		log.Printf("Put %q failed: %v", entry.Name, err)
		return &proto.DirPutResponse{Error: errors.MarshalError(err)}, nil
	}
	return &putResponse, nil
}

// MakeDirectory implements upspin.DirServer.
func (s *Server) MakeDirectory(ctx gContext.Context, req *proto.DirMakeDirectoryRequest) (*proto.DirMakeDirectoryResponse, error) {
	log.Printf("MakeDirectory %q", req.Name)

	dir, err := s.dirFor(ctx)
	if err != nil {
		return nil, err
	}
	entry, err := dir.MakeDirectory(upspin.PathName(req.Name))
	if err != nil {
		log.Printf("MakeDirectory %q failed: %v", req.Name, err)
		return &proto.DirMakeDirectoryResponse{Error: errors.MarshalError(err)}, nil
	}
	b, err := entry.Marshal()
	if err != nil {
		return nil, err
	}
	resp := &proto.DirMakeDirectoryResponse{
		Entry: b,
	}
	return resp, nil
}

// Glob implements upspin.DirServer.
func (s *Server) Glob(ctx gContext.Context, req *proto.DirGlobRequest) (*proto.DirGlobResponse, error) {
	log.Printf("Glob %q", req.Pattern)

	dir, err := s.dirFor(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := dir.Glob(req.Pattern)
	if err != nil {
		log.Printf("Glob %q failed: %v", req.Pattern, err)
		return &proto.DirGlobResponse{Error: errors.MarshalError(err)}, nil
	}
	data, err := proto.DirEntryBytes(entries)
	resp := &proto.DirGlobResponse{
		Entries: data,
	}
	return resp, err
}

// Delete implements upspin.DirServer.
func (s *Server) Delete(ctx gContext.Context, req *proto.DirDeleteRequest) (*proto.DirDeleteResponse, error) {
	log.Printf("Delete %q", req.Name)

	dir, err := s.dirFor(ctx)
	if err != nil {
		return nil, err
	}
	err = dir.Delete(upspin.PathName(req.Name))
	if err != nil {
		log.Printf("Delete %q failed: %v", req.Name, err)
		return &proto.DirDeleteResponse{Error: errors.MarshalError(err)}, nil
	}
	return &deleteResponse, nil
}

// WhichAccess implements upspin.DirServer.
func (s *Server) WhichAccess(ctx gContext.Context, req *proto.DirWhichAccessRequest) (*proto.DirWhichAccessResponse, error) {
	log.Printf("WhichAccess %q", req.Name)

	dir, err := s.dirFor(ctx)
	if err != nil {
		return nil, err
	}
	name, err := dir.WhichAccess(upspin.PathName(req.Name))
	if err != nil {
		log.Printf("WhichAccess %q failed: %v", req.Name, err)
	}
	resp := &proto.DirWhichAccessResponse{
		Error: errors.MarshalError(err),
		Name:  string(name),
	}
	return resp, nil
}

// Configure implements upspin.Service
func (s *Server) Configure(ctx gContext.Context, req *proto.ConfigureRequest) (*proto.ConfigureResponse, error) {
	log.Printf("Configure %q", req.Options)
	dir, err := s.dirFor(ctx)
	if err != nil {
		return nil, err
	}
	err = dir.Configure(req.Options...)
	if err != nil {
		log.Printf("Configure %q failed: %v", req.Options, err)
	}
	return &configureResponse, err
}

// Endpoint implements upspin.Service
func (s *Server) Endpoint(ctx gContext.Context, req *proto.EndpointRequest) (*proto.EndpointResponse, error) {
	log.Print("Endpoint")
	dir, err := s.dirFor(ctx)
	if err != nil {
		return nil, err
	}
	endpoint := dir.Endpoint()
	resp := &proto.EndpointResponse{
		Endpoint: &proto.Endpoint{
			Transport: int32(endpoint.Transport),
			NetAddr:   string(endpoint.NetAddr),
		},
	}
	return resp, nil
}
