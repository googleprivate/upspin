package path

import (
	"testing"

	"upspin.googlesource.com/upspin.git/upspin"
)

func newP(elems []string) Parsed {
	return Parsed{
		User:  "u@google.com",
		Elems: elems,
	}
}

type parseTest struct {
	path     upspin.PathName
	parse    Parsed
	isRoot   bool
	filePath string
}

var goodParseTests = []parseTest{
	{"u@google.com", newP([]string{}), true, "/"},
	{"u@google.com/", newP([]string{}), true, "/"},
	{"u@google.com/a", newP([]string{"a"}), false, "/a"},
	{"u@google.com/a/", newP([]string{"a"}), false, "/a"},
	{"u@google.com/a///b/c/d/", newP([]string{"a", "b", "c", "d"}), false, "/a/b/c/d"},
	{"u@google.com//a///b/c/d//", newP([]string{"a", "b", "c", "d"}), false, "/a/b/c/d"},
	// Longer than the backing array in Parsed.
	{"u@google.com/a/b/c/d/e/f/g/h/i/j/k/l/m",
		newP([]string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"}),
		false,
		"/a/b/c/d/e/f/g/h/i/j/k/l/m"},
	// Dot.
	{"u@google.com/.", newP([]string{}), true, "/"},
	{"u@google.com/a/../b", newP([]string{"b"}), false, "/b"},
	{"u@google.com/./a///b/./c/d/./.", newP([]string{"a", "b", "c", "d"}), false, "/a/b/c/d"},
	// Dot-Dot.
	{"u@google.com/..", newP([]string{}), true, "/"},
	{"u@google.com/a/../b", newP([]string{"b"}), false, "/b"},
	{"u@google.com/../a///b/../c/d/..", newP([]string{"a", "c"}), false, "/a/c"},
}

func TestParse(t *testing.T) {
	for _, test := range goodParseTests {
		pn, err := Parse(test.path)
		if err != nil {
			t.Errorf("%q: unexpected error %v", test.path, err)
			continue
		}
		if !pn.Equal(test.parse) {
			t.Errorf("%q: expected %v got %v", test.path, test.parse, pn)
			continue
		}
		if test.isRoot != pn.IsRoot() {
			t.Errorf("%q: expected IsRoot %v, got %v", test.path, test.isRoot, pn.IsRoot())
		}
		filePath := pn.FilePath()
		if filePath != test.filePath {
			t.Errorf("%q: DirPath expected %v got %v", test.path, test.filePath, filePath)
		}
	}
}

func TestCountMallocs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping malloc count in short mode")
	}
	parse := func() {
		Parse("u@google.com/a/b/c/d/e/f/g")
	}
	mallocs := testing.AllocsPerRun(100, parse)
	if mallocs != 1 {
		t.Errorf("got %d allocs, want <=1", int64(mallocs))
	}
}

var badParseTests = []upspin.PathName{
	"u@x/a/b",  // User name too short.
	"user/a/b", // Invalid user name.
}

func TestBadParse(t *testing.T) {
	for _, test := range badParseTests {
		_, err := Parse(test)
		if err == nil {
			t.Errorf("%q: error, got none", test)
			continue
		}
	}
}

// The join and clean tests are based on those in Go's path/path_test.go.
type JoinTest struct {
	elem []string
	path upspin.PathName
}

var jointests = []JoinTest{
	// zero parameters
	{[]string{}, ""},

	// one parameter
	{[]string{""}, ""},
	{[]string{"a"}, "a"},

	// two parameters
	{[]string{"a", "b"}, "a/b"},
	{[]string{"a", ""}, "a"},
	{[]string{"", "b"}, "b"},
	{[]string{"/", "a"}, "/a"},
	{[]string{"/", ""}, "/"},
	{[]string{"a/", "b"}, "a/b"},
	{[]string{"a/", ""}, "a"},
	{[]string{"", ""}, ""},
}

// join takes a []string and passes it to Join.
func join(args ...string) upspin.PathName {
	if len(args) == 0 {
		return Join("")
	}
	return Join(upspin.PathName(args[0]), args[1:]...)
}

func TestJoin(t *testing.T) {
	for _, test := range jointests {
		if p := join(test.elem...); p != test.path {
			t.Errorf("join(%q) = %q, want %q", test.elem, p, test.path)
		}
	}
}

type pathTest struct {
	path, result upspin.PathName
}

var cleantests = []pathTest{
	// Already clean
	{"", "."},
	{"abc", "abc"},
	{"abc/def", "abc/def"},
	{"a/b/c", "a/b/c"},
	{".", "."},
	{"..", ".."},
	{"../..", "../.."},
	{"../../abc", "../../abc"},
	{"/abc", "/abc"},
	{"/", "/"},

	// Remove trailing slash
	{"abc/", "abc"},
	{"abc/def/", "abc/def"},
	{"a/b/c/", "a/b/c"},
	{"./", "."},
	{"../", ".."},
	{"../../", "../.."},
	{"/abc/", "/abc"},

	// Remove doubled slash
	{"abc//def//ghi", "abc/def/ghi"},
	{"//abc", "/abc"},
	{"///abc", "/abc"},
	{"//abc//", "/abc"},
	{"abc//", "abc"},

	// Remove . elements
	{"abc/./def", "abc/def"},
	{"/./abc/def", "/abc/def"},
	{"abc/.", "abc"},

	// Remove .. elements
	{"abc/def/ghi/../jkl", "abc/def/jkl"},
	{"abc/def/../ghi/../jkl", "abc/jkl"},
	{"abc/def/..", "abc"},
	{"abc/def/../..", "."},
	{"/abc/def/../..", "/"},
	{"abc/def/../../..", ".."},
	{"/abc/def/../../..", "/"},
	{"abc/def/../../../ghi/jkl/../../../mno", "../../mno"},

	// Combinations
	{"abc/./../def", "def"},
	{"abc//./../def", "def"},
	{"abc/../../././../def", "../../def"},

	// Now some real Upspin paths.
	{"joe@blow.com", "joe@blow.com/"}, // User root always has a trailing slash.
	{"joe@blow.com/", "joe@blow.com/"},
	{"joe@blow.com/..", "joe@blow.com/"},
	{"joe@blow.com/../", "joe@blow.com/"},
	{"joe@blow.com/a/b/../b/c", "joe@blow.com/a/b/c"},
}

func TestClean(t *testing.T) {
	for _, test := range cleantests {
		if s := Clean(test.path); s != test.result {
			t.Errorf("Clean(%q) = %q, want %q", test.path, s, test.result)
		}
		if s := Clean(test.result); s != test.result {
			t.Errorf("Clean(%q) = %q, want %q", test.result, s, test.result)
		}
	}
}

func TestUserAndDomain(t *testing.T) {
	type cases struct {
		userName upspin.UserName
		user     string
		domain   string
		err      error
	}
	var (
		tests = []cases{
			{upspin.UserName("me@here.com"), "me", "here.com", nil},
			{upspin.UserName("@"), "", "", errUserName},
			{upspin.UserName("a@bcom"), "", "", errUserName},
			{upspin.UserName("a@b@.com"), "", "", errUserName},
			{upspin.UserName("@bbc.com"), "", "", errUserName},
			{upspin.UserName("abc.com@"), "", "", errUserName},
			{upspin.UserName("a@b.co"), "a", "b.co", nil},
			{upspin.UserName("me@here/.com"), "", "", errUserName},
		}
	)
	for _, test := range tests {
		u, d, err := UserAndDomain(test.userName)
		if err != test.err {
			t.Fatalf("Expected %q, got %q", test.err, err)
		}
		if err != nil {
			// Already validated the error
			continue
		}
		if u != test.user {
			t.Errorf("Expected user %q, got %q", test.user, u)
		}
		if d != test.domain {
			t.Errorf("Expected domain %q, got %q", test.domain, u)
		}
	}
}

type compareTest struct {
	path1, path2 upspin.PathName
	expect       int
}

var compareTests = []compareTest{
	// Some the same
	{"joe@bar.com", "joe@bar.com", 0},
	{"joe@bar.com/", "joe@bar.com", 0},
	{"joe@bar.com/", "joe@bar.com/", 0},
	{"joe@bar.com/a/b/c", "joe@bar.com/a/b/c", 0},
	// Same domain sorts by user.
	{"joe@bar.com", "adam@bar.com", 1},
	{"joe@bar.com/a/b/c", "adam@bar.com/a/b/c", 1},
	{"adam@bar.com", "joe@bar.com", -1},
	{"adam@bar.com/a/b/c", "joe@bar.com/a/b/c", -1},
	// Different paths.
	{"joe@bar.com/a/b/c", "joe@bar.com/a/b/d", -1},
	{"joe@bar.com/a/b/d", "joe@bar.com/a/b/c", 1},
	// Different length paths.
	{"joe@bar.com/a/b/c", "joe@bar.com/a/b/c/d", -1},
	{"joe@bar.com/a/b/c/d", "joe@bar.com/a/b/c", 1},
}

func TestCompare(t *testing.T) {
	for _, test := range compareTests {
		p1, err := Parse(test.path1)
		if err != nil {
			t.Fatalf("%s: %s\n", test.path1, err)
		}
		p2, err := Parse(test.path2)
		if err != nil {
			t.Fatalf("%s: %s\n", test.path2, err)
		}
		if got := p1.Compare(p2); got != test.expect {
			t.Errorf("Compare(%q, %q) = %d; expected %d", test.path1, test.path2, got, test.expect)
		}
		// Verify for compare too.
		if (p1.Compare(p2) == 0) != p1.Equal(p2) {
			t.Errorf("Equal(%q, %q) = %t; expected otherwise", test.path1, test.path2, p1.Equal(p2))
		}
	}
}

type pathTestWithCount struct {
	path   upspin.PathName
	count  int
	expect upspin.PathName
}

var dropPathTests = []pathTestWithCount{
	{"a@b.co/a/b", 1, "a@b.co/a"},
	{"a@b.co/a/b", 2, "a@b.co/"},
	// Won't go past the root.
	{"a@b.co/a/b", 3, "a@b.co/"},
	// Multiple slashes are OK.
	{"a@b.co/a/b///", 1, "a@b.co/a"},
	{"a@b.co///a//////b///c/////", 2, "a@b.co/a"},
}

func TestDropPath(t *testing.T) {
	for _, test := range dropPathTests {
		got := DropPath(test.path, test.count)
		if got != test.expect {
			t.Errorf("DropPath(%q, %d) = %q; expected %q", test.path, test.count, got, test.expect)
		}
	}
}

var firstPathTests = []pathTestWithCount{
	{"a@b.co/a/b/c/d", 0, "a@b.co/"},
	{"a@b.co/a/b/c/d", 1, "a@b.co/a"},
	{"a@b.co/a/b/c/d", 2, "a@b.co/a/b"},
	{"a@b.co/a/b/c/d/", 3, "a@b.co/a/b/c"},
	{"a@b.co/a/b/c/d/", 4, "a@b.co/a/b/c/d"},
	{"a@b.co/a/b/c/d", 10, "a@b.co/a/b/c/d"},
	{"a@b.co/a/b/c/d/", 10, "a@b.co/a/b/c/d"},
	// Multiple slashes are OK.
	{"a@b.co/a/b///", 1, "a@b.co/a"},
	{"a@b.co///a//////b///c/////", 2, "a@b.co/a/b"},
}

func TestFirstPath(t *testing.T) {
	for _, test := range firstPathTests {
		got := FirstPath(test.path, test.count)
		if got != test.expect {
			t.Errorf("FirstPath(%q, %d) = %q; expected %q", test.path, test.count, got, test.expect)
		}
	}
}
