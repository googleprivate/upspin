// The user service implements two main pieces of functionality for the Upspin service: root directory
package main

import (
	"encoding/json"
	"errors"
	"expvar"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"upspin.googlesource.com/upspin.git/auth"
	"upspin.googlesource.com/upspin.git/cloud/gcp"
	"upspin.googlesource.com/upspin.git/cloud/netutil"
	"upspin.googlesource.com/upspin.git/cloud/netutil/jsonmsg"
	"upspin.googlesource.com/upspin.git/path"
	"upspin.googlesource.com/upspin.git/upspin"
)

// userServer is the implementation of the User Service on GCP.
type userServer struct {
	cloudClient gcp.GCP
}

// userEntry stores all known information for a given user. The fields
// are exported because JSON parsing needs access to them.
type userEntry struct {
	User      upspin.UserName    // User's email address (e.g. bob@bar.com).
	Keys      []upspin.PublicKey // Known keys for the user.
	Endpoints []upspin.Endpoint  // Known endpoints for the user's directory entry.
}

const (
	minKeyLen = 12
)

var (
	projectID             = flag.String("project", "upspin", "Our cloud project ID.")
	bucketName            = flag.String("bucket", "g-upspin-user", "The name of an existing bucket within the project.")
	readOnly              = flag.Bool("readonly", false, "Whether this server instance is read-only.")
	port                  = flag.Int("port", 8082, "TCP port to serve.")
	sslCertificateFile    = flag.String("cert", "/etc/letsencrypt/live/upspin.io/fullchain.pem", "Path to SSL certificate file")
	sslCertificateKeyFile = flag.String("key", "/etc/letsencrypt/live/upspin.io/privkey.pem", "Path to SSL certificate key file")
	errKeyTooShort        = errors.New("key length too short")
	errInvalidEmail       = errors.New("invalid email format")
	userStats             *serverStatus
)

// validateUserEmail checks whether the given email is valid. For
// now, it only checks the form "a@b.co", but in the future it could
// verify DNS entries and perform other checks. A nil error indicates
// validity.
func validateUserEmail(userEmail string) error {
	_, err := path.Parse(upspin.PathName(userEmail) + "/")
	if err != nil {
		return errInvalidEmail
	}
	return nil
}

// validateKey checks whether a key appears to conform to one of the
// known key formats. It does not reject unknown formats, but it does
// reject keys that are too short to be valid in any current of future
// format. A nil error indicates validity.
func validateKey(key upspin.PublicKey) error {
	// TODO: use keyload.parsePublicKey to check it conforms.
	if len(key) < minKeyLen {
		return errKeyTooShort
	}
	return nil
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}

func isKeyInSlice(key upspin.PublicKey, slice []upspin.PublicKey) bool {
	for _, k := range slice {
		if key == k {
			return true
		}
	}
	return false
}

// addKeyHandler handles the HTTP PUT/POST request for adding a new
// key for a given user. Required parameters are user=<email> and
// key=<bytes>. Minimal validation is done on the email, just to check
// for proper form. Very minimal validation is done on the key.
func (u *userServer) addKeyHandler(w http.ResponseWriter, r *http.Request) {
	context := "addkey: "
	user := u.preambleParseRequestAndGetUser(context, netutil.Post, w, r)
	if user == "" {
		// An error has already been sent out on w.
		return
	}

	key := upspin.PublicKey(r.FormValue("key"))
	err := validateKey(key)
	if err != nil {
		netutil.SendJSONError(w, context, err)
		return
	}
	// Appends to the current user entry, if any.
	ue, err := u.fetchUserEntry(user)
	if err != nil {
		// If this is a Not Found error, then allocate a new userEntry and continue.
		if isNotFound(err) {
			log.Printf("User %q not found on GCP, adding new one", user)
			ue = &userEntry{
				User: upspin.UserName(user),
				Keys: make([]upspin.PublicKey, 0, 1),
			}
		} else {
			netutil.SendJSONError(w, context, err)
			return
		}
	}
	// Check that the key is not already there.
	if !isKeyInSlice(key, ue.Keys) {
		// Place key at head of slice to indicate higher priority.
		ue.Keys = append([]upspin.PublicKey{key}, ue.Keys...)
		err = u.putUserEntry(user, ue)
		if err != nil {
			netutil.SendJSONError(w, context, err)
			return
		}
		log.Printf("Added key %s for user %v\n", key, user)
	}
	netutil.SendJSONErrorString(w, "success")
}

// addRootHandler handles the HTTP PUT/POST request for adding a new
// directory endpoint for a given user. Required parameters are user=<email> and
// endpoint=<upspin.Endpoint>. Minimal validation is done on the email, just to check
// for proper form. Very minimal validation is done on the endpoint.
func (u *userServer) addRootHandler(w http.ResponseWriter, r *http.Request) {
	context := "addroot: "
	user := u.preambleParseRequestAndGetUser(context, netutil.Post, w, r)
	if user == "" {
		// An error has already been sent out on w.
		return
	}

	// Parse the new endpoint
	endpointJSON := []byte(r.FormValue("endpoint"))
	if len(endpointJSON) == 0 {
		netutil.SendJSONErrorString(w, "empty endpoint")
		return
	}
	var endpoint upspin.Endpoint
	err := json.Unmarshal(endpointJSON, &endpoint)
	if err != nil {
		netutil.SendJSONError(w, context, err)
		return
	}

	// Get the user entry from GCP.
	ue, err := u.fetchUserEntry(user)
	if err != nil {
		// If this is a Not Found error, then allocate a new userEntry and continue.
		if isNotFound(err) {
			log.Printf("User %q not found on GCP, adding new one", user)
			ue = &userEntry{
				User:      upspin.UserName(user),
				Endpoints: make([]upspin.Endpoint, 0, 1),
			}
		} else {
			netutil.SendJSONError(w, context, err)
			return
		}
	}
	// Place the endpoint at the head of the slice to indicate higher priority.
	ue.Endpoints = append([]upspin.Endpoint{endpoint}, ue.Endpoints...)
	err = u.putUserEntry(user, ue)
	if err != nil {
		netutil.SendJSONError(w, context, err)
	}
	log.Printf("Added root %v for user %v", endpoint, user)
	netutil.SendJSONErrorString(w, "success")
}

// getHandler handles the /get request to lookup the user
// information. The user=<email> parameter is required.
func (u *userServer) getHandler(w http.ResponseWriter, r *http.Request) {
	context := "get: "
	if userStats != nil {
		userStats.addLookup(1)
	}
	if r.TLS != nil {
		log.Printf("Encrypted connection. Cipher: %d", r.TLS.CipherSuite)
	}
	user := u.preambleParseRequestAndGetUser(context, netutil.Get, w, r)
	if user == "" {
		// An error has already been sent out on w.
		return
	}
	// Get the user entry from GCP.
	ue, err := u.fetchUserEntry(user)
	if err != nil {
		netutil.SendJSONError(w, context, err)
		return
	}
	// Reply to user
	log.Printf("Lookup request for user %v", user)
	jsonmsg.SendUserLookupResponse(ue.User, ue.Endpoints, ue.Keys, w)
}

func (u *userServer) deleteHandler(w http.ResponseWriter, r *http.Request) {
	netutil.SendJSONErrorString(w, "not implemented")
}

// preambleParseRequestAndGetUser performs the common tasks between
// all the Handler functions and returns the user present in the
// request, or the empty string if one is not found. An error message
// is sent as the response if an error is encountered.
func (u *userServer) preambleParseRequestAndGetUser(context string, expectedReqType string, w http.ResponseWriter, r *http.Request) string {
	// Validate request type
	switch expectedReqType {
	case netutil.Post:
		if r.Method != "POST" && r.Method != "PUT" {
			netutil.SendJSONErrorString(w, fmt.Sprintf("%sonly handles POST http requests", context))
			return ""
		}
	case netutil.Get:
		if r.Method != "GET" {
			netutil.SendJSONErrorString(w, fmt.Sprintf("%sonly handles GET http requests", context))
			return ""
		}
	default:
	}
	// Parse the form and validate the user parameter
	err := r.ParseForm()
	if err != nil {
		netutil.SendJSONError(w, context, err)
		return ""
	}
	user := r.FormValue("user")
	if err = validateUserEmail(user); err != nil {
		netutil.SendJSONError(w, context, err)
		return ""
	}
	return user
}

// fetchUserEntry reads the user entry for a given user from permanent storage on GCP.
func (u *userServer) fetchUserEntry(user string) (*userEntry, error) {
	// Get the user entry from GCP
	buf, err := u.cloudClient.Download(user)
	if err != nil {
		log.Printf("Error downloading: %s", err)
		return nil, err
	}
	// Now convert it to a userEntry
	var ue userEntry
	err = json.Unmarshal(buf, &ue)
	if err != nil {
		return nil, err
	}
	log.Printf("Fetched user entry for %s", user)
	return &ue, nil
}

// putUserEntry writes the user entry for a user to permanent storage on GCP.
func (u *userServer) putUserEntry(user string, userEntry *userEntry) error {
	if userEntry == nil {
		return errors.New("nil userEntry")
	}
	jsonBuf, err := json.Marshal(userEntry)
	if err != nil {
		return fmt.Errorf("conversion to JSON failed: %v", err)
	}
	_, err = u.cloudClient.Put(user, jsonBuf)
	return err
}

type serverStatus struct {
	NumRegisteredUsers int
	NumLookupsLastHour int
	LastReset          time.Time

	cloudClient gcp.GCP
}

func (uc *serverStatus) update() interface{} {
	users, err := uc.cloudClient.ListDir("")
	if err == nil {
		uc.NumRegisteredUsers = len(users)
	}
	uc.addLookup(0)
	return uc
}

func (uc *serverStatus) addLookup(count int) {
	now := time.Now()
	if now.Add(-1 * time.Hour).After(uc.LastReset) {
		uc.LastReset = now
		uc.NumLookupsLastHour = 0
	}
	uc.NumLookupsLastHour += count
}

// newUserServer creates a UserService from a pre-configured GCP instance and an HTTP client.
func newUserServer(cloudClient gcp.GCP) *userServer {
	u := &userServer{
		cloudClient: cloudClient,
	}
	return u
}

func main() {
	flag.Parse()
	os.Args = nil
	gcp := gcp.New(*projectID, *bucketName, gcp.BucketOwnerFullCtrl)
	userStats = &serverStatus{cloudClient: gcp}
	u := newUserServer(gcp)
	if !*readOnly {
		// TODO: these should be authenticated too.
		http.HandleFunc("/addkey", u.addKeyHandler)
		http.HandleFunc("/addroot", u.addRootHandler)
		http.HandleFunc("/delete", u.deleteHandler)
	}
	http.HandleFunc("/get", u.getHandler)
	expvar.Publish("user stats", expvar.Func(userStats.update))

	if *sslCertificateFile != "" && *sslCertificateKeyFile != "" {
		server, err := auth.NewSecureServer(*port, *sslCertificateFile, *sslCertificateKeyFile)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("Starting HTTPS server with SSL.")
		log.Fatal(server.ListenAndServeTLS(*sslCertificateFile, *sslCertificateKeyFile))
	} else {
		log.Println("Not using SSL certificate. Starting regular HTTP server.")
		log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
	}
}
