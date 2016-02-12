package parser

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"upspin.googlesource.com/upspin.git/upspin"
)

// LocationResponse interprets the body of an HTTP response as
// Location and returns it. If it's not a Location, it tries to read
// an error message instead.
func LocationResponse(body []byte) (*upspin.Location, error) {
	var loc, zeroLoc upspin.Location
	err := json.Unmarshal(body, &loc)
	if err != nil || loc == zeroLoc {
		return nil, ErrorResponse(body)
	}
	return &loc, nil
}

// KeyResponse interprets the body of an HTTP response as a key in a
// proper JSON structure (example "{key:'foo'}"). If it's not in the
// format of a key, it tries to read an error message instead.
func KeyResponse(body []byte) (string, error) {
	type KeyMessage struct {
		Key string
	}
	var key, zeroKey KeyMessage
	err := json.Unmarshal(body, &key)
	if err != nil || key == zeroKey {
		return "", ErrorResponse(body)
	}
	return key.Key, nil
}

// DirEntryResponse interprets the body of an HTTP response as
// a DirEntry and returns it. If it's not a DirEntry, it tries to read
// an error message instead.
func DirEntryResponse(body []byte) (*upspin.DirEntry, error) {
	var dir upspin.DirEntry
	err := json.Unmarshal(body, &dir)
	if err != nil || dir.Name == "" {
		return nil, ErrorResponse(body)
	}
	return &dir, nil
}

// ErrorResponse interprets the body of an HTTP response as a server
// error (which could contain the string "Success" for successful
// operations that do not return data).
func ErrorResponse(body []byte) error {
	type ErrorMessage struct {
		Error string
	}
	var serverErr, zeroErr ErrorMessage
	err := json.Unmarshal(body, &serverErr)
	if err != nil || serverErr == zeroErr {
		strErr := fmt.Sprintf("can't parse reply from server: %v, %v", err, string(body))
		log.Println(strErr)
		return errors.New(strErr)
	}
	if serverErr.Error == "success" {
		return nil
	}
	log.Printf("Returning error parsed: %q\n", serverErr.Error)
	return errors.New(serverErr.Error)
}
