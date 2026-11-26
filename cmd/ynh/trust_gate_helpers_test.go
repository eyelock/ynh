package main

import (
	"io"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
)

// installTrustedTestHarness installs a harness as installListTestHarness does
// and records its acceptance, so a test that runs or checks it passes the
// trust gate. A test of the gate itself installs with installListTestHarness
// and leaves it unaccepted.
func installTrustedTestHarness(t *testing.T, home, name, harnessJSON string) string {
	t.Helper()
	id := installListTestHarness(t, home, name, harnessJSON)
	if err := cmdTrustTo([]string{"accept", id}, io.Discard, io.Discard); err != nil {
		t.Fatalf("accepting %s: %v", id, err)
	}
	return id
}

// skipCheckTrustGate stands the gate of `ynh check` aside for one test, for a
// fixture a trust record cannot cover.
func skipCheckTrustGate(t *testing.T) {
	t.Helper()
	old := checkTrustGate
	checkTrustGate = func(string, *harness.Harness, io.Writer) error { return nil }
	t.Cleanup(func() { checkTrustGate = old })
}
