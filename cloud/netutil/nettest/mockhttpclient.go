// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package nettest

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"

	"upspin.io/cloud/netutil"
)

var (
	// AnyRequest is a request that matches any kind of request.
	AnyRequest = NewRequest(nil, "*", "*", []byte("*"))
)

// MockHTTPClient is a simple HTTP client that saves the Request given
// to it and always responds with the preset Response. It then allows
// a verification step to check whether the expected requests match
// the ones received.
type MockHTTPClient struct {
	http.Client
	requestsReceived []*http.Request
	requestsExpected []*http.Request
	responses        []MockHTTPResponse
}

// MockHTTPResponse contains either an error or an actual
// http.Response.
type MockHTTPResponse struct {
	Error    error
	Response *http.Response
}

// NewMockHTTPClient creates an instance pre-loaded with the responses
// that will be returned when Do is invoked on the HTTP client, in
// order and the expected requests that generate the responses. After
// using the mock, call Verify to check that the expected requests
// were actually sent. To make a new Response, use helper method
// NewMockHTTPResponse. Both slices are copied so further
// modifications after creation are not reflected in the mock.
func NewMockHTTPClient(responsesToSend []MockHTTPResponse, requestsExpected []*http.Request) *MockHTTPClient {
	mock := MockHTTPClient{
		requestsReceived: make([]*http.Request, 0, len(requestsExpected)),
		requestsExpected: make([]*http.Request, len(requestsExpected)),
		responses:        make([]MockHTTPResponse, len(responsesToSend)),
	}
	// We make copies to avoid bugs with subsequent slice manipulation by callers.
	copy(mock.requestsExpected, requestsExpected)
	copy(mock.responses, responsesToSend)
	return &mock
}

// NewMockHTTPResponse creates a MockHTTPResponse with a nil error and
// a minimal http.Response that contains a given status code, a body
// type (such as "text/html", "application/json") and
// contents. Manipulate the Response field of the returned object if
// necessary to fine-tune it.
func NewMockHTTPResponse(statusCode int, bodyType string, data []byte) MockHTTPResponse {
	header := http.Header{}
	header.Add(netutil.ContentType, bodyType)
	header.Add(netutil.ContentLength, fmt.Sprint(len(data)))
	status := fmt.Sprint(statusCode)
	resp := &http.Response{
		Status:     status,
		StatusCode: statusCode,
		Header:     header,
		Body:       &readCloser{bytes.NewReader(data)},
	}
	return MockHTTPResponse{Error: nil, Response: resp}
}

// Requests returns the request sent to the HTTP client.
func (m *MockHTTPClient) Requests() []*http.Request {
	return m.requestsReceived
}

// Do is analogous to HTTPClient.Do and satisfies HTTPClientInterface.
func (m *MockHTTPClient) Do(request *http.Request) (resp *http.Response, err error) {
	m.requestsReceived = append(m.requestsReceived, request)
	if len(m.responses) == 0 {
		log.Fatal("Not enough mock responses exist")
	}
	toReply := m.responses[0]
	m.responses = m.responses[1:]
	return toReply.Response, toReply.Error
}

// TestingInterface is a simplified version of testing.T, used for
// testing the mock itself. In regular test, just pass a real
// *testing.T.
type TestingInterface interface {
	// Errorf logs an error and continues execution
	Errorf(format string, args ...interface{})

	// Fatalf logs an error, marks it as fatal and continues execution
	Fatalf(format string, args ...interface{})
}

// Verify checks that all expected and all received requests are
// equivalent, by checking their URL fields, type of request
// (GET/POST) and payload, if any. It calls Fatal and Error on t if
// any mismatches are encountered.
func (m *MockHTTPClient) Verify(t TestingInterface) {
	received := m.Requests()
	expected := m.requestsExpected
	if len(expected) != len(received) {
		t.Fatalf("Length of expected requests does not match. Expected %d, got %d", len(expected), len(received))
		return
	}
	for i, e := range expected {
		// Short-circuit the rest since we know AnyRequest is all wildcards.
		if e == AnyRequest {
			continue
		}
		r := received[i]
		if e.Method != "*" && e.Method != r.Method {
			t.Errorf("Request method mismatch. Expected %v, got %v", e.Method, r.Method)
		}
		// If Path is a wildcard, ignore everything else about the URL
		if e.URL.Path != "*" {
			if e.URL.Scheme != r.URL.Scheme {
				t.Errorf("Scheme mismatch. Expected %v, got %v", e.URL.Scheme, r.URL.Scheme)
			}
			if e.URL.Host != r.URL.Host {
				t.Errorf("URL host mismatch. Expected %v, got %v", e.URL.Host, r.URL.Host)
			}
			if e.URL.Path != r.URL.Path {
				t.Errorf("URL path mismatch. Expected %v, got %v", e.URL.Path, r.URL.Path)
			}
			if e.URL.RawQuery != r.URL.RawQuery {
				t.Errorf("Query mismatch. Expected %v, got %v", e.URL.RawQuery, r.URL.RawQuery)
			}
		}
		isWildCard := contentsEquivalent(t, e.Body, r.Body)
		if !isWildCard {
			if e.Header.Get(netutil.ContentType) != r.Header.Get(netutil.ContentType) {
				t.Errorf("Content type mismatch. Expected %v, got %v", e.Header.Get(netutil.ContentType), r.Header.Get(netutil.ContentType))
			}
			// This is probably unnecessary as
			// compareBytes has already compared lengths
			// in the body. But to ensure the request was
			// created properly, we still check it.
			if e.ContentLength != r.ContentLength {
				t.Errorf("Content length mismatch. Expected %v, got %v", e.ContentLength, r.ContentLength)
			}
		}
	}
}

// contentsEquivalent verifies if the contents given in expectedBody
// and receivedBody are equivalent. The two will be equivalent only in
// two situations: 1) the expectedBody contains a wildcard ("*") or 2)
// all bytes from expectedBody match receivedBody. This function
// returns true in the first case (wildcard), to indicate that no
// further checks are necessary. If t is a regular instance of
// testing.T, this function may not return and instead emit a fatal.
func contentsEquivalent(t TestingInterface, expectedBody, receivedBody io.ReadCloser) bool {
	if expectedBody == nil && receivedBody == nil {
		// No body is a match
		return false
	}
	var e []byte // expected contents
	var err error
	if expectedBody != nil {
		defer expectedBody.Close()
		e, err = ioutil.ReadAll(expectedBody)
		if err != nil {
			t.Fatalf("Error reading expected body: %v", err)
			return false
		}
	}
	if len(e) == 1 && string(e[0]) == "*" {
		// a "*" matches anything
		return true
	}
	if expectedBody == nil && receivedBody != nil {
		t.Fatalf("Received non-empty body, but expected empty")
		return false
	}
	// Compare the actual bytes
	defer receivedBody.Close()
	r, err := ioutil.ReadAll(receivedBody)
	if err != nil {
		t.Fatalf("Error reading received body: %v", err)
		return false
	}
	if len(e) != len(r) {
		t.Fatalf("Request body length mismatch. Expected %v, got %v", len(e), len(r))
		return false
	}
	mismatchIndexes := make([]int, 0, 10)
	for i, b := range e {
		if b != r[i] {
			mismatchIndexes = append(mismatchIndexes, i)
		}
	}
	if len(mismatchIndexes) > 0 {
		t.Errorf("Body contents mismatch. Number of mismatched bytes: %d", len(mismatchIndexes))
		for count, i := range mismatchIndexes {
			if count > 5 {
				t.Errorf("Too many errors... %d left", len(mismatchIndexes)-count)
				continue
			}
			t.Errorf("Byte %v: Expected %v, got %v", i, e[i], r[i])
		}
	}
	return false
}

// NewRequest is a convenience function to create an HTTP request of a given type with a given payload.
func NewRequest(t TestingInterface, reqType, request string, payload []byte) *http.Request {
	var b io.Reader
	if payload != nil {
		b = bytes.NewBuffer(payload)
	}
	r, err := http.NewRequest(reqType, request, b)
	if err != nil {
		t.Fatalf("Error creating a request: %v", err)
	}
	return r
}

// readCloser adds a Close method to a reader.
type readCloser struct {
	io.Reader
}

func (r *readCloser) Close() error {
	return nil
}
