// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"crypto/tls"
	"log"
	"os"
	"time"

	"upspin.io/access"
	"upspin.io/errors"
	"upspin.io/upspin"
	"upspin.io/user/usercache"
)

// Config holds the configuration parameters for instantiating a server (HTTP or gRPC).
type Config struct {
	// Lookup looks up user keys.
	Lookup func(userName upspin.UserName) ([]upspin.PublicKey, error)

	// AllowUnauthenticatedConnections allows unauthenticated connections, making it the caller's
	// responsibility to check Handler.IsAuthenticated.
	AllowUnauthenticatedConnections bool

	// TimeFunc returns the current time. If nil, time.Now() will be used. Mostly only used for testing.
	TimeFunc func() time.Time
}

// NewDefaultTLSConfig creates a new TLS config based on the certificate files given.
func NewDefaultTLSConfig(certFile string, certKeyFile string) (*tls.Config, error) {
	const NewDefaultTLSConfig = "NewDefaultTLSConfig"
	certReadable, err := isReadableFile(certFile)
	if err != nil {
		return nil, errors.E(NewDefaultTLSConfig, errors.Invalid, errors.Errorf("SSL certificate in %q: %q", certFile, err))
	}
	if !certReadable {
		return nil, errors.E(NewDefaultTLSConfig, errors.Invalid, errors.Errorf("certificate file %q not readable", certFile))
	}
	keyReadable, err := isReadableFile(certKeyFile)
	if err != nil {
		return nil, errors.E(NewDefaultTLSConfig, errors.Invalid, errors.Errorf("SSL key in %q: %v", certKeyFile, err))
	}
	if !keyReadable {
		return nil, errors.E(NewDefaultTLSConfig, errors.Invalid, errors.Errorf("certificate key file %q not readable", certKeyFile))
	}

	cert, err := tls.LoadX509KeyPair(certFile, certKeyFile)
	if err != nil {
		return nil, errors.E(NewDefaultTLSConfig, err)
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

// PublicUserKeyService returns a Lookup function that looks up users public keys.
// The lookup function returned is bound to a well-known public Upspin user service.
func PublicUserKeyService(ctx upspin.Context) func(userName upspin.UserName) ([]upspin.PublicKey, error) {
	ctx = usercache.Global(ctx)
	return func(userName upspin.UserName) ([]upspin.PublicKey, error) {
		log.Printf("Calling User.Lookup for user %s", userName)
		_, keys, err := ctx.User().Lookup(userName)
		log.Printf("Lookup answered: %v, %v", keys, err)
		if err != nil {
			return nil, errors.E("PublicUserKeyService", err)
		}
		return keys, nil
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
