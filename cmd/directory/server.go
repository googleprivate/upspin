package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	goPath "path"
	"strings"

	"upspin.googlesource.com/upspin.git/access"
	"upspin.googlesource.com/upspin.git/auth"
	"upspin.googlesource.com/upspin.git/cache"
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

	logErr = log.New(os.Stderr, "", log.Ldate|log.Ltime|log.LUTC)
	logMsg = log.New(os.Stdout, "", log.Ldate|log.Ltime|log.LUTC)
)

type dirServer struct {
	cloudClient gcp.GCP // handle for GCP bucket g-upspin-directory
	storeClient *storeClient
	dirCache    *cache.LRU // caches <upspin.PathName, upspin.DirEntry>. It is thread safe.
	rootCache   *cache.LRU // caches <upspin.UserName, root>. It is thread safe.
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

func (d *dirServer) getPathFromRequest(handlerPrefix string, r *http.Request) (*path.Parsed, error) {
	prefixLen := len(handlerPrefix)    // how many characters to skip from the URL path
	if len(r.URL.Path) < prefixLen+7 { // 7 is the magic minLen for a root "a@b.co/"
		return nil, newDirError("", "", "invalid pathname")
	}
	pathName := upspin.PathName(r.URL.Path[prefixLen:]) // skip this handler's prefix.
	parsed, err := path.Parse(pathName)
	if err != nil {
		return nil, newDirError("", pathName, err.Error())
	}
	return &parsed, nil
}

// dirHandler handles directory requests. It supports GET, POST/PUT, and DELETE which implement Directory.Get,
// Directory.Put and Directory.Delete respectively.
func (d *dirServer) dirHandler(sess auth.Session, w http.ResponseWriter, r *http.Request) {
	// First step is verifying the path name
	parsed, err := d.getPathFromRequest("/dir/", r)
	if err != nil {
		netutil.SendJSONError(w, context, err)
		return
	}
	switch r.Method {
	case netutil.Get:
		dirEntry, err := d.getHandler(sess, parsed, r)
		if err != nil {
			netutil.SendJSONError(w, context, err)
			return
		}
		netutil.SendJSONReply(w, dirEntry)
		return
	case netutil.Delete:
		err = d.deleteDirEntry(sess, parsed, r)
	case netutil.Post, netutil.Put:
		buf := netutil.BufferRequest(w, r, maxBuffSizePerReq) // closes r.Body
		if buf == nil {
			// Request was invalid and was closed. Nothing else to do.
			return
		}
		dirEntry := new(upspin.DirEntry)
		err = json.Unmarshal(buf, dirEntry)
		if err != nil {
			retErr := newDirError("Put", dirEntry.Name, fmt.Sprintf("unmarshal: %s", err))
			netutil.SendJSONError(w, context, retErr)
			logErr.Println(retErr)
			return
		}
		err = d.putDir(sess, parsed, dirEntry)
	default:
		netutil.SendJSONErrorString(w, "Only POST, PUT, GET and DELETE requests are accepted")
		return
	}
	if err != nil {
		netutil.SendJSONError(w, context, err)
		return
	}
	netutil.SendJSONErrorString(w, "success")
}

// putDir writes or overwrites a complete dirEntry to the back
// end, provided several checks have passed first.
func (d *dirServer) putDir(sess auth.Session, parsed *path.Parsed, dirEntry *upspin.DirEntry) error {
	const op = "Put"
	parsedAgain, err := path.Parse(dirEntry.Name) // canonicalizes dirEntry.Name
	if err != nil {
		return err
	}
	if !parsed.Equal(parsedAgain) {
		return newDirError(op, parsed.Path(), "inconsistent DirEntry.Name")
	}
	if err := verifyMetadata(parsed.Path(), dirEntry.Metadata); err != nil {
		return err
	}
	// If we're creating the root, handle it elsewhere.
	if parsed.IsRoot() {
		// We handle root elsewhere because otherwise this code would be riddled with "if IsRoot..."
		return d.handleRootCreation(sess, parsed, dirEntry)
	}

	// Check ACLs before we go any further, so we don't leak information about the existence of files and directories.
	canCreate, err := d.hasRight(op, sess.User(), access.Create, dirEntry.Name)
	if err != nil {
		return newDirError(op, dirEntry.Name, err.Error())
	}
	canWrite, err := d.hasRight(op, sess.User(), access.Write, dirEntry.Name)
	if err != nil {
		return newDirError(op, dirEntry.Name, err.Error())
	}
	if dirEntry.Metadata.IsDir && !canCreate || !dirEntry.Metadata.IsDir && !canWrite {
		return newDirError(op, dirEntry.Name, access.ErrPermissionDenied.Error())
	}
	// Find parent.
	parentParsedPath := parsed.Drop(1) // Can't fail as this is not called for roots.
	parentDirEntry, err := d.getDirEntry(&parentParsedPath)
	if err != nil {
		if err == errEntryNotFound {
			// Give a more descriptive error
			err = newDirError(op, parsed.Path(), "parent path not found")
		}
		return err
	}
	// Verify parent IsDir (redundant, but just to be safe).
	if !parentDirEntry.Metadata.IsDir {
		logErr.Printf("WARN: bad inconsistency. Parent of path is not a directory: %s", parentDirEntry.Name)
		return newDirError(op, parsed.Path(), "parent is not a directory")
	}

	// Verify whether there's a directory with same name.
	canonicalPath := parsed.Path()
	existingDirEntry, err := d.getNonRoot(canonicalPath)
	if err != nil && err != errEntryNotFound {
		return newDirError(op, canonicalPath, err.Error())

	}
	if err == nil {
		if existingDirEntry.Metadata.IsDir {
			return newDirError(op, canonicalPath, "directory already exists")
		}
		if dirEntry.Metadata.IsDir {
			return newDirError(op, canonicalPath, "overwriting file with directory")
		}
	}

	// Canonicalize path.
	dirEntry.Name = canonicalPath

	// Finally, store the new entry.
	err = d.putNonRoot(canonicalPath, dirEntry)
	if err != nil {
		return err
	}

	// Patch the parent: bump sequence number.
	parentDirEntry.Metadata.Sequence++
	err = d.putDirEntry(&parentParsedPath, parentDirEntry)
	if err != nil {
		return err
	}

	// If this is an Access file or Group file, we have some extra work to do.
	if access.IsAccessFile(canonicalPath) {
		err = d.updateAccess(parsed, &dirEntry.Location)
		if err != nil {
			return err
		}
	}
	if access.IsGroupFile(canonicalPath) {
		logMsg.Printf("Invalidating group file %s", canonicalPath)
		// By removing the group we guarantee we won't be using its old definition, if any.
		// Since we parse groups lazily, this is correct and generally efficient.
		_ = access.RemoveGroup(canonicalPath) // error is ignored on purpose. If group was not there, no harm done.
	}

	logMsg.Printf("%s: %q %q", op, sess.User(), dirEntry.Name)
	return nil
}

func (d *dirServer) getHandler(sess auth.Session, parsed *path.Parsed, r *http.Request) (*upspin.DirEntry, error) {
	const op = "Get"
	// Check ACLs before attempting to read the dirEntry to avoid leaking information about the existence of paths.
	canRead, err := d.hasRight(op, sess.User(), access.Read, parsed.Path())
	if err != nil {
		err = newDirError(op, "", err.Error()) // path is included in the original error message.
		logErr.Printf("Access error Read: %s", err)
		return nil, err
	}
	canList, err := d.hasRight(op, sess.User(), access.List, parsed.Path())
	if err != nil {
		err = newDirError(op, "", err.Error()) // path is included in the original error message.
		logErr.Printf("Access error List: %s", err)
		return nil, err
	}
	// If the user has no rights, we're done.
	if !canRead && !canList {
		return nil, newDirError(op, parsed.Path(), access.ErrPermissionDenied.Error())
	}
	// Look up entry
	var dirEntry *upspin.DirEntry
	if !parsed.IsRoot() {
		dirEntry, err = d.getNonRoot(parsed.Path())
	} else {
		root, err := d.getRoot(parsed.User)
		if err == nil {
			dirEntry = &root.dirEntry
		}
	}
	if err != nil {
		if err == errEntryNotFound {
			err = newDirError("get", parsed.Path(), "path not found")
		}
		return nil, err
	}
	// We have a dirEntry and ACLs check. But we still must clear Location if user does not have Read rights.
	if !canRead {
		logMsg.Printf("Zeroing out location information in Get for user %s on path %s", sess.User(), parsed)
		dirEntry.Location = upspin.Location{}
	}
	logMsg.Printf("Got dir entry for user %s: path %s: %s", sess.User(), parsed.Path(), dirEntry)
	return dirEntry, nil
}

func (d *dirServer) globHandler(sess auth.Session, w http.ResponseWriter, r *http.Request) {
	const op = "Glob"
	parsed, err := d.getPathFromRequest("/glob/", r)
	if err != nil {
		netutil.SendJSONError(w, context, newDirError(op, "", err.Error()))
		return
	}
	// Check if pattern is a valid go path pattern
	_, err = goPath.Match(parsed.FilePath(), "")
	if err != nil {
		netutil.SendJSONError(w, context, newDirError(op, parsed.Path(), err.Error()))
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
		if match, _ := goPath.Match(parsed.String(), path); match {
			// Now fetch each DirEntry we need
			logMsg.Printf("Looking up: %s for glob %s", path, parsed.String())
			de, err := d.getNonRoot(upspin.PathName(path))
			if err != nil {
				netutil.SendJSONError(w, context, newDirError(op, parsed.Path(), err.Error()))
			}
			// Verify if user has proper list ACL.
			canList, err := d.hasRight(op, sess.User(), access.List, de.Name)
			if err != nil {
				logErr.Printf("Error checking access for user: %s on %s: %s", sess.User(), de.Name, err)
				continue
			}
			canRead, err := d.hasRight(op, sess.User(), access.Read, de.Name)
			if err != nil {
				logErr.Printf("Error checking access for user: %s on %s: %s", sess.User(), de.Name, err)
				continue
			}
			if !canRead && !canList {
				logMsg.Printf("User %s can't Glob %s", sess.User(), de.Name)
				continue
			}
			// If the user can't read a path, clear out its Location.
			if !canRead {
				de.Location = upspin.Location{}
			}
			dirEntries = append(dirEntries, de)
		}
	}
	netutil.SendJSONReply(w, dirEntries)
}

// deleteDirEntry handles deleting names and their associated DirEntry.
func (d *dirServer) deleteDirEntry(sess auth.Session, parsed *path.Parsed, r *http.Request) error {
	const op = "Delete"

	// Check ACLs before attempting to get the dirEntry to avoid leaking information about the existence of paths.
	canDelete, err := d.hasRight(op, sess.User(), access.Delete, parsed.Path())
	if err != nil {
		err = newDirError(op, "", err.Error()) // path is included in the original error message.
		logErr.Printf("Access error for Delete: %s", err)
		return err
	}
	if !canDelete {
		return newDirError(op, parsed.Path(), access.ErrPermissionDenied.Error())
	}
	// Otherwise, locate the entry first.
	dirEntry, err := d.getDirEntry(parsed)
	if err != nil {
		return err
	}
	parsedPath := parsed.Path()
	// Only empty directories can be removed.
	if dirEntry.Metadata.IsDir {
		err = d.isDirEmpty(parsedPath)
		if err != nil {
			return newDirError(op, parsedPath, err.Error())
		}
	}
	// Attempt to delete it from GCP.
	if err = d.deletePath(parsedPath); err != nil {
		return newDirError(op, parsedPath, err.Error())
	}
	// If this was an Access file, we need to delete it from the root as well.
	if access.IsAccessFile(parsedPath) {
		err = d.deleteAccess(parsed)
		if err != nil {
			return err
		}
	}
	return nil
}

func newDirServer(cloudClient gcp.GCP, store *storeClient) *dirServer {
	d := &dirServer{
		cloudClient: cloudClient,
		storeClient: store,
		dirCache:    cache.NewLRU(1000), // TODO: adjust numbers
		rootCache:   cache.NewLRU(1000), // TODO: adjust numbers
	}
	return d
}

func main() {
	flag.Parse()

	ah := auth.NewHandler(&auth.Config{
		Lookup: auth.PublicUserKeyService(),
		AllowUnauthenticatedConnections: *noAuth,
	})

	s := newStoreClient(auth.NewClient(dirServerName, auth.NewFactotum(&upspin.Context{KeyPair: dirServerKeys}), &http.Client{}))
	d := newDirServer(gcp.New(*projectID, *bucketName, gcp.ProjectPrivate), s)

	http.HandleFunc("/dir/", ah.Handle(d.dirHandler)) // dir handles GET, PUT/POST and DELETE.
	http.HandleFunc("/glob/", ah.Handle(d.globHandler))

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
