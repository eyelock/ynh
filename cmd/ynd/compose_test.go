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

func TestCmdComposeWithProfile(t *testing.T) {
	srcDir := createComposeHarness(t)

	// With --profile staging, hooks should be replaced by the profile's hooks
	var stdout bytes.Buffer
	if err := cmdComposeTo([]string{srcDir, "--profile", "staging"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdComposeTo: %v", err)
	}

	var got composeOutput
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Profile merges hooks: staging replaces before_tool
	bt, ok := got.Hooks["before_tool"]
	if !ok {
		t.Fatal("hooks missing before_tool after profile merge")
	}
	if len(bt) != 1 || bt[0].Command != "echo staging" {
		t.Errorf("profile should replace before_tool hook, got: %+v", bt)
	}
}

func TestCmdComposeProfileEnvVar(t *testing.T) {
	srcDir := createComposeHarness(t)
	t.Setenv("YNH_PROFILE", "staging")

	var stdout bytes.Buffer
	if err := cmdComposeTo([]string{srcDir}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdComposeTo: %v", err)
	}

	var got composeOutput
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Should resolve profile from env var
	bt, ok := got.Hooks["before_tool"]
	if !ok {
		t.Fatal("hooks missing before_tool after env-var profile")
	}
	if len(bt) != 1 || bt[0].Command != "echo staging" {
		t.Errorf("env-var profile should replace before_tool hook, got: %+v", bt)
	}
}

func TestCmdComposeHarnessEnvVar(t *testing.T) {
	srcDir := createComposeHarness(t)
	t.Setenv("YNH_HARNESS", srcDir)

	var stdout bytes.Buffer
	// No positional arg — should resolve from env var
	if err := cmdComposeTo(nil, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdComposeTo: %v", err)
	}

	var got composeOutput
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Name != "compose-test" {
		t.Errorf("name = %q, want compose-test", got.Name)
	}
}

func TestCmdComposeTextRich(t *testing.T) {
	srcDir := createComposeHarness(t)

	var stdout bytes.Buffer
	if err := cmdComposeTo([]string{srcDir, "--format", "text"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdComposeTo: %v", err)
	}

	out := stdout.String()

	// Identity
	if !strings.Contains(out, "compose-test") {
		t.Error("expected harness name")
	}
	if !strings.Contains(out, "1.0.0") {
		t.Error("expected version")
	}
	if !strings.Contains(out, "Test harness for compose") {
		t.Error("expected description")
	}
	if !strings.Contains(out, "claude") {
		t.Error("expected vendor")
	}

	// Artifacts
	if !strings.Contains(out, "greet") {
		t.Error("expected skill greet")
	}
	if !strings.Contains(out, "deploy") {
		t.Error("expected skill deploy")
	}
	if !strings.Contains(out, "reviewer") {
		t.Error("expected agent reviewer")
	}
	if !strings.Contains(out, "no-debug") {
		t.Error("expected rule no-debug")
	}

	// Hooks
	if !strings.Contains(out, "before_tool") {
		t.Error("expected hook event")
	}
	if !strings.Contains(out, "echo before") {
		t.Error("expected hook command")
	}
	if !strings.Contains(out, "matcher=Write") {
		t.Error("expected hook matcher")
	}

	// MCP Servers
	if !strings.Contains(out, "test-server") {
		t.Error("expected MCP server name")
	}
	if !strings.Contains(out, "node") {
		t.Error("expected MCP server command")
	}

	// Profiles
	if !strings.Contains(out, "staging") {
		t.Error("expected profile name")
	}

	// Focuses
	if !strings.Contains(out, "quick") {
		t.Error("expected focus name")
	}
	if !strings.Contains(out, "Be concise") {
		t.Error("expected focus prompt")
	}
	if !strings.Contains(out, "review") {
		t.Error("expected focus name review")
	}
	if !strings.Contains(out, "profile=staging") {
		t.Error("expected focus profile")
	}
}

func TestCmdComposeMissingFormatValue(t *testing.T) {
	err := cmdComposeTo([]string{".", "--format"}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error for missing --format value")
	}
}

func TestCmdComposeMissingHarnessValue(t *testing.T) {
	err := cmdComposeTo([]string{"--harness"}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error for missing --harness value")
	}
}

func TestCmdComposeMissingProfileValue(t *testing.T) {
	err := cmdComposeTo([]string{".", "--profile"}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error for missing --profile value")
	}
}

func TestCmdComposeHarnessFlag(t *testing.T) {
	srcDir := createComposeHarness(t)

	var stdout bytes.Buffer
	if err := cmdComposeTo([]string{"--harness", srcDir}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdComposeTo: %v", err)
	}

	var got composeOutput
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Name != "compose-test" {
		t.Errorf("name = %q, want compose-test", got.Name)
	}
}

func TestCmdComposeExtraArg(t *testing.T) {
	srcDir := createComposeHarness(t)
	err := cmdComposeTo([]string{srcDir, "extra"}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error for extra argument")
	}
	if !strings.Contains(err.Error(), "unexpected") {
		t.Errorf("error should mention unexpected, got: %v", err)
	}
}

func TestCmdComposeHelp(t *testing.T) {
	err := cmdComposeTo([]string{"--help"}, io.Discard, io.Discard)
	if err != errHelp {
		t.Fatalf("expected errHelp, got: %v", err)
	}
}

func TestCmdComposePickFilterWithLocalInclude(t *testing.T) {
	// Regression: pickSet was built from "skills/name" paths but compared
	// against bare "name" values returned by ScanArtifactsDir, so picked
	// skills were always dropped.
	dir := t.TempDir()

	// Create include harness with two skills
	incDir := filepath.Join(dir, "include-harness")
	for _, name := range []string{"swift-lang", "go-lang"} {
		skillDir := filepath.Join(incDir, "skills", name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
			[]byte("---\nname: "+name+"\n---\n"+name+" skill.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(incDir, ".harness.json"),
		[]byte(`{"name":"inc","version":"0.1.0","default_vendor":"claude"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Main harness picks only swift-lang from the include
	hj := map[string]any{
		"name":           "main",
		"version":        "1.0.0",
		"default_vendor": "claude",
		"includes": []map[string]any{
			{"local": incDir, "pick": []string{"skills/swift-lang"}},
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

	if len(got.Artifacts.Skills) != 1 {
		t.Fatalf("skills = %v, want exactly [swift-lang]", got.Artifacts.Skills)
	}
	if got.Artifacts.Skills[0].Name != "swift-lang" {
		t.Errorf("skill name = %q, want swift-lang", got.Artifacts.Skills[0].Name)
	}
}

func TestCmdComposeTextWithMCPURL(t *testing.T) {
	dir := t.TempDir()

	hj := map[string]any{
		"name":           "mcp-url-test",
		"version":        "1.0.0",
		"default_vendor": "claude",
		"mcp_servers": map[string]any{
			"remote": map[string]any{
				"url": "https://api.example.com/mcp",
			},
		},
	}
	data, _ := json.MarshalIndent(hj, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := cmdComposeTo([]string{dir, "--format", "text"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdComposeTo: %v", err)
	}

	if !strings.Contains(stdout.String(), "https://api.example.com/mcp") {
		t.Error("expected URL MCP server in text output")
	}
}

func TestCompose_Sensors(t *testing.T) {
	dir := t.TempDir()
	hj := map[string]any{
		"name":    "sensor-h",
		"version": "0.1.0",
		"focus": map[string]any{
			"audit": map[string]any{"prompt": "audit the diff"},
		},
		"sensors": map[string]any{
			"build": map[string]any{
				"category": "maintainability",
				"source":   map[string]any{"command": "make check"},
				"output":   map[string]any{"format": "text"},
			},
			"sec": map[string]any{
				"source": map[string]any{"focus": "audit"},
				"output": map[string]any{"format": "markdown"},
			},
			"inline-judge": map[string]any{
				"source": map[string]any{
					"focus": map[string]any{"prompt": "judge coverage"},
				},
				"output": map[string]any{"format": "markdown"},
			},
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
		t.Fatalf("unmarshal: %v\noutput: %s", err, stdout.String())
	}
	if len(got.Sensors) != 3 {
		t.Fatalf("expected 3 sensors, got %d: %+v", len(got.Sensors), got.Sensors)
	}
	if got.Sensors["build"].Source.Command != "make check" {
		t.Errorf("build sensor command = %q", got.Sensors["build"].Source.Command)
	}
	if got.Sensors["sec"].Source.Focus == nil || got.Sensors["sec"].Source.Focus.Name != "audit" {
		t.Errorf("sec sensor focus = %+v", got.Sensors["sec"].Source.Focus)
	}
	inline := got.Sensors["inline-judge"].Source.Focus
	if inline == nil || !inline.Inline || inline.Prompt != "judge coverage" {
		t.Errorf("inline-judge focus = %+v", inline)
	}
}
