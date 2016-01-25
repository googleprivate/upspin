package gcp

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	projectId  = "upspin"
	bucketName = "upspin-test"
)

var (
	client *GCP = New(projectId, bucketName, DefaultWriteACL)
	// The time is important because we never delete this file, but instead overwrite it.
	testData = []byte(fmt.Sprintf("This is test at %v", time.Now()))
)

// This is more of a regression test as it uses the running cloud
// storage in prod. However, since GCP is always available, we accept
// to rely on it.
func TestPutAndGet(t *testing.T) {
	link, err := client.Put("test-file", testData)
	if err != nil {
		t.Fatalf("Can't put: %v", err)
	}
	if !strings.HasPrefix(link, "https://") {
		t.Errorf("Link is not https")
	}
	retLink, err := client.Get("test-file")
	if err != nil {
		t.Fatalf("Can't get: %v", err)
	}
	if retLink != link {
		t.Errorf("Not the same link as stored: %v vs received: %v", link, retLink)
	}
	resp, err := http.Get(retLink)
	if err != nil {
		t.Errorf("Couldn't get link: %v", err)
	}
	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Can't read http body: %v", err)
	}
	if string(data) != string(testData) {
		t.Errorf("Data mismatch. Expected '%q' got '%q'", string(testData), string(data))
	}
}
