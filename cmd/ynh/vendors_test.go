package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/config"
)

func TestCmdVendorsOutput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YNH_HOME", "")

	var stdout, stderr bytes.Buffer
	if err := cmdVendorsTo(nil, &stdout, &stderr); err != nil {
		t.Fatalf("cmdVendorsTo: %v", err)
	}

	out := stdout.String()

	// Header must include all five columns
	if !strings.Contains(out, "NAME") {
		t.Error("missing NAME header")
	}
	if !strings.Contains(out, "DISPLAY NAME") {
		t.Error("missing DISPLAY NAME header")
	}
	if !strings.Contains(out, "CLI") {
		t.Error("missing CLI header")
	}
	if !strings.Contains(out, "CONFIG DIR") {
		t.Error("missing CONFIG DIR header")
	}
	if !strings.Contains(out, "AVAILABLE") {
		t.Error("missing AVAILABLE header")
	}

	// All three vendors must appear
	if !strings.Contains(out, "claude") {
		t.Error("missing claude vendor")
	}
	if !strings.Contains(out, "codex") {
		t.Error("missing codex vendor")
	}
	if !strings.Contains(out, "cursor") {
		t.Error("missing cursor vendor")
	}

	// Display names must appear
	if !strings.Contains(out, "Claude Code") {
		t.Error("missing Claude Code display name")
	}
	if !strings.Contains(out, "OpenAI Codex") {
		t.Error("missing OpenAI Codex display name")
	}

	// Available column must have true or false for each row
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 4 { // header + 3 vendors
		t.Fatalf("expected at least 4 lines, got %d", len(lines))
	}
	for _, line := range lines[1:] { // skip header
		if !strings.Contains(line, "true") && !strings.Contains(line, "false") {
			t.Errorf("vendor line missing availability: %s", line)
		}
	}
}

func TestCmdVendorsJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YNH_HOME", "")

	var stdout, stderr bytes.Buffer
	if err := cmdVendorsTo([]string{"--format", "json"}, &stdout, &stderr); err != nil {
		t.Fatalf("cmdVendorsTo JSON: %v", err)
	}

	var entries []vendorEntry
	if err := json.Unmarshal(stdout.Bytes(), &entries); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}

	if len(entries) < 3 {
		t.Fatalf("expected at least 3 vendors, got %d", len(entries))
	}

	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = true
		if e.DisplayName == "" {
			t.Errorf("vendor %q missing display_name", e.Name)
		}
		if e.CLI == "" {
			t.Errorf("vendor %q missing cli", e.Name)
		}
		if e.ConfigDir == "" {
			t.Errorf("vendor %q missing config_dir", e.Name)
		}
	}

	if !names["claude"] {
		t.Error("missing claude vendor")
	}
	if !names["codex"] {
		t.Error("missing codex vendor")
	}
	if !names["cursor"] {
		t.Error("missing cursor vendor")
	}
}

func TestCmdVendorsJSON_AvailableIsBool(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YNH_HOME", "")

	var stdout, stderr bytes.Buffer
	if err := cmdVendorsTo([]string{"--format", "json"}, &stdout, &stderr); err != nil {
		t.Fatalf("cmdVendorsTo JSON: %v", err)
	}

	// Parse as generic to verify "available" is a boolean, not a string
	var raw []map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	for _, entry := range raw {
		avail, ok := entry["available"]
		if !ok {
			t.Error("missing available field")
			continue
		}
		if _, isBool := avail.(bool); !isBool {
			t.Errorf("available should be bool, got %T", avail)
		}
	}
}

func TestCmdVendorsJSON_SupportsInitialPrompt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YNH_HOME", "")

	var stdout, stderr bytes.Buffer
	if err := cmdVendorsTo([]string{"--format", "json"}, &stdout, &stderr); err != nil {
		t.Fatalf("cmdVendorsTo JSON: %v", err)
	}

	var raw []map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	for _, entry := range raw {
		val, ok := entry["supports_initial_prompt"]
		if !ok {
			t.Errorf("vendor %q missing supports_initial_prompt field", entry["name"])
			continue
		}
		if _, isBool := val.(bool); !isBool {
			t.Errorf("vendor %q supports_initial_prompt should be bool, got %T", entry["name"], val)
		}
	}

	// All three current vendors support initial prompt.
	byName := map[string]map[string]interface{}{}
	for _, entry := range raw {
		if name, ok := entry["name"].(string); ok {
			byName[name] = entry
		}
	}
	for _, name := range []string{"claude", "codex", "cursor"} {
		e, ok := byName[name]
		if !ok {
			t.Errorf("missing vendor %q", name)
			continue
		}
		if e["supports_initial_prompt"] != true {
			t.Errorf("vendor %q supports_initial_prompt = %v, want true", name, e["supports_initial_prompt"])
		}
	}
}

func TestCmdVendors_InvalidFormat(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YNH_HOME", "")

	var stdout, stderr bytes.Buffer
	err := cmdVendorsTo([]string{"--format", "yaml"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
}

func TestCmdVendorsJSON_BackendRows(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", "")

	cfg := &config.Config{
		Backends: map[string]config.BackendDef{
			"ollama": {
				Vendors: map[string]config.BackendConnection{
					"claude": {BaseURL: "http://localhost:11434", AuthToken: "ollama"},
					"codex":  {BaseURL: "http://localhost:11434/v1/"},
				},
			},
		},
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("saving config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := cmdVendorsTo([]string{"--format", "json"}, &stdout, &stderr); err != nil {
		t.Fatalf("cmdVendorsTo JSON: %v", err)
	}

	var entries []vendorEntry
	if err := json.Unmarshal(stdout.Bytes(), &entries); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}

	byName := map[string]vendorEntry{}
	for _, e := range entries {
		byName[e.Name] = e
	}

	claudeRow, ok := byName["ollama/claude"]
	if !ok {
		t.Fatal("missing backend row \"ollama/claude\"")
	}
	if claudeRow.CLI != "claude" || claudeRow.ConfigDir != ".claude" {
		t.Errorf("ollama/claude row = %+v, want cli=claude config_dir=.claude", claudeRow)
	}

	if _, ok := byName["ollama/codex"]; !ok {
		t.Fatal("missing backend row \"ollama/codex\"")
	}

	// Plain vendor rows still present alongside backend rows.
	if _, ok := byName["claude"]; !ok {
		t.Error("missing plain \"claude\" vendor row")
	}
}

func TestCmdVendorsJSON_BackendRowsExpandToInstalledModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"qwen3:latest"}]}`))
	}))
	defer server.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", "")

	cfg := &config.Config{
		Backends: map[string]config.BackendDef{
			"ollama": {
				Type: "ollama",
				Vendors: map[string]config.BackendConnection{
					"claude": {BaseURL: server.URL},
				},
			},
		},
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("saving config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := cmdVendorsTo([]string{"--format", "json"}, &stdout, &stderr); err != nil {
		t.Fatalf("cmdVendorsTo JSON: %v", err)
	}

	var entries []vendorEntry
	if err := json.Unmarshal(stdout.Bytes(), &entries); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}

	byName := map[string]vendorEntry{}
	for _, e := range entries {
		byName[e.Name] = e
	}

	if _, ok := byName["ollama/claude/qwen3:latest"]; !ok {
		t.Fatalf("missing model-expanded row \"ollama/claude/qwen3:latest\"; got names: %v", names(entries))
	}
	if _, ok := byName["ollama/claude"]; ok {
		t.Error("bare \"ollama/claude\" row should not appear once models were successfully enumerated")
	}
}

func names(entries []vendorEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name
	}
	return out
}
