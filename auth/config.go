// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"crypto/tls"
	"os"
	"time"

	"upspin.io/access"
	"upspin.io/bind"
	"upspin.io/errors"
	"upspin.io/key/usercache"
	"upspin.io/upspin"
)

// Config holds the configuration parameters for instantiating a server (HTTP or gRPC).
type Config struct {
	// Lookup looks up user keys.
	Lookup func(userName upspin.UserName) (upspin.PublicKey, error)

	// AllowUnauthenticatedConnections allows unauthenticated connections, making it the caller's
	// responsibility to check Handler.IsAuthenticated.
	AllowUnauthenticatedConnections bool

	// TimeFunc returns the current time. If nil, time.Now() will be used. Mostly only used for testing.
	TimeFunc func() time.Time
}

// NewDefaultTLSConfig creates a new TLS config based on the certificate files given.
func NewDefaultTLSConfig(certFile string, certKeyFile string) (*tls.Config, error) {
	const op = "auth.NewDefaultTLSConfig"
	certReadable, err := isReadableFile(certFile)
	if err != nil {
		return nil, errors.E(op, errors.Invalid, errors.Errorf("SSL certificate in %q: %q", certFile, err))
	}
	if !certReadable {
		return nil, errors.E(op, errors.Invalid, errors.Errorf("certificate file %q not readable", certFile))
	}
	keyReadable, err := isReadableFile(certKeyFile)
	if err != nil {
		return nil, errors.E(op, errors.Invalid, errors.Errorf("SSL key in %q: %v", certKeyFile, err))
	}
	if !keyReadable {
		return nil, errors.E(op, errors.Invalid, errors.Errorf("certificate key file %q not readable", certKeyFile))
	}

	cert, err := tls.LoadX509KeyPair(certFile, certKeyFile)
	if err != nil {
		return nil, errors.E(op, err)
	}

	tlsConfig := &tls.Config{
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
		MinVersion:               tls.VersionTLS12,
		PreferServerCipherSuites: true, // Use our choice, not the client's choice
		CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		Certificates:             []tls.Certificate{cert},
	}
	tlsConfig.BuildNameToCertificate()
	return tlsConfig, nil
}

// PublicUserKeyService returns a Lookup function that looks up user's public keys.
// The lookup function returned is bound to a well-known public Upspin user service.
func PublicUserKeyService(ctx upspin.Context) func(userName upspin.UserName) (upspin.PublicKey, error) {
	const op = "auth.PublicUserKeyService"
	ctx = usercache.Global(ctx)
	return func(userName upspin.UserName) (upspin.PublicKey, error) {
		key, err := bind.KeyServer(ctx, ctx.KeyEndpoint())
		if err != nil {
			return "", errors.E(op, err)
		}
		u, err := key.Lookup(userName)
		if err != nil {
			return "", errors.E(op, err)
		}
		return u.PublicKey, nil
	}
}

// isReadableFile reports whether the file exists and is readable.
// If the error is non-nil, it means there might be a file or directory
// with that name but we cannot read it.
func isReadableFile(path string) (bool, error) {
	// Is it stattable and is it a plain file?
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil // Item does not exist.
		}
		return false, err // Item is problematic.
	}
	if info.IsDir() {
		return false, errors.Str("is directory")
	}
	// Is it readable?
	fd, err := os.Open(path)
	if err != nil {
		return false, access.ErrPermissionDenied
	}
	fd.Close()
	return true, nil // Item exists and is readable.
}
