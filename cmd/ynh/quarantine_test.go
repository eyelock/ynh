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

func setupQuarantine(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	qDir := filepath.Join(home, ".quarantine", "broken")
	if err := os.MkdirAll(qDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// One quarantined entry.
	if err := os.MkdirAll(filepath.Join(qDir, "broken-harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Persisted manifest decorating the entry.
	manifest := `{
		"schema_version": 2,
		"migrated_at": "2026-05-05T00:00:00Z",
		"entries": [],
		"quarantined": [
			{
				"original_path": "/orig/broken-harness",
				"quarantined":   "` + filepath.Join(qDir, "broken-harness") + `",
				"reason":        "no source URL"
			}
		]
	}`
	if err := os.WriteFile(filepath.Join(home, ".migration-manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestCmdQuarantine_List_Empty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	var stdout bytes.Buffer
	if err := cmdQuarantineTo([]string{"list"}, &stdout, io.Discard); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(stdout.String(), "No quarantined") {
		t.Errorf("expected 'No quarantined' message, got: %s", stdout.String())
	}
}

func TestCmdQuarantine_List_JSON(t *testing.T) {
	setupQuarantine(t)

	var stdout bytes.Buffer
	if err := cmdQuarantineTo([]string{"list", "--format", "json"}, &stdout, io.Discard); err != nil {
		t.Fatalf("list --json: %v", err)
	}

	var got []quarantineEntry
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, stdout.String())
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
	if got[0].Name != "broken-harness" {
		t.Errorf("name = %q, want broken-harness", got[0].Name)
	}
	if got[0].Reason != "no source URL" {
		t.Errorf("reason = %q, want 'no source URL'", got[0].Reason)
	}
	if got[0].OriginalPath != "/orig/broken-harness" {
		t.Errorf("original_path = %q", got[0].OriginalPath)
	}
}

func TestCmdQuarantine_Drop(t *testing.T) {
	home := setupQuarantine(t)
	target := filepath.Join(home, ".quarantine", "broken", "broken-harness")

	if err := cmdQuarantineTo([]string{"drop", "broken-harness"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("dropped entry still exists: %v", err)
	}
}

func TestCmdQuarantine_Drop_NotFound(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	var stderr bytes.Buffer
	err := cmdQuarantineTo([]string{"drop", "nonexistent"}, io.Discard, &stderr)
	if err == nil {
		t.Fatal("expected error dropping nonexistent entry")
	}
}

func TestCmdQuarantine_Restore_ExplicitDest(t *testing.T) {
	home := setupQuarantine(t)
	source := filepath.Join(home, ".quarantine", "broken", "broken-harness")
	dst := filepath.Join(home, "restored-here")

	if err := cmdQuarantineTo([]string{"restore", "broken-harness", "--to", dst}, io.Discard, io.Discard); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Errorf("destination not created: %v", err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Errorf("quarantined source still exists after restore")
	}
}

func TestCmdQuarantine_Restore_NoManifestEntry_RequiresTo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	qDir := filepath.Join(home, ".quarantine", "broken")
	_ = os.MkdirAll(filepath.Join(qDir, "ad-hoc"), 0o755)

	var stderr bytes.Buffer
	err := cmdQuarantineTo([]string{"restore", "ad-hoc"}, io.Discard, &stderr)
	if err == nil {
		t.Fatal("expected error when manifest has no entry and --to is missing")
	}
}

func TestCmdQuarantine_UnknownSubcommand(t *testing.T) {
	var stderr bytes.Buffer
	err := cmdQuarantineTo([]string{"frobnicate"}, io.Discard, &stderr)
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}
