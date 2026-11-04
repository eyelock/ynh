package vendor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eyelock/ynh/internal/mcpexec"
)

// CodexMCPLaunch is what loading a run directory's MCP servers on Codex takes:
// the -c overrides, the environment variables the Codex process needs, and
// the warnings to print.
type CodexMCPLaunch struct {
	Args     []string
	Env      []string // NAME=value entries added to the Codex process's environment
	Warnings []string
}

// CodexMCP returns what loads, for one launch, every MCP server in runDir's
// .mcp.json, the file Codex's GenerateMCPConfig writes. Codex reads no MCP
// file from a session's working directory (only mcp_servers in its config.toml
// layers, and a project's .codex/ layer only when trusted), so the root
// .mcp.json alone leaves a harness's servers unloaded. For each server it
// passes
//
//	-c mcp_servers.<name>.command="..." -c mcp_servers.<name>.args=[...]
//	-c mcp_servers.<name>.env={K="${V}"} -c mcp_servers.<name>.cwd="..."
//	-c mcp_servers.<name>.url="..."
//	-c mcp_servers.<name>.env_http_headers={Name="VAR"}
//
// The -c layer is always enabled, as for agents (CodexAgentArgs), and nothing
// is written to the user's Codex config.
//
// A command line is readable by any local process (ps), and the file holds
// the values of the harness's ${VAR} references already expanded, so no value
// of a server's env or headers goes on the command line:
//
//   - A stdio server with an env is started through `ynh mcp-exec`, with its
//     env values left as ${YNH_MCP_n} references that the launcher expands
//     from a 0600 file in runDir (mcpexec.EnvFileName), as for a Claude
//     delegate's servers.
//   - A remote server's headers are passed with env_http_headers, which names
//     the environment variable of the Codex process that holds each value; the
//     values are returned in Env, under names (YNH_MCP_HEADER_SECRET_n) that
//     Codex's default excludes match, and
//     shell_environment_policy.ignore_default_excludes=false keeps them out of
//     the environment of Codex's shell tool.
//
// Secrets are numbered in sorted server-name and key order, so the same file
// gives the same numbering on every call (each call re-reads the file).
//
// Codex has no legacy HTTP+SSE transport, only stdio and streamable HTTP, so
// a server declared "type": "sse" is not loaded and a warning says so. A name
// that cannot be a -c key is not loaded either. A missing or unreadable file
// loads nothing.
func CodexMCP(runDir string) CodexMCPLaunch {
	var out CodexMCPLaunch
	if runDir == "" {
		return out
	}
	file := filepath.Join(runDir, ".mcp.json")
	data, err := os.ReadFile(file)
	if err != nil {
		return out
	}
	var doc struct {
		MCPServers map[string]struct {
			Type    string            `json:"type"`
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
			Cwd     string            `json:"cwd"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		out.Warnings = []string{fmt.Sprintf("Codex MCP file %s: %v", file, err)}
		return out
	}
	names := make([]string, 0, len(doc.MCPServers))
	for n := range doc.MCPServers {
		names = append(names, n)
	}
	sort.Strings(names)
	absDir := mustAbs(runDir)
	secrets := map[string]string{}
	n := 0
	for _, name := range names {
		if !codexAgentNameKey.MatchString(name) {
			out.Warnings = append(out.Warnings, fmt.Sprintf("MCP server %q: Codex cannot load it, since its name must use only letters, digits, '-' and '_' to be passed with -c (a '.' would split the key)", name))
			continue
		}
		s := doc.MCPServers[name]
		if s.Type == "sse" {
			out.Warnings = append(out.Warnings, fmt.Sprintf("MCP server %q: Codex cannot load it, since it speaks the legacy HTTP+SSE transport and Codex supports only stdio and streamable HTTP servers", name))
			continue
		}
		key := "mcp_servers." + name + "."
		add := func(k, v string) { out.Args = append(out.Args, "-c", key+k+"="+v) }
		if s.Command != "" {
			command, args := s.Command, s.Args
			var envTable map[string]string
			if len(s.Env) > 0 {
				if containsRef(append([]string{s.Command}, s.Args...)) {
					out.Warnings = append(out.Warnings, fmt.Sprintf("MCP server %q: Codex cannot load it, since its command or args hold a literal ${...} that the launcher keeping its env off the command line would try to expand", name))
					continue
				}
				command, args = mcpexec.Launcher, append([]string{"mcp-exec", "--env-file", filepath.Join(absDir, mcpexec.EnvFileName), "--", s.Command}, s.Args...)
				envTable = make(map[string]string, len(s.Env))
				for _, k := range sortedStringKeys(s.Env) {
					v := s.Env[k]
					ref := fmt.Sprintf("YNH_MCP_%d", n)
					n++
					secrets[ref] = v
					envTable[k] = "${" + ref + "}"
				}
			}
			add("command", tomlString(command))
			if len(args) > 0 {
				add("args", tomlStringArray(args))
			}
			if s.Cwd != "" {
				add("cwd", tomlString(s.Cwd))
			}
			if envTable != nil {
				add("env", tomlInlineTable(envTable))
			}
		} else if s.URL != "" {
			add("url", tomlString(s.URL))
			if len(s.Headers) > 0 {
				table := make(map[string]string, len(s.Headers))
				for _, k := range sortedStringKeys(s.Headers) {
					v := s.Headers[k]
					ref := fmt.Sprintf("YNH_MCP_HEADER_SECRET_%d", n)
					n++
					out.Env = append(out.Env, ref+"="+v)
					table[k] = ref
				}
				add("env_http_headers", tomlInlineTable(table))
			}
		}
	}
	if len(out.Env) > 0 {
		// Codex's shell tool inherits the Codex process's environment, so the
		// header values would be readable by the model. Codex's default policy
		// drops names containing KEY, SECRET or TOKEN, but only when
		// ignore_default_excludes is false, and it is true unless the user
		// sets it. Checked against codex-cli 0.166.0: renaming the variables
		// alone left YNH_MCP_HEADER_SECRET_0 in `env`. Setting exclude would
		// replace the user's own list, so this sets only that flag: the
		// user's exclude stays, and the default excludes (which also drop the
		// user's own *KEY*, *SECRET* and *TOKEN* variables) apply for the run.
		out.Args = append(out.Args, "-c", "shell_environment_policy.ignore_default_excludes=false")
	}
	if len(secrets) > 0 {
		if err := writeMCPEnvFile(absDir, secrets); err != nil {
			// Without the file the wrapped servers cannot start; load none of
			// the stdio servers rather than put their values on the command line.
			return CodexMCPLaunch{Warnings: append(out.Warnings, fmt.Sprintf("Codex MCP servers not loaded: writing %s: %v", mcpexec.EnvFileName, err))}
		}
	}
	return out
}

func sortedStringKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func mustAbs(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

func containsRef(ss []string) bool {
	for _, s := range ss {
		if strings.Contains(s, "${") {
			return true
		}
	}
	return false
}

// writeMCPEnvFile writes the secrets to <dir>/.env.ynh, mode 0600.
func writeMCPEnvFile(dir string, secrets map[string]string) error {
	data, err := mcpexec.Encode(secrets)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, mcpexec.EnvFileName)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
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
