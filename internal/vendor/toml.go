package vendor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eyelock/ynh/internal/plugin"
)

// TODO: replace with encoding/toml if it enters stdlib or if Codex config grows beyond flat tables

// renderMCPTOML produces a minimal TOML representation of MCP server declarations
// for the Codex config.toml format. It only handles the flat table structure
// needed for MCP servers (string values, string arrays, and sub-tables for env/headers).
func renderMCPTOML(servers map[string]plugin.MCPServer) string {
	if len(servers) == 0 {
		return ""
	}

	var b strings.Builder

	// Sort server names for deterministic output
	var names []string
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)

	for i, name := range names {
		server := servers[name]

		if i > 0 {
			b.WriteString("\n")
		}

		// Main server section
		fmt.Fprintf(&b, "[mcp_servers.%s]\n", name)

		if server.Command != "" {
			fmt.Fprintf(&b, "command = %s\n", tomlQuote(server.Command))
		}
		if server.URL != "" {
			fmt.Fprintf(&b, "url = %s\n", tomlQuote(server.URL))
		}
		if len(server.Args) > 0 {
			fmt.Fprintf(&b, "args = %s\n", tomlStringArray(server.Args))
		}

		// Env sub-table
		if len(server.Env) > 0 {
			fmt.Fprintf(&b, "\n[mcp_servers.%s.env]\n", name)
			writeTomlMap(&b, server.Env)
		}

		// Headers sub-table
		if len(server.Headers) > 0 {
			fmt.Fprintf(&b, "\n[mcp_servers.%s.headers]\n", name)
			writeTomlMap(&b, server.Headers)
		}
	}

	return b.String()
}

// tomlQuote wraps a string in double quotes, escaping backslashes and double quotes.
func tomlQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// tomlStringArray formats a Go string slice as a TOML array.
func tomlStringArray(items []string) string {
	var quoted []string
	for _, item := range items {
		quoted = append(quoted, tomlQuote(item))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// writeTomlMap writes sorted key = "value" pairs from a map.
func writeTomlMap(b *strings.Builder, m map[string]string) {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(b, "%s = %s\n", k, tomlQuote(m[k]))
	}
}
