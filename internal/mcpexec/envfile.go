// Package mcpexec keeps secrets off the command line of an MCP server.
//
// Claude Code does not expand ${VAR} in the servers of a subagent given with
// --agents, so ynh would have to put the expanded values in that argument,
// where any local process can read them with ps. Instead ynh writes the
// values to a file in the run directory (EnvFileName), leaves ${VAR} literal
// in the server definition, and starts the server through `ynh mcp-exec`,
// which reads the file and expands the references in its own process.
package mcpexec

import (
	"bytes"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// EnvFileName is the name of the per-delegate secrets file. It matches the
// common .env.* ignore patterns, and ynh only ever writes it under a run
// directory.
const EnvFileName = ".env.ynh"

var varName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Encode renders vars as an env file: one NAME="value" line per variable,
// sorted by name. The value is a double-quoted string with Go escapes
// (\n, \", \\, \xNN, \u...), so any value, including one with =, quotes,
// newlines or arbitrary bytes, sits on one line and Parse returns it exactly.
// A name that is not a valid variable name is an error.
func Encode(vars map[string]string) ([]byte, error) {
	var b bytes.Buffer
	for _, name := range slices.Sorted(maps.Keys(vars)) {
		if !varName.MatchString(name) {
			return nil, fmt.Errorf("invalid variable name %q", name)
		}
		b.WriteString(name)
		b.WriteByte('=')
		b.WriteString(strconv.Quote(vars[name]))
		b.WriteByte('\n')
	}
	return b.Bytes(), nil
}

// Parse reads what Encode writes. Blank lines and lines starting with # are
// skipped. A value that is not a double-quoted string, a bad name, or a name
// that appears twice is an error naming the line.
func Parse(data []byte) (map[string]string, error) {
	vars := make(map[string]string)
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		name, quoted, ok := strings.Cut(line, "=")
		if !ok || !varName.MatchString(name) {
			return nil, fmt.Errorf("line %d: want NAME=\"value\"", i+1)
		}
		value, err := strconv.Unquote(quoted)
		if err != nil || !strings.HasPrefix(quoted, `"`) {
			return nil, fmt.Errorf("line %d: the value of %s is not a double-quoted string", i+1, name)
		}
		if _, dup := vars[name]; dup {
			return nil, fmt.Errorf("line %d: %s is set twice", i+1, name)
		}
		vars[name] = value
	}
	return vars, nil
}
