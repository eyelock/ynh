package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/config"
)

// writeSourceHarness creates a minimal .harness.json in dir.
func writeSourceHarness(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"name":"` + name + `","version":"0.1.0","description":"` + name + ` harness"}`
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCmdSources_NoArgs(t *testing.T) {
	err := cmdSources([]string{})
	if err == nil {
		t.Fatal("expected error for no args")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdSources_UnknownSubcommand(t *testing.T) {
	err := cmdSources([]string{"bogus"})
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
	if !strings.Contains(err.Error(), "unknown sources subcommand") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdSourcesAdd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", "")
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{DefaultVendor: "claude"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	// Create a source directory with two harnesses
	srcDir := filepath.Join(home, "my-sources")
	writeSourceHarness(t, filepath.Join(srcDir, "alice"), "alice")
	writeSourceHarness(t, filepath.Join(srcDir, "bob"), "bob")

	var stdout bytes.Buffer
	err := cmdSourcesAdd([]string{srcDir, "--name", "dev", "--description", "Dev sources"}, &stdout)
	if err != nil {
		t.Fatalf("cmdSourcesAdd: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, `"dev"`) {
		t.Errorf("output missing source name: %s", out)
	}
	if !strings.Contains(out, "2 harness(es) found") {
		t.Errorf("output missing harness count: %s", out)
	}

	// Verify config was updated
	loaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(loaded.Sources))
	}
	if loaded.Sources[0].Name != "dev" {
		t.Errorf("source name = %q, want %q", loaded.Sources[0].Name, "dev")
	}
	if loaded.Sources[0].Description != "Dev sources" {
		t.Errorf("source description = %q", loaded.Sources[0].Description)
	}
}

func TestCmdSourcesAdd_DeriveNameFromPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", "")
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{DefaultVendor: "claude"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	srcDir := filepath.Join(home, "assistants")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	err := cmdSourcesAdd([]string{srcDir}, &stdout)
	if err != nil {
		t.Fatalf("cmdSourcesAdd: %v", err)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Sources[0].Name != "assistants" {
		t.Errorf("derived name = %q, want %q", loaded.Sources[0].Name, "assistants")
	}
}

func TestCmdSourcesAdd_DuplicateName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", "")
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	srcDir := filepath.Join(home, "sources")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		DefaultVendor: "claude",
		Sources:       []config.Source{{Name: "dev", Path: "/old"}},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	err := cmdSourcesAdd([]string{srcDir, "--name", "dev"}, &stdout)
	if err == nil {
		t.Fatal("expected error for duplicate name")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdSourcesAdd_NonexistentPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", "")
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{DefaultVendor: "claude"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	err := cmdSourcesAdd([]string{"/nonexistent/12345"}, &stdout)
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdSourcesListText(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", "")
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	srcDir := filepath.Join(home, "sources")
	writeSourceHarness(t, filepath.Join(srcDir, "alice"), "alice")

	cfg := &config.Config{
		DefaultVendor: "claude",
		Sources:       []config.Source{{Name: "dev", Path: srcDir, Description: "Dev harnesses"}},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := cmdSourcesListTo(nil, &stdout, &stderr)
	if err != nil {
		t.Fatalf("cmdSourcesListTo: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "NAME") {
		t.Error("missing header")
	}
	if !strings.Contains(out, "dev") {
		t.Error("missing source name")
	}
	if !strings.Contains(out, "Dev harnesses") {
		t.Error("missing description")
	}
}

func TestCmdSourcesListJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", "")
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	srcDir := filepath.Join(home, "sources")
	writeSourceHarness(t, filepath.Join(srcDir, "alice"), "alice")
	writeSourceHarness(t, filepath.Join(srcDir, "bob"), "bob")

	cfg := &config.Config{
		DefaultVendor: "claude",
		Sources:       []config.Source{{Name: "dev", Path: srcDir, Description: "Dev"}},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := cmdSourcesListTo([]string{"--format", "json"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("cmdSourcesListTo: %v", err)
	}

	var entries []sourceListEntry
	if err := json.Unmarshal(stdout.Bytes(), &entries); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Name != "dev" {
		t.Errorf("name = %q", entries[0].Name)
	}
	if entries[0].Harnesses != 2 {
		t.Errorf("harnesses = %d, want 2", entries[0].Harnesses)
	}
}

func TestCmdSourcesList_Empty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", "")
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{DefaultVendor: "claude"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := cmdSourcesListTo(nil, &stdout, &stderr)
	if err != nil {
		t.Fatalf("cmdSourcesListTo: %v", err)
	}
	if !strings.Contains(stdout.String(), "No sources configured") {
		t.Error("expected empty message")
	}
}

func TestCmdSourcesRemove(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", "")
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		DefaultVendor: "claude",
		Sources: []config.Source{
			{Name: "a", Path: "/a"},
			{Name: "b", Path: "/b"},
		},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	err := cmdSourcesRemove([]string{"a"}, &stdout)
	if err != nil {
		t.Fatalf("cmdSourcesRemove: %v", err)
	}
	if !strings.Contains(stdout.String(), `Removed source "a"`) {
		t.Errorf("unexpected output: %s", stdout.String())
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(loaded.Sources))
	}
	if loaded.Sources[0].Name != "b" {
		t.Errorf("remaining source = %q", loaded.Sources[0].Name)
	}
}

func TestCmdSourcesRemove_NotFound(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", "")
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{DefaultVendor: "claude"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	err := cmdSourcesRemove([]string{"nonexistent"}, &stdout)
	if err == nil {
		t.Fatal("expected error for not found")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestResolveInstallSource_FromSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", "")

	srcDir := filepath.Join(home, "sources")
	writeSourceHarness(t, filepath.Join(srcDir, "alice"), "alice")

	cfg := &config.Config{
		Sources: []config.Source{{Name: "dev", Path: srcDir}},
	}

	result, err := resolveInstallSource("alice", "", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.sourceType != "source" {
		t.Errorf("sourceType = %q, want %q", result.sourceType, "source")
	}
	if result.localPath != filepath.Join(srcDir, "alice") {
		t.Errorf("localPath = %q, want %q", result.localPath, filepath.Join(srcDir, "alice"))
	}
	if result.sourceName != "dev" {
		t.Errorf("sourceName = %q, want %q", result.sourceName, "dev")
	}
}

func TestResolveInstallSource_SourceNotFound_FallsToRegistry(t *testing.T) {
	cfg := &config.Config{
		Sources: []config.Source{{Name: "dev", Path: "/nonexistent"}},
	}

	// Should fall through to registry search, which errors because no registries
	_, err := resolveInstallSource("alice", "", cfg)
	if err == nil {
		t.Fatal("expected error (no registries configured)")
	}
	if !strings.Contains(err.Error(), "no registries configured") {
		t.Errorf("unexpected error: %v", err)
	}
}
