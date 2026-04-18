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

func TestCmdSearchNoRegistries(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{DefaultVendor: "claude"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	// With no registries and no sources, text mode shows "No results"
	var stdout, stderr bytes.Buffer
	err := cmdSearchTo([]string{"something"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "No results") {
		t.Errorf("expected no results message, got: %s", stdout.String())
	}
}

func TestCmdSearch_NoQuery_ListsAll(t *testing.T) {
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
		Sources:       []config.Source{{Name: "dev", Path: srcDir}},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := cmdSearchTo([]string{}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("no-query search should succeed: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "alice") {
		t.Error("expected alice in no-query results")
	}
	if !strings.Contains(out, "bob") {
		t.Error("expected bob in no-query results")
	}
}

func TestCmdSearch_NoQuery_JSON(t *testing.T) {
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
		Sources:       []config.Source{{Name: "dev", Path: srcDir}},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := cmdSearchTo([]string{"--format", "json"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("no-query JSON search should succeed: %v", err)
	}

	var results []searchResultEntry
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestCmdSearch_SourcesOnly(t *testing.T) {
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
		Sources:       []config.Source{{Name: "dev", Path: srcDir}},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := cmdSearchTo([]string{"alice"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("cmdSearchTo: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "alice") {
		t.Error("missing alice in results")
	}
	if strings.Contains(out, "bob") {
		t.Error("bob should not match 'alice' query")
	}
	if !strings.Contains(out, "dev (source)") {
		t.Error("missing FROM annotation")
	}
}

func TestCmdSearch_SourcesJSON(t *testing.T) {
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
		Sources:       []config.Source{{Name: "dev", Path: srcDir}},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := cmdSearchTo([]string{"alice", "--format", "json"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("cmdSearchTo: %v", err)
	}

	var results []searchResultEntry
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "alice" {
		t.Errorf("name = %q", results[0].Name)
	}
	if results[0].From.Type != "source" {
		t.Errorf("from.type = %q", results[0].From.Type)
	}
	if results[0].From.Name != "dev" {
		t.Errorf("from.name = %q", results[0].From.Name)
	}
}

func TestCmdSearch_NoResultsJSON(t *testing.T) {
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
	err := cmdSearchTo([]string{"nothing", "--format", "json"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("cmdSearchTo: %v", err)
	}

	// Should be an empty array, not null
	out := strings.TrimSpace(stdout.String())
	if out != "[]" {
		t.Errorf("expected [], got %q", out)
	}
}

func TestCmdSearch_InvalidFormat(t *testing.T) {
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
	err := cmdSearchTo([]string{"test", "--format", "yaml"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
}

func TestCmdSearch_MatchesDescription(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", "")
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	srcDir := filepath.Join(home, "sources")
	dir := filepath.Join(srcDir, "myharness")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"name":"myharness","version":"0.1.0","description":"A golang development assistant"}`
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		DefaultVendor: "claude",
		Sources:       []config.Source{{Name: "dev", Path: srcDir}},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := cmdSearchTo([]string{"golang"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("cmdSearchTo: %v", err)
	}
	if !strings.Contains(stdout.String(), "myharness") {
		t.Error("should match by description")
	}
}
