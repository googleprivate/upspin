package auth

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"upspin.googlesource.com/upspin.git/bind"
	"upspin.googlesource.com/upspin.git/upspin"
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

const (
	userServiceAddr = "https://upspin.io:8082"
)

// NewDefaultTLSConfig creates a new TLS config based on the certificate files given.
func NewDefaultTLSConfig(certFile string, certKeyFile string) (*tls.Config, error) {
	certReadable, err := isReadableFile(certFile)
	if err != nil {
		return nil, fmt.Errorf("Problem with SSL certificate in %q: %q", certFile, err)
	}
	if !certReadable {
		return nil, fmt.Errorf("Certificate %q not readable", certFile)
	}
	keyReadable, err := isReadableFile(certKeyFile)
	if err != nil {
		return nil, fmt.Errorf("Problem with SSL key %q: %v", certKeyFile, err)
	}
	if !keyReadable {
		return nil, fmt.Errorf("Certificate key %q not readable", certKeyFile)
	}

	cert, err := tls.LoadX509KeyPair(certFile, certKeyFile)

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
func PublicUserKeyService() func(userName upspin.UserName) ([]upspin.PublicKey, error) {
	context := &upspin.Context{}
	e := upspin.Endpoint{
		Transport: upspin.GCP,
		NetAddr:   upspin.NetAddr(userServiceAddr),
	}
	u, err := bind.User(context, e)
	if err != nil {
		log.Fatalf("Can't bind to User service: %v", err)
	}
	return func(userName upspin.UserName) ([]upspin.PublicKey, error) {
		_, keys, err := u.Lookup(userName)
		return keys, err
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
		return false, errors.New("is directory")
	}
	// Is it readable?
	fd, err := os.Open(path)
	if err != nil {
		return false, errors.New("permission denied")
	}
	fd.Close()
	return true, nil // Item exists and is readable.
}
