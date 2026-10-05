package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ynd's copy of the structured-error helpers in cmd/ynh/cliformat.go and
// cmd/ynh/paths.go. The two binaries are separate main packages, so each
// keeps its own; keep the two in step.

// Error codes for the structured-output error envelope, drawn from the closed
// set in docs/schema/shared/enums.schema.json.
const (
	errCodeInvalidInput = "invalid_input"
	errCodeNotFound     = "not_found"
	errCodeConfigError  = "config_error"
	errCodeIOError      = "io_error"
)

// detectJSONFormat scans args for "--format json" without doing full parsing,
// so an error in an argument before the flag is still reported as JSON.
func detectJSONFormat(args []string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--format" && args[i+1] == "json" {
			return true
		}
	}
	return false
}

// cliError reports a command-level error in the shape --format asks for.
// Without structured output it returns a plain error for main to print as
// "Error: ...". With it, it writes the error envelope from
// docs/cli-structured.md to stderr and returns errStructuredReported, which
// main turns into a non-zero exit with nothing further printed.
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
	enc := json.NewEncoder(stderr)
	enc.SetEscapeHTML(false)
	if encErr := enc.Encode(env); encErr != nil {
		return fmt.Errorf("%s", message)
	}
	return errStructuredReported
}

// errStructuredReported means the command has already written the error
// envelope to stderr.
var errStructuredReported = errors.New("structured error already reported")
