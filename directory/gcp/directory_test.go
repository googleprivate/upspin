package directory

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"testing"

	"upspin.googlesource.com/upspin.git/access"
	"upspin.googlesource.com/upspin.git/cloud/netutil"
	"upspin.googlesource.com/upspin.git/cloud/netutil/nettest"
	store "upspin.googlesource.com/upspin.git/store/gcp"
	"upspin.googlesource.com/upspin.git/upspin"
)

const (
	pathName    = "bob@jones.com/myroot/mysubdir"
	badPathName = "invalid/path/name"
)

var (
	errBadConnection       = errors.New("bad internet connection")
	errBadPatternUserName  = errors.New("bad user name in path")
	errMkdirBadConnection  = newError("MakeDirectory", pathName, errBadConnection)
	errLookupBadConnection = newError("Lookup", pathName, errBadConnection)
	errPutBadConnection    = newError("Put", pathName, errBadConnection)
	errGlobBadPattern      = newError("Glob", badPathName, errBadPatternUserName)
	key                    = "the key"
	fileContents           = []byte("contents of file")
	reference              = upspin.Reference{
		Key: key,
	}
	location = upspin.Location{
		Reference: reference,
		Endpoint: upspin.Endpoint{
			Transport: upspin.GCP,
			NetAddr:   upspin.NetAddr("http://localhost:8080"),
		},
	}
	dirEntry = upspin.DirEntry{
		Name:     pathName,
		Location: location,
		Metadata: upspin.Metadata{
			IsDir:    false,
			Sequence: 0,
			PackData: []byte("Packed metadata"),
		},
	}
)

func TestMkdirError(t *testing.T) {
	d := newErroringDirectoryClient()

	_, err := d.MakeDirectory(upspin.PathName(pathName))
	if err == nil {
		t.Fatalf("Expected error, got none")
	}
	if err.Error() != errMkdirBadConnection.Error() {
		t.Fatalf("Expected error %v, got %v", errMkdirBadConnection, err)
	}
}

func TestMkdir(t *testing.T) {
	mock := nettest.NewMockHTTPClient(newMockMkdirResponse(t))

	d := newDirectory("http://localhost:8080", nil, mock)

	loc, err := d.MakeDirectory(upspin.PathName(pathName))
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if loc != location {
		t.Fatalf("Location differs. Expected %v, got %v", location, loc)
	}
	// Verifies request was sent correctly
	mkdirEntry := dirEntry
	mkdirEntry.Location = upspin.Location{}
	mkdirEntry.Metadata.IsDir = true
	mkdirEntry.Metadata.PackData = nil
	request := nettest.NewRequest(t, netutil.Post, "http://localhost:8080/put", toJSON(t, mkdirEntry))
	mock.Verify(t, []*http.Request{request})
}

func newMockMkdirResponse(t *testing.T) []nettest.MockHTTPResponse {
	return []nettest.MockHTTPResponse{newMockLocationResponse(t)}
}

func newMockLocationResponse(t *testing.T) nettest.MockHTTPResponse {
	loc, err := json.Marshal(location)
	if err != nil {
		t.Fatalf("JSON marshal failed: %v", err)
	}
	return newResp(loc)
}

func newMockKeyResponse(t *testing.T) nettest.MockHTTPResponse {
	keyJSON, err := json.Marshal(&struct{ Key string }{Key: key})
	if err != nil {
		t.Fatalf("JSON marshal failed: %v", err)
	}
	return newResp(keyJSON)
}

func newMockLookupResponse(t *testing.T) []nettest.MockHTTPResponse {
	dir, err := json.Marshal(dirEntry)
	if err != nil {
		t.Fatalf("JSON marshal failed: %v", err)
	}
	resp := newResp(dir)
	return []nettest.MockHTTPResponse{resp}
}

func TestLookupError(t *testing.T) {
	d := newErroringDirectoryClient()

	_, err := d.Lookup(upspin.PathName(pathName))
	if err == nil {
		t.Fatalf("Expected error, got none")
	}
	if err.Error() != errLookupBadConnection.Error() {
		t.Fatalf("Expected error %v, got %v", errLookupBadConnection, err)
	}
}

func TestLookup(t *testing.T) {
	mock := nettest.NewMockHTTPClient(newMockLookupResponse(t))

	d := newDirectory("http://localhost:8080", nil, mock)

	dir, err := d.Lookup(pathName)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if dir == nil {
		t.Fatal("Got a nil dirEntry")
	}
	if !dirEntryEquals(&dirEntry, dir) {
		t.Fatalf("Invalid dirEntry. Expected %v, got %v", dirEntry, dir)
	}
}

func dirEntryEquals(a, b *upspin.DirEntry) bool {
	if string(a.Name) != string(b.Name) {
		log.Println("Pathnames differ")
		return false
	}
	if a.Metadata.IsDir != b.Metadata.IsDir {
		log.Println("IsDir differ")
		return false
	}
	if a.Metadata.Sequence != b.Metadata.Sequence {
		log.Println("Sequences differ")
		return false
	}
	for i, k := range a.Metadata.PackData {
		if k != b.Metadata.PackData[i] {
			log.Println("PackData differ")
			return false
		}
	}
	return true
}

func newErroringDirectoryClient() upspin.Directory {
	resp := nettest.MockHTTPResponse{
		Error:    errBadConnection,
		Response: nil,
	}
	mock := nettest.NewMockHTTPClient([]nettest.MockHTTPResponse{resp})

	return newDirectory("http://localhost:8080", nil, mock)
}

func newStore(client store.HTTPClientInterface) upspin.Store {
	context := store.Context{
		Client: client,
	}
	e := upspin.Endpoint{
		Transport: upspin.GCP,
		NetAddr:   upspin.NetAddr("http://localhost:8080"),
	}
	s, err := access.BindStore(context, e)
	if err != nil {
		log.Fatalf("Can't bind: %v", err)
	}
	return s
}

// newDirectoryClientWithStoreClient creates an upspin.Directory that
// contains a valid upspin.Store which replies successfully to a Put
// request. The dirClientResponse is loaded onto the Directory client
// for testing. Returns the Directory as well as the mock client for
// post-request inspections.
func newDirectoryClientWithStoreClient(t *testing.T, dirClientResponse nettest.MockHTTPResponse) (upspin.Directory, *nettest.MockHTTPClient) {
	// The HTTP client will return a sequence of responses, the
	// first one will be to the Store.Put request, then the second
	// to the Directory.Put request.  Setup the mock client
	mock := nettest.NewMockHTTPClient([]nettest.MockHTTPResponse{newMockKeyResponse(t), dirClientResponse})

	// Get a Store client
	s := newStore(mock)

	// Get a Directory client
	return newDirectory("http://localhost:9090", s, mock), mock
}

func TestPutError(t *testing.T) {
	d, _ := newDirectoryClientWithStoreClient(t, nettest.MockHTTPResponse{
		Error:    errBadConnection,
		Response: nil,
	})

	_, err := d.Put(upspin.PathName(pathName), []byte("contents"), []byte("Packed metadata"))
	if err == nil {
		t.Fatalf("Expected error, got none")
	}
	if err.Error() != errPutBadConnection.Error() {
		t.Fatalf("Expected error %v, got %v", errPutBadConnection, err)
	}
}

func TestPut(t *testing.T) {
	respSuccess := nettest.NewMockHTTPResponse(200, "application/json", []byte(`{"error":"Success"}`))

	d, mock := newDirectoryClientWithStoreClient(t, respSuccess)

	// Issue the put request
	loc, err := d.Put(upspin.PathName(pathName), fileContents, []byte("Packed metadata"))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if loc.Reference.Key != key {
		t.Fatalf("Invalid key in location. Expected %v, got %v", key, loc.Reference.Key)
	}
	// In the first request we don't care to match the body
	// exactly, since that's what Store sent to the server and
	// Store has been tested elsewhere.
	storeRequest := nettest.NewRequest(t, netutil.Post, "http://localhost:8080/put", []byte("*"))

	// Verify we sent to the Directory service the Reference.Key we got back from the Store server
	dirEntryJSON := toJSON(t, dirEntry)
	expectedRequest := nettest.NewRequest(t, netutil.Post, "http://localhost:9090/put", dirEntryJSON)

	mock.Verify(t, []*http.Request{storeRequest, expectedRequest})
}

func TestGlobBadPattern(t *testing.T) {
	resp := nettest.MockHTTPResponse{
		Error:    errGlobBadPattern,
		Response: nil,
	}
	mock := nettest.NewMockHTTPClient([]nettest.MockHTTPResponse{resp})

	d := newDirectory("http://localhost:8080", nil, mock)

	_, err := d.Glob(badPathName)
	if err == nil {
		t.Fatal("Expected error, got none")
	}
	if err.Error() != errGlobBadPattern.Error() {
		t.Fatalf("Expected error %q, got %q", errGlobBadPattern, err)
	}
}

func TestGlob(t *testing.T) {
	// Set up all the responses from the server:
	// First, the server will give us 3 paths from a /list request.
	// Then it will send two DirEntry due to our two Lookup requests.
	// We later check that we issued one list request and two Lookup requests.

	const (
		path0 = "a@b.co/dir1/file1.txt"
		path1 = "a@b.co/dir1/file2.txt"
	)
	responses := []nettest.MockHTTPResponse{
		newResp([]byte(fmt.Sprintf(`{ "Names": ["%v","%v","a@b.co/dir1/file3.pdf"]}`, path0, path1))),
		newResp(toJSON(t, newDirEntry(upspin.PathName(path0)))),
		newResp(toJSON(t, newDirEntry(upspin.PathName(path1)))),
	}
	expectedRequests := []*http.Request{
		nettest.NewRequest(t, netutil.Get, "http://localhost:9090/list?prefix=a@b.co/dir1", nil),
		nettest.NewRequest(t, netutil.Get, fmt.Sprintf("http://localhost:9090/get?pathname=%v", path0), nil),
		nettest.NewRequest(t, netutil.Get, fmt.Sprintf("http://localhost:9090/get?pathname=%v", path1), nil),
	}

	mock := nettest.NewMockHTTPClient(responses)
	d := newDirectory("http://localhost:9090", nil, mock)

	dirEntries, err := d.Glob("a@b.co/dir1/*.txt")
	if err != nil {
		t.Fatalf("Unexpected error occurred: %v", err)
	}
	if len(dirEntries) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(dirEntries))
	}
	if string(dirEntries[0].Name) != path0 {
		t.Errorf("Expected 0th entry %v, got %v", path0, dirEntries[0].Name)
	}
	if string(dirEntries[1].Name) != path1 {
		t.Errorf("Expected 1st entry %v, got %v", path1, dirEntries[1].Name)
	}
	mock.Verify(t, expectedRequests)
}

// newDirEntry creates a new DirEntry with the given path name
func newDirEntry(pathName upspin.PathName) *upspin.DirEntry {
	return &upspin.DirEntry{
		Name: pathName,
	}
}

// toJSON is a convenience function for marshaling data into JSON
func toJSON(t *testing.T, data interface{}) []byte {
	d, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("Can't marshal to JSON: %v", err)
	}
	return d
}

// newResp is a convenience function that creates a successful MockHTTPResponse with JSON data.
func newResp(data []byte) nettest.MockHTTPResponse {
	return nettest.NewMockHTTPResponse(200, "application/json", data)
}

func newDirectory(serverURL string, storeService upspin.Store, client HTTPClientInterface) upspin.Directory {
	context := Context{
		StoreService: storeService,
		Client:       client,
	}
	e := upspin.Endpoint{
		Transport: upspin.GCP,
		NetAddr:   upspin.NetAddr(serverURL),
	}
	dir, err := access.BindDirectory(context, e)
	if err != nil {
		log.Fatalf("Can't BindDirectory: %v", err)
	}
	return dir
}
