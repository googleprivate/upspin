// Package auth handles authentication of Upspin users.
package auth

import (
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"upspin.googlesource.com/upspin.git/cloud/netutil"
	"upspin.googlesource.com/upspin.git/upspin"
)

// HTTPClient is a thin wrapper around a standard HTTP Client that implements authentication transparently. It caches state
// so that not every request needs to be signed. HTTPClient is optimized to work with a single server endpoint.
// It will work with any number of servers, but it keeps state about the last one, so using it with many servers will
// decrease its performance.
type HTTPClient struct {
	// Protects all fields from concurrent access, except client, which does not need locking.
	sync.Mutex

	// Caches the base URL of the last server connected with.
	url *url.URL

	// Records the time we last authenticated with the server.
	// NOTE: this may seem like a premature optimization, but a comment in tls.ConnectionState indicates that
	// resumed connections don't get a TLS unique token, which prevents us from implicitly authenticating the
	// connection. To prevent a round-trip to the server, we preemptively re-auth every AuthIntervalSec
	timeLastAuth time.Time

	// The user we authenticate for.
	user upspin.UserName

	// The user's keys.
	factotum *Factotum

	// The underlying HTTP client
	client netutil.HTTPClientInterface
}

var _ netutil.HTTPClientInterface = (*HTTPClient)(nil)

const (
	// AuthIntervalSec is the maximum allowed time between unauthenticated requests to the same server.
	AuthIntervalSec = 5 * 60 // 5 minutes
)

var (
	errNoUser = &clientError{"no user set"}
	errNoKeys = &clientError{"no keys set"}
)

// NewClient returns a new HTTPClient that handles auth for the named user and underlying HTTP client.
func NewClient(user upspin.UserName, factotum *Factotum, httClient netutil.HTTPClientInterface) *HTTPClient {
	return &HTTPClient{
		user:     user,
		factotum: factotum,
		client:   httClient,
	}
}

// NewAnonymousClient returns a new HTTPClient that does not yet know about the user name.
// To complete setup, use SetUserName and SetUserKeys.
func NewAnonymousClient(httClient netutil.HTTPClientInterface) *HTTPClient {
	return &HTTPClient{
		client: httClient,
	}
}

// SetUserName sets the user name for this HTTPClient instance.
func (c *HTTPClient) SetUserName(user upspin.UserName) {
	c.Lock()
	c.user = user
	c.Unlock()
}

// SetUserKeys sets the factotum for this HTTPClient instance.
func (c *HTTPClient) SetUserKeys(factotum *Factotum) {
	c.Lock()
	c.factotum = factotum
	c.Unlock()
}

// Do implements netutil.HTTPClientInterface.
func (c *HTTPClient) Do(req *http.Request) (resp *http.Response, err error) {
	c.prepareRequest(req)
	if req.URL == nil {
		// Let the native client handle this weirdness.
		return c.doWithoutSign(req)
	}
	if req.URL.Scheme != "https" {
		// No point in doing authentication.
		return c.doWithoutSign(req)
	}
	c.Lock() // Will be unlocked by doWithSign.
	if c.url == nil || c.url.Host != req.URL.Host {
		// Must sign gain.
		return c.doWithSign(req)
	}
	// It's better to avoid a round trip and sign requests that can't be played back.
	if !isReplayable(req) {
		return c.doWithSign(req)
	}
	now := time.Now()
	if c.timeLastAuth.Add(time.Duration(AuthIntervalSec) * time.Second).Before(now) {
		return c.doWithSign(req)
	}
	c.Unlock()
	return c.doWithoutSign(req)
}

// prepareRequest sets the necessary fields for auth on the request, common to both signed and unsigned requests.
func (c *HTTPClient) prepareRequest(req *http.Request) {
	req.Header.Set(userNameHeader, string(c.user)) // Set the username
	req.Header.Set("Date", time.Now().Format(time.RFC850))
}

// isReplayable reports whether the request can be played back to the server safely.
func isReplayable(req *http.Request) bool {
	if req.Body == nil && req.Method == "GET" {
		// A GET request without payload is always replayable.
		return true
	}
	// Note: In general, if the body can seek to the beginning again, it should be replayable. However, Go's HTTP
	// client peeks inside the buffer, making it hard for us to wrap another buffer with an implentation of
	// io.ReadSeeker (it can be resolved, but not without reverse-engineering the native HTTP client, which is a bad idea).
	return false
}

// doWithoutSign does not initially sign the request, but if the request fails with error code 401, we try up to one more
// time with signing, if possible. It must be called with the mutex NOT held.
func (c *HTTPClient) doWithoutSign(req *http.Request) (*http.Response, error) {
	resp, err := c.client.Do(req)
	if err != nil {
		return resp, newError(err)
	}
	if resp.StatusCode == http.StatusUnauthorized && req.URL.Scheme == "https" {
		if isReplayable(req) {
			c.Lock()
			return c.doWithSign(req)
		}
	}
	return resp, err
}

// doAuth performs signature authentication and caches the server and time of this last signed request.
// It must be called with the mutex held.
func (c *HTTPClient) doWithSign(req *http.Request) (*http.Response, error) {
	if c.user == "" {
		c.Unlock()
		return nil, errNoUser
	}
	if c.factotum.PackingString() == "" {
		c.Unlock()
		return nil, errNoKeys
	}
	err := signRequest(c.user, c.factotum, req)
	if err != nil {
		c.Unlock()
		return nil, newError(err)
	}
	c.url = req.URL
	c.timeLastAuth = time.Now()
	c.Unlock()
	return c.client.Do(req)
}

// signRequest sets the necessary headers in the HTTP request to authenticate a user, by signing the request with the given key.
func signRequest(userName upspin.UserName, factotum *Factotum, req *http.Request) error {
	req.Header.Set(signatureTypeHeader, factotum.PackingString())
	// The hash includes the user name and the key type.
	hash := hashUserRequest(userName, req)
	sig, err := factotum.UserSign(hash)
	if err != nil {
		return err
	}
	req.Header.Set(signatureHeader, fmt.Sprintf("%s %s", sig.R, sig.S))
	return nil
}

type clientError struct {
	errorMsg string
}

// Error implements error
func (c *clientError) Error() string {
	return fmt.Sprintf("HTTPClient: %s", c.errorMsg)
}

func newError(err error) error {
	return &clientError{err.Error()}
}
