// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grpcauth

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"testing"

	gContext "golang.org/x/net/context"

	"upspin.io/auth"
	prototest "upspin.io/auth/grpcauth/testdata"
	"upspin.io/cloud/https"
	"upspin.io/context"
	"upspin.io/errors"
	"upspin.io/factotum"
	"upspin.io/upspin"
)

var (
	p256Public  = upspin.PublicKey("p256\n104278369061367353805983276707664349405797936579880352274235000127123465616334\n26941412685198548642075210264642864401950753555952207894712845271039438170192\n")
	p256Private = "82201047360680847258309465671292633303992565667422607675215625927005262185934"
	user        = upspin.UserName("joe@blow.com")
	grpcServer  SecureServer
	srv         *server
	cli         *client
)

type server struct {
	// Automatically handles authentication by implementing the Authenticate server method.
	SecureServer
	t         *testing.T
	iteration int
}

func lookup(userName upspin.UserName) (upspin.PublicKey, error) {
	if userName == user {
		return upspin.PublicKey(p256Public), nil
	}
	return "", errors.Str("No user here")
}

func pickPort() (port string) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	_, port, err = net.SplitHostPort(listener.Addr().String())
	if err != nil {
		log.Fatalf("Failed to parse listener address: %v", err)
	}
	listener.Close()
	return port
}

func startServer() (port string) {
	config := auth.Config{
		Lookup: lookup,
	}
	var err error
	grpcServer, err = NewSecureServer(config)
	if err != nil {
		log.Fatal(err)
	}
	srv = &server{
		SecureServer: grpcServer,
	}
	port = pickPort()
	prototest.RegisterTestServiceServer(grpcServer.GRPCServer(), srv)
	log.Printf("Starting e2e server on port %s", port)
	http.Handle("/", grpcServer.GRPCServer())
	go https.ListenAndServe("test", fmt.Sprintf("localhost:%s", port), nil)
	return port
}

func (s *server) DoATrump(ctx gContext.Context, req *prototest.DoATrumpRequest) (*prototest.DoATrumpResponse, error) {
	// Validate that we have a session. If not, it's an auth error.
	session, err := s.GetSessionFromContext(ctx)
	if err != nil {
		s.t.Fatal(err)
	}
	if session.User() != user {
		s.t.Fatalf("Expected user %q, got %q", user, session.User())
	}
	if req.PeopleDemand == demands[s.iteration] {
		resp := &prototest.DoATrumpResponse{
			TrumpResponse: expectedResponses[s.iteration],
		}
		log.Printf("Trump: telling the people: %q", resp.TrumpResponse)
		s.iteration++
		return resp, nil
	}
	s.t.Fatalf("iteration %d: invalid request %q", s.iteration, req.PeopleDemand)
	return nil, nil // not reached
}

type client struct {
	*AuthClientService // For handling Authenticate, Ping and Close.
	grpcClient         prototest.TestServiceClient
	demandCount        int
}

func (c *client) TellTrump(t *testing.T, demand string) (response string) {
	gCtx, err := c.NewAuthContext()
	if err != nil {
		t.Fatal(err)
	}
	req := &prototest.DoATrumpRequest{
		PeopleDemand: demand,
	}
	log.Printf("Client: Telling Trump: %q", req.PeopleDemand)
	resp, err := c.grpcClient.DoATrump(gCtx, req)
	if err != nil {
		t.Fatal(err)
	}
	c.demandCount++
	return resp.TrumpResponse
}

func startClient(port string) {
	f, err := factotum.New(p256Public, p256Private)
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.New().SetUserName(user).SetFactotum(f)

	authClient, err := NewGRPCClient(ctx, upspin.NetAddr("localhost:"+port), KeepAliveInterval, AllowSelfSignedCertificate)
	if err != nil {
		log.Fatal(err)
	}
	grpcClient := prototest.NewTestServiceClient(authClient.GRPCConn())
	authClient.SetService(grpcClient)
	cli = &client{
		AuthClientService: authClient,
		grpcClient:        grpcClient,
	}
}

func TestAll(t *testing.T) {
	t.Log("Starting testing...")

	// Inject testing into the server.
	srv.t = t

	if len(demands) < 1 {
		t.Fatalf("Programmer error. Make at least one demand!")
	}
	for i := range demands {
		t.Logf("Calling Trump: %d", i)
		response := cli.TellTrump(t, demands[i])
		if expectedResponses[i] != response {
			t.Errorf("Demand %d: Expected response %q, got %q", i, expectedResponses[i], response)
		}
	}

	// Verify server and client changed state.
	if srv.iteration != len(demands) {
		t.Errorf("Expected server to be on iteration %d, was on %d", len(demands), srv.iteration)
	}
	if cli.demandCount != srv.iteration {
		t.Errorf("Expected client to be on iteration %d, was on %d", srv.iteration, cli.demandCount)
	}

	if cli.LastActivity().IsZero() {
		t.Errorf("Expected keep alive go routine to be alive.")
	}
}

var (
	demands = []string{
		"Make America great again",
		"Embrace all people",
		"Free the slaves!",
	}
	expectedResponses = []string{
		"Invade Luxemborg",
		"Build a wall!",
		"I said 'Waaaaaalll'",
	}
)

func TestMain(m *testing.M) {
	port := startServer()
	startClient(port) // Blocks until it's connected to the server.

	// Start testing.
	code := m.Run()

	// Terminate cleanly.
	log.Printf("Finishing...")
	cli.Close()
	srv.Stop()

	// Report test results.
	log.Printf("Finishing e2e tests: %d", code)
	os.Exit(code)
}
