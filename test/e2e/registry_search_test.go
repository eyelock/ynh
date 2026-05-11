//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// TestRegistry_AddListRemove walks `ynh registry add/list/remove` for a
// fake URL — the add path doesn't actually fetch, so this exercises the
// config plumbing without needing a real registry endpoint. Locks the
// graceful empty-list message too.
func TestRegistry_AddListRemove(t *testing.T) {
	s := newSandbox(t)

	out, _ := s.mustRunYnh(t, "registry", "list")
	if !strings.Contains(out, "No registries configured") {
		t.Errorf("expected empty-list message, got:\n%s", out)
	}

	fakeURL := "https://registry.example.invalid"
	out, _ = s.mustRunYnh(t, "registry", "add", fakeURL)
	if !strings.Contains(out, "Added registry") {
		t.Errorf("expected 'Added registry', got: %s", out)
	}

	// Duplicate add must error.
	_, errOut, err := s.runYnh(t, "registry", "add", fakeURL)
	if err == nil {
		t.Errorf("expected duplicate registry add to fail")
	} else if !strings.Contains(errOut, "already") {
		t.Errorf("expected 'already' in duplicate error, got: %s", errOut)
	}

	out, _ = s.mustRunYnh(t, "registry", "list")
	if !strings.Contains(out, fakeURL) {
		t.Errorf("registry list missing %q\n%s", fakeURL, out)
	}

	s.mustRunYnh(t, "registry", "remove", fakeURL)

	out, _ = s.mustRunYnh(t, "registry", "list")
	if !strings.Contains(out, "No registries") {
		t.Errorf("expected empty-list message after remove, got:\n%s", out)
	}
}

// TestSearch_EmptyResult asserts `ynh search <name>` with no registries
// configured prints the documented "No harnesses found" message rather
// than erroring or emitting an empty table.
func TestSearch_EmptyResult(t *testing.T) {
	s := newSandbox(t)

	out, _ := s.mustRunYnh(t, "search")
	if !strings.Contains(out, "No harnesses found") {
		t.Errorf("expected 'No harnesses found' message, got:\n%s", out)
	}
}
