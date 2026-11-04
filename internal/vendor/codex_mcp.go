package vendor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CodexMCPArgs returns the config overrides that load, for one launch, every
// MCP server in runDir's .mcp.json, the file Codex's GenerateMCPConfig writes.
// Codex reads no MCP file from a session's working directory (only
// mcp_servers in its config.toml layers, and a project's .codex/ layer only
// when trusted), so the root .mcp.json alone leaves a harness's servers
// unloaded. For each server it passes
//
//	-c mcp_servers.<name>.command="..." -c mcp_servers.<name>.args=[...]
//	-c mcp_servers.<name>.env={K="v"}   -c mcp_servers.<name>.cwd="..."
//	-c mcp_servers.<name>.url="..."     -c mcp_servers.<name>.http_headers={K="v"}
//
// The -c layer is always enabled, as for agents (CodexAgentArgs), and nothing
// is written to the user's Codex config. A name that cannot be a -c key is not
// loaded and a warning says so. A missing or unreadable file loads nothing.
func CodexMCPArgs(runDir string) (args []string, warnings []string) {
	if runDir == "" {
		return nil, nil
	}
	file := filepath.Join(runDir, ".mcp.json")
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, nil
	}
	var doc struct {
		MCPServers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
			Cwd     string            `json:"cwd"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, []string{fmt.Sprintf("Codex MCP file %s: %v", file, err)}
	}
	names := make([]string, 0, len(doc.MCPServers))
	for n := range doc.MCPServers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		if !codexAgentNameKey.MatchString(name) {
			warnings = append(warnings, fmt.Sprintf("MCP server %q: Codex cannot load it, since its name must use only letters, digits, '-' and '_' to be passed with -c (a '.' would split the key)", name))
			continue
		}
		s := doc.MCPServers[name]
		key := "mcp_servers." + name + "."
		add := func(k, v string) { args = append(args, "-c", key+k+"="+v) }
		if s.Command != "" {
			add("command", tomlString(s.Command))
			if len(s.Args) > 0 {
				add("args", tomlStringArray(s.Args))
			}
			if s.Cwd != "" {
				add("cwd", tomlString(s.Cwd))
			}
			if len(s.Env) > 0 {
				add("env", tomlInlineTable(s.Env))
			}
		} else if s.URL != "" {
			add("url", tomlString(s.URL))
			if len(s.Headers) > 0 {
				add("http_headers", tomlInlineTable(s.Headers))
			}
		}
	}
	return args, warnings
}

func tomlStringArray(ss []string) string {
	parts := make([]string, len(ss))
	for i, s := range ss {
		parts[i] = tomlString(s)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// tomlInlineTable encodes m as an inline table with every key quoted, so a
// key with a '-' or '.' stays one key.
func tomlInlineTable(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = tomlString(k) + " = " + tomlString(m[k])
	}
	return "{" + strings.Join(parts, ", ") + "}"
}
