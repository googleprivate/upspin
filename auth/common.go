// Package auth handles authentication of Upspin users.
// This module implements common functionality between clients and server objects.
package auth

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"sort"

	"upspin.googlesource.com/upspin.git/upspin"
)

const (
	// Header tagas must be in canonical format (first letter capitalized)
	userNameHeader      = "X-Upspin-Username"
	signatureHeader     = "X-Upspin-Signature"
	signatureTypeHeader = "X-Upspin-Signature-Type"
)

var (
	errMissingSignature = errors.New("missing signature in header")
	allowedHeaders      = map[string]bool{"Date": true, userNameHeader: true, signatureTypeHeader: true}
)

func hashUserRequest(userName upspin.UserName, r *http.Request) []byte {
	sha := sha256.New()
	keys := make([]string, 0, len(allowedHeaders))
	for k, _ := range r.Header {
		if _, ok := allowedHeaders[k]; !ok {
			// Do not use other custom headers, as they may be added by proxies along the way.
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		value, _ := r.Header[k] // known to exist
		sha.Write([]byte(fmt.Sprintf("%s:%s", k, value)))
	}

	// Request method (GET, PUT, etc)
	sha.Write([]byte(r.Method))
	// The fully-formatted URL
	sha.Write([]byte(r.URL.Path))
	// TODO: anything else?
	return sha.Sum(nil)
}
