// Package path provides tools for parsing and printing file names.
package path

import (
	"bytes"
	"path"
	"strings"
)

// UserName represents a user's name, such as "user@google.com".
// It is just a string type that helps make function signatures clearer.
type UserName string

// Name represents a full path name including the user name prefix.
// It is just a string type that helps make function signatures clearer.
type Name string

// Parsing of file names. File names always start with a user name in mail-address form,
// followed by a slash and a possibly empty pathname that follows. Thus the root of
// user@google.com's name space is "user@google.com/".

// Parsed represents a successfully parsed path name.
type Parsed struct {
	User  UserName // Must be present and non-empty.
	Elems []string // If empty, refers to the root for the user.
}

func (p Parsed) String() string {
	var b bytes.Buffer
	b.WriteString(string(p.User))
	if len(p.Elems) == 0 {
		b.WriteByte('/')
	} else {
		for _, elem := range p.Elems {
			b.WriteByte('/')
			b.WriteString(string(elem))
		}
	}
	return b.String()
}

// Path is a helper that returns the string representation with type Name.
func (p Parsed) Path() Name {
	return Name(p.String())
}

var (
	pn0 = Parsed{}
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
// and returns its parsed form.
func Parse(pathName Name) (Parsed, error) {
	name := string(pathName)
	// Pull off the user name.
	slash := strings.IndexByte(name, '/')
	if slash < 0 {
		// No slash.
		return pn0, NameError{string(pathName), "no slash in path"}
	}
	if slash < 6 {
		// No user name. Must be at least "u@x.co". Silly test - do more.
		return pn0, NameError{string(pathName), "no user name in path"}
	}
	user, name := name[:slash], path.Clean(name[slash:])
	if strings.Count(user, "@") != 1 {
		// User name must contain exactly one "@".
		return pn0, NameError{string(pathName), "bad user name in path"}
	}
	elems := strings.Split(name, "/") // Include the slash - it's rooted.
	// First element will be empty because we start with a slash: empty string before it.
	elems = elems[1:]
	// There will be a trailing empty element if the name is just /, the root.
	if name == "/" {
		elems = elems[1:]
	}
	for _, elem := range elems {
		if len(elem) > 255 {
			return pn0, NameError{string(pathName), "name element too long"}
		}
	}
	pn := Parsed{
		User:  UserName(user),
		Elems: elems,
	}
	return pn, nil
}

func cleanPath(pathName Name) string {
	return path.Clean(string(pathName))
}

// First returns a parsed name with only the first n elements after the user name.
func (p Parsed) First(n int) Parsed {
	p.Elems = p.Elems[:n]
	return p
}

// Drop returns a parsed name with the last n elements dropped.
func (p Parsed) Drop(n int) Parsed {
	p.Elems = p.Elems[:len(p.Elems)-n]
	return p
}
