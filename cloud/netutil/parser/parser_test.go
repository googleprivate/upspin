package parser

import (
	"encoding/json"
	"testing"

	"upspin.googlesource.com/upspin.git/upspin"
)

var (
	location = upspin.Location{
		Endpoint: upspin.Endpoint{
			Transport: upspin.GCP,
			NetAddr:   "some server",
		},
		Reference: upspin.Reference{
			Key: "abcd",
		},
	}
	dirEntry = upspin.DirEntry{
		Name:     "foo",
		Location: location,
	}
)

func TestLocationResponse(t *testing.T) {
	locJSON, err := json.Marshal(location)
	if err != nil {
		t.Fatal(err)
	}
	loc, err := LocationResponse(locJSON)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if loc == nil {
		t.Fatal("Expected a valid location, got nil")
	}
	if loc.Reference.Key != location.Reference.Key {
		t.Errorf("Expected key %v, got %v", location.Reference.Key, loc.Reference.Key)
	}
}

func TestLocationResponseBadError(t *testing.T) {
	loc, err := LocationResponse([]byte(`{"endpoint:"foo", "bla bla bla"}`))
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if loc != nil {
		t.Fatalf("Expected a zero Location, got %v", *loc)
	}
	expectedError := `can't parse reply from server: invalid character 'f' after object key, {"endpoint:"foo", "bla bla bla"}`
	if err.Error() != expectedError {
		t.Fatalf("Expected error %q, got %q", expectedError, err)
	}
}

func TestLocationResponseBadErrorAgain(t *testing.T) {
	loc, err := LocationResponse([]byte(""))
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if loc != nil {
		t.Fatalf("Expected a zero Location, got %v", *loc)
	}
	expectedError := "can't parse reply from server: unexpected end of JSON input, "
	if err.Error() != expectedError {
		t.Fatalf("Expected error %q, got %q", expectedError, err)
	}
}

func TestLocationResponseWithProperError(t *testing.T) {
	loc, err := LocationResponse([]byte(`{"error":"something bad happened"}`))
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if loc != nil {
		t.Fatalf("Expected a zero Location, got %v", *loc)
	}
	expectedError := "something bad happened"
	if err.Error() != expectedError {
		t.Fatalf("Expected error %q, got %q", expectedError, err)
	}
}

func TestKeyResponse(t *testing.T) {
	key, err := KeyResponse([]byte(`{"key": "1234"}`))
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if key != "1234" {
		t.Errorf("Expected key 1234, got %v", key)
	}
}

func TestKeyResponseBadError(t *testing.T) {
	key, err := KeyResponse([]byte("bla bla bla"))
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if key != "" {
		t.Fatalf("Expected a nil key, got %v", key)
	}
	expectedError := "can't parse reply from server: invalid character 'b' looking for beginning of value, bla bla bla"
	if err.Error() != expectedError {
		t.Fatalf("Expected error %q, got %q", expectedError, err)
	}
}

func TestDirEntryResponse(t *testing.T) {
	dirJSON, err := json.Marshal(dirEntry)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := DirEntryResponse(dirJSON)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if dir == nil {
		t.Fatal("Expected a valid dirEntry, got nil")
	}
	if dir.Location.Reference.Key != dirEntry.Location.Reference.Key {
		t.Errorf("Expected key %v, got %v", dirEntry.Location.Reference.Key, dir.Location.Reference.Key)
	}
}

func TestDirEntryResponseBadError(t *testing.T) {
	dir, err := DirEntryResponse([]byte(`{"Name":"path","Location":"loc"}`))
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if dir != nil {
		t.Fatalf("Expected a nil DirEntry, got %v", *dir)
	}
	expectedError := `can't parse reply from server: <nil>, {"Name":"path","Location":"loc"}`
	if err.Error() != expectedError {
		t.Fatalf("Expected error %q, got %q", expectedError, err)
	}
}

func TestDirEntryResponseZeroDirEntry(t *testing.T) {
	dir, err := DirEntryResponse(nil)
	if err == nil {
		t.Fatal("Expected error, got none")
	}
	if dir != nil {
		t.Errorf("Expected nil dir, got %v", *dir)
	}
	expectedError := "can't parse reply from server: unexpected end of JSON input, "
	if err.Error() != expectedError {
		t.Fatalf("Expected error %q, got %q", expectedError, err)
	}
}

func TestDirEntryResponseWithProperError(t *testing.T) {
	dir, err := DirEntryResponse([]byte(`{"error":"something terrible happened"}`))
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if dir != nil {
		t.Fatalf("Expected a nil DirEntry, got %v", *dir)
	}
	expectedError := "something terrible happened"
	if err.Error() != expectedError {
		t.Fatalf("Expected error %q, got %q", expectedError, err)
	}
}
