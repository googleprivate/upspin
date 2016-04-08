package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	goPath "path"
	"strings"

	"upspin.googlesource.com/upspin.git/access"
	"upspin.googlesource.com/upspin.git/auth"
	"upspin.googlesource.com/upspin.git/cloud/gcp"
	"upspin.googlesource.com/upspin.git/cloud/netutil"
	"upspin.googlesource.com/upspin.git/path"
	"upspin.googlesource.com/upspin.git/upspin"

	_ "upspin.googlesource.com/upspin.git/user/gcpuser"
)

const (
	maxBuffSizePerReq = 1 << 20 // 1MB max buff size per request
	context           = "DirService: "
)

var (
	projectID             = flag.String("project", "upspin", "Our cloud project ID.")
	bucketName            = flag.String("bucket", "g-upspin-directory", "The name of an existing bucket within the project.")
	port                  = flag.Int("port", 8081, "TCP port to serve.")
	noAuth                = flag.Bool("noauth", false, "Disable authentication.")
	sslCertificateFile    = flag.String("cert", "/etc/letsencrypt/live/upspin.io/fullchain.pem", "Path to SSL certificate file")
	sslCertificateKeyFile = flag.String("key", "/etc/letsencrypt/live/upspin.io/privkey.pem", "Path to SSL certificate key file")
	errEntryNotFound      = newDirError("download", "", "pathname not found")

	logErr = log.New(os.Stderr, "", log.Ldate|log.Ltime|log.LUTC)
	logMsg = log.New(os.Stdout, "", log.Ldate|log.Ltime|log.LUTC)
)

type dirServer struct {
	cloudClient gcp.GCP // handle for GCP bucket g-upspin-directory
	storeClient *storeClient
}

type dirError struct {
	op    string
	path  upspin.PathName
	error string
}

func (d dirError) Error() string {
	var buf bytes.Buffer
	if d.op != "" {
		buf.WriteString(d.op)
		buf.WriteString(": ")
	}
	if len(d.path) > 0 {
		buf.WriteString(string(d.path))
		buf.WriteString(": ")
	}
	buf.WriteString(d.error)
	return buf.String()
}

func newDirError(op string, path upspin.PathName, err string) *dirError {
	return &dirError{
		op:    op,
		path:  path,
		error: err,
	}
}

// verifyMetadata checks that the metadata is minimally valid.
func verifyMetadata(path upspin.PathName, meta upspin.Metadata) error {
	if meta.Sequence < 0 {
		return newDirError("verifyMeta", path, "invalid sequence number")
	}
	return nil
}

// canonicalizePath returns parsed.Path() except if parsed is the root it returns the username as pathname.
func canonicalizePath(parsed *path.Parsed) upspin.PathName {
	if !parsed.IsRoot() {
		// Canonicalize the pathname if not root
		return parsed.Path()
	}
	// The root is special. Remove the backslash so that GCP has a file with metadata for the root.
	return upspin.PathName(parsed.User)
}

// dirHandler handles file put requests, for storing or updating
// metadata information.
func (d *dirServer) dirHandler(sess auth.Session, w http.ResponseWriter, r *http.Request) {
	const op = "Put"
	if r.Method != netutil.Post && r.Method != netutil.Patch {
		netutil.SendJSONErrorString(w, "/put only handles POST or PATCH HTTP requests")
		return
	}
	buf := netutil.BufferRequest(w, r, maxBuffSizePerReq) // closes r.Body
	if buf == nil {
		// Request was invalid and was closed. Nothing else to do.
		return
	}
	dirEntry := &upspin.DirEntry{}
	err := json.Unmarshal(buf, dirEntry)
	if err != nil {
		retErr := newDirError(op, dirEntry.Name, fmt.Sprintf("unmarshal: %s", err))
		netutil.SendJSONError(w, context, retErr)
		logErr.Println(retErr)
		return
	}
	parsed, err := path.Parse(dirEntry.Name) // canonicalizes dirEntry.Name
	if err != nil {
		netutil.SendJSONError(w, context, err)
		return
	}

	// TODO: verify ACLs before applying dir entry

	switch r.Method {
	case netutil.Post:
		d.putDirHandler(sess, w, &parsed, dirEntry)
	case netutil.Patch:
		d.patchHandler(sess, w, parsed.Path(), dirEntry)
	default:
		netutil.SendJSONError(w, context, fmt.Errorf("invalid HTTP method: %q", r.Method))
	}
}

// patchHandler handles directory patch requests, for making partial updates to directory entries. parsedPath is a validated dirEntry.Name.
func (d *dirServer) patchHandler(sess auth.Session, w http.ResponseWriter, parsedPath upspin.PathName, dirEntry *upspin.DirEntry) {
	const op = "patch"
	// Check that only allowed fields are being updated.
	dirEntry.Name = "" // Name is not updatable.
	if err := d.verifyUpdatableFields(dirEntry); err != nil {
		netutil.SendJSONError(w, context, newDirError(op, parsedPath, err.Error()))
		return
	}
	// Lookup original dir entry.
	origDirEntry, err := d.getMeta(parsedPath)
	if err != nil {
		netutil.SendJSONError(w, context, err)
		return
	}
	// Merge fields.
	mergedDirEntry := d.mergeDirEntries(origDirEntry, dirEntry) // NOTE: mergedDirEntry is an alias for origDirEntry.
	// Apply mutation on stable storage.
	err = d.putMeta(parsedPath, mergedDirEntry)
	if err != nil {
		netutil.SendJSONError(w, context, err)
		return
	}
	logMsg.Printf("%s: %q %q", op, sess.User(), mergedDirEntry.Name)
	netutil.SendJSONErrorString(w, "success")
}

// verifyNonUpdatableFields reports an error if a partial dirEntry contains non-updatable fields.
func (d *dirServer) verifyUpdatableFields(dir *upspin.DirEntry) error {
	if dir.Name != "" {
		return errors.New("Name is not updatable")
	}
	// Location may be updatable in the future, but right now it is not supported.
	var zeroLoc upspin.Location
	if dir.Location != zeroLoc {
		return errors.New("Location is not updatable")
	}
	// Here we're simply checking whether there is a non-zero value in IsDir.
	if dir.Metadata.IsDir {
		return errors.New("IsDir is not updatable")
	}
	// All other metadata fields are updatable.
	return nil
}

// mergeDirEntries merges dst and src together and returns dst. Only updatable fields are merged.
func (d *dirServer) mergeDirEntries(dst, src *upspin.DirEntry) *upspin.DirEntry {
	if src.Metadata.Sequence != 0 {
		dst.Metadata.Sequence = src.Metadata.Sequence
	}
	if src.Metadata.Size != 0 {
		dst.Metadata.Size = src.Metadata.Size
	}
	if src.Metadata.Time != 0 {
		dst.Metadata.Time = src.Metadata.Time
	}
	if src.Metadata.PackData != nil {
		dst.Metadata.PackData = src.Metadata.PackData
	}
	return dst
}

// putDirHandler writes or overwrites a complete dirEntry to the back
// end, provided several checks have passed first.
func (d *dirServer) putDirHandler(sess auth.Session, w http.ResponseWriter, parsed *path.Parsed, dirEntry *upspin.DirEntry) {
	const op = "Put"
	if err := verifyMetadata(parsed.Path(), dirEntry.Metadata); err != nil {
		netutil.SendJSONError(w, context, err)
		return
	}
	// Get the parent dir, unless we're creating the root.
	if parsed.IsRoot() {
		// We handle root elsewhere because otherwise this code would be riddled with "if IsRoot..."
		d.handleRootCreation(sess, w, parsed, dirEntry)
		return
	}
	parentParsedPath := parsed.Drop(1)
	parentDirEntry, err := d.getMeta(canonicalizePath(&parentParsedPath))
	if err != nil {
		if err == errEntryNotFound {
			// Give a more descriptive error
			err = newDirError(op, parsed.Path(), "parent path not found")
		}
		netutil.SendJSONError(w, context, err)
		return
	}
	// Verify parent IsDir (redundant, but just to be safe).
	if !parentDirEntry.d.Metadata.IsDir {
		logErr.Printf("WARN: bad inconsistency. Parent of path is not a directory: %s", parentDirEntry.d.Name)
		netutil.SendJSONError(w, context, newDirError(op, parsed.Path(), "parent is not a directory"))
		return
	}

	// Verify whether there's a directory with same name.
	canonicalPath := canonicalizePath(parsed)
	existingDirEntry, err := d.getMeta(canonicalPath)
	if err != nil && err != errEntryNotFound {
		netutil.SendJSONError(w, context, newDirError(op, canonicalPath, err.Error()))
		return
	}
	if err == nil {
		if existingDirEntry.d.Metadata.IsDir {
			netutil.SendJSONError(w, context, newDirError(op, canonicalPath, "directory already exists"))
			return
		}
		if dirEntry.Metadata.IsDir {
			netutil.SendJSONError(w, context, newDirError(op, canonicalPath, "overwriting file with directory"))
			return
		}
	}

	// Canonicalize path
	dirEntry.Name = canonicalPath

	// Annotate the dir entry prior to storage.
	adir := &annotatedDirEntry{
		d:      dirEntry,
		access: nil,
	}
	// Directories have the same access permission as their parents until an Access file is Put.
	if dirEntry.Metadata.IsDir {
		adir.access = parentDirEntry.access
	}

	// Finally, store the new entry.
	err = d.putMeta(canonicalPath, adir)
	if err != nil {
		netutil.SendJSONError(w, context, err)
		return
	}

	// Patch the parent: bump sequence number and, if this is an Access file, fetch it, parse it and store it.
	err = d.updateParentDir(parentDirEntry, adir)
	if err != nil {
		netutil.SendJSONError(w, context, err)
		return
	}

	logMsg.Printf("%s: %q %q", op, sess.User(), dirEntry.Name)
	netutil.SendJSONErrorString(w, "success")
}

// updateParentDir changes a given parentDirEntry according to a currentDirEntry that is being Put. This involves
// bumping the sequence number of the parent and, if currentDirEntry is an Access file, retrieving it from Store,
// parsing it and annotating it on the parent dir entry.
func (d *dirServer) updateParentDir(parentDirEntry *annotatedDirEntry, currentDirEntry *annotatedDirEntry) error {
	parentDirEntry.d.Metadata.Sequence++

	// If currentDirEntry is an Access file, we have work to do.
	if access.IsAccessFile(currentDirEntry.d.Name) {
		// Get the contents of it.
		//contents, err := d.storeClient.Get(currentDirEntry.d.Location)
	}

	return d.putMeta(parentDirEntry.d.Name, parentDirEntry)
}

func (d *dirServer) handleRootCreation(sess auth.Session, w http.ResponseWriter, parsed *path.Parsed, dirEntry *upspin.DirEntry) {
	const op = "Put"
	canonicalPath := canonicalizePath(parsed)
	_, err := d.getMeta(canonicalPath)
	if err != nil && err != errEntryNotFound {
		netutil.SendJSONError(w, context, newDirError(op, canonicalPath, err.Error()))
		return
	}
	if err == nil {
		netutil.SendJSONError(w, context, newDirError(op, canonicalPath, "directory already exists"))
		return
	}
	// Create an annotatedDirEntry with a blank Access file. It will be overwritten when
	// the user creates an Access file in the root.
	acc, err := access.Parse(canonicalPath, []byte(""))
	if err != nil { // Should never happen on an empty file, but better safe than sorry.
		netutil.SendJSONError(w, context, newDirError(op, canonicalPath, err.Error()))
		return
	}
	adir := annotatedDirEntry{
		d:      dirEntry,
		access: acc,
	}
	// Store the entry.
	err = d.putMeta(canonicalPath, &adir)
	if err != nil {
		netutil.SendJSONError(w, context, err)
		return
	}
	logMsg.Printf("%s: %q %q", op, sess.User(), dirEntry.Name)
	netutil.SendJSONErrorString(w, "success")
}

func (d *dirServer) getHandler(sess auth.Session, w http.ResponseWriter, r *http.Request) {
	const op = "Get"
	pathnames := d.verifyFormParams(op, "", w, r, "pathname")
	if len(pathnames) == 0 {
		// Nothing to be done. Error sent to client.
		return
	}
	var parsedPath upspin.PathName
	p, err := path.Parse(upspin.PathName(pathnames[0]))
	if err != nil {
		netutil.SendJSONError(w, context, err)
		return
	}
	parsedPath = canonicalizePath(&p)

	dirEntry, err := d.getMeta(parsedPath)
	if err != nil {
		if err == errEntryNotFound {
			err = newDirError("get", parsedPath, "path not found")
		}
		netutil.SendJSONError(w, context, err)
		return
	}
	// We have a dirEntry. Marshal it and send it back.
	// TODO: verify ACLs before replying.
	logMsg.Printf("Got dir entry for user %s: path %s: %s", sess.User(), parsedPath, dirEntry)
	netutil.SendJSONReply(w, dirEntry)
}

func (d *dirServer) globHandler(sess auth.Session, w http.ResponseWriter, r *http.Request) {
	const op = "Glob"
	patterns := d.verifyFormParams(op, "", w, r, "pattern")
	if len(patterns) == 0 {
		// Nothing to be done. Error sent to client.
		return
	}
	pathPattern := upspin.PathName(patterns[0])
	parsed, err := path.Parse(pathPattern)
	if err != nil {
		netutil.SendJSONError(w, context, newDirError(op, pathPattern, err.Error()))
		return
	}
	// Check if pattern is a valid go path pattern
	_, err = goPath.Match(parsed.FilePath(), "")
	if err != nil {
		netutil.SendJSONError(w, context, newDirError(op, pathPattern, err.Error()))
		return
	}

	// As an optimization, we look for the longest prefix that
	// does not contain a metacharacter -- this saves us from
	// doing a full list operation if the matter of interest is
	// deep in a sub directory.
	clear := len(parsed.Elems)
	for i, elem := range parsed.Elems {
		if strings.ContainsAny(elem, "*?[]^") {
			clear = i
			break
		}
	}
	prefix := parsed.First(clear).String()
	depth := len(parsed.Elems) - clear

	var names []string
	if depth == 1 {
		if !strings.HasSuffix(prefix, "/") {
			prefix = prefix + "/"
		}
		names, err = d.cloudClient.ListDir(prefix)
	} else {
		names, err = d.cloudClient.ListPrefix(prefix, int(depth))
	}
	if err != nil {
		netutil.SendJSONError(w, context, err)
		return
	}

	dirEntries := make([]*upspin.DirEntry, 0, len(names))
	// Now do the actual globbing.
	for _, path := range names {
		// error is ignored as pattern is known valid
		if match, _ := goPath.Match(patterns[0], path); match {
			// Now fetch each DirEntry we need
			logMsg.Printf("Looking up: %s for glob %s", path, patterns[0])
			de, err := d.getMeta(upspin.PathName(path))
			if err != nil {
				netutil.SendJSONError(w, context, newDirError(op, pathPattern, err.Error()))
			}
			// Verify if user has proper list ACL.
			hasAccess, err := d.hasRight(op, sess.User(), access.List, de)
			if err != nil {
				logErr.Printf("Error checking access for user: %s: %s", sess.User(), err)
				continue
			}
			if hasAccess {
				dirEntries = append(dirEntries, de.d)
			}
		}
	}
	netutil.SendJSONReply(w, dirEntries)
}

// hasRight reports whether user has the right on the dir entry.
func (d *dirServer) hasRight(op string, user upspin.UserName, right access.Right, adir *annotatedDirEntry) (bool, error) {
	var acc *access.Access
	var err error
	var pathName upspin.PathName
	if adir.access != nil {
		pathName = adir.d.Name
		acc = adir.access
	} else {
		if adir.d.Metadata.IsDir {
			pathName = adir.d.Name
		} else {
			parsed, err := path.Parse(adir.d.Name)
			if err != nil {
				return false, err
			}
			pathName = parsed.Drop(1).Path()
		}
		acc, err = d.getAccessFor(pathName)
		if err != nil {
			return false, err
		}
		if acc == nil {
			// Access should only be nil for file entries, and we shouldn't be looking up access for a file, only for dirs.
			err = newDirError(op, pathName, "access file entry is nil")
			logErr.Printf("WARN: %s", err)
			return false, err
		}
	}
	can, pathsToFetch, err := acc.Can(user, right, pathName)
	if err == access.ErrNeedGroup {
		// TODO: fetch groups.
		return false, fmt.Errorf("need more groups: %+v TBD", pathsToFetch)
	}
	return can, nil
}

// verifyFormParams parses the request form and looks for the presence of each one of the listed fields.
// If a field is not found, it returns an error to the user. If all are found, it returns their value in
// the same order as requested.
func (d *dirServer) verifyFormParams(op string, path upspin.PathName, w http.ResponseWriter, r *http.Request, fields ...string) []string {
	err := r.ParseForm()
	if err != nil {
		netutil.SendJSONError(w, context, err)
		return nil
	}
	values := make([]string, len(fields))
	for i, k := range fields {
		v := r.FormValue(k)
		if v == "" {
			errMsg := fmt.Sprintf("missing %s in request", k)
			logErr.Print(errMsg)
			netutil.SendJSONError(w, context, newDirError(op, path, errMsg))
			return nil
		}
		values[i] = v
	}
	return values
}

func newDirServer(cloudClient gcp.GCP, store *storeClient) *dirServer {
	d := &dirServer{
		cloudClient: cloudClient,
		storeClient: store,
	}
	return d
}

func main() {
	flag.Parse()

	ah := auth.NewHandler(&auth.Config{
		Lookup: auth.PublicUserKeyService(),
		AllowUnauthenticatedConnections: *noAuth,
	})

	s := newStoreClient(auth.NewClient(dirServerName, dirServerKeys, &http.Client{}))
	d := newDirServer(gcp.New(*projectID, *bucketName, gcp.ProjectPrivate), s)

	// TODO: put and get are HTTP verbs so this is ambiguous. Change this here
	// and in clients to /dir and /lookup respectively.
	http.HandleFunc("/put", ah.Handle(d.dirHandler))
	http.HandleFunc("/get", ah.Handle(d.getHandler))
	http.HandleFunc("/glob", ah.Handle(d.globHandler))

	if *sslCertificateFile != "" && *sslCertificateKeyFile != "" {
		server, err := auth.NewSecureServer(*port, *sslCertificateFile, *sslCertificateKeyFile)
		if err != nil {
			logErr.Fatal(err)
		}
		logErr.Println("Starting HTTPS server with SSL.")
		logErr.Fatal(server.ListenAndServeTLS(*sslCertificateFile, *sslCertificateKeyFile))
	} else {
		logErr.Println("Not using SSL certificate. Starting regular HTTP server.")
		logErr.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
	}
	logErr.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}
