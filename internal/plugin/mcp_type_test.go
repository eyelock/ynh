package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPServer_Transport(t *testing.T) {
	for name, tc := range map[string]struct {
		s    MCPServer
		want string
	}{
		"command implies stdio":     {MCPServer{Command: "npx"}, MCPTypeStdio},
		"url implies streamable":    {MCPServer{URL: "https://x/mcp"}, MCPTypeStreamableHTTP},
		"declared sse wins":         {MCPServer{Type: MCPTypeSSE, URL: "https://x/sse"}, MCPTypeSSE},
		"declared stdio wins":       {MCPServer{Type: MCPTypeStdio, Command: "x"}, MCPTypeStdio},
		"empty server reads stdio":  {MCPServer{}, MCPTypeStdio},
		"declared but invalid kept": {MCPServer{Type: "ws", URL: "wss://x"}, "ws"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := tc.s.Transport(); got != tc.want {
				t.Errorf("Transport() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestValidateMCPServers_Type(t *testing.T) {
	for name, tc := range map[string]struct {
		s    MCPServer
		want string // substring of the single issue, or "" for none
	}{
		"stdio with command":    {MCPServer{Type: MCPTypeStdio, Command: "x"}, ""},
		"http with url":         {MCPServer{Type: MCPTypeStreamableHTTP, URL: "https://x"}, ""},
		"sse with url":          {MCPServer{Type: MCPTypeSSE, URL: "https://x"}, ""},
		"stdio without command": {MCPServer{Type: MCPTypeStdio, URL: "https://x"}, "type stdio requires command"},
		"http without url":      {MCPServer{Type: MCPTypeStreamableHTTP, Command: "x"}, "type streamable-http requires url"},
		"sse without url":       {MCPServer{Type: MCPTypeSSE, Command: "x"}, "type sse requires url"},
		"unknown type":          {MCPServer{Type: "local", Command: "x"}, `unknown type "local"`},
	} {
		t.Run(name, func(t *testing.T) {
			issues := ValidateMCPServers(map[string]MCPServer{"s": tc.s})
			if tc.want == "" {
				if len(issues) != 0 {
					t.Errorf("issues = %v, want none", issues)
				}
				return
			}
			if len(issues) != 1 || !strings.Contains(issues[0], tc.want) {
				t.Errorf("issues = %v, want one containing %q", issues, tc.want)
			}
		})
	}
}

func TestValidateMCPServers_SortedByName(t *testing.T) {
	issues := ValidateMCPServers(map[string]MCPServer{"zeta": {}, "alpha": {}})
	if len(issues) != 2 || !strings.HasPrefix(issues[0], "mcp_servers.alpha") {
		t.Errorf("issues = %v, want alpha first", issues)
	}
}

func TestLoadMCPJSON_ClaudeTypeSpelling(t *testing.T) {
	dir := t.TempDir()
	data := `{"mcpServers":{
	  "remote":{"type":"http","url":"https://x/mcp"},
	  "legacy":{"type":"sse","url":"https://x/sse"},
	  "local":{"type":"stdio","command":"x"},
	  "untyped":{"command":"y"}}}`
	if err := os.WriteFile(filepath.Join(dir, MCPJSONFile), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	servers, err := LoadMCPJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"remote": MCPTypeStreamableHTTP, "legacy": MCPTypeSSE, "local": MCPTypeStdio, "untyped": ""} {
		if got := servers[name].Type; got != want {
			t.Errorf("%s: type = %q, want %q", name, got, want)
		}
	}
}

func TestLoadMCPJSON_UnsupportedTypeRefused(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, MCPJSONFile), []byte(`{"mcpServers":{"ws":{"type":"ws","url":"wss://x"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMCPJSON(dir); err == nil || !strings.Contains(err.Error(), `unsupported type "ws"`) {
		t.Errorf("err = %v, want unsupported type", err)
	}
}
