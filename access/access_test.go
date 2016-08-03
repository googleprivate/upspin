// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package access

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"upspin.io/errors"
	"upspin.io/path"
	"upspin.io/upspin"
)

const (
	testFile      = "me@here.com/Access"
	testGroupFile = "me@here.com/Group/family"
)

var empty = []string{}

var (
	accessText = []byte(`
  r : foo@bob.com ,a@b.co x@y.uk # a comment. Notice commas and spaces.

w:writer@a.bc # comment r: ignored@incomment.com
l: lister@n.mn  # other comment a: ignored@too.com
Read : reader@reader.org
# Some comment r: a: w: read: write ::::
WRITE: anotherwriter@a.bc
  create,DeLeTe  :admin@c.com`)

	groupText = []byte("#This is my family\nfred@me.com, ann@me.com\njoe@me.com\n")
)

func BenchmarkParse(b *testing.B) {
	for n := 0; n < b.N; n++ {
		_, err := Parse(testFile, accessText)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestParse(t *testing.T) {
	a, err := Parse(testFile, accessText)
	if err != nil {
		t.Fatal(err)
	}

	match(t, a.list[Read], []string{"a@b.co", "foo@bob.com", "reader@reader.org", "x@y.uk"})
	match(t, a.list[Write], []string{"anotherwriter@a.bc", "writer@a.bc"})
	match(t, a.list[List], []string{"lister@n.mn"})
	match(t, a.list[Create], []string{"admin@c.com"})
	match(t, a.list[Delete], []string{"admin@c.com"})
}

func TestParseEmpty(t *testing.T) {
	a, err := Parse(testFile, []byte(""))
	if err != nil {
		t.Fatal(err)
	}
	for i := Read; i < numRights; i++ {
		match(t, a.list[i], nil)
	}

	// Nil should be OK too.
	a, err = Parse(testFile, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := Read; i < numRights; i++ {
		match(t, a.list[i], nil)
	}
}

type accessEqualTest struct {
	path1   upspin.PathName
	access1 string
	path2   upspin.PathName
	access2 string
	expect  bool
}

var accessEqualTests = []accessEqualTest{
	{
		// Same, but formatted differently. Parse and sort will fix.
		"a1@b.com/Access",
		"r:joe@foo.com, fred@foo.com\n",
		"a1@b.com/Access",
		"# A comment\nr:fred@foo.com, joe@foo.com\n",
		true,
	},
	{
		// Different names.
		"a1@b.com/Access",
		"r:joe@foo.com, fred@foo.com\n",
		"a2@b.com/Access",
		"# A comment\nr:fred@foo.com, joe@foo.com\n",
		false,
	},
	{
		// Same name, different contents.
		"a1@b.com/Access",
		"r:joe@foo.com, fred@foo.com\n",
		"a1@b.com/Access",
		"# A comment\nr:fred@foo.com, zot@foo.com\n",
		false,
	},
}

func TestAccessEqual(t *testing.T) {
	for i, test := range accessEqualTests {
		a1, err := Parse(test.path1, []byte(test.access1))
		if err != nil {
			t.Fatalf("%d: %s: %s\n", i, test.path1, err)
		}
		a2, err := Parse(test.path2, []byte(test.access2))
		if err != nil {
			t.Fatalf("%d: %s: %s\n", i, test.path2, err)
		}
		if a1.Equal(a2) != test.expect {
			t.Errorf("%d: equal(%q, %q) should be %t, is not", i, test.path1, test.path2, test.expect)
		}
	}
}

func TestParseGroup(t *testing.T) {
	parsed, err := path.Parse(testGroupFile)
	if err != nil {
		t.Fatal(err)
	}
	group, err := parseGroup(parsed, groupText)
	if err != nil {
		t.Fatal(err)
	}

	match(t, group, []string{"fred@me.com", "ann@me.com", "joe@me.com"})
}

func TestParseAllocs(t *testing.T) {
	allocs := testing.AllocsPerRun(100, func() {
		Parse(testFile, accessText)
	})
	t.Log("allocs:", allocs)
	if allocs != 23 {
		t.Fatal("expected 23 allocations, got ", allocs)
	}
}

func TestGroupParseAllocs(t *testing.T) {
	parsed, err := path.Parse(testGroupFile)
	if err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(100, func() {
		parseGroup(parsed, groupText)
	})
	t.Log("allocs:", allocs)
	if allocs != 6 {
		t.Fatal("expected 6 allocations, got ", allocs)
	}
}

func TestHasAccessNoGroups(t *testing.T) {
	const (
		owner = upspin.UserName("me@here.com")

		// This access file defines readers and writers but no other rights.
		text = "r: reader@r.com, reader@foo.bar, *@nsa.gov\n" +
			"w: writer@foo.bar\n"
	)
	a, err := Parse(testFile, []byte(text))
	if err != nil {
		t.Fatal(err)
	}

	check := func(user upspin.UserName, right Right, file upspin.PathName, truth bool) {
		ok, groups, err := a.canNoGroupLoad(user, right, file)
		if groups != nil {
			t.Fatalf("non-empty groups %q", groups)
		}
		if err != nil {
			t.Fatal(err)
		}
		if ok == truth {
			return
		}
		if ok {
			t.Errorf("%s can %s %s", user, right, file)
		} else {
			t.Errorf("%s cannot %s %s", user, right, file)
		}
	}

	// Owner can read anything and write Access files.
	check(owner, Read, "me@here.com/foo/bar", true)
	check(owner, Read, "me@here.com/foo/Access", true)
	check(owner, List, "me@here.com/foo/bar", true)
	check(owner, Create, "me@here.com/foo/Access", true)
	check(owner, Write, "me@here.com/foo/Access", true)

	// Permitted others can read.
	check("reader@foo.bar", Read, "me@here.com/foo/bar", true)

	// Unpermitted others cannot read.
	check("writer@foo.bar", List, "me@here.com/foo/bar", false)

	// Permitted others can write.
	check("writer@foo.bar", Write, "me@here.com/foo/bar", true)

	// Unpermitted others cannot write.
	check("reader@foo.bar", Write, "me@here.com/foo/bar", false)

	// Non-owners cannot list (it's not in the Access file).
	check("reader@foo.bar", List, "me@here.com/foo/bar", false)
	check("writer@foo.bar", List, "me@here.com/foo/bar", false)

	// No one can create (it's not in the Access file).
	check(owner, Create, "me@here.com/foo/bar", false)
	check("writer@foo.bar", Create, "me@here.com/foo/bar", false)

	// No one can delete (it's not in the Access file).
	check(owner, Delete, "me@here.com/foo/bar", false)
	check("writer@foo.bar", Delete, "me@here.com/foo/bar", false)

	// Wildcard that should match.
	check("joe@nsa.gov", Read, "me@here.com/foo/bar", true)

	// Wildcard that should not match.
	check("*@nasa.gov", Read, "me@here.com/foo/bar", false)

	// User can write Access file.
	check(owner, Write, "me@here.com/foo/Access", true)

	// User can write Group file.
	check(owner, Write, "me@here.com/Group/bar", true)

	// Other user cannot write Access file.
	check("writer@foo.bar", Write, "me@here.com/foo/Access", false)

	// Other user cannot write Group file.
	check("writer@foo.bar", Write, "me@here.com/Group/bar", false)
}

// This is a simple test of basic group functioning. We still need a proper full-on test with
// a populated tree.
func TestHasAccessWithGroups(t *testing.T) {
	groups = make(map[upspin.PathName][]path.Parsed) // Forget any existing groups in the cache.

	const (
		owner = upspin.UserName("me@here.com")

		// This access file defines readers and writers but no other rights.
		accessText = "r: reader@r.com, reader@foo.bar, family\n" +
			"w: writer@foo.bar\n" +
			"d: family"

		// This is a simple group for a family.
		groupName = upspin.PathName("me@here.com/Group/family")
		groupText = "# My family\n sister@me.com, brother@me.com\n"
	)

	loadTest := func(name upspin.PathName) ([]byte, error) {
		switch name {
		case "me@here.com/Group/family":
			return []byte("# My family\n sister@me.com, brother@me.com\n"), nil
		default:
			return nil, errors.Errorf("%s not found", name)
		}
	}

	a, err := Parse(testFile, []byte(accessText))
	if err != nil {
		t.Fatal(err)
	}

	check := func(user upspin.UserName, right Right, file upspin.PathName, truth bool) {
		ok, err := a.Can(user, right, file, loadTest)
		if ok == truth {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Errorf("%s can %s %s", user, right, file)
		} else {
			t.Errorf("%s cannot %s %s", user, right, file)
		}
	}

	// Permitted group can read.
	check("sister@me.com", Read, "me@here.com/foo/bar", true)

	// Unknown member cannot read.
	check("aunt@me.com", Read, "me@here.com/foo/bar", false)

	// Group cannot write.
	check("sister@me.com", Write, "me@here.com/foo/bar", false)

	// The owner of a group is a member of the group.
	check("me@here.com", Delete, "me@here.com/foo/bar", true)

	err = RemoveGroup("me@here.com/Group/family")
	if err != nil {
		t.Fatal(err)
	}
	// Sister can't read anymore and family group is needed.
	ok, missingGroups, err := a.canNoGroupLoad("sister@me.com", Read, "me@here.com/foo/bar")
	if ok {
		t.Errorf("Expected no permission")
	}
	if len(missingGroups) != 1 {
		t.Fatalf("Expected one missing group, got %d", len(missingGroups))
	}
}

func TestParseEmptyFile(t *testing.T) {
	accessText := []byte("\n # Just a comment.\n\r\t # Nothing to see here \n \n \n\t\n")
	a, err := Parse(testFile, accessText)
	if err != nil {
		t.Fatal(err)
	}

	match(t, a.list[Read], empty)
	match(t, a.list[Write], empty)
	match(t, a.list[List], empty)
	match(t, a.list[Create], empty)
	match(t, a.list[Delete], empty)
}

func TestParseStar(t *testing.T) {
	accessText := []byte("*: joe@blow.com")
	a, err := Parse(testFile, accessText)
	if err != nil {
		t.Fatal(err)
	}
	joe := []string{"joe@blow.com"}
	match(t, a.list[Read], joe)
	match(t, a.list[Write], joe)
	match(t, a.list[List], joe)
	match(t, a.list[Create], joe)
	match(t, a.list[Delete], joe)
}

func TestParseContainsGroupName(t *testing.T) {
	accessText := []byte("r: family,*@google.com,edpin@google.com/Group/friends")
	a, err := Parse(testFile, accessText)
	if err != nil {
		t.Fatal(err)
	}
	match(t, a.list[Read], []string{"*@google.com", "edpin@google.com/Group/friends", "me@here.com/Group/family"})
	match(t, a.list[Write], empty)
	match(t, a.list[List], empty)
	match(t, a.list[Create], empty)
	match(t, a.list[Delete], empty)
}

func TestParseWrongFormat1(t *testing.T) {
	const (
		expectedErr = testFile + ":1: invalid right: \"rrrr\""
	)
	accessText := []byte("rrrr: bob@abc.com") // "rrrr" is wrong. should be just "r"
	_, err := Parse(testFile, accessText)
	if err == nil {
		t.Fatal("Expected error, got none")
	}
	if !strings.Contains(err.Error(), expectedErr) {
		t.Errorf("Expected prefix %s, got %s", expectedErr, err)
	}
}

func TestParseWrongFormat2(t *testing.T) {
	const (
		expectedErr = testFile + ":2: syntax error: invalid users list: "
	)
	accessText := []byte("#A comment\n r: a@b.co : x")
	_, err := Parse(testFile, accessText)
	if err == nil {
		t.Fatal("Expected error, got none")
	}
	if !strings.Contains(err.Error(), expectedErr) {
		t.Errorf("Expected prefix %s, got %s", expectedErr, err)
	}
}

func TestParseWrongFormat3(t *testing.T) {
	const (
		expectedErr = testFile + ":1: syntax error: invalid rights"
	)
	accessText := []byte(": bob@abc.com") // missing access format text.
	_, err := Parse(testFile, accessText)
	if err == nil {
		t.Fatal("Expected error, got none")
	}
	if !strings.Contains(err.Error(), expectedErr) {
		t.Errorf("Expected prefix %s, got %s", expectedErr, err)
	}
}

func TestParseWrongFormat4(t *testing.T) {
	const (
		expectedErr = testFile + ":1: invalid right: \"rea\""
	)
	accessText := []byte("rea:bob@abc.com") // invalid access format text.
	_, err := Parse(testFile, accessText)
	if err == nil {
		t.Fatal("Expected error, got none")
	}
	if !strings.Contains(err.Error(), expectedErr) {
		t.Errorf("Expected prefix %s, got %s", expectedErr, err)
	}
}

func TestParseMissingAccessField(t *testing.T) {
	const (
		expectedErr = testFile + ":1: syntax error: no colon on line: "
	)
	accessText := []byte("bob@abc.com")
	_, err := Parse(testFile, accessText)
	if err == nil {
		t.Fatal("Expected error, got none")
	}
	if !strings.Contains(err.Error(), expectedErr) {
		t.Errorf("Expected prefix %s, got %s", expectedErr, err)
	}
}

func TestParseTooManyFieldsOnSingleLine(t *testing.T) {
	const (
		expectedErr = testFile + ":3: syntax error: invalid users list: "
	)
	accessText := []byte("\n\nr: a@b.co r: c@b.co")
	_, err := Parse(testFile, accessText)
	if err == nil {
		t.Fatal("Expected error, got none")
	}
	if !strings.Contains(err.Error(), expectedErr) {
		t.Errorf("Expected prefix %s, got %s", expectedErr, err)
	}
}

func TestParseBadGroupPath(t *testing.T) {
	accessText := []byte("r: notanemail/Group/family")
	_, err := Parse(testFile, accessText)
	if err == nil {
		t.Fatal("expected error, got none")
	}
	if !strings.Contains(err.Error(), "group") {
		t.Fatalf("expected group error, got: %v", err)
	}
}

func TestParseBadGroupFile(t *testing.T) {
	parsed, err := path.Parse(testGroupFile)
	if err != nil {
		t.Fatal(err)
	}
	// Multiple commas not allowed.
	_, err = parseGroup(parsed, []byte("joe@me.com ,, fred@me.com"))
	if err == nil {
		t.Fatal("expected error, got none")
	}
}

func TestParseBadGroupMember(t *testing.T) {
	parsed, err := path.Parse(testGroupFile)
	if err != nil {
		t.Fatal(err)
	}
	_, err = parseGroup(parsed, []byte("joe@me.com, fred@"))
	if err == nil {
		t.Fatal("expected error, got none")
	}
	if !strings.Contains(err.Error(), "no user name") {
		t.Fatalf("expected missing user name error, got: %v", err)
	}
}

func TestMarshal(t *testing.T) {
	a, err := Parse(testFile, accessText)
	if err != nil {
		t.Fatal(err)
	}
	buf, err := a.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	b, err := UnmarshalJSON(testFile, buf)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, a, b)
}

func TestNew(t *testing.T) {
	const path = upspin.PathName("bob@foo.com/my/private/sub/dir/Access")
	a, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := Parse(path, []byte("r,w,d,c,l: bob@foo.com"))
	if err != nil {
		t.Fatal(err)
	}
	if !a.Equal(expected) {
		t.Errorf("Expected %s to equal %s", a, expected)
	}
}

func TestUsersNoGroupLoad(t *testing.T) {
	acc, err := Parse("bob@foo.com/Access",
		[]byte("r: sue@foo.com, tommy@foo.com, joe@foo.com\nw: bob@foo.com, family"))
	if err != nil {
		t.Fatal(err)
	}
	readersList, groupsNeeded, err := acc.usersNoGroupLoad(Read)
	if err != nil {
		t.Fatalf("Expected no error, got %s", err)
	}
	if len(groupsNeeded) != 0 {
		t.Errorf("Expected no groups, got %d", len(groupsNeeded))
	}
	expectedReaders := []string{"bob@foo.com", "sue@foo.com", "tommy@foo.com", "joe@foo.com"}
	expectEqual(t, expectedReaders, listFromUserName(readersList))
	writersList, groupsNeeded, err := acc.usersNoGroupLoad(Write)
	if err != nil {
		t.Fatalf("Expected no error; got %s", err)
	}
	if groupsNeeded == nil {
		t.Fatalf("Expected groups to be needed")
	}
	expectedWriters := []string{"bob@foo.com"}
	expectEqual(t, expectedWriters, listFromUserName(writersList))
	groupsExpected := []string{"bob@foo.com/Group/family"}
	expectEqual(t, groupsExpected, listFromPathName(groupsNeeded))
	// Add the missing group.
	err = AddGroup("bob@foo.com/Group/family", []byte("sis@foo.com, uncle@foo.com, grandparents"))
	if err != nil {
		t.Fatal(err)
	}
	// Try again.
	writersList, groupsNeeded, err = acc.usersNoGroupLoad(Write)
	if err != nil {
		t.Fatalf("Round 2: Expected no error %s", err)
	}
	if groupsNeeded == nil {
		t.Fatalf("Round 2: Expected groups to be needed")
	}
	groupsExpected = []string{"bob@foo.com/Group/grandparents"}
	expectEqual(t, groupsExpected, listFromPathName(groupsNeeded))
	// Add grandparents and for good measure, add the family again.
	err = AddGroup("bob@foo.com/Group/grandparents", []byte("grandpamoe@antifoo.com family"))
	if err != nil {
		t.Fatal(err)
	}
	writersList, groupsNeeded, err = acc.usersNoGroupLoad(Write)
	if err != nil {
		t.Fatal(err)
	}
	expectedWriters = []string{"bob@foo.com", "sis@foo.com", "uncle@foo.com", "grandpamoe@antifoo.com"}
	expectEqual(t, expectedWriters, listFromUserName(writersList))
}

func usersCheck(t *testing.T, load func(upspin.PathName) ([]byte, error), file upspin.PathName, data []byte, expected []string) {
	acc, err := Parse(file, data)
	if err != nil {
		t.Fatal(err)
	}
	readersList, err := acc.Users(Read, load)
	if err != nil {
		t.Fatalf("Expected no error, got %s", err)
	}
	expectEqual(t, expected, listFromUserName(readersList))
}

func TestUsers(t *testing.T) {
	loaded := false
	loadTest := func(name upspin.PathName) ([]byte, error) {
		loaded = true
		switch name {
		case "bob@foo.com/Group/friends":
			return []byte("nancy@foo.com, anna@foo.com"), nil
		default:
			return nil, errors.Errorf("%s not found", name)
		}
	}

	usersCheck(t, loadTest, "bob@foo.com/Access",
		[]byte("r: bob@foo.com, sue@foo.com, tommy@foo.com, joe@foo.com, friends"),
		[]string{"bob@foo.com", "sue@foo.com", "tommy@foo.com", "joe@foo.com", "nancy@foo.com", "anna@foo.com"})
	if !loaded {
		t.Fatalf("group file was not loaded")
	}

	// Retry with owner left out of Access.
	usersCheck(t, loadTest, "bob@foo.com/Access",
		[]byte("r: sue@foo.com, tommy@foo.com, joe@foo.com, friends"),
		[]string{"bob@foo.com", "sue@foo.com", "tommy@foo.com", "joe@foo.com", "nancy@foo.com", "anna@foo.com"})

	// Retry with repeated readers and no group.
	usersCheck(t, loadTest, "bob@foo.com/Access",
		[]byte("r: al@foo.com, sue@foo.com, bob@foo.com, tommy@foo.com, al@foo.com"),
		[]string{"bob@foo.com", "sue@foo.com", "tommy@foo.com", "al@foo.com"})

	// Retry with empty Access.
	usersCheck(t, loadTest, "bob@foo.com/Access",
		[]byte(""),
		[]string{"bob@foo.com"})

}

// We test the differenceString function used in assertEqual.
func TestDifferenceString(t *testing.T) {
	a, err := Parse(testFile, accessText)
	if err != nil {
		t.Fatal(err)
	}
	buf, err := a.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	b, err := UnmarshalJSON(upspin.PathName("me@there.com/random/Access"), buf)
	if err != nil {
		t.Fatal(err)
	}
	diff := differenceString(a, b)
	// Verify failure
	expected := "Owners don't match: me@here.com != me@there.com"
	if diff != expected {
		t.Fatalf("Expected error %s, got %s", expected, diff)
	}

	// Tweak a to force a failure.
	p, err := path.Parse("hello@here.com/foo")
	a.list[Read][1] = p // We know a.list has 3 entries.
	b, err = UnmarshalJSON(testFile, buf)
	if err != nil {
		t.Fatal(err)
	}
	diff = differenceString(a, b)
	// Verify failure
	expected = "Missing hello@here.com/foo in list b for right read"
	if diff != expected {
		t.Fatalf("Expected error %s, got %s", expected, diff)
	}
}

func assertEqual(t *testing.T, a, b *Access) {
	if str := differenceString(a, b); str != "" {
		t.Log(a)
		t.Log(b)
		t.Fatal(str)
	}
}

// differenceString returns a string describing the high-level differences between a and b or
// an empty string if they are equal.
func differenceString(a, b *Access) string {
	if len(a.list) != len(b.list) {
		return fmt.Sprintf("Lists of rights not equal length: %d != %d", len(a.list), len(b.list))
	}
	if len(a.list) != int(numRights) {
		return fmt.Sprintf("Lists of rights not equal to length of rights %d != %d", len(a.list), numRights)
	}
	for r, al := range a.list { // for each right r
		right := Right(r)
		bl := b.list[right]
		if len(al) != len(bl) {
			return fmt.Sprintf("Lists for right %s not equal length: %d != %d", right, len(al), len(bl))
		}
		bChecked := make([]int, len(bl)) // list of times each entry in b was visited.
	Outer:
		for _, pa := range al {
			for i := 0; i < len(bl); i++ {
				pb := bl[i]
				if pa.Equal(pb) {
					bChecked[i]++
					continue Outer
				}
			}
			return fmt.Sprintf("Missing %s in list b for right %s", pa, right)
		}
		for i, b := range bChecked {
			if b != 1 {
				return fmt.Sprintf("%s appears %d times, expected 1", bl[i], b)
			}
		}
	}
	if a.owner != b.owner {
		return fmt.Sprintf("Owners don't match: %s != %s", a.owner, b.owner)
	}
	if a.domain != b.domain {
		return fmt.Sprintf("Domains don't match: %s != %s", a.domain, b.domain)
	}
	if !a.parsed.Equal(b.parsed) {
		return fmt.Sprintf("Names don't match: %s != %s", a.parsed, b.parsed)
	}
	return ""
}

func TestIsAccessFile(t *testing.T) {
	tests := []struct {
		name     upspin.PathName
		isAccess bool
	}{
		{"a@b.com/Access", true},
		{"a@b.com/foo/bar/Access", true},
		{"a@b.com/NotAccess", false},
		{"a@b.com//Access/", true},     // Extra slashes don't matter.
		{"a@b.com//Access/foo", false}, //Access is not a directory.
		{"/Access/foo", false},         // No user.
	}
	for _, test := range tests {
		isAccess := IsAccessFile(test.name)
		if isAccess == test.isAccess {
			continue
		}
		if isAccess {
			t.Errorf("%q is not an access file; IsAccessFile says it is", test.name)
		}
		if !isAccess {
			t.Errorf("%q is an access file; IsAccessFile says not", test.name)
		}
	}
}

func TestIsGroupFile(t *testing.T) {
	tests := []struct {
		name    upspin.PathName
		isGroup bool
	}{
		{"a@b.com/Group/foo", true},
		{"a@b.com/Group/foo/bar", true},
		{"a@b.com//Group/", false},   // No file.
		{"a@b.com//Group/foo", true}, // Extra slashes don't matter.
		{"/Group/foo", false},        // No user.
	}
	for _, test := range tests {
		isGroup := IsGroupFile(test.name)
		if isGroup == test.isGroup {
			continue
		}
		if isGroup {
			t.Errorf("%q is not a group file; IsGroupFile says it is", test.name)
		}
		if !isGroup {
			t.Errorf("%q is a group file; IsGroupFile says not", test.name)
		}
	}
}

// match requires the two slices to be equivalent, assuming no duplicates.
// The print of the path (ignoring the final / for a user name) must match the string.
// The lists are sorted, because Access.Parse sorts them.
func match(t *testing.T, want []path.Parsed, expect []string) {
	if len(want) != len(expect) {
		t.Fatalf("Expected %d paths %q, got %d: %v", len(expect), expect, len(want), want)
	}
	for i, path := range want {
		var compare string
		if path.IsRoot() {
			compare = string(path.User())
		} else {
			compare = path.String()
		}
		if compare != expect[i] {
			t.Errorf("User %s not found in at position %d in list", compare, i)
			t.Errorf("expect: %q; got %q", expect, want)
		}
	}
}

// expectState checks whether the results of IsAccessFile match with expectations and if not it fails the test.
func expectState(t *testing.T, expectIsFile bool, pathName upspin.PathName) {
	isFile := IsAccessFile(pathName)
	if expectIsFile != isFile {
		t.Fatalf("Expected %v, got %v", expectIsFile, isFile)
	}
}

// expectEqual fails if the two lists do not have the same contents, irrespective of order.
func expectEqual(t *testing.T, expected []string, gotten []string) {
	sort.Strings(expected)
	sort.Strings(gotten)
	if len(expected) != len(gotten) {
		t.Fatalf("Length mismatched, expected %d, got %d: %v vs %v", len(expected), len(gotten), expected, gotten)
	}
	if !reflect.DeepEqual(expected, gotten) {
		t.Fatalf("Expected %v got %v", expected, gotten)
	}
}

func listFromPathName(p []upspin.PathName) []string {
	ret := make([]string, len(p))
	for i, v := range p {
		ret[i] = string(v)
	}
	return ret
}

func listFromUserName(u []upspin.UserName) []string {
	ret := make([]string, len(u))
	for i, v := range u {
		ret[i] = string(v)
	}
	return ret
}
