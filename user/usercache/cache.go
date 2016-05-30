// Package usercache pushes a user cache in front of context.User.
package usercache

import (
	"time"

	"upspin.io/cache"
	"upspin.io/upspin"
)

type entry struct {
	expires time.Time // when the information expires.
	eps     []upspin.Endpoint
	pub     []upspin.PublicKey
}

type userCache struct {
	uncached upspin.User
	entries  *cache.LRU
	duration time.Duration
}

// Install a cache onto the User service.  After this all User service requests will
// be filtered through the cache.
//
// TODO(p): Install is not concurrency safe since context is assumed to be immutable
// everywhere else.  Not sure this needs to be fixed but should at least be noted.
func Install(context *upspin.Context) {
	// Avpoid installing more than once.
	if _, ok := context.User.(*userCache); ok {
		return
	}

	c := &userCache{
		uncached: context.User,
		entries:  cache.NewLRU(256),
		duration: time.Minute * 15,
	}
	context.User = c
}

// Lookup implements upspin.User.Lookup.
func (c *userCache) Lookup(name upspin.UserName) ([]upspin.Endpoint, []upspin.PublicKey, error) {
	v, ok := c.entries.Get(name)

	// If we have an unexpired binding, use it.
	if ok {
		if !time.Now().After(v.(*entry).expires) {
			e := v.(*entry)
			return e.eps, e.pub, nil
		}
		// TODO(p): change the LRU stuff to have a Remove method.
	}

	// Not found, look it up.
	eps, pub, err := c.uncached.Lookup(name)
	if err != nil {
		return nil, nil, err
	}
	e := &entry{
		expires: time.Now().Add(c.duration),
		eps:     eps,
		pub:     pub,
	}
	c.entries.Add(name, e)
	return eps, pub, nil
}

// Dial implements upspin.User.Dial.
func (c *userCache) Dial(context *upspin.Context, e upspin.Endpoint) (upspin.Service, error) {
	return c, nil
}

// ServerUserName implements upspin.User.ServerUserName.
func (c *userCache) ServerUserName() string {
	return c.uncached.ServerUserName()
}

// Configure implements upspin.Service.
func (c *userCache) Configure(options ...string) error {
	panic("unimplemented")
}

// Endpoint implements upspin.Service.
func (c *userCache) Endpoint() upspin.Endpoint {
	panic("unimplemented")
}

// Ping implements upspin.Service.
func (c *userCache) Ping() bool {
	return true
}

// Close implements upspin.Service.
func (c *userCache) Close() {
}

// Authenticate implements upspin.Service.
func (c *userCache) Authenticate(*upspin.Context) error {
	return nil
}

// SetDuration sets the duration until entries expire.  Primarily
// intended for testing.
func (c *userCache) SetDuration(d time.Duration) {
	c.duration = d
}
