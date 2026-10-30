package vendor

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/eyelock/ynh/internal/plugin"
)

// mcpPluginRootVar names, per vendor, the variable its plugin loader expands
// to the plugin's install path in an MCP server's command and cwd. Only a
// vendor with a documented or verified variable is listed:
//   - claude: ${CLAUDE_PLUGIN_ROOT} (code.claude.com/docs/en/plugins-reference)
//   - cursor: ${CURSOR_PLUGIN_ROOT} in command, args, env and cwd
//     (cursor.com/docs/reference/plugins)
//   - copilot: ${PLUGIN_ROOT}, set in a stdio MCP server's environment and
//     expanded in its args, env and cwd (docs.github.com, CLI plugin
//     reference); CONFIRMED live to expand in a command, Copilot CLI 1.0.105,
//     2026-10-30.
//
// Codex documents no placeholder for an MCP server, so it is absent: a "./"
// command there is left as written and warned about, not guessed.
var mcpPluginRootVar = map[string]string{
	"claude":  "CLAUDE_PLUGIN_ROOT",
	"cursor":  "CURSOR_PLUGIN_ROOT",
	"copilot": "PLUGIN_ROOT",
}

// AnchorMCPServers rewrites, for a plugin export, each stdio server's "./"
// command and "./" cwd to the vendor's plugin-root placeholder, the way hook
// commands are anchored: a server runs in the user's project, not in the
// plugin, so a bare "./scripts/x.sh" would not find the script the export
// ships. The input is not modified. A vendor with no documented placeholder
// keeps the server as written and gets a warning naming it.
func AnchorMCPServers(vendorName string, servers map[string]plugin.MCPServer) (map[string]plugin.MCPServer, []string) {
	rootVar, known := mcpPluginRootVar[vendorName]
	out := make(map[string]plugin.MCPServer, len(servers))
	var warnings []string
	for _, name := range slices.Sorted(maps.Keys(servers)) {
		s := servers[name]
		if s.Transport() == plugin.MCPTypeStdio && (strings.HasPrefix(s.Command, "./") || strings.HasPrefix(s.Cwd, "./")) {
			if !known {
				warnings = append(warnings, fmt.Sprintf("mcp server %q runs a ./ path, which %s documents no plugin-root placeholder for, so it is written as is and will not find the script", name, vendorName))
			} else {
				if rest, ok := strings.CutPrefix(s.Command, "./"); ok {
					s.Command = "${" + rootVar + "}/" + rest
				}
				if rest, ok := strings.CutPrefix(s.Cwd, "./"); ok {
					s.Cwd = "${" + rootVar + "}/" + rest
				}
			}
		}
		out[name] = s
	}
	return out, warnings
}
