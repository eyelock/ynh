package main

import (
	"io"
	"os"

	"github.com/eyelock/ynh/internal/mcpexec"
)

// cmdMCPExec starts an MCP server with its secrets expanded from an env file.
// It is internal: ynh writes the server definitions that call it, and it is
// not listed in `ynh help`. It replaces the process, so the server's stdin and
// stdout are this process's own, the MCP channel; nothing is ever written to
// stdout here.
func cmdMCPExec(args []string) error {
	plan, err := mcpexec.Prepare(args, os.Environ())
	if err != nil {
		return err
	}
	return plan.Exec()
}

// cmdMCPHeaders prints the headers a remote MCP server needs as a JSON
// object, with their secrets expanded from an env file. It is the command
// behind a server's headersHelper, so its stdout is the object and nothing
// else; an error goes to stderr and the exit status.
func cmdMCPHeaders(args []string, stdout io.Writer) error {
	return mcpexec.Headers(args, stdout)
}
