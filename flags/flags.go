// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package flags defines command-line flags to make them consistent between binaries.
// Not all flags make sense for all binaries.
package flags // import "upspin.io/flags"

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"upspin.io/config"
	"upspin.io/log"
)

// flagVar represents a flag in this package.
type flagVar struct {
	set  func()        // Set the value at parse time.
	arg  func() string // Return the argument to set the flag.
	arg2 func() string // Return the argument to set the second flag; usually nil.
}

const (
	defaultBlockSize  = 1024 * 1024 // Keep in sync with upspin.BlockSize.]
	defaultHTTPAddr   = ":80"
	defaultHTTPSAddr  = ":443"
	defaultLog        = "info"
	defaultServerKind = ServerKindInProcess

	ServerKindInProcess  = "inprocess"
	ServerKindServer     = "server"
	ServerKindFilesystem = "filesystem"
)

var (
	// BlockSize is the block size used when writing large files. The default is 1MB.
	BlockSize = defaultBlockSize

	// CacheDir specifies the directory for the various file caches.
	defaultCacheDir = filepath.Join(config.Home(), "upspin")
	CacheDir        = defaultCacheDir

	// Config names the Upspin configuration file to use.
	defaultConfig = filepath.Join(config.Home(), "upspin", "config")
	Config        = defaultConfig

	// HTTPAddr is the network address on which to listen for incoming
	// insecure network connections.
	HTTPAddr = defaultHTTPAddr

	// HTTPSAddr is the network address on which to listen for incoming
	// secure network connections.
	HTTPSAddr = defaultHTTPSAddr

	// LetsEncryptCache is the location of a file in which the Let's
	// Encrypt certificates are stored. The containing directory should
	// be owner-accessible only (chmod 0700).
	LetsEncryptCache = ""

	// Log sets the level of logging (implements flag.Value).
	Log logFlag

	// NetAddr is the publicly accessible network address of this server.
	NetAddr = ""

	// Project is the project name on GCP; used by servers, upspin-deploy,
	// and cmd/upspin setupdomain.
	Project = ""

	// ServerConfig specifies configuration options ("key=value") for servers.
	ServerConfig []string

	// ServerKind is the implementation kind of this server.
	ServerKind = defaultServerKind

	// StoreServerName is the Upspin user name of the StoreServer.
	StoreServerUser = ""

	// Prudent sets an extra security mode in the client to check for malicious or
	// buggy servers, at possible cost in performance or convenience.
	// Specifically, one check is that the writer listed in a directory entry is
	// either the owner or a user currently with write permission. This protects
	// against a forged directory entry at the cost of potentially blocking
	// a legitimate file written by a user who no longer has write permission.
	Prudent = false

	// TLSCertFile and TLSKeyFile specify the location of a TLS
	// certificate/key pair used for serving TLS (HTTPS).
	TLSCertFile = ""
	TLSKeyFile  = ""
)

// flags is a map of flag registration functions keyed by flag name,
// used by Parse to register specific (or all) flags.
var flags = map[string]*flagVar{
	"addr": strVar(&NetAddr, "addr", NetAddr, "publicly accessible network address (`host:port`)"),
	"blocksize": &flagVar{
		set: func() {
			flag.IntVar(&BlockSize, "blocksize", BlockSize, "`size` of blocks when writing large files")
		},
		arg: func() string {
			if BlockSize == defaultBlockSize {
				return ""
			}
			return fmt.Sprintf("-blocksize=%d", BlockSize)
		},
	},
	"cachedir":  strVar(&CacheDir, "cachedir", CacheDir, "`directory` containing all file caches"),
	"config":    strVar(&Config, "config", Config, "user's configuration `file`"),
	"http":      strVar(&HTTPAddr, "http", HTTPAddr, "`address` for incoming insecure network connections"),
	"https":     strVar(&HTTPSAddr, "https", HTTPSAddr, "`address` for incoming secure network connections"),
	"kind":      strVar(&ServerKind, "kind", ServerKind, "server implementation `kind` (inprocess, server, etc.)"),
	"letscache": strVar(&LetsEncryptCache, "letscache", "", "Let's Encrypt cache `directory`"),
	"log": &flagVar{
		set: func() {
			Log.Set("info")
			flag.Var(&Log, "log", "`level` of logging: debug, info, error, disabled")
		},
		arg: func() string { return strArg("log", Log.String(), defaultLog) },
	},
	"project": strVar(&Project, "project", Project, "GCP `project` name"),
	"serverconfig": &flagVar{
		set: func() {
			flag.Var(configFlag{&ServerConfig}, "serverconfig", "comma-separated list of configuration options (key=value) for this server")
		},
		arg: func() string { return strArg("serverconfig", configFlag{&ServerConfig}.String(), "") },
	},
	"storeserveruser": strVar(&StoreServerUser, "storeserveruser", "", "user name of the StoreServer"),
	"prudent": &flagVar{
		set: func() {
			flag.BoolVar(&Prudent, "prudent", false, "protect against malicious directory server")
		},
		arg: func() string {
			if !Prudent {
				return ""
			}
			return "-prudent"
		},
	},
	"tls": &flagVar{
		set: func() {
			flag.StringVar(&TLSCertFile, "tls_cert", "", "TLS Certificate `file` in PEM format")
			flag.StringVar(&TLSKeyFile, "tls_key", "", "TLS Key `file` in PEM format")
		},
		arg:  func() string { return strArg("tls_cert", TLSCertFile, "") },
		arg2: func() string { return strArg("tls_key", TLSKeyFile, "") },
	},
}

// Parse registers the command-line flags for the given flag names
// and calls flag.Parse. Passing zero names registers all flags.
// Passing an unknown name triggers a panic.
//
// For example:
// 	flags.Parse("config", "endpoint") // Register Config and Endpoint.
// or
// 	flags.Parse() // Register all flags.
func Parse(names ...string) {
	Register(names...)
	flag.Parse()
}

// Register registers the command-line flags for the given flag names.
// Passing zero names install all flags.
// Passing an unknown name triggers a panic.
//
// For example:
// 	flags.Register("config", "endpoint") // Register Config and Endpoint.
// or
// 	flags.Register() // Register all flags.
func Register(names ...string) {
	if len(names) == 0 {
		// Register all flags if no names provided.
		for _, flag := range flags {
			flag.set()
		}
	} else {
		for _, n := range names {
			flag, ok := flags[n]
			if !ok {
				panic(fmt.Sprintf("unknown flag %q", n))
			}
			flag.set()
		}
	}
}

// Args returns a slice of -flag=value strings that will recreate
// the state of the flags. Flags set to their default value are elided.
func Args() []string {
	var args []string
	for _, flag := range flags {
		arg := flag.arg()
		if arg == "" {
			continue
		}
		args = append(args, arg)
		if flag.arg2 != nil {
			args = append(args, flag.arg2())
		}
	}
	return args
}

// strVar returns a flagVar for the given string flag.
func strVar(value *string, name, _default, usage string) *flagVar {
	return &flagVar{
		set: func() {
			flag.StringVar(value, name, _default, usage)
		},
		arg: func() string {
			return strArg(name, *value, _default)
		},
	}
}

// strArg returns a command-line argument that will recreate the flag,
// or the empty string if the value is the default.
func strArg(name, value, _default string) string {
	if value == _default {
		return ""
	}
	return "-" + name + "=" + value
}

type logFlag string

// String implements flag.Value.
func (f logFlag) String() string {
	return string(f)
}

// Set implements flag.Value.
func (f *logFlag) Set(level string) error {
	err := log.SetLevel(level)
	if err != nil {
		return err
	}
	*f = logFlag(log.GetLevel())
	return nil
}

// Get implements flag.Getter.
func (logFlag) Get() interface{} {
	return log.GetLevel()
}

type configFlag struct {
	s *[]string
}

// String implements flag.Value.
func (f configFlag) String() string {
	if f.s == nil {
		return ""
	}
	return strings.Join(*f.s, ",")
}

// Set implements flag.Value.
func (f configFlag) Set(s string) error {
	ss := strings.Split(strings.TrimSpace(s), ",")
	// Drop empty elements.
	for i := 0; i < len(ss); i++ {
		if ss[i] == "" {
			ss = append(ss[:i], ss[i+1:]...)
		}
	}
	*f.s = ss
	return nil
}

// Get implements flag.Getter.
func (f configFlag) Get() interface{} {
	if f.s == nil {
		return ""
	}
	return *f.s
}
