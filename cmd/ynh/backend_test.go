package main

import (
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/eyelock/ynh/internal/config"
)

func setupBackendTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YNH_HOME", "")
}

func TestCmdBackendAddAndList(t *testing.T) {
	setupBackendTestHome(t)

	if err := cmdBackend([]string{"add", "ollama", "claude", "--base-url", "http://localhost:11434", "--auth-token", "ollama", "--type", "ollama"}); err != nil {
		t.Fatalf("backend add failed: %v", err)
	}
	if err := cmdBackend([]string{"add", "ollama", "codex", "--base-url", "http://localhost:11434/v1/"}); err != nil {
		t.Fatalf("backend add (codex) failed: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	def, ok := cfg.Backends["ollama"]
	if !ok {
		t.Fatal("backend \"ollama\" not saved")
	}
	if def.Type != "ollama" {
		t.Errorf("Type = %q, want %q", def.Type, "ollama")
	}
	claude, ok := def.Vendors["claude"]
	if !ok || claude.BaseURL != "http://localhost:11434" || claude.AuthToken != "ollama" {
		t.Errorf("claude connection = %+v", claude)
	}
	codex, ok := def.Vendors["codex"]
	if !ok || codex.BaseURL != "http://localhost:11434/v1/" {
		t.Errorf("codex connection = %+v", codex)
	}
}

func TestCmdBackendAddRejectsUnknownVendor(t *testing.T) {
	setupBackendTestHome(t)

	err := cmdBackend([]string{"add", "ollama", "not-a-vendor", "--base-url", "http://localhost:11434"})
	if err == nil {
		t.Fatal("expected error for unknown vendor")
	}
}

func TestCmdBackendAddRequiresBaseURL(t *testing.T) {
	setupBackendTestHome(t)

	err := cmdBackend([]string{"add", "ollama", "claude"})
	if err == nil {
		t.Fatal("expected error when --base-url is missing")
	}
}

func TestCmdBackendAddRejectsDuplicate(t *testing.T) {
	setupBackendTestHome(t)

	if err := cmdBackend([]string{"add", "ollama", "claude", "--base-url", "http://localhost:11434"}); err != nil {
		t.Fatalf("first add failed: %v", err)
	}
	err := cmdBackend([]string{"add", "ollama", "claude", "--base-url", "http://localhost:99999"})
	if err == nil {
		t.Fatal("expected error adding a duplicate backend/vendor pair")
	}
}

func TestCmdBackendListJSON(t *testing.T) {
	setupBackendTestHome(t)

	if err := cmdBackend([]string{"add", "ollama", "claude", "--base-url", "http://localhost:11434", "--auth-token", "ollama", "--type", "ollama"}); err != nil {
		t.Fatalf("backend add failed: %v", err)
	}

	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w

	err := cmdBackend([]string{"list", "--format", "json"})

	if closeErr := w.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	os.Stdout = old

	if err != nil {
		t.Fatalf("backend list failed: %v", err)
	}

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}

	var entries []backendListEntry
	if err := json.Unmarshal(out, &entries); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d: %+v", len(entries), entries)
	}
	if entries[0].Backend != "ollama" || entries[0].Vendor != "claude" {
		t.Errorf("entry = %+v", entries[0])
	}
	if !entries[0].HasAuthToken {
		t.Error("HasAuthToken = false, want true")
	}
}

func TestCmdBackendRemoveVendorOnly(t *testing.T) {
	setupBackendTestHome(t)

	if err := cmdBackend([]string{"add", "ollama", "claude", "--base-url", "http://localhost:11434"}); err != nil {
		t.Fatalf("backend add failed: %v", err)
	}
	if err := cmdBackend([]string{"add", "ollama", "codex", "--base-url", "http://localhost:11434/v1/"}); err != nil {
		t.Fatalf("backend add (codex) failed: %v", err)
	}

	if err := cmdBackend([]string{"remove", "ollama", "claude"}); err != nil {
		t.Fatalf("backend remove failed: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	def := cfg.Backends["ollama"]
	if _, ok := def.Vendors["claude"]; ok {
		t.Error("claude connection still present after removal")
	}
	if _, ok := def.Vendors["codex"]; !ok {
		t.Error("codex connection should survive removing only claude")
	}
}

func TestCmdBackendRemoveWholeBackend(t *testing.T) {
	setupBackendTestHome(t)

	if err := cmdBackend([]string{"add", "ollama", "claude", "--base-url", "http://localhost:11434"}); err != nil {
		t.Fatalf("backend add failed: %v", err)
	}
	if err := cmdBackend([]string{"remove", "ollama"}); err != nil {
		t.Fatalf("backend remove failed: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	if _, ok := cfg.Backends["ollama"]; ok {
		t.Error("backend \"ollama\" still present after removal")
	}
}

func TestCmdBackendRemoveUnknown(t *testing.T) {
	setupBackendTestHome(t)

	if err := cmdBackend([]string{"remove", "does-not-exist"}); err == nil {
		t.Fatal("expected error removing unknown backend")
	}
}
