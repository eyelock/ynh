//go:build e2e

package e2e

import (
	"path/filepath"
	"testing"
)

// TestFocus_UpdateClearProfile_LocalInstall asserts that
// `ynh focus update --clear-profile` drops the focus's profile binding while
// preserving the prompt. The flag mirrors the cleared-pointer semantics
// documented in docs/focus.md.
func TestFocus_UpdateClearProfile_LocalInstall(t *testing.T) {
	s := newSandbox(t)
	clone := cloneAssistantsAtSHA(t)
	sourceDir := filepath.Join(clone, "e2e-fixtures", "minimal")

	s.mustRunYnh(t, "install", sourceDir)

	// Seed: profile + focus that binds to it.
	s.mustRunYnh(t, "profile", "add", "local/minimal", "audit")
	s.mustRunYnh(t, "focus", "add", "local/minimal", "scan", "scan everything", "--profile", "audit")

	mf := readExtendedManifest(t, sourceDir)
	if mf.Focuses["scan"].Profile != "audit" {
		t.Fatalf("seed: expected profile=audit, got %+v", mf.Focuses["scan"])
	}

	// Clear the profile binding.
	s.mustRunYnh(t, "focus", "update", "local/minimal", "scan", "--clear-profile")

	mf = readExtendedManifest(t, sourceDir)
	got := mf.Focuses["scan"]
	if got.Profile != "" {
		t.Errorf("--clear-profile should drop the binding, got %q", got.Profile)
	}
	if got.Prompt != "scan everything" {
		t.Errorf("--clear-profile must preserve prompt, got %q", got.Prompt)
	}

	// And the now-unreferenced profile must be removable.
	if _, _, err := s.runYnh(t, "profile", "remove", "local/minimal", "audit"); err != nil {
		t.Errorf("profile remove failed after clear-profile: %v", err)
	}
}
