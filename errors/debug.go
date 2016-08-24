// Copyright 2016 The Upspin Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build debug

package errors

import (
	"bytes"
	"fmt"
	"runtime"
	"strings"
)

type stack struct {
	callers []uintptr
}

func (e *Error) populateStack() {
	e.callers = callers()

	e2, ok := e.Err.(*Error)
	if !ok {
		return
	}

	// Move distinct callers from inner error to outer error
	// (and throw the common callers away)
	// so that we only print the stack trace once.
	i := 0
	ok = false
	for ; i < len(e.callers) && i < len(e2.callers); i++ {
		if e.callers[len(e.callers)-1-i] != e2.callers[len(e2.callers)-1-i] {
			break
		}
		ok = true
	}
	if ok { // The stacks have some PCs in common.
		head := e2.callers[:len(e2.callers)-i]
		tail := e.callers
		e.callers = make([]uintptr, len(head)+len(tail))
		copy(e.callers, head)
		copy(e.callers[len(head):], tail)
		e2.callers = nil
	}
}

func (e *Error) printStack(b *bytes.Buffer) {
	printCallers := callers()

	// Iterate backward through e.callers (the last in the stack is the
	// earliest call, such as main) skipping over the PCs that are shared
	// by the error stack and by this function call stack, printing the
	// names of the functions and their file names and line numbers.
	var prev string // the name of the last-seen function
	var diff bool   // do the print and error call stacks differ now?
	for i := 0; i < len(e.callers); i++ {
		pc := e.callers[len(e.callers)-1-i]
		fn := runtime.FuncForPC(pc)
		name := fn.Name()

		if !diff && i < len(printCallers) {
			ppc := printCallers[len(printCallers)-1-i]
			pname := runtime.FuncForPC(ppc).Name()
			if name == pname {
				// both stacks share this PC, skip it.
				continue
			}
			// No match, don't consider printCallers again.
			diff = true
		}

		// Don't print the same function twice.
		// (Can happen when multiple error stacks have been coalesced.)
		if name == prev {
			continue
		}

		// Find the uncommon prefix between this and the previous
		// function name, separating by dots and slashes.
		trim := 0
		for {
			j := strings.IndexAny(name[trim:], "./")
			if j < 0 {
				break
			}
			if !strings.HasPrefix(prev, name[:j+trim]) {
				break
			}
			trim += j + 1 // skip over the separator
		}

		// Do the printing.
		pad(b, ":\n\t")
		file, line := fn.FileLine(pc)
		fmt.Fprintf(b, "%v:%d: ", file, line)
		if trim > 0 {
			b.WriteString("...")
		}
		b.WriteString(name[trim:])

		prev = name
	}
}

func callers() []uintptr {
	var stk [64]uintptr
	n := runtime.Callers(4, stk[:])
	return stk[:n]
}
