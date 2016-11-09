// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Upspin is a simple utility for exercising the upspin client against the user's default context.
package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	// We deliberately use native Go logs for this command-line tool
	// as there is no need to report errors to GCP.
	// Our dependencies will still use the Upspin logs
	"log"

	"upspin.io/bind"
	"upspin.io/client"
	"upspin.io/context"
	"upspin.io/flags"
	"upspin.io/metric"
	"upspin.io/path"
	"upspin.io/upspin"

	// Load useful packers
	_ "upspin.io/pack/ee"
	_ "upspin.io/pack/plain"

	// Load required transports
	"upspin.io/transports"
)

var commands = map[string]func(*State, ...string){
	"countersign":   (*State).countersign,
	"cp":            (*State).cp,
	"deletestorage": (*State).deletestorage,
	"get":           (*State).get,
	"info":          (*State).info,
	"keygen":        (*State).keygen,
	"link":          (*State).link,
	"ls":            (*State).ls,
	"mkdir":         (*State).mkdir,
	"put":           (*State).put,
	"repack":        (*State).repack,
	"rotate":        (*State).rotate,
	"rm":            (*State).rm,
	"share":         (*State).share,
	"signup":        (*State).signup,
	"snapshot":      (*State).snapshot,
	"tar":           (*State).tar,
	"user":          (*State).user,
	"whichaccess":   (*State).whichAccess,
}

type State struct {
	op           string // Name of the subcommand we are running.
	client       upspin.Client
	context      upspin.Context
	sharer       *Sharer
	exitCode     int // Exit with non-zero status for minor problems.
	interactive  bool
	metricsSaver metric.Saver
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("upspin: ")
	flag.Usage = usage
	flags.Parse() // enable all flags

	if len(flag.Args()) < 1 {
		usage()
	}

	state := newState(strings.ToLower(flag.Arg(0)))
	args := flag.Args()[1:]

	// Shell cannot be in commands because of the initialization loop,
	// and anyway we should avoid recursion in the interpreter.
	if state.op == "shell" {
		state.shell(args...)
		return
	}
	fn := commands[state.op]
	if fn == nil {
		fmt.Fprintf(os.Stderr, "upspin: no such command %q\n", flag.Arg(0))
		usage()
	}
	fn(state, args...)
	state.cleanup()
	os.Exit(state.exitCode)
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage of upspin:\n")
	fmt.Fprintf(os.Stderr, "\tupspin [globalflags] <command> [flags] <path>\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	var cmdStrs []string
	for cmd := range commands {
		cmdStrs = append(cmdStrs, cmd)
	}
	sort.Strings(cmdStrs)
	fmt.Fprintf(os.Stderr, "\tshell (Interactive mode)\n")
	for _, cmd := range cmdStrs {
		fmt.Fprintf(os.Stderr, "\t%s\n", cmd)
	}
	fmt.Fprintf(os.Stderr, "Global flags:\n")
	flag.PrintDefaults()
	os.Exit(2)
}

// exitf prints the error and exits the program.
// If we are interactive, it pops up to the interpreter.
// We don't use log (although the packages we call do) because the errors
// are for regular people.
func (s *State) exitf(format string, args ...interface{}) {
	format = fmt.Sprintf("upspin: %s: %s\n", s.op, format)
	fmt.Fprintf(os.Stderr, format, args...)
	if s.interactive {
		panic("exit")
	}
	s.cleanup()
	os.Exit(1)
}

// exit calls s.exitf with the error.
func (s *State) exit(err error) {
	s.exitf("%s", err)
}

// failf logs the error and sets the exit code. It does not exit the program.
func (s *State) failf(format string, args ...interface{}) {
	format = fmt.Sprintf("upspin: %s: %s\n", s.op, format)
	fmt.Fprintf(os.Stderr, format, args...)
	s.exitCode = 1
}

// fail calls s.failf with the error.
func (s *State) fail(err error) {
	s.failf("%v", err)
}

func (s *State) parseFlags(fs *flag.FlagSet, args []string, help, usage string) {
	helpFlag := fs.Bool("help", false, "print more information about the command")
	usageFn := func() {
		fmt.Fprintf(os.Stderr, "Usage: upspin %s\n", usage)
		if *helpFlag {
			fmt.Fprintln(os.Stderr, help)
		}
		// How many flags?
		n := 0
		fs.VisitAll(func(*flag.Flag) { n++ })
		if n > 0 {
			fmt.Fprintf(os.Stderr, "Flags:\n")
			fs.PrintDefaults()
		}
		if s.interactive {
			panic("exit")
		}
		os.Exit(2)
	}
	fs.Usage = usageFn
	err := fs.Parse(args)
	if err != nil {
		s.exit(err)
	}
	if *helpFlag {
		fs.Usage()
	}
}

// readAll reads all contents from a local input file or from stdin if
// the input file name is empty
func (s *State) readAll(fileName string) []byte {
	var input *os.File
	var err error
	if fileName == "" {
		input = os.Stdin
	} else {
		input = s.openLocal(fileName)
		defer input.Close()
	}

	data, err := ioutil.ReadAll(input)
	if err != nil {
		s.exit(err)
	}
	return data
}

func newState(op string) *State {
	s := &State{
		op: op,
	}
	if op == "signup" {
		// signup is special since there is no user yet.
		return s
	}
	ctx, err := context.FromFile(flags.Context)
	if err != nil && err != context.ErrNoFactotum {
		s.exit(err)
	}
	transports.Init(ctx)
	s.client = client.New(ctx)
	s.context = ctx
	s.sharer = newSharer(s)
	s.maybeEnableMetrics()
	return s
}

func (s *State) DirServer() upspin.DirServer {
	dir, err := bind.DirServer(s.context, s.context.DirEndpoint())
	if err != nil {
		s.exit(err)
	}
	return dir
}

func (s *State) KeyServer() upspin.KeyServer {
	key, err := bind.KeyServer(s.context, s.context.KeyEndpoint())
	if err != nil {
		s.exit(err)
	}
	return key
}

// end terminates any necessary state.
func (s *State) cleanup() {
	s.finishMetricsIfEnabled()
}

func (s *State) maybeEnableMetrics() {
	gcloudProject := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	if strings.Contains(gcloudProject, "upspin-test") {
		gcloudProject = "upspin-test"
	} else if strings.Contains(gcloudProject, "upspin-prod") {
		gcloudProject = "upspin-prod"
	} else {
		return
	}
	var err error
	if s.metricsSaver, err = metric.NewGCPSaver(gcloudProject, "app", "cmd/upspin"); err == nil {
		metric.RegisterSaver(s.metricsSaver)
	} else {
		log.Printf("saving metrics: %q", err)
	}
}

func (s *State) finishMetricsIfEnabled() {
	if s.metricsSaver == nil {
		return
	}
	// Allow time for metrics to propagate.
	for i := 0; metric.NumProcessed() > s.metricsSaver.NumProcessed() && i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
	}
}

// hasGlobChar reports whether the string contains a Glob metacharacter.
func hasGlobChar(pattern string) bool {
	return strings.ContainsAny(pattern, `\*?[`)
}

// globAllUpspin processes the arguments, which should be Upspin paths,
// expanding glob patterns.
func (s *State) globAllUpspin(args []string) []upspin.PathName {
	paths := make([]upspin.PathName, 0, len(args))
	for _, arg := range args {
		paths = append(paths, s.globUpspin(arg)...)
	}
	return paths
}

// globUpspin glob-expands the argument, which must be a syntactically
// valid Upspin glob pattern (including a plain path name).
func (s *State) globUpspin(pattern string) []upspin.PathName {
	// Must be a valid Upspin path.
	parsed, err := path.Parse(upspin.PathName(pattern))
	if err != nil {
		s.exit(err)
	}
	// If it has no metacharacters, leave it alone but clean it.
	if !hasGlobChar(pattern) {
		return []upspin.PathName{path.Clean(upspin.PathName(pattern))}
	}
	var out []upspin.PathName
	entries, err := s.client.Glob(parsed.String())
	if err != nil {
		s.exit(err)
	}
	for _, entry := range entries {
		out = append(out, entry.Name)
	}
	return out
}

// globOneUpspin glob-expands the argument, which must result in a
// single Upspin path.
func (s *State) globOneUpspin(pattern string) upspin.PathName {
	strs := s.globUpspin(pattern)
	if len(strs) != 1 {
		s.exitf("more than one file matches %s", pattern)
	}
	return strs[0]
}

// globLocal glob-expands the argument, which should be a syntactically
// valid glob pattern (including a plain file name).
func (s *State) globLocal(pattern string) []string {
	// If it has no metacharacters, leave it alone.
	if !hasGlobChar(pattern) {
		return []string{pattern}
	}
	strs, err := filepath.Glob(pattern)
	if err != nil {
		// Bad pattern, so treat as a literal.
		return []string{pattern}
	}
	return strs
}

// globOneLocal glob-expands the argument, which must result in a
// single local file name.
func (s *State) globOneLocal(pattern string) string {
	strs := s.globLocal(pattern)
	if len(strs) != 1 {
		s.exitf("more than one file matches %s", pattern)
	}
	return strs[0]
}

func (s *State) openLocal(path string) *os.File {
	f, err := os.Open(path)
	if err != nil {
		s.exitf(err.Error())
	}
	return f
}

func (s *State) createLocal(path string) *os.File {
	f, err := os.Create(path)
	if err != nil {
		s.exitf(err.Error())
	}
	return f
}

// intFlag returns the value of the named integer flag in the flag set.
func intFlag(fs *flag.FlagSet, name string) int {
	return fs.Lookup(name).Value.(flag.Getter).Get().(int)
}

// boolFlag returns the value of the named boolean flag in the flag set.
func boolFlag(fs *flag.FlagSet, name string) bool {
	return fs.Lookup(name).Value.(flag.Getter).Get().(bool)
}

// stringFlag returns the value of the named string flag in the flag set.
func stringFlag(fs *flag.FlagSet, name string) string {
	return fs.Lookup(name).Value.(flag.Getter).Get().(string)
}
