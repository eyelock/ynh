package vendor

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const codexApprovalKey = "default_tools_approval_mode"

// ApproveMCPTools approves, for one one-shot Codex run, the tools of every MCP
// server in runDir's .mcp.json (the harness's own declaration, which
// CodexMCP loads): codex exec has no one to ask, and its approval policy is
// "never", so an unapproved MCP call fails with "MCP tool call requires
// approval, but approval policy is never". It sets, per server,
//
//	-c mcp_servers.<name>.default_tools_approval_mode="approve"
//
// the server's default for tools without a setting of their own, so a user's
// per-tool mcp_servers.<name>.tools.<tool>.approval_mode still applies. It
// touches no server ynh did not load (the user's own servers keep Codex's
// policy) and leaves a server alone whose default_tools_approval_mode the
// user's config.toml or the caller's own -c already sets, so their choice is
// merged with, not replaced.
func (c *Codex) ApproveMCPTools(runDir string, callerArgs []string, warn io.Writer) []string {
	return codexApproveMCPTools(runDir, callerArgs, codexUserConfigPath(), warn)
}

func codexApproveMCPTools(runDir string, callerArgs []string, userConfig string, warn io.Writer) []string {
	servers := CodexMCP(runDir).Servers
	if len(servers) == 0 {
		return nil
	}
	var cfg string
	if userConfig != "" {
		if data, err := os.ReadFile(userConfig); err == nil {
			cfg = string(data)
		}
	}
	var out []string
	for _, name := range servers {
		key := "mcp_servers." + name + "." + codexApprovalKey
		if codexCallerSetsKey(callerArgs, key) || codexConfigSetsApproval(cfg, name) {
			if warn != nil {
				_, _ = fmt.Fprintf(warn, "ynh: MCP server %q: keeping your %s, so ynh does not approve its tools\n", name, codexApprovalKey)
			}
			continue
		}
		out = append(out, "-c", key+"="+tomlString("approve"))
	}
	return out
}

// codexCallerSetsKey reports whether the caller's args carry a -c override of key.
func codexCallerSetsKey(args []string, key string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-c" || args[i] == "--config" {
			if strings.HasPrefix(strings.ReplaceAll(args[i+1], " ", ""), key+"=") {
				return true
			}
		}
	}
	return false
}

// codexConfigSetsApproval reports whether a config.toml sets
// default_tools_approval_mode for server name, in [mcp_servers.<name>] or as a
// dotted key.
func codexConfigSetsApproval(cfg, name string) bool {
	inTable := false
	for _, line := range strings.Split(cfg, "\n") {
		line = strings.TrimSpace(stripTOMLComment(line))
		if strings.HasPrefix(line, "[") {
			t := strings.ReplaceAll(strings.Trim(line, "[] "), `"`, "")
			inTable = t == "mcp_servers."+name
			continue
		}
		k, _, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.ReplaceAll(strings.TrimSpace(k), `"`, "")
		if (inTable && k == codexApprovalKey) || k == "mcp_servers."+name+"."+codexApprovalKey {
			return true
		}
	}
	return false
}
