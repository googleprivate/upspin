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

	"strings"

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
	invalidFormat = "%s: %d: unrecognized text: %q"
)

func Parse(pathName upspin.PathName, data []byte) (*Parsed, error) {
	var p Parsed
	s := bufio.NewScanner(bytes.NewReader(data))
	for lineNum := 0; s.Scan(); lineNum++ {
		if s.Err() != nil {
			return nil, s.Err()
		}
		line := s.Bytes()
		if isAllBlank(line) {
			continue
		}
		state, elems, offending := parseLine(line)
		switch state {
		case readers:
			p.Readers = append(p.Readers, elems...)
		case writers:
			p.Writers = append(p.Writers, elems...)
		case appenders:
			p.Appenders = append(p.Appenders, elems...)
		case invalid:
			return nil, fmt.Errorf(invalidFormat, pathName, lineNum+1, line[offending:])
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

// toLower lower cases a single character.
func toLower(b byte) byte {
	// An old trick: In ASCII the characters line up bitwise so this changes any letter to lower case.
	return b | ('a' - 'A')
}

func isAllBlank(buf []byte) bool {
	for _, b := range buf {
		if b != ' ' && b != '\t' {
			return false
		}
	}
	return true
}

// equalsLower reports whether word, once lower-cased, is equal to want.
func equalsLower(word string, want string) bool {
	if len(word) != len(want) {
		return false
	}
	for i := 0; i < len(word); i++ {
		if toLower(word[i]) != want[i] {
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
	switch toLower(line[first]) {
	case 'r':
		if len(token) == 1 || equalsLower(token, read) {
			return readers
		}
	case 'w':
		if len(token) == 1 || equalsLower(token, write) {
			return writers
		}
	case 'a':
		if len(token) == 1 || equalsLower(token, append) {
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
	if state == newSection {
		// This can only happen if there was no ":" found or on blank lines.
		return invalid, nil, 0
	}
	return state, ids, -1
}

// IsAccessFile reports whether the pathName contains a file named Access, which is special.
func IsAccessFile(pathName upspin.PathName) bool {
	return strings.HasSuffix(string(pathName), AccessFile)
}
