package vendor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// MCP isolation on the vendors that have no "only these servers" switch.
//
// Claude Code has one (--strict-mcp-config). Codex, Copilot CLI and OpenCode
// have none, but each lets a launch turn off a server by name and can list
// the servers it has configured, so ynh asks the vendor which servers it
// would load, keeps the ones the harness composes, and turns off the rest for
// this launch only. Nothing in the user's own config is written. Cursor has
// neither a flag nor a per-launch disable (Cursor.IsolateMCP).
//
// What is turned off is what the vendor's own listing reports from the
// directory ynh runs in: the user's config, the project's, and (Copilot)
// installed plugins and built-ins. A server the listing does not report, or
// one the harness names the same as the user's, is left as it is.

// MCPEnvIsolator is implemented by an adapter whose isolation is environment
// variables rather than launch arguments (OpenCode). `ynh run` sets the
// returned "NAME=value" pairs in its own environment before it launches, and
// the vendor inherits them. It replaces IsolateMCP for that adapter; the
// warning has the same meaning.
type MCPEnvIsolator interface {
	IsolateMCPEnv(configPath string) (env []string, warning string)
}

// mcpListTimeout bounds a vendor's server listing, which a wedged install
// could otherwise hang forever. A variable so a test can shorten it.
var mcpListTimeout = 20 * time.Second

// mcpListRunner runs a vendor CLI and returns its standard output. A variable
// so tests never run the real binary. env, when non-nil, replaces the
// process environment.
var mcpListRunner = func(bin string, args []string, env []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mcpListTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	if env != nil {
		cmd.Env = env
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// jsonFrom returns data from its first '{' or '[' on, so a vendor's banner or
// update notice ahead of the JSON does not break parsing.
func jsonFrom(data []byte) []byte {
	if i := bytes.IndexAny(data, "{["); i > 0 {
		return data[i:]
	}
	return data
}

// harnessMCPNames returns the names of the servers in a {"mcpServers": {...}}
// document at path, or in the OpenCode shape {"mcp": {"servers": {...}}} when
// opencode is true. A missing or unreadable file has none.
func harnessMCPNames(path string, opencode bool) map[string]bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	names := map[string]bool{}
	if opencode {
		var doc struct {
			MCP struct {
				Servers map[string]json.RawMessage `json:"servers"`
			} `json:"mcp"`
		}
		if json.Unmarshal(data, &doc) == nil {
			for n := range doc.MCP.Servers {
				names[n] = true
			}
		}
		return names
	}
	var doc struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(data, &doc) == nil {
		for n := range doc.MCPServers {
			names[n] = true
		}
	}
	return names
}

// codexMCPListNames returns the servers `codex mcp list --json` reports.
func codexMCPListNames(bin string) ([]string, error) {
	out, err := mcpListRunner(bin, []string{"mcp", "list", "--json"}, nil)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(jsonFrom(out), &rows); err != nil {
		return nil, fmt.Errorf("reading `codex mcp list --json`: %w", err)
	}
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.Name)
	}
	return names, nil
}

// copilotMCPListNames returns the servers `copilot mcp list --json` reports:
// the user's, the workspace's, installed plugins' and the built-ins.
func copilotMCPListNames(bin string) ([]string, error) {
	out, err := mcpListRunner(bin, []string{"mcp", "list", "--json"}, nil)
	if err != nil {
		return nil, err
	}
	var doc struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(jsonFrom(out), &doc); err != nil {
		return nil, fmt.Errorf("reading `copilot mcp list --json`: %w", err)
	}
	names := make([]string, 0, len(doc.MCPServers))
	for n := range doc.MCPServers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

// codexIsolationArgs turns off, for one launch, every server Codex lists that
// the harness does not compose: -c mcp_servers.<name>.enabled=false. A name
// -c cannot carry (one with a '.') is reported rather than skipped silently.
func codexIsolationArgs(listed []string, keep map[string]bool) (args []string, skipped []string) {
	for _, n := range listed {
		if keep[n] {
			continue
		}
		if !codexAgentNameKey.MatchString(n) {
			skipped = append(skipped, n)
			continue
		}
		args = append(args, "-c", "mcp_servers."+n+".enabled=false")
	}
	return args, skipped
}

// copilotIsolationArgs turns off, for one launch, every server Copilot lists
// that the harness does not compose, and the built-ins as a group.
func copilotIsolationArgs(listed []string, keep map[string]bool) []string {
	args := []string{"--disable-builtin-mcps"}
	for _, n := range listed {
		if !keep[n] {
			args = append(args, "--disable-mcp-server", n)
		}
	}
	return args
}

// opencodeListedServers returns the servers `opencode debug config` reports,
// merged in the order it lists its sources (later wins), each as the raw
// object so a disabled copy can repeat every field OpenCode requires.
func opencodeListedServers(bin string) (map[string]map[string]any, error) {
	env := make([]string, 0, len(os.Environ()))
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, opencodeConfigDirEnv+"=") {
			env = append(env, e)
		}
	}
	out, err := mcpListRunner(bin, []string{"--standalone", "debug", "config"}, env)
	if err != nil {
		return nil, err
	}
	var sources []struct {
		Info struct {
			MCP struct {
				Servers map[string]map[string]any `json:"servers"`
			} `json:"mcp"`
		} `json:"info"`
	}
	if err := json.Unmarshal(jsonFrom(out), &sources); err != nil {
		return nil, fmt.Errorf("reading `opencode debug config`: %w", err)
	}
	merged := map[string]map[string]any{}
	for _, s := range sources {
		for n, srv := range s.Info.MCP.Servers {
			merged[n] = srv
		}
	}
	return merged, nil
}

// opencodeIsolationContent builds the OPENCODE_CONFIG_CONTENT that turns off,
// for one launch, every listed server the harness does not compose. OpenCode
// replaces a server's whole object when a higher-precedence source repeats
// its name, so each copy repeats the server's own fields and adds
// "disabled": true. existing is the caller's own OPENCODE_CONFIG_CONTENT, if
// any, which is kept.
func opencodeIsolationContent(listed map[string]map[string]any, keep map[string]bool, existing string) (string, error) {
	doc := map[string]any{}
	if strings.TrimSpace(existing) != "" {
		if err := json.Unmarshal([]byte(existing), &doc); err != nil {
			return "", fmt.Errorf("your OPENCODE_CONFIG_CONTENT is not a JSON object: %w", err)
		}
	}
	mcp, _ := doc["mcp"].(map[string]any)
	if mcp == nil {
		mcp = map[string]any{}
	}
	servers, _ := mcp["servers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	for n, srv := range listed {
		if keep[n] {
			continue
		}
		cp := make(map[string]any, len(srv)+1)
		for k, v := range srv {
			cp[k] = v
		}
		cp["disabled"] = true
		servers[n] = cp
	}
	mcp["servers"] = servers
	doc["mcp"] = mcp
	data, err := json.Marshal(doc)
	return string(data), err
}

// IsolateMCPEnv turns off, for one launch, the servers OpenCode would load
// besides the harness's: OPENCODE_CONFIG_CONTENT is the highest-precedence
// config source a launch can set, and it needs no write to the user's config.
func (o *OpenCode) IsolateMCPEnv(configPath string) ([]string, string) {
	const fail = "MCP isolation could not be applied on OpenCode (%v): servers in your OpenCode config will still load"
	bin, err := exec.LookPath(opencodeCLI)
	if err != nil {
		return nil, fmt.Sprintf(fail, err)
	}
	listed, err := opencodeListedServers(bin)
	if err != nil {
		return nil, fmt.Sprintf(fail, err)
	}
	keep := harnessMCPNames(filepath.Join(configPath, ".opencode", "opencode.json"), true)
	content, err := opencodeIsolationContent(listed, keep, os.Getenv("OPENCODE_CONFIG_CONTENT"))
	if err != nil {
		return nil, fmt.Sprintf(fail, err)
	}
	return []string{"OPENCODE_CONFIG_CONTENT=" + content}, ""
}
