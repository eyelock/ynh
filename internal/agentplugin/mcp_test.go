package agentplugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// specMCP is the mcp.json example from §7.2.1.
const specMCP = `{
  "$schema": "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json",
  "mcpServers": {
    "local-validator": {
      "type": "stdio",
      "command": "./bin/validator",
      "args": ["--data", "${PLUGIN_DATA}/validator"],
      "env": {"CONFIG": "${PLUGIN_ROOT}/config.json"},
      "cwd": "${PLUGIN_ROOT}"
    },
    "deployment-api": {
      "type": "streamable-http",
      "url": "https://deploy.example.com/mcp",
      "headers": {"X-Tenant": "public-tenant"}
    },
    "legacy-events": {
      "type": "sse",
      "url": "https://legacy.example.com/sse"
    }
  }
}`

func TestParseMCP_SpecExample(t *testing.T) {
	cfg, diags, err := ParseMCP(t.TempDir(), []byte(specMCP))
	if err != nil {
		t.Fatalf("ParseMCP: %v", err)
	}
	if len(diags) != 0 {
		t.Errorf("diags = %v", diags)
	}
	if len(cfg.Servers) != 3 {
		t.Fatalf("servers = %v, want 3", cfg.Servers)
	}
	if s := cfg.Servers["local-validator"]; s.Type != TransportStdio || s.Cwd != PlaceholderRoot || s.Args[1] != "${PLUGIN_DATA}/validator" {
		t.Errorf("local-validator = %+v", s)
	}
	if s := cfg.Servers["legacy-events"]; s.Type != TransportSSE {
		t.Errorf("legacy-events = %+v", s)
	}
}

func TestParseMCP_TopLevelFailures(t *testing.T) {
	for name, data := range map[string]string{
		"not json":            `nope`,
		"array":               `[]`,
		"missing $schema":     `{"mcpServers":{}}`,
		"wrong version":       `{"$schema":"https://agent-plugins.org/schemas/1.1.0/mcp.schema.json","mcpServers":{}}`,
		"plugin schema id":    `{"$schema":"` + PluginSchemaID + `","mcpServers":{}}`,
		"missing mcpServers":  `{"$schema":"` + MCPSchemaID + `"}`,
		"extra top-level":     `{"$schema":"` + MCPSchemaID + `","mcpServers":{},"version":"1"}`,
		"mcpServers not obj":  `{"$schema":"` + MCPSchemaID + `","mcpServers":[]}`,
		"mcpServers is null":  `{"$schema":"` + MCPSchemaID + `","mcpServers":null}`,
		"mcpServers a string": `{"$schema":"` + MCPSchemaID + `","mcpServers":"x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := ParseMCP(t.TempDir(), []byte(data)); err == nil {
				t.Errorf("expected top-level error for %s", data)
			}
		})
	}
	if cfg, _, err := ParseMCP(t.TempDir(), []byte(`{"$schema":"`+MCPSchemaID+`","mcpServers":{}}`)); err != nil || len(cfg.Servers) != 0 {
		t.Errorf("empty mcpServers is valid (§7.2.1): cfg=%+v err=%v", cfg, err)
	}
}

// Each entry here is invalid on its own and must be skipped while a valid
// sibling loads (§7.2.2 rule 3).
func TestParseMCP_InvalidEntriesAreSkippedNotFatal(t *testing.T) {
	cases := map[string]string{
		"missing type":           `{"command":"x"}`,
		"unknown type":           `{"type":"websocket","url":"wss://x"}`,
		"stdio without command":  `{"type":"stdio","args":[]}`,
		"stdio with url":         `{"type":"stdio","command":"x","url":"https://x"}`,
		"http without url":       `{"type":"streamable-http"}`,
		"http with command":      `{"type":"streamable-http","url":"https://x/mcp","command":"x"}`,
		"unknown field":          `{"type":"stdio","command":"x","shell":true}`,
		"env sets PLUGIN_ROOT":   `{"type":"stdio","command":"x","env":{"PLUGIN_ROOT":"/tmp"}}`,
		"env sets PLUGIN_DATA":   `{"type":"stdio","command":"x","env":{"PLUGIN_DATA":"/tmp"}}`,
		"env value not string":   `{"type":"stdio","command":"x","env":{"A":1}}`,
		"shell command string":   `{"type":"stdio","command":"node server.js"}`,
		"absolute command":       `{"type":"stdio","command":"/usr/bin/node"}`,
		"relative without ./":    `{"type":"stdio","command":"bin/server"}`,
		"command escapes root":   `{"type":"stdio","command":"../bin/server"}`,
		"dot-slash escapes root": `{"type":"stdio","command":"./../x"}`,
		"placeholder in command": `{"type":"stdio","command":"./${PLUGIN_ROOT}/x"}`,
		"cwd bare relative":      `{"type":"stdio","command":"x","cwd":"data"}`,
		"cwd absolute":           `{"type":"stdio","command":"x","cwd":"/data"}`,
		"cwd escapes root":       `{"type":"stdio","command":"x","cwd":"./../data"}`,
		"cwd root escapes":       `{"type":"stdio","command":"x","cwd":"${PLUGIN_ROOT}/../x"}`,
		"cwd data escapes":       `{"type":"stdio","command":"x","cwd":"${PLUGIN_DATA}/../x"}`,
		"cwd placeholder suffix": `{"type":"stdio","command":"x","cwd":"${PLUGIN_ROOT}x"}`,
		"url relative":           `{"type":"streamable-http","url":"/mcp"}`,
		"url ftp":                `{"type":"streamable-http","url":"ftp://x/mcp"}`,
		"url http non-loopback":  `{"type":"streamable-http","url":"http://deploy.example.com/mcp"}`,
		"url userinfo":           `{"type":"streamable-http","url":"https://user:pw@x/mcp"}`,
		"url fragment":           `{"type":"sse","url":"https://x/mcp#frag"}`,
		"header bad name":        `{"type":"streamable-http","url":"https://x/mcp","headers":{"X Tenant":"a"}}`,
		"header line break":      `{"type":"streamable-http","url":"https://x/mcp","headers":{"X-A":"a\nb"}}`,
		"header dup casing":      `{"type":"streamable-http","url":"https://x/mcp","headers":{"X-A":"1","x-a":"2"}}`,
		"headers not strings":    `{"type":"streamable-http","url":"https://x/mcp","headers":{"X-A":1}}`,
	}
	for name, entry := range cases {
		t.Run(name, func(t *testing.T) {
			data := `{"$schema":"` + MCPSchemaID + `","mcpServers":{"bad":` + entry + `,"good":{"type":"stdio","command":"npx"}}}`
			cfg, diags, err := ParseMCP(t.TempDir(), []byte(data))
			if err != nil {
				t.Fatalf("a bad entry must not fail the file: %v", err)
			}
			if _, ok := cfg.Servers["bad"]; ok {
				t.Errorf("entry %s was accepted", entry)
			}
			if _, ok := cfg.Servers["good"]; !ok {
				t.Error("valid sibling was lost")
			}
			if len(diags) != 1 || !strings.Contains(diags[0].Path, "bad") || !strings.HasPrefix(diags[0].Message, "skipped: ") {
				t.Errorf("diags = %v, want one skip report for bad", diags)
			}
		})
	}
}

func TestParseMCP_ValidVariants(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, entry := range map[string]string{
		"bare command":         `{"type":"stdio","command":"npx","args":["-y","x"]}`,
		"bundled command":      `{"type":"stdio","command":"./bin/server"}`,
		"absent bundled path":  `{"type":"stdio","command":"./bin/not-built-yet"}`,
		"cwd dot":              `{"type":"stdio","command":"x","cwd":"./data"}`,
		"cwd root":             `{"type":"stdio","command":"x","cwd":"${PLUGIN_ROOT}"}`,
		"cwd root sub":         `{"type":"stdio","command":"x","cwd":"${PLUGIN_ROOT}/data"}`,
		"cwd data":             `{"type":"stdio","command":"x","cwd":"${PLUGIN_DATA}"}`,
		"cwd data sub":         `{"type":"stdio","command":"x","cwd":"${PLUGIN_DATA}/cache"}`,
		"placeholders in args": `{"type":"stdio","command":"x","args":["${PLUGIN_ROOT}/a","${PLUGIN_DATA}/b"]}`,
		"http localhost":       `{"type":"streamable-http","url":"http://localhost:3000/mcp"}`,
		"http 127.0.0.1":       `{"type":"streamable-http","url":"http://127.0.0.1/mcp"}`,
		"http ipv6 loopback":   `{"type":"sse","url":"http://[::1]:8080/sse"}`,
		"https with headers":   `{"type":"streamable-http","url":"https://x/mcp","headers":{"X-Tenant":"t","Accept":"application/json"}}`,
		"https with query":     `{"type":"streamable-http","url":"https://x/mcp?v=1"}`,
	} {
		t.Run(name, func(t *testing.T) {
			data := `{"$schema":"` + MCPSchemaID + `","mcpServers":{"s":` + entry + `}}`
			cfg, diags, err := ParseMCP(dir, []byte(data))
			if err != nil {
				t.Fatalf("ParseMCP: %v", err)
			}
			if _, ok := cfg.Servers["s"]; !ok || len(diags) != 0 {
				t.Errorf("entry %s rejected: %v", entry, diags)
			}
		})
	}
}

func TestReadMCP_Boundaries(t *testing.T) {
	t.Run("absent is valid absence", func(t *testing.T) {
		cfg, diags, err := ReadMCP(t.TempDir())
		if cfg != nil || diags != nil || err != nil {
			t.Errorf("got %v %v %v, want nil nil nil", cfg, diags, err)
		}
	})
	t.Run("directory named mcp.json invalidates the component", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, MCPFile), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ReadMCP(dir); err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("bundled command symlinked outside the root is skipped", func(t *testing.T) {
		outside := t.TempDir()
		write(t, outside, "server", "#!/bin/sh\n")
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(outside, "server"), filepath.Join(dir, "bin", "server")); err != nil {
			t.Skip("symlinks unavailable:", err)
		}
		write(t, dir, MCPFile, `{"$schema":"`+MCPSchemaID+`","mcpServers":{"s":{"type":"stdio","command":"./bin/server"}}}`)
		cfg, diags, err := ReadMCP(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(cfg.Servers) != 0 || len(diags) != 1 || !strings.Contains(diags[0].Message, "outside the plugin root") {
			t.Errorf("servers=%v diags=%v", cfg.Servers, diags)
		}
	})
}

func TestReadMCP_SymlinkInsideRootIsFine(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "config/mcp.json", `{"$schema":"`+MCPSchemaID+`","mcpServers":{}}`)
	if err := os.Symlink(filepath.Join(dir, "config", "mcp.json"), filepath.Join(dir, MCPFile)); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	cfg, _, err := ReadMCP(dir)
	if err != nil || cfg == nil {
		t.Errorf("cfg=%v err=%v", cfg, err)
	}
}

func TestIsManifest_NotJSON(t *testing.T) {
	if IsManifest([]byte("not json")) {
		t.Error("garbage reported as a manifest")
	}
}
