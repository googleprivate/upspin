package gcpstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"upspin.googlesource.com/upspin.git/cloud/netutil"
	"upspin.googlesource.com/upspin.git/cloud/netutil/nettest"
	"upspin.googlesource.com/upspin.git/upspin"
)

const (
	errSomethingBad = "Something bad happened on the internet"
	errBrokenPipe   = "The internet has a broken pipe"
	contentRef      = "my ref"
)

var (
	refStruct = struct{ Ref string }{Ref: contentRef}

	newLocation = upspin.Location{
		Reference: "new reference",
		Endpoint: upspin.Endpoint{
			Transport: upspin.GCP,
			NetAddr:   upspin.NetAddr("http://localhost:8080"),
		},
	}
)

func TestStorePutError(t *testing.T) {
	// The server will error out.
	resp := nettest.MockHTTPResponse{
		Error:    errors.New(errSomethingBad),
		Response: nil,
	}
	mock := nettest.NewMockHTTPClient([]nettest.MockHTTPResponse{resp}, []*http.Request{nettest.AnyRequest})
	s := New("http://localhost:8080", mock)

	_, err := s.Put([]byte("contents"))

	expected := fmt.Sprintf("Store: Put: %v", errSomethingBad)
	if err.Error() != expected {
		t.Fatalf("Server reply failed: expected %v got %v", expected, err)
	}

	mock.Verify(t)
}

func TestStorePut(t *testing.T) {
	// The server will respond with a location for the object.
	req := nettest.NewRequest(t, netutil.Post, "http://localhost:8080/put", []byte("*"))
	mock := nettest.NewMockHTTPClient(createMockPutResponse(t), []*http.Request{req})
	s := New("http://localhost:8080", mock)

	contents := []byte("contents")
	ref, err := s.Put(contents)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if ref != contentRef {
		t.Fatalf("Server gave us wrong location. Expected %v, got %v", contentRef, ref)
	}
	// Verify the server received the proper request
	mock.Verify(t)

	// Further ensure we sent the right number of bytes
	bytesSent := mock.Requests()[0].ContentLength
	if bytesSent != 245 {
		t.Errorf("Wrong number of bytes sent. Expected 245, got %v", bytesSent)
	}
}

func TestStoreGetError(t *testing.T) {
	// The server will error out.
	resp := nettest.MockHTTPResponse{
		Error:    errors.New(errBrokenPipe),
		Response: nil,
	}
	mock := nettest.NewMockHTTPClient([]nettest.MockHTTPResponse{resp}, []*http.Request{nettest.AnyRequest})
	s := New("http://localhost:8080", mock)

	_, _, err := s.Get("1234")

	if err == nil {
		t.Fatalf("Expected an error, got nil")
	}
	expected := fmt.Sprintf("Store: Get: 1234: server error: %s", errBrokenPipe)
	if err.Error() != expected {
		t.Fatalf("Server reply failed: expected %v got %v", expected, err)
	}
	mock.Verify(t)
}

func TestStoreGetErrorEmptyRef(t *testing.T) {
	// Our request is invalid.
	mock := nettest.NewMockHTTPClient(nil, nil)
	s := New("http://localhost:8080", mock)

	_, _, err := s.Get("")

	if err == nil {
		t.Fatalf("Expected an error, got nil")
	}
	expected := fmt.Sprintf("Store: Get: %s", invalidRefError)
	if err.Error() != expected {
		t.Fatalf("Server reply failed: expected %v got %v", expected, err)
	}
}

func TestStoreGetRedirect(t *testing.T) {
	// The server will redirect the client to a new location
	const LookupRef = "XX some hash XX"
	mock := nettest.NewMockHTTPClient(createMockGetResponse(t), []*http.Request{
		nettest.NewRequest(t, netutil.Get, fmt.Sprintf("http://localhost:8080/get?ref=%s", LookupRef), nil),
	})

	s := New("http://localhost:8080", mock)

	data, locs, err := s.Get(LookupRef)

	if data != nil {
		t.Fatal("Got data when we expected to get redirected")
	}
	if err != nil {
		t.Fatalf("Got an unexpected error: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("Expected 1 location, got %d", len(locs))
	}
	if locs[0] != newLocation {
		t.Fatalf("Server gave us wrong location. Expected %v, got %v", newLocation, locs[0])
	}
	// Verifies request was sent correctly
	mock.Verify(t)
}

func TestStoreDeleteInvalidRef(t *testing.T) {
	// No requests are sent
	mock := nettest.NewMockHTTPClient(
		[]nettest.MockHTTPResponse{},
		[]*http.Request{})

	s := New("http://localhost:8080", mock)
	err := s.Delete("")
	if err == nil {
		t.Fatal("Expected error, got none")
	}
	errInvalidRef := "Store: Delete: invalid reference"
	if err.Error() != errInvalidRef {
		t.Fatalf("Expected error %v, got %v", errInvalidRef, err)
	}
	mock.Verify(t)
}

func TestStoreDelete(t *testing.T) {
	const Ref = "xyz"
	mock := nettest.NewMockHTTPClient(
		[]nettest.MockHTTPResponse{nettest.NewMockHTTPResponse(200, "application/json", []byte(`{"error":"success"}`))},
		[]*http.Request{nettest.NewRequest(t, netutil.Post, fmt.Sprintf("http://localhost:8080/delete?ref=%s", Ref), nil)})

	s := New("http://localhost:8080", mock)
	err := s.Delete(Ref)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	mock.Verify(t)
}

func createMockGetResponse(t *testing.T) []nettest.MockHTTPResponse {
	newLocJSON, err := json.Marshal(newLocation)
	if err != nil {
		t.Fatalf("JSON marshal failed: %v", err)
	}
	resp := nettest.NewMockHTTPResponse(200, "application/json", newLocJSON)
	return []nettest.MockHTTPResponse{resp}
}

func createMockPutResponse(t *testing.T) []nettest.MockHTTPResponse {
	refStructJSON, err := json.Marshal(refStruct)
	if err != nil {
		t.Fatalf("JSON marshal failed: %v", err)
	}
	resp := nettest.NewMockHTTPResponse(200, "application/json", refStructJSON)
	return []nettest.MockHTTPResponse{resp}
}
