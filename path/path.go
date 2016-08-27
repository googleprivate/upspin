// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package path provides tools for parsing and printing file names.
// File names always start with a user name in mail-address form,
// followed by a slash and a possibly empty path name that follows. Thus the root of
// user@google.com's name space is "user@google.com/". But Parse also allows
// "user@google.com" to refer to the user's root directory.
package path

import (
	"encoding/json"
	"strings"

	gopath "path"

	"upspin.io/errors"
	"upspin.io/upspin"
)

// Parsed represents a successfully parsed path name.
type Parsed struct {
	// The parsed path is just a clean string. We compute what we need in the methods.
	path upspin.PathName // The path of the file in canonical form; always accurate.
}

// UnmarshalJSON is needed because Parsed has unexported fields.
func (p *Parsed) UnmarshalJSON(data []byte) error {
	err := json.Unmarshal(data, &p.path)
	if err != nil {
		return err
	}
	return nil
}

// MarshalJSON is needed because Parsed has unexported fields.
func (p *Parsed) MarshalJSON() ([]byte, error) {
	return json.Marshal(&p.path)
}

func (p Parsed) String() string {
	return string(p.path)
}

// Path returns the string representation with type upspin.PathName.
func (p Parsed) Path() upspin.PathName {
	return p.path
}

// User returns the name of the user that owns this path.
func (p Parsed) User() upspin.UserName {
	slash := strings.IndexByte(string(p.path), '/')
	return upspin.UserName(p.path[:slash])
}

// Elem returns the nth element of the path.
// It panics if n is out of range.
func (p Parsed) Elem(n int) string {
	str := string(p.path)
	// We start with -1 to treat the user name as an element before the 0th one.
	for i := -1; i < n; i++ {
		slash := strings.IndexByte(str, '/')
		if slash < 0 {
			panic("Elem out of range")
		}
		str = str[slash+1:]
	}
	slash := strings.IndexByte(str, '/')
	if slash < 0 {
		return str
	}
	return str[:slash]
}

// NElem returns number of elements in the path.
func (p Parsed) NElem() int {
	str := string(p.path)
	n := strings.Count(str, "/")
	if n == 1 && str[len(str)-1] == '/' { // User root
		n = 0
	}
	return n
}

// FilePath returns just the path under the root directory part of the
// pathname, without the leading user name or slash.
func (p Parsed) FilePath() string {
	str := string(p.path)
	return str[strings.IndexByte(str, '/')+1:]
}

// Parse parses a full file name, including the user, validates it,
// and returns its parsed form. If the name is a user root directory,
// the trailing slash is optional. The name is 'cleaned' (see the Clean
// function) to canonicalize it.
func Parse(pathName upspin.PathName) (Parsed, error) {
	const op = "path.Parse"
	name := string(pathName)
	// Pull off the user name.
	var user string
	slash := strings.IndexByte(name, '/')
	if slash < 0 {
		user = name
	} else {
		user = name[:slash]
	}
	if _, _, err := UserAndDomain(upspin.UserName(user)); err != nil {
		// Bad user name.
		return Parsed{}, err
	}
	p := Parsed{
		// If pathName is already clean, which it usually is, this will not allocate.
		path: Clean(pathName),
	}
	return p, nil
}

// First returns a parsed name with only the first n elements after the user name.
func (p Parsed) First(n int) Parsed {
	p.path = FirstPath(p.path, n)
	return p
}

// Drop returns a parsed name with the last n elements dropped.
func (p Parsed) Drop(n int) Parsed {
	p.path = DropPath(p.path, n)
	return p
}

// DropPath returns the path name with the last n elements dropped.
// It assumes the path is reasonably well-formed (there must be a
// user name; multiple slashes are OK) but it does not handle .. (dot-dot).
// The result has been "cleaned" by the Clean function.
func DropPath(pathName upspin.PathName, n int) upspin.PathName {
	str := string(Clean(pathName))
	firstSlash := strings.IndexByte(str, '/')
	for ; n > 0; n-- {
		lastSlash := strings.LastIndexByte(str, '/')
		if lastSlash == firstSlash {
			lastSlash++
			str = str[:lastSlash]
			break
		}
		str = str[:lastSlash]
	}
	return upspin.PathName(str)
}

// FirstPath returns the path name with the first n elements dropped.
// It assumes the path is reasonably well-formed (there must be a
// user name; multiple slashes are OK) but it does not handle .. (dot-dot).
// The result has been "cleaned" by the Clean function.
func FirstPath(pathName upspin.PathName, n int) upspin.PathName {
	str := string(Clean(pathName))
	slash := strings.IndexByte(str, '/')
	firstSlash := slash
	for i := 0; i < n; i++ {
		nextSlash := strings.IndexByte(str[slash+1:], '/')
		if nextSlash < 0 {
			// End of string.
			return upspin.PathName(str)
		}
		slash += 1 + nextSlash
	}
	// If all we have left is a user name, make sure to include the trailing slash.
	if slash == firstSlash {
		slash++
	}
	return upspin.PathName(str[:slash])
}

// IsRoot reports whether a parsed name refers to the user's root.
func (p Parsed) IsRoot() bool {
	str := string(p.path)
	return strings.IndexByte(str, '/') == len(str)-1
}

// Equal reports whether the two parsed path names are equal.
func (p Parsed) Equal(q Parsed) bool {
	return p.path == q.path
}

// Compare returns -1, 0, or 1 according to whether p is less than, equal to,
// or greater than q. The comparison is elementwise starting with the domain name,
// then the user name, then the path elements.
func (p Parsed) Compare(q Parsed) int {
	if p.path == q.path {
		return 0
	}
	pUser, pDomain, _ := UserAndDomain(p.User()) // Ignoring errors.
	qUser, qDomain, _ := UserAndDomain(q.User()) // Ignoring errors.
	switch {
	case pDomain < qDomain:
		return -1
	case pDomain > qDomain:
		return 1
	}
	switch {
	case pUser < qUser:
		return -1
	case pUser > qUser:
		return 1
	}
	// User names are equal.
	for i := 0; i < p.NElem(); i++ {
		s := p.Elem(i)
		switch {
		case i >= q.NElem():
			// p has more path elements but they are all equal up to here.
			return 1
		case s > q.Elem(i):
			return 1
		case s < q.Elem(i):
			return -1
		}
	}
	// q has more path elements but they are all equal up to here.
	return -1
}

// Join appends any number of path elements onto a (possibly empty)
// Upspin path, adding a separating slash if necessary. All empty
// strings are ignored. The result, if non-empty, is passed through
// Clean. There is no guarantee that the resulting path is a valid
// Upspin path. This differs from path.Join in that it requires a
// first argument of type upspin.PathName.
func Join(path upspin.PathName, elems ...string) upspin.PathName {
	// Do what we can to avoid unnecessary allocation.
	joined := upspin.PathName("")
	for i, e := range elems {
		if e != "" {
			joined = upspin.PathName(strings.Join(elems[i:], "/"))
			break
		}
	}
	switch {
	case path == "" && joined == "":
		return ""
	case path == "" && joined != "":
		// Nothing to do.
	case path != "" && joined == "":
		joined = path
	case path != "" && joined != "":
		joined = path + "/" + joined
	}
	return Clean(joined)
}

// Clean applies Go's path.Clean to an Upspin path.
func Clean(path upspin.PathName) upspin.PathName {
	// First slash separates user from path. It might not be there.
	slash := strings.IndexByte(string(path), '/')
	var userPart, filePart upspin.PathName
	if slash >= 0 {
		userPart = path[:slash] // Exclude the slash itself.
		filePart = path[slash:] // Include the slash itself.
	} else {
		userPart = path
		filePart = "/"
	}
	_, _, err := UserAndDomain(upspin.UserName(userPart))
	if err != nil {
		// No user name at all, so just call Go's clean. Probably won't happen
		// outside of tests, but one could imagine calling it on the file part
		// of a path.
		return upspin.PathName(gopath.Clean(string(path)))
	}
	// Path is a good user name plus a path name, separated by a slash.
	// Assume the user name is OK and process the rest.
	cleanFilePart := upspin.PathName(gopath.Clean(string(filePart)))
	// If that's the path we started with, the original was clean.
	if slash >= 0 && cleanFilePart == filePart {
		// All is well in the original.
		return path
	}
	return userPart + cleanFilePart
}

// UserAndDomain splits an upspin.UserName into user and domain and returns the pair.
// It also validates the name as an e-mail address and lower-cases the  domain
// so it is canonical.
// TODO: Need to think about Unicode user names.
//
// The rules are:
//
// <name> := <user name>@<domain name>
//
// <domain name> :=
//
// - each . separated token < 64 characters
// - character set for tokens [a-z0-9\-]
// - final token at least two characters
// - whole name < 254 characters
// - characters are case insensitive
// - final period is OK.
//
// We ignore the rules of punycode, which is defined in https://tools.ietf.org/html/rfc3490 .
//
// <user name> :=
//
// Names are are constrained by what SMTP will allow, so these should be good:
//
// - upper and lower case A-Z, case matters.
// - 0-9  !#$%&'*+-/=?^_{|}~
// - utf // TODO.
// - spaces
//
// Spaces may be problematic for us but we allow them here. TODO?
//
// Facebook and Google constrain you to [a-zA-Z0-9+-.],
// ignoring the period and, in Google only, ignoring everything
// from a plus sign onwards. We accept this set but do not follow
// the ignore rules.
//
func UserAndDomain(userName upspin.UserName) (user string, domain string, err error) {
	name := string(userName)
	if len(userName) >= 254 {
		return errUserName(userName, "name too long")
	}
	if strings.Count(name, "@") != 1 {
		return errUserName(userName, "user name must contain one @ symbol")
	}
	at := strings.IndexByte(name, '@')
	user, domain = name[:at], name[at+1:]
	if user == "" {
		return errUserName(userName, "missing user name")
	}
	if domain == "" {
		return errUserName(userName, "missing domain name")
	}
	if strings.Count(domain, ".") == 0 {
		return errUserName(userName, "domain name must contain a period")
	}
	// Valid user name?
	for _, c := range user {
		if !okUserNameChar(c) {
			return errUserName(userName, "bad symbol in user name")
		}
	}
	// Valid domain name?
	period := -1 // First time through loop will fail if first byte is a period.
	isUpper := false
	for i, c := range domain {
		if !okDomainChar(c) {
			return errUserName(userName, "bad symbol in domain name")
		}
		if c == '.' {
			if i-1 >= period+64 {
				return errUserName(userName, "invalid domain name element")
			}
			if i-1 == period || i-1 >= period+64 {
				return errUserName(userName, "invalid domain name element")
			}
			period = i
		}
		if 'A' <= c && c <= 'Z' {
			isUpper = true
		}
	}
	// Last domain element must be at least two bytes  (".co")
	if period+2 >= len(domain) {
		return errUserName(userName, "invalid domain name")
	}
	// Lower-case the domain name if necessary.
	if isUpper {
		domain = strings.ToLower(domain)
	}
	return user, domain, nil
}

func errUserName(user upspin.UserName, msg string) (string, string, error) {
	const op = "path.UserAndDomain"
	return "", "", errors.E(op, errors.Syntax, user, errors.Str(msg))
}

// See the comments for UserAndDomain.
func okUserNameChar(r rune) bool {
	switch {
	case 'a' <= r && r <= 'z':
		return true
	case 'A' <= r && r <= 'Z':
		return true
	case '0' <= r && r <= '9':
		return true
	case strings.ContainsRune("!#$%&'*.+-/=?^_{|}~", r):
		return true
	}
	return false
}

// See the comments for UserAndDomain.
func okDomainChar(r rune) bool {
	switch {
	case 'a' <= r && r <= 'z':
		return true
	case 'A' <= r && r <= 'Z':
		return true
	case '0' <= r && r <= '9':
		return true
	case strings.ContainsRune("+-.", r):
		return true
	}
	return false
}
