package main

import (
	"testing"

	"github.com/eyelock/ynh/internal/vendor"
)

func TestResolveVendorEnv(t *testing.T) {
	t.Run("returns empty when unset", func(t *testing.T) {
		t.Setenv("YNH_VENDOR", "")
		if got := resolveVendorEnv(); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("returns value when set", func(t *testing.T) {
		t.Setenv("YNH_VENDOR", "cursor")
		if got := resolveVendorEnv(); got != "cursor" {
			t.Errorf("expected cursor, got %q", got)
		}
	})
}

func TestResolveVendorDefault(t *testing.T) {
	t.Run("flag takes priority", func(t *testing.T) {
		t.Setenv("YNH_VENDOR", "cursor")
		if got := resolveVendorDefault("codex"); got != "codex" {
			t.Errorf("expected codex, got %q", got)
		}
	})

	t.Run("env var when no flag", func(t *testing.T) {
		t.Setenv("YNH_VENDOR", "cursor")
		if got := resolveVendorDefault(""); got != "cursor" {
			t.Errorf("expected cursor, got %q", got)
		}
	})

	t.Run("default when neither", func(t *testing.T) {
		t.Setenv("YNH_VENDOR", "")
		if got := resolveVendorDefault(""); got != vendor.DefaultName {
			t.Errorf("expected %q, got %q", vendor.DefaultName, got)
		}
	})
}

func TestResolveHarnessEnv(t *testing.T) {
	t.Run("returns empty when unset", func(t *testing.T) {
		t.Setenv("YNH_HARNESS", "")
		if got := resolveHarnessEnv(); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("returns value when set", func(t *testing.T) {
		t.Setenv("YNH_HARNESS", "/tmp/my-harness")
		if got := resolveHarnessEnv(); got != "/tmp/my-harness" {
			t.Errorf("expected /tmp/my-harness, got %q", got)
		}
	})
}

func TestSkipConfirmEnv(t *testing.T) {
	t.Run("false when neither set", func(t *testing.T) {
		t.Setenv("YNH_YES", "")
		t.Setenv("CI", "")
		if skipConfirmEnv() {
			t.Error("expected false")
		}
	})

	t.Run("true when YNH_YES set", func(t *testing.T) {
		t.Setenv("YNH_YES", "1")
		t.Setenv("CI", "")
		if !skipConfirmEnv() {
			t.Error("expected true")
		}
	})

	t.Run("true when CI set", func(t *testing.T) {
		t.Setenv("YNH_YES", "")
		t.Setenv("CI", "true")
		if !skipConfirmEnv() {
			t.Error("expected true")
		}
	})

	t.Run("true when both set", func(t *testing.T) {
		t.Setenv("YNH_YES", "1")
		t.Setenv("CI", "true")
		if !skipConfirmEnv() {
			t.Error("expected true")
		}
	})
}
