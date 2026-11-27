package vendor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ClaudeOwnMCPAllowRule is the Claude Code flag that allows every tool of the
// MCP servers the harness called name declares itself (its own and those of
// its included harnesses, which are under the same trust), and nothing else:
// not the user's servers, not ynh-delegates. It is "" when there are none.
// Claude Code names the tools mcp__plugin_<harness>_<server>__<tool>, with
// any character of a name outside letters, digits, _ and - written as _; a rule
// naming the server allows all of its tools.
func ClaudeOwnMCPAllowRule(name string, servers []string) string {
	var rules []string
	for _, s := range servers {
		rules = append(rules, "mcp__plugin_"+claudeMCPName(name)+"_"+claudeMCPName(s))
	}
	if len(rules) == 0 {
		return ""
	}
	return "--allowedTools=" + strings.Join(rules, ",")
}

func claudeMCPName(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, s)
}

// ClaudeOwnMCPServers lists, sorted, the servers the harness declares in the
// assembled directory configPath (.claude/.mcp.json), leaving out the two ynh
// adds itself, ynh-delegates and ynh-parent, which are not the harness's own.
func ClaudeOwnMCPServers(configPath string) []string {
	data, err := os.ReadFile(filepath.Join(configPath, ".claude", ".mcp.json"))
	if err != nil {
		return nil
	}
	var doc struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return nil
	}
	var names []string
	for n := range doc.MCPServers {
		if n == "ynh-delegates" || n == "ynh-parent" {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
