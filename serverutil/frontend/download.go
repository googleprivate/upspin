// Copyright 2017 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file contains an http.Handler implementation
// that serves Upspin release tarballs.

package frontend

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"upspin.io/client"
	"upspin.io/log"
	"upspin.io/path"
	"upspin.io/upspin"
)

// osArchHuman describes operating system and processor architectures
// in human-readable form.
var osArchHuman = map[string]string{
	"linux_amd64": "Linux 64-bit x86",
}

const (
	// updateDownloadsInterval is the interval between refreshing the list
	// of available binary releases.
	updateDownloadsInterval = 1 * time.Minute

	// releaseUser is the tree in which the releases are kept.
	releaseUser = "release@upspin.io"

	// downloadPath is the HTTP base path for the download handler.
	downloadPath = "/dl/"

	// tarballExpr defines the file name for the release tarballs.
	tarballExpr = `^upspin\.([a-z0-9]+_[a-z0-9]+).tar.gz$`

	// tarballFormat is a format string that formats a release tarball file
	// name. Its only argument is the os_arch combination for the release.
	tarballFormat = "upspin.%s.tar.gz"
)

var tarballRE = regexp.MustCompile(tarballExpr)

// newDownloadHandler initializes and returns a new downloadHandler.
func newDownloadHandler(cfg upspin.Config) http.Handler {
	h := &downloadHandler{
		client:  client.New(cfg),
		latest:  make(map[string]time.Time),
		tarball: make(map[string]*tarball),
	}
	go func() {
		for {
			err := h.updateTarballs()
			if err != nil {
				log.Error.Printf("download: error updating tarballs: %v", err)
			}
			time.Sleep(updateDownloadsInterval)
		}
	}()
	return h
}

// downloadHandler is an http.Handler that serves a directory of available
// Upspin release binaries and tarballs of those binaries.
// It keeps the latest tarball bytes for each os-arch combination in memory.
type downloadHandler struct {
	client upspin.Client

	mu      sync.RWMutex
	latest  map[string]time.Time // [os_arch]last-update-time
	tarball map[string]*tarball  // [os_arch]tarball
}

func (h *downloadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, downloadPath)

	if p == "" {
		// Show listing of available releases.
		var tarballs []*tarball
		h.mu.RLock()
		for _, tb := range h.tarball {
			tarballs = append(tarballs, tb)
		}
		h.mu.RUnlock()
		sort.Slice(tarballs, func(i, j int) bool {
			return tarballs[i].osArch < tarballs[j].osArch
		})

		err := downloadTmpl.Execute(w, pageData{
			Content: tarballs,
		})
		if err != nil {
			log.Error.Printf("download: rendering downloadTmpl: %v", err)
		}
		return
	}

	// Parse the request path to see if it's a tarball.
	m := tarballRE.FindStringSubmatch(p)
	if m == nil {
		http.NotFound(w, r)
		return
	}
	osArch := m[1]

	h.mu.RLock()
	tb := h.tarball[osArch]
	h.mu.RUnlock()
	if tb == nil {
		http.NotFound(w, r)
		return
	}

	// Send the tarball.
	b := tb.bytes()
	if len(b) == 0 {
		http.Error(w, "An error occurred preparing the release tarball. Please try again later.", http.StatusInternalServerError)
		return
	}
	w.Write(b)
}

// updateTarballs refreshes the list of release binaries in releaseUser's tree
// and updates the latest and tarball maps appropriately.
func (h *downloadHandler) updateTarballs() error {
	// Fetch the list of available os_arch combinations.
	des, err := h.client.Glob(releaseUser + "/latest/*")
	if err != nil {
		return err
	}
	var osArches []string
	for _, de := range des {
		p, _ := path.Parse(de.Name)
		osArches = append(osArches, p.Elem(1))
	}

	// Update h.latest and h.tarball for each osArch.
	for _, osArch := range osArches {
		h.mu.RLock()
		latest := h.latest[osArch]
		h.mu.RUnlock()

		des, err := h.client.Glob(releaseUser + "/latest/" + osArch + "/*")
		if err != nil {
			return err
		}
		updated := false
		for _, de := range des {
			if t := de.Time.Go(); t.After(latest) {
				latest = t
				updated = true
			}
		}
		if !updated {
			continue
		}

		// Add the tarball to the list.
		tb := &tarball{osArch: osArch}
		h.mu.Lock()
		h.latest[osArch] = latest
		h.tarball[osArch] = tb
		h.mu.Unlock()

		// Build the tarball in the background.
		go func() {
			if err := tb.build(h.client, des); err != nil {
				log.Error.Printf("download: error building tarball for %v: %v", tb.osArch, err)

				// Remove the broken tarball from the list.
				h.mu.Lock()
				if h.latest[tb.osArch] == latest {
					delete(h.latest, tb.osArch)
					delete(h.tarball, tb.osArch)
				}
				h.mu.Unlock()
			} else {
				log.Info.Printf("download: built new tarball for %v", tb.osArch)
			}
		}()

		log.Info.Printf("download: new release available for %v built at %v", osArch, latest)
	}

	return nil
}

// tarball represents a release tarball and its contents.
type tarball struct {
	osArch string

	mu   sync.RWMutex
	data []byte

	size int64 // Set atomically.
}

// FileName returns the file name of the tarball.
func (tb *tarball) FileName() string {
	return fmt.Sprintf(tarballFormat, tb.osArch)
}

// OSArch returns the operating system and processor architecture for this
// tarball in human-readable form.
func (tb *tarball) OSArch() string {
	return osArchHuman[tb.osArch]
}

// SizeMB returns the size of the tarball in human-readable form,
// or the empty string if the tarball has not been built yet.
func (tb *tarball) Size() string {
	size := atomic.LoadInt64(&tb.size)
	if size == 0 {
		return ""
	}
	return fmt.Sprintf("%.1fMB", float64(size)/(1<<20))
}

// bytes returns the tarball bytes.
// If an error occurred when constructing the tarball, bytes returns nil.
func (tb *tarball) bytes() []byte {
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	return tb.data
}

// build builds the tarball and updates the data and size fields.
func (tb *tarball) build(c upspin.Client, des []*upspin.DirEntry) error {
	tb.mu.Lock()
	data, err := buildTarball(c, des)
	tb.data = data
	tb.mu.Unlock()
	atomic.StoreInt64(&tb.size, int64(len(tb.data)))
	return err
}

// buildTarball fetches des using the given Client and
// assembles a tarball containing those files.
func buildTarball(c upspin.Client, des []*upspin.DirEntry) ([]byte, error) {
	// No need to check zw and tw write errors
	// as they cannnot fail writing to a bytes.Buffer.
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, de := range des {
		b, err := c.Get(de.Name)
		if err != nil {
			return nil, err
		}

		p, _ := path.Parse(de.Name)
		tw.WriteHeader(&tar.Header{
			Name:    p.Elem(2),
			Mode:    0755,
			Size:    int64(len(b)),
			ModTime: de.Time.Go(),
		})
		tw.Write(b)
	}
	tw.Close()
	zw.Close()
	return buf.Bytes(), nil
}
