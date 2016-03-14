// Package access parses access control files.
//
// Access files have the following format:
// <access type>: <email>[, <email>, ...]
//
// Anything after a '#' character is ignored
//
// Example:
//
// Read: email@domain,com, email2@domain.com
// Write: writer@domain.com, writer2@domain.com, writer3@exmaple,com
// Append: appender@example.com # This is a comment
package access

import (
	"bufio"
	"bytes"
	"fmt"

	"upspin.googlesource.com/upspin.git/path"
	"upspin.googlesource.com/upspin.git/upspin"
)

const (
	// Name of Access file. Exported in case it's useful on its own. Prefer using IsAccessFile below.
	AccessFile = "Access"
)

// Parsed contains the parsed path names found in the ACL file, one for each section.
type Parsed struct {
	Readers   []path.Parsed
	Writers   []path.Parsed
	Appenders []path.Parsed
}

type state int

const (
	newSection state = iota
	readers
	writers
	appenders
	invalid
)

const (
	invalidFormat = "line %d: unrecognized text: %q"
)

func Parse(data []byte) (*Parsed, error) {
	var p Parsed
	s := bufio.NewScanner(bytes.NewReader(data))
	for lineNum := 0; s.Scan(); lineNum++ {
		line := s.Bytes()
		state, elems, offending := parseLine(line)
		switch state {
		case readers:
			p.Readers = append(p.Readers, elems...)
		case writers:
			p.Writers = append(p.Writers, elems...)
		case appenders:
			p.Appenders = append(p.Appenders, elems...)
		case invalid:
			return nil, fmt.Errorf(invalidFormat, lineNum, line[offending:])
		}
	}
	return &p, nil
}

func isSpace(b byte) bool {
	switch b {
	case ' ', '\r', '\f', '\v', '\n', '\t':
		return true
	default:
		return false

	}
}

func toLower(r rune) rune {
	return r | ' '
}

func isAllBlank(buf []byte) bool {
	for _, b := range buf {
		if b != ' ' && b != '\t' {
			return false
		}
	}
	return true
}

// matchesUpper returns true if a case-insensitive comparison between a token and an lower-cased string matches.
func matchesLower(token string, toMatchInLower string) bool {
	if len(token) != len(toMatchInLower) {
		return false
	}
	for i, r := range token {
		if toLower(r) != rune(toMatchInLower[i]) {
			return false
		}
	}
	return true
}

func stateFromFile(line []byte, first, last int) state {
	if first < 0 || last < 0 || first > last {
		return invalid
	}
	// Try to avoid allocations here: do not call strings.ToUpper here as it performs allocations.
	token := string(line[first : last+1])
	const (
		read   = "read"
		append = "append"
		write  = "write"
	)
	switch toLower(rune(line[first])) {
	case 'r':
		if len(token) == 1 || matchesLower(token, read) {
			return readers
		}
	case 'w':
		if len(token) == 1 || matchesLower(token, write) {
			return writers
		}
	case 'a':
		if len(token) == 1 || matchesLower(token, append) {
			return appenders
		}
	}
	return invalid
}

// parseLine returns the section the line belongs to (readers, appenders, etc) and a list of non-comment,
// non-marker strings as found. In case of error, state will be invalid and the position of the offending character is
// returned as an int.
func parseLine(line []byte) (state, []path.Parsed, int) {
	state := newSection
	lastNonEmpty := 0
	firstNonEmpty := -1
	var ids []path.Parsed
	lastChar := len(line) - 1
	for i, c := range line {
		if c == '#' {
			return state, ids, -1
		}
		if state == newSection {
			if c != ':' {
				if !isSpace(c) {
					if firstNonEmpty < 0 {
						firstNonEmpty = i
					}
					lastNonEmpty = i
				}
				continue
			}
			// Found a colon. Check what the previous non-whitespace character was.
			state = stateFromFile(line, firstNonEmpty, lastNonEmpty)
			if state == invalid {
				return state, nil, i
			}
			lastNonEmpty = i + 1
			continue
		}
		// Have we found a separator?
		if isSpace(c) || c == ',' || i == lastChar {
			if i == lastChar {
				i++
			}
			// Our token is from sectionIndex to i, if non-empty
			token := line[lastNonEmpty:i]
			if isAllBlank(token) {
				lastNonEmpty = i + 1
				continue
			}
			lastNonEmpty = i + 1
			// Perform the necessary allocation and path parsing
			p, err := path.Parse(upspin.PathName(token) + "/")
			if err != nil || len(p.Elems) > 0 {
				// Ignore groups for now.
				continue
			}
			ids = append(ids, p)
			continue
		}
		// Can't have another section on the same line
		if c == ':' {
			return invalid, nil, i
		}
	}
	return state, ids, -1
}

// IsAccessFile reports whether the pathName is a valid Upspin path
// name and is for a file named Access, which is special. For
// convenience, it also returns the parsed path in case there were no
// errors parsing it.
func IsAccessFile(pathName upspin.PathName) (bool, *path.Parsed) {
	p, err := path.Parse(pathName)
	if err != nil {
		return false, nil
	}
	n := len(p.Elems)
	return n > 0 && p.Elems[n-1] == AccessFile, &p
}
