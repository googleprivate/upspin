// Package path provides tools for parsing and printing file names.
package path

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	gopath "path"

	"upspin.googlesource.com/upspin.git/upspin"
)

// Parsing of file names. File names always start with a user name in mail-address form,
// followed by a slash and a possibly empty pathname that follows. Thus the root of
// user@google.com's name space is "user@google.com/". But Parse also allows
// "user@google.com" to refer to the user's root directory.

// Parsed represents a successfully parsed path name.
type Parsed struct {
	User      upspin.UserName // Must be present and non-empty.
	Elems     []string        // If empty, refers to the root for the user.
	cleanPath upspin.PathName // The path of the file in canonical form; always accurate.
}

// UnmarshalJSON is needed because cleanPath must be created when the object
// is constructed, but it is not exported.
func (p *Parsed) UnmarshalJSON(data []byte) error {
	// Use this type because it won't have the method already bound; we get the default.
	type jsonParsed Parsed
	var j jsonParsed
	err := json.Unmarshal(data, &j)
	if err != nil {
		return err
	}
	*p = Parsed(j)
	p.cleanPath = upspin.PathName(p.toString())
	return nil
}

func (p Parsed) toString() string {
	var b bytes.Buffer
	b.WriteString(string(p.User))
	p.filePath(&b)
	return b.String()
}

func (p Parsed) String() string {
	return string(p.cleanPath)
}

// Path is a helper that returns the string representation with type upspin.PathName.
func (p Parsed) Path() upspin.PathName {
	return p.cleanPath
}

// FilePath returns just the path under the root directory part of the
// pathname, without the leading user name.
// TODO: Use cleanPath when possible.
func (p Parsed) FilePath() string {
	var b bytes.Buffer
	p.filePath(&b)
	return b.String()
}

func (p Parsed) filePath(b *bytes.Buffer) {
	b.WriteByte('/')
	lim := len(p.Elems) - 1
	for i, elem := range p.Elems {
		b.WriteString(string(elem))
		if i < lim {
			b.WriteByte('/')
		}
	}
}

var (
	pn0         = Parsed{}
	errUserName = errors.New("user name not properly formatted")
)

// NameError gives information about an erroneous path name, including the name and error description.
type NameError struct {
	name  string
	error string
}

// Name is the path name that caused the error.
func (n NameError) Name() string {
	return n.name
}

// Error is the implementation of the error interface for NameError.
func (n NameError) Error() string {
	return n.error
}

// Parse parses a full file name, including the user, validates it,
// and returns its parsed form. If the name is a user root directory,
// the trailing slash is optional.
func Parse(pathName upspin.PathName) (Parsed, error) {
	name := string(pathName)
	// Pull off the user name.
	var user string
	slash := strings.IndexByte(name, '/')
	if slash < 0 {
		user, name = name, ""
	} else {
		user, name = name[:slash], name[slash:]
	}
	if len(user) < 6 {
		// No user name. Must be at least "u@x.co". Silly test - do more.
		return pn0, NameError{string(pathName), "no user name in path"}
	}
	if strings.Count(user, "@") != 1 {
		// User name must contain exactly one "@".
		return pn0, NameError{string(pathName), "bad user name in path"}
	}
	p := Parsed{
		User: upspin.UserName(user),
	}
	// Allocate the elems slice all at once, to avoid reallocation in append.
	elems := make([]string, 0, strings.Count(string(pathName), "/"))
	// Split into elements. Don't call strings.Split because it allocates. Also we
	// can process . and .. in this loop.
	for {
		for len(name) > 0 && name[0] == '/' {
			name = name[1:]
		}
		if name == "" {
			break
		}
		var i int
		for i = 0; i < len(name); i++ {
			if name[i] == '/' {
				break
			}
		}
		// Handle "." and "..".
		switch name[:i] {
		case ".":
			// Do nothing.
		case "..":
			// Drop previous element.
			if len(elems) > 0 {
				elems = elems[:len(elems)-1]
			}
		default:
			elems = append(elems, name[:i])
		}
		name = name[i:]
		if name == "" {
			break
		}
	}
	for _, elem := range elems {
		if len(elem) > 255 {
			return pn0, NameError{string(pathName), "name element too long"}
		}
	}
	p.Elems = elems
	p.cleanPath = Clean(pathName)
	return p, nil
}

// First returns a parsed name with only the first n elements after the user name.
func (p Parsed) First(n int) Parsed {
	p.Elems = p.Elems[:n]
	p.cleanPath = FirstPath(p.cleanPath, n)
	return p
}

// Drop returns a parsed name with the last n elements dropped.
func (p Parsed) Drop(n int) Parsed {
	p.Elems = p.Elems[:len(p.Elems)-n]
	p.cleanPath = DropPath(p.cleanPath, n)
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
	return len(p.Elems) == 0
}

// Equal reports whether the two parsed path names are equal.
func (p Parsed) Equal(q Parsed) bool {
	if len(p.Elems) != len(q.Elems) {
		return false
	}
	if p.User != q.User {
		return false
	}
	for i, elem := range p.Elems {
		if elem != q.Elems[i] {
			return false
		}
	}
	return true
}

// Compare returns -1, 0, or 1 according to whether p is less than, equal to,
// or greater than q. The comparison is elementwise starting with the domain name,
// then the user name, then the path elements.
func (p Parsed) Compare(q Parsed) int {
	pUser, pDomain, _ := UserAndDomain(p.User) // Ignoring errors.
	qUser, qDomain, _ := UserAndDomain(q.User) // Ignoring errors.
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
	// User names are equal. Iterate over paths.
	for i, s := range p.Elems {
		switch {
		case i >= len(q.Elems):
			// p has more path elements but they are all equal up to here.
			return 1
		case s > q.Elems[i]:
			return 1
		case s < q.Elems[i]:
			return -1
		}
	}
	if len(p.Elems) == len(q.Elems) {
		return 0
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
func UserAndDomain(userName upspin.UserName) (user string, domain string, err error) {
	u := string(userName)
	if strings.Count(u, "@") != 1 {
		return "", "", errUserName
	}
	if strings.Count(u, "/") != 0 {
		return "", "", errUserName
	}
	i := strings.IndexByte(u, '@')
	user = u[:i]
	if len(user) < 1 {
		return "", "", errUserName
	}
	domain = u[i+1:]
	if len(domain) < 4 {
		return "", "", errUserName
	}
	if strings.Count(domain, ".") < 1 {
		return "", "", errUserName
	}
	return
}
