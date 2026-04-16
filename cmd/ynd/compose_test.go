package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func createComposeHarness(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	hj := map[string]any{
		"name":           "compose-test",
		"version":        "1.0.0",
		"description":    "Test harness for compose",
		"default_vendor": "claude",
		"hooks": map[string]any{
			"before_tool": []map[string]any{
				{"command": "echo before", "matcher": "Write"},
			},
		},
		"mcp_servers": map[string]any{
			"test-server": map[string]any{
				"command": "node",
				"args":    []string{"server.js"},
			},
		},
		"profiles": map[string]any{
			"staging": map[string]any{
				"hooks": map[string]any{
					"before_tool": []map[string]any{
						{"command": "echo staging"},
					},
				},
			},
		},
		"focus": map[string]any{
			"quick": map[string]any{
				"prompt": "Be concise",
			},
			"review": map[string]any{
				"profile": "staging",
				"prompt":  "Review mode",
			},
		},
	}
	data, _ := json.MarshalIndent(hj, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Create skills
	for _, name := range []string{"greet", "deploy"} {
		skillDir := filepath.Join(dir, "skills", name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
			[]byte("---\nname: "+name+"\n---\n"+name+" skill.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Create an agent
	agentDir := filepath.Join(dir, "agents")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "reviewer.md"),
		[]byte("---\nname: reviewer\n---\nReview code.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a rule
	ruleDir := filepath.Join(dir, "rules")
	if err := os.MkdirAll(ruleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ruleDir, "no-debug.md"),
		[]byte("No debug logging in production.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestCmdComposeJSONBasic(t *testing.T) {
	srcDir := createComposeHarness(t)

	var stdout bytes.Buffer
	if err := cmdComposeTo([]string{srcDir}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdComposeTo: %v", err)
	}

	var got composeOutput
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, stdout.String())
	}

	if got.Name != "compose-test" {
		t.Errorf("name = %q, want compose-test", got.Name)
	}
	if got.Version != "1.0.0" {
		t.Errorf("version = %q, want 1.0.0", got.Version)
	}
	if got.Description != "Test harness for compose" {
		t.Errorf("description = %q", got.Description)
	}
	if got.DefaultVendor != "claude" {
		t.Errorf("default_vendor = %q, want claude", got.DefaultVendor)
	}

	// Artifacts
	if got.Counts.Skills != 2 {
		t.Errorf("counts.skills = %d, want 2", got.Counts.Skills)
	}
	if got.Counts.Agents != 1 {
		t.Errorf("counts.agents = %d, want 1", got.Counts.Agents)
	}
	if got.Counts.Rules != 1 {
		t.Errorf("counts.rules = %d, want 1", got.Counts.Rules)
	}
	if got.Counts.Commands != 0 {
		t.Errorf("counts.commands = %d, want 0", got.Counts.Commands)
	}

	// Verify artifact details
	if len(got.Artifacts.Skills) != 2 {
		t.Fatalf("artifacts.skills = %d, want 2", len(got.Artifacts.Skills))
	}
	// Skills should be named and sourced from the harness
	for _, s := range got.Artifacts.Skills {
		if s.Source != "compose-test" {
			t.Errorf("skill %q source = %q, want compose-test", s.Name, s.Source)
		}
	}

	// Hooks
	if got.Hooks == nil {
		t.Fatal("hooks is nil")
	}
	bt, ok := got.Hooks["before_tool"]
	if !ok {
		t.Fatal("hooks missing before_tool")
	}
	if len(bt) != 1 || bt[0].Command != "echo before" {
		t.Errorf("hooks before_tool = %+v", bt)
	}
	if bt[0].Matcher != "Write" {
		t.Errorf("hooks before_tool matcher = %q, want Write", bt[0].Matcher)
	}

	// MCP Servers
	if got.MCPServers == nil {
		t.Fatal("mcp_servers is nil")
	}
	srv, ok := got.MCPServers["test-server"]
	if !ok {
		t.Fatal("mcp_servers missing test-server")
	}
	if srv.Command != "node" {
		t.Errorf("mcp command = %q, want node", srv.Command)
	}

	// Profiles
	if len(got.Profiles) != 1 || got.Profiles[0] != "staging" {
		t.Errorf("profiles = %v, want [staging]", got.Profiles)
	}

	// Focuses
	if got.Focuses == nil {
		t.Fatal("focuses is nil")
	}
	if f, ok := got.Focuses["quick"]; !ok || f.Prompt != "Be concise" {
		t.Errorf("focus quick = %+v", got.Focuses["quick"])
	}
	if f, ok := got.Focuses["review"]; !ok || f.Profile != "staging" {
		t.Errorf("focus review = %+v", got.Focuses["review"])
	}

	// Includes and delegates should be empty arrays
	if len(got.Includes) != 0 {
		t.Errorf("includes should be empty, got %d", len(got.Includes))
	}
	if len(got.DelegatesTo) != 0 {
		t.Errorf("delegates_to should be empty, got %d", len(got.DelegatesTo))
	}

	// Output must end with a newline
	if !strings.HasSuffix(stdout.String(), "\n") {
		t.Error("JSON output does not end with a newline")
	}
}

func TestCmdComposeJSONEmptyArrays(t *testing.T) {
	dir := t.TempDir()

	hj := `{"name": "bare", "version": "0.1.0", "default_vendor": "claude"}`
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), []byte(hj), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := cmdComposeTo([]string{dir}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdComposeTo: %v", err)
	}

	out := stdout.String()
	// Artifacts arrays should be []
	if !strings.Contains(out, `"skills": []`) {
		t.Errorf("expected skills: [], got: %s", out)
	}
	if !strings.Contains(out, `"agents": []`) {
		t.Errorf("expected agents: [], got: %s", out)
	}
	// Includes and delegates should be []
	if !strings.Contains(out, `"includes": []`) {
		t.Errorf("expected includes: [], got: %s", out)
	}
	if !strings.Contains(out, `"delegates_to": []`) {
		t.Errorf("expected delegates_to: [], got: %s", out)
	}
	// Profiles should be []
	if !strings.Contains(out, `"profiles": []`) {
		t.Errorf("expected profiles: [], got: %s", out)
	}
}

func TestCmdComposeTextFormat(t *testing.T) {
	srcDir := createComposeHarness(t)

	var stdout bytes.Buffer
	if err := cmdComposeTo([]string{srcDir, "--format", "text"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdComposeTo: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "compose-test") {
		t.Errorf("expected harness name in output")
	}
	if !strings.Contains(out, "Artifacts") {
		t.Errorf("expected Artifacts section")
	}
	if !strings.Contains(out, "greet") {
		t.Errorf("expected skill name in output")
	}
}

func TestCmdComposeNoArgs(t *testing.T) {
	err := cmdComposeTo(nil, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error for no args")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdComposeUnknownFlag(t *testing.T) {
	err := cmdComposeTo([]string{"--nope"}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestCmdComposeInvalidFormat(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"),
		[]byte(`{"name":"test","version":"0.1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := cmdComposeTo([]string{dir, "--format", "yaml"}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
	if !strings.Contains(err.Error(), "yaml") {
		t.Errorf("error should mention yaml, got: %v", err)
	}
}

func TestCmdComposeNoDescription(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"),
		[]byte(`{"name":"bare","version":"0.1.0","default_vendor":"claude"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := cmdComposeTo([]string{dir}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdComposeTo: %v", err)
	}

	if strings.Contains(stdout.String(), `"description"`) {
		t.Errorf("empty description should be omitted, got: %s", stdout.String())
	}
}

func TestCmdComposeWithDelegates(t *testing.T) {
	dir := t.TempDir()

	hj := map[string]any{
		"name":           "with-delegates",
		"version":        "1.0.0",
		"default_vendor": "claude",
		"delegates_to": []map[string]any{
			{"git": "github.com/eyelock/helper", "path": "harnesses/helper"},
		},
	}
	data, _ := json.MarshalIndent(hj, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := cmdComposeTo([]string{dir}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdComposeTo: %v", err)
	}

	var got composeOutput
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(got.DelegatesTo) != 1 {
		t.Fatalf("delegates_to = %d, want 1", len(got.DelegatesTo))
	}
	if got.DelegatesTo[0].Git != "github.com/eyelock/helper" {
		t.Errorf("delegate git = %q", got.DelegatesTo[0].Git)
	}
	if got.DelegatesTo[0].Path != "harnesses/helper" {
		t.Errorf("delegate path = %q", got.DelegatesTo[0].Path)
	}
}
