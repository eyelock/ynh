package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Error codes used in the structured-output error envelope. Every structured
// command draws from this set so codes are stable across commands and typos
// fail to compile. Add new codes here rather than inlining string literals
// at the call site.
const (
	errCodeInvalidInput = "invalid_input"
	errCodeNotFound     = "not_found"
	errCodeConfigError  = "config_error"
	errCodeIOError      = "io_error"
)

// cliError reports a command-level error in the shape required by the current
// output format:
//
//   - structured=false: returns a plain Go error for main.go to print verbatim.
//   - structured=true:  writes a single JSON object to stderr matching the
//     error envelope in docs/cli-structured.md, then returns a plain error
//     so the process still exits non-zero.
//
// Shared by every command that supports --format json.
func cliError(stderr io.Writer, structured bool, code, message string) error {
	if !structured {
		return fmt.Errorf("%s", message)
	}
	env := struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{}
	env.Error.Code = code
	env.Error.Message = message
	data, marshalErr := json.Marshal(env)
	if marshalErr != nil {
		// Fallback: the envelope itself is simple enough that marshal should
		// never fail, but be defensive so the caller still exits non-zero.
		return fmt.Errorf("%s", message)
	}
	_, _ = fmt.Fprintln(stderr, string(data))
	// Return a sentinel-style error so main.go still exits non-zero.
	// The message duplicates what's already on stderr; main.go's "Error: ..."
	// line would be printed after the JSON envelope. To keep structured stderr
	// clean, we return errStructuredReported which main.go suppresses.
	return errStructuredReported
}

// errStructuredReported indicates the command has already emitted a structured
// error envelope to stderr. main.go treats this as "exit non-zero, print
// nothing further" so structured consumers see a clean error object and
// nothing else. Callers may wrap with %w — main.go uses errors.Is.
var errStructuredReported = errors.New("structured error already reported")
