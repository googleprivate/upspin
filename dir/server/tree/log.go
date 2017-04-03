// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tree

// This file defines and implements three components for record keeping for a
// Tree:
//
// 1) Writer - writes log entries to the end of the log file.
// 2) Reader - reads log entries from any offset of the log file.
// 3) LogIndex - saves the most recent commit point in the log and the root.
//
// The structure on disk is, relative to a log directory:
//
// tree.root.<username>  - root entry for username
// tree.index.<username> - last processed log index for username
// d.tree.log.<username> - subdirectory for username, containing:
// <offset> - logs greater than offset but less than the next offset file.
//
// There is also a legacy file tree.log.<username> which is hard linked to
// offset zero in the d.tree.log.<username> directory.
//

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"upspin.io/errors"
	"upspin.io/log"
	"upspin.io/upspin"
)

// Operation is the kind of operation performed on the DirEntry.
type Operation int

// Operations on dir entries that are logged.
const (
	Put Operation = iota
	Delete
)

// LogEntry is the unit of logging.
type LogEntry struct {
	Op    Operation
	Entry upspin.DirEntry
}

// Writer is an append-only log of LogEntry.
type Writer struct {
	user upspin.UserName // user for whom this log is intended.

	mu         *sync.Mutex // protects fields below.
	file       *os.File    // file descriptor for the log.
	fileOffset int64       // offset of the first record from the file.
}

// Reader reads LogEntries from the log.
type Reader struct {
	// wmu is a copy of the writer's lock, for use when reading from the
	// same file as the write log. It must be held after mu if it will be
	// held.
	wmu *sync.Mutex

	// mu protects the fields below. If wmu must be held, it must be
	// held after mu.
	mu sync.Mutex

	// fileOffset is the offset of the first record from the file we're
	// reading now.
	fileOffset int64

	// file is the file for the current log, indicated by fileOffset.
	file *os.File

	// offsets is a sorted list of the known offsets available for reading.
	offsets []int64
}

// LogIndex reads and writes from/to stable storage the log state information
// and the user's root entry. It is used by Tree to track its progress
// processing the log and storing the root.
type LogIndex struct {
	user upspin.UserName // user for whom this logindex is intended.

	mu        *sync.Mutex // protects the files, making reads/write atomic.
	indexFile *os.File    // file descriptor for the last index in the log.
	rootFile  *os.File    // file descriptor for the root of the tree.
}

const oldStyleLogFilePrefix = "tree.log."

// NewLogs returns a new Writer log and a new LogIndex for a user, logging to
// and from a given directory accessible to the local file system. If directory
// already contains a log or a log index for the user they are opened and
// returned. Otherwise they are created.
//
// Only one Writer per user can be opened in a directory or unpredictable
// results may occur.
func NewLogs(user upspin.UserName, directory string) (*Writer, *LogIndex, error) {
	const op = "dir/server/tree.NewLogs"

	subdir := logSubDir(user, directory) // user's sub directory.
	off := logOffsetsFor(subdir)
	if off[0] == 0 { // Possibly starting a new log.
		// Is there an existing, old-style log file? If so, hard link it
		// to the zero offset entry in the user's subdirectory.
		oldLogName := filepath.Join(directory, oldStyleLogFilePrefix+string(user))
		_, err := os.Stat(oldLogName)
		if err == nil {
			// Only hardlink if the link does not yet exist.
			if _, err := os.Stat(logFile(user, 0, directory)); err != nil {
				err = os.Link(oldLogName, logFile(user, 0, directory))
				if err != nil {
					return nil, nil, errors.E(op, errors.IO, err)
				}
			}
		}
		// Other errors are ignored. If they're bad enough, we'll fail
		// below.
	}

	loc := logFile(user, off[0], directory)
	loggerFile, err := os.OpenFile(loc, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return nil, nil, errors.E(op, errors.IO, err)
	}
	l := &Writer{
		user:       user,
		mu:         &sync.Mutex{},
		file:       loggerFile,
		fileOffset: off[0],
	}

	rloc := rootFile(user, directory)
	iloc := indexFile(user, directory)
	rootFile, err := os.OpenFile(rloc, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, nil, errors.E(op, errors.IO, err)
	}
	indexFile, err := os.OpenFile(iloc, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, nil, errors.E(op, errors.IO, err)
	}
	li := &LogIndex{
		user:      user,
		mu:        &sync.Mutex{},
		indexFile: indexFile,
		rootFile:  rootFile,
	}
	return l, li, nil
}

// HasLog reports whether user has logs in directory.
func HasLog(user upspin.UserName, directory string) (bool, error) {
	const op = "dir/server/tree.HasLog"
	var firstErr error
	for _, name := range []string{
		filepath.Join(directory, oldStyleLogFilePrefix+string(user)),
		logSubDir(user, directory),
	} {
		_, err := os.Stat(name)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			if firstErr != nil {
				firstErr = errors.E(op, errors.IO, err)
			}
		}
		return true, nil
	}
	return false, firstErr
}

// DeleteLogs deletes all data for a user in directory. Any existing logs
// associated with user must not be used subsequently.
func DeleteLogs(user upspin.UserName, directory string) error {
	const op = "dir/server/tree.DeleteLogs"
	for _, fn := range []string{
		filepath.Join(directory, oldStyleLogFilePrefix+string(user)),
		rootFile(user, directory),
		indexFile(user, directory),
	} {
		err := os.Remove(fn)
		if err != nil {
			return errors.E(op, errors.IO, err)
		}
	}
	// Remove the user's log directory, if any, with all its contents.
	// Note: RemoveAll returns nil if the subdir does not exist.
	return os.RemoveAll(logSubDir(user, directory))
}

// ListUsers lists all known users in directory.
// TODO: do not allow a pattern; it's error-prone and exposes too much internal
// state that can lead to had-to-refactor code in the future.
func ListUsers(pattern string, directory string) ([]upspin.UserName, error) {
	const op = "dir/server/tree.ListUsers"
	prefix := rootFile("", directory)
	matches, err := filepath.Glob(rootFile(upspin.UserName(pattern), directory))
	if err != nil {
		return nil, errors.E(op, errors.IO, err)
	}
	users := make([]upspin.UserName, len(matches))
	for i, m := range matches {
		users[i] = upspin.UserName(strings.TrimPrefix(m, prefix))
	}
	return users, nil
}

func logFile(user upspin.UserName, offset int64, directory string) string {
	return filepath.Join(logSubDir(user, directory), fmt.Sprintf("%d", offset))
}

func logSubDir(user upspin.UserName, directory string) string {
	return filepath.Join(directory, "d.tree.log."+string(user))
}

func indexFile(user upspin.UserName, directory string) string {
	return filepath.Join(directory, "tree.index."+string(user))
}

func rootFile(user upspin.UserName, directory string) string {
	return filepath.Join(directory, "tree.root."+string(user))
}

// logOffsetsFor returns in descending order a list of log offsets in a log
// directory for a user. If no log directory exists, one is created and the only
// offset returned is 0.
func logOffsetsFor(directory string) []int64 {
	offs, err := filepath.Glob(filepath.Join(directory, "*"))
	if err != nil {
		return []int64{0}
	}
	if len(offs) == 0 {
		// No log directory. Create it now.
		err := os.Mkdir(directory, 0700)
		if err != nil {
			log.Error.Printf("dir/server/tree.logOffsetsFor: %s", err)
			// Ignore the error. We will error out later again.
		}
		return []int64{0}
	}
	var offsets []int64
	for _, o := range offs {
		off, err := strconv.ParseInt(filepath.Base(o), 10, 64)
		if err != nil {
			log.Error.Printf("dir/server/tree.logOffsetsFor: Can't parse log offset: %s", o)
			continue
		}
		offsets = append(offsets, off)
	}
	sort.Slice(offsets, func(i, j int) bool { return offsets[i] > offsets[j] })
	return offsets
}

// User returns the user name who owns the root of the tree that this log represents.
func (w *Writer) User() upspin.UserName {
	return w.user
}

// Append appends a LogEntry to the end of the log.
func (w *Writer) Append(e *LogEntry) error {
	const op = "dir/server/tree.Append"
	buf, err := e.marshal()
	if err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	prevOffs := lastOffset(w.file)

	// File is append-only, so this is guaranteed to write to the tail.
	n, err := w.file.Write(buf)
	if err != nil {
		return errors.E(op, errors.IO, err)
	}
	err = w.file.Sync()
	if err != nil {
		return errors.E(op, errors.IO, err)
	}
	// Sanity check: flush worked and the new offset is the expected one.
	newOffs := prevOffs + int64(n)
	if newOffs != lastOffset(w.file) {
		// This might indicate a race somewhere, despite the locks.
		return errors.E(op, errors.IO, errors.Errorf("file.Sync did not update offset: expected %d, got %d", newOffs, lastOffset(w.file)))
	}
	return nil
}

// ReadAt reads an entry from the log at offset. It returns the log entry and
// the next offset.
func (r *Reader) ReadAt(offset int64) (le LogEntry, next int64, err error) {
	const op = "dir/server/tree.ReadAt"

	// TODO: Decide whether we need to lock wmu.

	r.mu.Lock()
	defer r.mu.Unlock()

	// The maximum offset we can satisfy with the current log file.
	maxOff := r.fileOffset + lastOffset(r.file)

	// Is the requested offset outside the bounds of the current log file?
	before := offset < r.fileOffset
	after := offset > maxOff
	if before || after {
		if after {
			// Load new file offsets in case there was a log rotation.
			r.offsets = logOffsetsFor(filepath.Dir(r.file.Name()))
		}
		readOffset := r.readOffset(offset)
		// Locate the file and open it.
		err := r.openLogAtOffset(readOffset, filepath.Dir(r.file.Name()))
		if err != nil {
			return le, 0, errors.E(op, errors.IO, err)
		}
		// Recompute maxOff for the new file.
		maxOff = r.fileOffset + lastOffset(r.file)
	}

	// If we're reading from the tail file (max r.readOffsets), then we
	// must lock wmu.
	if r.offsets[0] == r.fileOffset {
		r.wmu.Lock()
		defer r.wmu.Unlock()
	}

	// Are we past the end of the current file?
	if offset >= maxOff {
		return le, maxOff, nil
	}

	_, err = r.file.Seek(offset-r.fileOffset, io.SeekStart)
	if err != nil {
		return le, 0, errors.E(op, errors.IO, err)
	}
	next = offset
	checker := newChecker(r.file)
	defer checker.close()

	err = le.unmarshal(checker)
	if err != nil {
		return le, next, err
	}
	next = next + int64(checker.count)
	return
}

// readOffset returns the log we must read from to satisfy offset. If offset
// is not in the range of what we have stored it returns -1.
func (r *Reader) readOffset(offset int64) int64 {
	for _, o := range r.offsets { // r.offsets are in descending order.
		if offset >= o {
			return o
		}
	}
	return -1
}

// LastOffset returns the offset of the end of the file or -1 on error.
func (w *Writer) LastOffset() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return lastOffset(w.file)
}

// LastOffset returns the offset of the end of the file or -1 on error.
func (r *Reader) LastOffset() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	// If we're reading from the same file as the current Writer, lock it.
	// Order is important.
	if r.fileOffset == r.offsets[0] {
		r.wmu.Lock()
		defer r.wmu.Unlock()
	}

	return lastOffset(r.file)
}

// lastOffset returns the offset of the end of the file or -1 on error.
// The file must be changed simultaneously with this call.
func lastOffset(f *os.File) int64 {
	fi, err := f.Stat()
	if err != nil {
		return -1
	}
	return fi.Size()
}

// Truncate truncates the log at offset.
func (w *Writer) Truncate(offset int64) error {
	const op = "dir/server/tree.Truncate"
	w.mu.Lock()
	defer w.mu.Unlock()

	err := w.file.Truncate(offset)
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

// NewReader makes a reader of the log.
func (w *Writer) NewReader() (*Reader, error) {
	const op = "dir/server/tree.NewReader"

	r := &Reader{}

	// Order is important.
	r.mu.Lock()
	defer r.mu.Unlock()
	w.mu.Lock()
	defer w.mu.Unlock()

	r.wmu = w.mu
	r.offsets = logOffsetsFor(filepath.Dir(w.file.Name()))

	dir := filepath.Dir(w.file.Name())
	err := r.openLogAtOffset(w.fileOffset, dir)
	if err != nil {
		return nil, errors.E(op, errors.IO, err)
	}
	return r, nil
}

// openLogAtOffset opens the log file relative to a directory where the absolute
// offset is stored and sets it as this Reader's current file.
// r.mu must be held.
func (r *Reader) openLogAtOffset(offset int64, directory string) error {
	f, err := os.Open(filepath.Join(directory, fmt.Sprintf("%d", offset)))
	if err != nil {
		return err
	}
	if r.file != nil {
		r.file.Close()
	}
	r.file = f
	r.fileOffset = offset
	return nil
}

// Close closes the writer.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file != nil {
		err := w.file.Close()
		w.file = nil
		return err
	}
	return nil
}

// Close closes the reader.
func (r *Reader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.file != nil {
		err := r.file.Close()
		r.file = nil
		return err
	}
	return nil
}

// User returns the user name who owns the root of the tree that this
// log index represents.
func (li *LogIndex) User() upspin.UserName {
	return li.user
}

// Root returns the user's root by retrieving it from local stable storage.
func (li *LogIndex) Root() (*upspin.DirEntry, error) {
	const op = "dir/server/tree.LogIndex.Root"
	li.mu.Lock()
	defer li.mu.Unlock()

	var root upspin.DirEntry
	buf, err := readAllFromTop(op, li.rootFile)
	if err != nil {
		return nil, err
	}
	if len(buf) == 0 {
		return nil, errors.E(op, errors.NotExist, li.user, errors.Str("no root for user"))
	}
	more, err := root.Unmarshal(buf)
	if err != nil {
		return nil, errors.E(op, err)
	}
	if len(more) != 0 {
		return nil, errors.E(op, errors.IO, errors.Errorf("root has %d left over bytes", len(more)))
	}
	return &root, nil
}

// SaveRoot saves the user's root entry to stable storage.
func (li *LogIndex) SaveRoot(root *upspin.DirEntry) error {
	const op = "dir/server/tree.LogIndex.SaveRoot"
	buf, err := root.Marshal()
	if err != nil {
		return errors.E(op, err)
	}

	li.mu.Lock()
	defer li.mu.Unlock()
	return overwriteAndSync(op, li.rootFile, buf)
}

// DeleteRoot deletes the root.
func (li *LogIndex) DeleteRoot() error {
	const op = "dir/server/tree.LogIndex.DeleteRoot"
	li.mu.Lock()
	defer li.mu.Unlock()

	return overwriteAndSync(op, li.rootFile, []byte{})
}

// Clone makes a read-only copy of the log index.
func (li *LogIndex) Clone() (*LogIndex, error) {
	const op = "dir/server/tree.LogIndex.Clone"
	li.mu.Lock()
	defer li.mu.Unlock()

	idx, err := os.Open(li.indexFile.Name())
	if err != nil {
		return nil, errors.E(op, errors.IO, err)
	}
	root, err := os.Open(li.rootFile.Name())
	if err != nil {
		return nil, errors.E(op, errors.IO, err)
	}
	newLog := *li
	newLog.indexFile = idx
	newLog.rootFile = root
	return &newLog, nil
}

func overwriteAndSync(op string, f *os.File, buf []byte) error {
	_, err := f.Seek(0, io.SeekStart)
	if err != nil {
		return errors.E(op, errors.IO, err)
	}
	n, err := f.Write(buf)
	if err != nil {
		return errors.E(op, errors.IO, err)
	}
	err = f.Truncate(int64(n))
	if err != nil {
		return errors.E(op, errors.IO, err)
	}
	return f.Sync()
}

func readAllFromTop(op string, f *os.File) ([]byte, error) {
	_, err := f.Seek(0, io.SeekStart)
	if err != nil {
		return nil, errors.E(op, errors.IO, err)
	}
	buf, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, errors.E(op, errors.IO, err)
	}
	return buf, nil
}

// ReadOffset reads from stable storage the offset saved by SaveOffset.
func (li *LogIndex) ReadOffset() (int64, error) {
	const op = "dir/server/tree.LogIndex.ReadOffset"
	li.mu.Lock()
	defer li.mu.Unlock()

	buf, err := readAllFromTop(op, li.indexFile)
	if err != nil {
		return 0, errors.E(op, errors.IO, err)
	}
	if len(buf) == 0 {
		return 0, errors.E(op, errors.NotExist, li.user, errors.Str("no log offset for user"))
	}
	offset, n := binary.Varint(buf)
	if n <= 0 {
		return 0, errors.E(op, errors.IO, errors.Str("invalid offset read"))
	}
	return offset, nil
}

// SaveOffset saves to stable storage the offset to process next.
func (li *LogIndex) SaveOffset(offset int64) error {
	const op = "dir/server/tree.LogIndex.SaveOffset"
	if offset < 0 {
		return errors.E(op, errors.Invalid, errors.Str("negative offset"))
	}
	var tmp [16]byte // For use by PutVarint.
	n := binary.PutVarint(tmp[:], offset)

	li.mu.Lock()
	defer li.mu.Unlock()

	return overwriteAndSync(op, li.indexFile, tmp[:n])
}

// Close closes the LogIndex.
func (li *LogIndex) Close() error {
	li.mu.Lock()
	defer li.mu.Unlock()

	var firstErr error
	if li.indexFile != nil {
		firstErr = li.indexFile.Close()
		li.indexFile = nil
	}
	if li.rootFile != nil {
		err := li.rootFile.Close()
		li.rootFile = nil
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// marshal packs the LogEntry into a new byte slice for storage.
func (le *LogEntry) marshal() ([]byte, error) {
	const op = "dir/server/tree.LogEntry.marshal"
	var b []byte
	var tmp [16]byte // For use by PutVarint.
	// This should have been b = append(b, byte(le.Op)) since Operation
	// is known to fit in a byte. However, we already encode it with
	// Varint and changing it would cause backward-incompatible issues.
	n := binary.PutVarint(tmp[:], int64(le.Op))
	b = append(b, tmp[:n]...)

	entry, err := le.Entry.Marshal()
	if err != nil {
		return nil, errors.E(op, err)
	}
	b = appendBytes(b, entry)
	chksum := checksum(b)
	b = append(b, chksum[:]...)
	return b, nil
}

func checksum(buf []byte) [4]byte {
	var c [4]byte
	copy(c[:], checksumSalt[:])
	for i, b := range buf {
		c[i%4] ^= b
	}
	return c
}

func appendBytes(b, bytes []byte) []byte {
	var tmp [16]byte // For use by PutVarint.
	n := binary.PutVarint(tmp[:], int64(len(bytes)))
	b = append(b, tmp[:n]...)
	b = append(b, bytes...)
	return b
}

var checksumSalt = [4]byte{0xde, 0xad, 0xbe, 0xef}

// checker computes the checksum of a file as it reads bytes from it. It also
// reports the number of bytes read in its count field.
type checker struct {
	rd     *bufio.Reader
	count  int
	chksum [4]byte
}

var pool sync.Pool

func newChecker(r io.Reader) *checker {
	var chk *checker
	if c, ok := pool.Get().(*checker); c != nil && ok {
		chk = c
		chk.reset(r)
	} else {
		chk = &checker{rd: bufio.NewReader(r), chksum: checksumSalt}
	}
	return chk
}

// ReadByte implements io.ByteReader.
func (c *checker) ReadByte() (byte, error) {
	b, err := c.rd.ReadByte()
	if err == nil {
		c.chksum[c.count%4] = c.chksum[c.count%4] ^ b
		c.count++
	}
	return b, err
}

// resetChecksum resets the checksum and the counting of bytes, without
// affecting the reader state.
func (c *checker) resetChecksum() {
	c.count = 0
	c.chksum = checksumSalt
}

// reset clears all internal state: clears count, checksum and any buffering.
func (c *checker) reset(rd io.Reader) {
	c.rd.Reset(rd)
	c.resetChecksum()
}

// close closes the checker and releases internal storage. Future uses of it are
// invalid.
func (c *checker) close() {
	c.rd.Reset(nil)
	pool.Put(c)
}

// Read implements io.Reader.
func (c *checker) Read(p []byte) (n int, err error) {
	n, err = c.rd.Read(p)
	if err != nil {
		return
	}
	for i := 0; i < n; i++ {
		offs := (c.count + i) % 4
		c.chksum[offs] = c.chksum[offs] ^ p[i]
	}
	c.count += n
	return
}

func (c *checker) readChecksum() ([4]byte, error) {
	var chk [4]byte

	n, err := io.ReadFull(c.rd, chk[:])
	if err != nil {
		return chk, err
	}
	c.count += n
	return chk, nil
}

// unmarshal unpacks a marshaled LogEntry from a Reader and stores it in the
// receiver.
func (le *LogEntry) unmarshal(r *checker) error {
	const op = "dir/server/tree.LogEntry.unmarshal"
	operation, err := binary.ReadVarint(r)
	if err != nil {
		return errors.E(op, errors.IO, errors.Errorf("reading op: %s", err))
	}
	le.Op = Operation(operation)
	entrySize, err := binary.ReadVarint(r)
	if err != nil {
		return errors.E(op, errors.IO, errors.Errorf("reading entry size: %s", err))
	}
	if entrySize <= 0 {
		return errors.E(op, errors.IO, errors.Errorf("invalid entry size: %d", entrySize))
	}
	// Read exactly entrySize bytes.
	data := make([]byte, entrySize)
	_, err = io.ReadFull(r, data)
	if err != nil {
		return errors.E(op, errors.IO, errors.Errorf("reading %d bytes from entry: %s", entrySize, err))
	}
	leftOver, err := le.Entry.Unmarshal(data)
	if err != nil {
		return errors.E(op, errors.IO, err)
	}
	if len(leftOver) != 0 {
		return errors.E(op, errors.IO, errors.Errorf("%d bytes left; log misaligned for entry %+v", len(leftOver), le.Entry))
	}
	chk, err := r.readChecksum()
	if err != nil {
		return errors.E(op, errors.IO, errors.Errorf("reading checksum: %s", err))
	}
	if chk != r.chksum {
		return errors.E(op, errors.IO, errors.Errorf("invalid checksum: got %x, expected %x for entry %+v", r.chksum, chk, le.Entry))
	}
	return nil
}
