//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// ynh run refuses an installed harness that was never accepted, before any
// vendor starts, and runs it after `ynh trust accept` (ynh #950).
func TestTrustGate_RunRefusesUntilAccepted(t *testing.T) {
	s := newSandbox(t)
	harness := newHookedHarness(t, "gated")
	s.mustRunYnh(t, "install", harness)

	_, stderr, err := s.runYnh(t, "run", "local/gated", "-v", "claude")
	if err == nil {
		t.Fatal("an unaccepted harness ran")
	}
	for _, want := range []string{"refusing to run local/gated", "ynh trust accept local/gated"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}

	s.mustRunYnh(t, "trust", "accept", "local/gated")
	_, stderr, _ = s.runYnh(t, "run", "local/gated", "-v", "claude")
	if strings.Contains(stderr, "refusing to run") {
		t.Errorf("an accepted harness was refused:\n%s", stderr)
	}
}
