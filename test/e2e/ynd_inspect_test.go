//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestYnd_Inspect_NoCli_HelpMessage covers the documented graceful exit
// when no LLM CLI is detected on PATH: prints the install hints to stderr
// and exits 0 (not an error — running without a CLI is a recoverable
// state the user can fix by installing one).
//
// We point PATH at an empty tempdir so the auto-detection finds nothing,
// regardless of what's installed on the test machine.
func TestYnd_Inspect_NoCli_HelpMessage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH semantics differ on Windows")
	}
	emptyBin := t.TempDir()

	_, errOut, err := runYndInDirEnv(t, t.TempDir(),
		[]string{"PATH=" + emptyBin},
		"inspect")
	if err != nil {
		t.Fatalf("expected ynd inspect with no CLI to exit 0, got err=%v\nstderr:\n%s", err, errOut)
	}
	if !strings.Contains(errOut, "No supported LLM CLI") {
		t.Errorf("expected 'No supported LLM CLI' message in stderr, got:\n%s", errOut)
	}
}

// TestYnd_Inspect_VendorMissing_Errors covers the documented error when
// `-v <vendor>` is explicit but that CLI isn't on PATH. Distinct from the
// auto-detect-empty path — this one exits non-zero with a specific error.
func TestYnd_Inspect_VendorMissing_Errors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH semantics differ on Windows")
	}
	emptyBin := t.TempDir()

	_, errOut, err := runYndInDirEnv(t, t.TempDir(),
		[]string{"PATH=" + emptyBin},
		"inspect", "-v", "claude")
	if err == nil {
		t.Fatalf("expected error when explicit -v claude is not on PATH")
	}
	if !strings.Contains(errOut, "claude") || !strings.Contains(errOut, "not found") {
		t.Errorf("expected error to mention 'claude' and 'not found', got: %s", errOut)
	}
}

// TestYnd_Inspect_DispatchesToVendorCli proves the LLM dispatch path runs
// end-to-end: it spawns the vendor CLI binary on PATH and pipes the prompt
// to its stdin. We can't test the analysis output (LLM responses are
// non-deterministic) but we can prove dispatch fires by using a fake
// `claude` script that drops a marker file every time it's invoked.
//
// This locks the contract that ynd inspect actually shells out to the
// configured vendor — silent regression here would mean inspect goes quiet.
func TestYnd_Inspect_DispatchesToVendorCli(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake doesn't apply on Windows")
	}

	binDir := t.TempDir()
	markerDir := t.TempDir()
	marker := filepath.Join(markerDir, "claude-was-called")

	// Fake claude: drains stdin, touches marker, emits a minimal response.
	// Use absolute paths for builtins — PATH is overridden to just binDir,
	// so `touch`/`cat` aren't on PATH inside the script's shell.
	fakeScript := "#!/bin/sh\n: > '" + marker + "'\nwhile IFS= read -r _; do :; done\necho 'fake claude response'\n"
	fakePath := filepath.Join(binDir, "claude")
	if err := os.WriteFile(fakePath, []byte(fakeScript), 0o755); err != nil {
		t.Fatal(err)
	}

	// Project dir with one signal scanSignals will pick up.
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Run inspect: YNH_YES=1 skips interactive prompts; PATH points at our
	// fake. We don't assert success — the fake's response won't parse as
	// the analysis pipeline expects. We assert dispatch happened.
	stdout, stderr, _ := runYndInDirEnv(t, project,
		[]string{"PATH=" + binDir, "YNH_YES=1"},
		"inspect", "-v", "claude")

	if _, err := os.Stat(marker); err != nil {
		t.Errorf("fake claude was never invoked — LLM dispatch did not fire (marker missing): %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
}
