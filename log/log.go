// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package log exports logging primitives that log to stderr and also to Google Cloud Logging.
package log

// We call this log instead of logging for two reasons:
// 1) It's shorter to type;
// 2) it mimics Go's log package and can be used as a drop-in replacement for it.

import (
	goLog "log"
	"os"

	"golang.org/x/net/context"
	"google.golang.org/cloud"
	"google.golang.org/cloud/logging"

	"upspin.io/flags"
)

// Logger is the interface for logging messages.
type Logger interface {
	// Printf writes a formated message to the log.
	Printf(format string, v ...interface{})

	// Print writes a message to the log.
	Print(v ...interface{})

	// Println writes a line to the log.
	Println(v ...interface{})

	// Fatal writes a message to the log and aborts.
	Fatal(v ...interface{})

	// Fatalf writes a formated message to the log and aborts.
	Fatalf(format string, v ...interface{})
}

// Level is the level of logging.
type Level int

// Different levels of logging.
const (
	Ldebug    = Level(logging.Debug)
	Linfo     = Level(logging.Info)
	Lerror    = Level(logging.Error)
	Ldisabled = Level(4000) // Some big value we'll never use.
	Linvalid  = Level(-2)
)

// Pre-allocated Loggers at each logging level.
var (
	Debug = newLogger(Ldebug)
	Info  = newLogger(Linfo)
	Error = newLogger(Lerror)

	defaultClient *logging.Client
	defaultLogger Logger = goLog.New(os.Stderr, "", goLog.Ldate|goLog.Ltime|goLog.LUTC|goLog.Lmicroseconds)
)

type logger struct {
	level  logging.Level
	client *logging.Client
}

var _ Logger = (*logger)(nil)

// New creates a new logger at a given level, possibly backed by a Google Cloud Logging instance assigned to a
// project ID and logName if projectID is not empty and is a valid Google Cloud project ID.
func New(level Level, projectID, logName string) (Logger, error) {
	var client *logging.Client
	var err error
	if projectID != "" {
		client, err = newClient(projectID, logName)
		if err != nil {
			return nil, err
		}
	}
	return &logger{
		level:  logging.Level(level),
		client: client,
	}, nil
}

func (l Level) String() string {
	switch l {
	case Ldebug:
		return "debug"
	case Linfo:
		return "info"
	case Lerror:
		return "error"
	case Ldisabled:
		return "disabled"
	}
	return "unknown error type"
}

func logLevelFromString(s string) Level {
	switch s {
	case "debug":
		return Ldebug
	case "info":
		return Linfo
	case "error":
		return Lerror
	case "disabled":
		return Ldisabled
	}
	return Linvalid
}

// Printf writes a formated message to the log.
func (l *logger) Printf(format string, v ...interface{}) {
	if l.level < logging.Level(CurrentLevel()) {
		return // Don't log at lower levels.
	}
	if l.client != nil {
		l.client.Logger(l.level).Printf(format, v...)
	} else if defaultClient != nil {
		defaultClient.Logger(l.level).Printf(format, v...)
	}
	defaultLogger.Printf(format, v...)
}

// Print writes a message to the log.
func (l *logger) Print(v ...interface{}) {
	if l.level < logging.Level(CurrentLevel()) {
		return // Don't log at lower levels.
	}
	if l.client != nil {
		l.client.Logger(l.level).Print(v...)
	} else if defaultClient != nil {
		defaultClient.Logger(l.level).Print(v...)
	}
	defaultLogger.Print(v...)
}

// Println writes a line to the log.
func (l *logger) Println(v ...interface{}) {
	if l.level < logging.Level(CurrentLevel()) {
		return // Don't log at lower levels.
	}
	if l.client != nil {
		l.client.Logger(l.level).Println(v...)
	} else if defaultClient != nil {
		defaultClient.Logger(l.level).Println(v...)
	}
	defaultLogger.Println(v...)
}

// Fatal writes a message to the log and aborts, regardless of the current log level.
func (l *logger) Fatal(v ...interface{}) {
	// Fatal always logs.
	if l.client != nil {
		l.client.Logger(l.level).Print(v...)
	} else if defaultClient != nil {
		defaultClient.Logger(l.level).Print(v...)
	}
	defaultLogger.Fatal(v...)
}

// Fatalf writes a formated message to the log and aborts, regardless of the current log level.
func (l *logger) Fatalf(format string, v ...interface{}) {
	// Fatalf always logs.
	if l.client != nil {
		l.client.Logger(l.level).Printf(format, v...)
	} else if defaultClient != nil {
		defaultClient.Logger(l.level).Printf(format, v...)
	}
	defaultLogger.Fatalf(format, v...)
}

// SetLevel sets the current logging level. Lower levels than current will not be logged.
func SetLevel(level Level) {
	*flags.LogLevel = level.String()
}

// CurrentLevel returns the current logging level.
func CurrentLevel() Level {
	l := logLevelFromString(*flags.LogLevel)
	if l == Linvalid {
		Error.Printf("Invalid log level %q", *flags.LogLevel)
		return Linfo
	}
	return l
}

// At returns whether the level will be logged currently.
func At(level Level) bool {
	return CurrentLevel() <= level
}

// Printf writes a formated message to the log.
func Printf(format string, v ...interface{}) {
	Info.Printf(format, v...)
}

// Print writes a message to the log.
func Print(v ...interface{}) {
	Info.Print(v...)
}

// Println writes a line to the log.
func Println(v ...interface{}) {
	Info.Println(v...)
}

// Fatal writes a message to the log and aborts.
func Fatal(v ...interface{}) {
	Info.Fatal(v...)
}

// Fatalf writes a formated message to the log and aborts.
func Fatalf(format string, v ...interface{}) {
	Info.Fatalf(format, v...)
}

// Connect connects all non-custom loggers (those not created by New) in this address space to a GCP Logging
// instance writing to a given logName.
func Connect(projectID, logName string) error {
	var err error
	defaultClient, err = newClient(projectID, logName)
	if err != nil {
		return err
	}
	return nil
}

// newClient creates a new client connected to the GCP Logging API with an assigned logName.
func newClient(projectID, logName string) (*logging.Client, error) {
	client, err := logging.NewClient(context.Background(), projectID, logName, cloud.WithScopes(logging.Scope))
	if err != nil {
		return nil, err
	}
	return client, nil
}

// newLogger instantiates an implicit Logger using the default client.
func newLogger(level Level) Logger {
	return &logger{
		level: logging.Level(level),
	}
}
