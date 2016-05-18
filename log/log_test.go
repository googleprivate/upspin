package log

// TODO: This test is very simple and can be improved.

import (
	"fmt"
	"testing"
)

func TestLogLevel(t *testing.T) {
	const (
		msg1 = "log line1"
		msg2 = "log line2"
		msg3 = "log line3"
	)
	setMockLogger(fmt.Sprintf("%shello: %s", msg2, msg3), false)

	SetLevel(Linfo)
	if CurrentLevel() != Linfo {
		t.Fatalf("Expected %d, got %d", Linfo, CurrentLevel())
	}
	Debug.Println(msg1)             // not logged
	Info.Print(msg2)                // logged
	Error.Printf("hello: %s", msg3) // logged

	defaultLogger.(*mockLogger).Verify(t)
}

func TestFatal(t *testing.T) {
	const (
		msg = "will abort anyway"
	)
	setMockLogger(msg, true)

	SetLevel(Lerror)
	Info.Fatal(msg)

	defaultLogger.(*mockLogger).Verify(t)
}

func TestAt(t *testing.T) {
	SetLevel(Linfo)

	if At(Ldebug) {
		t.Errorf("Debug is expected to be disabled when level is info")
	}
	if !At(Lerror) {
		t.Errorf("Error is expected to be enabled when level is info")
	}
}

func setMockLogger(expected string, fatalExpected bool) {
	defaultLogger = &mockLogger{
		expected:      expected,
		fatalExpected: fatalExpected,
	}
}

type mockLogger struct {
	fatal         bool
	logged        string
	expected      string
	fatalExpected bool
}

func (ml *mockLogger) Printf(format string, v ...interface{}) {
	ml.logged += fmt.Sprintf(format, v...)
}

func (ml *mockLogger) Print(v ...interface{}) {
	ml.logged += fmt.Sprint(v...)
}

func (ml *mockLogger) Println(v ...interface{}) {
	ml.logged += fmt.Sprintln(v...)
}

func (ml *mockLogger) Fatal(v ...interface{}) {
	ml.fatal = true
	ml.Print(v...)
}

func (ml *mockLogger) Fatalf(format string, v ...interface{}) {
	ml.fatal = true
	ml.Printf(format, v...)
}

func (ml *mockLogger) Verify(t *testing.T) {
	if ml.logged != ml.expected {
		t.Errorf("Expected %q, got %q", ml.expected, ml.logged)
	}
	if ml.fatal != ml.fatalExpected {
		t.Errorf("Expected fatal %v, got %v", ml.fatalExpected, ml.fatal)
	}
}
