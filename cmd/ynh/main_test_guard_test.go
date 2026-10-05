package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/baseline"
)

// TestMain fails the package if the suite left a baseline in the source tree.
//
// `ynh check` writes `.ynh/baseline.json` relative to its working directory,
// which during a test run is the package directory. A test that omits --cwd
// therefore reads — and with --update-baseline writes — the repository's own
// ratchet. That happened: a stray entry turned an advisory failure into
// "known", and the suite's result started depending on what had run before it.
//
// Deleting the file would hide the next occurrence, so this reports instead.
//
// It also keeps telemetry away from the machine's own spool (see
// isolateTelemetry), and stops `ynh image` tests from asking a real docker
// about the base image: the suite must not depend on what this machine has
// pulled.
func TestMain(m *testing.M) {
	// Run as ynh itself, for a test that needs a real process to kill.
	if os.Getenv(execMainEnv) == "1" {
		main()
		os.Exit(0)
	}
	state, err := isolateTelemetry()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	dockerImageLabels = func(image string) ([]byte, error) {
		return nil, fmt.Errorf("docker is not consulted in tests (%s)", image)
	}
	code := m.Run()
	// Remove, not RemoveAll: it is empty unless a test wrote a spool into
	// the shared state home, which the next check would then report.
	if err := os.Remove(state); err != nil {
		fmt.Fprintf(os.Stderr, "\nthe test state home %s is not empty: a test wrote telemetry outside its own t.TempDir(): %v\n", state, err)
		if code == 0 {
			code = 1
		}
	}
	if _, err := os.Stat(baseline.Root(".")); err == nil {
		fmt.Fprintf(os.Stderr,
			"\nthis package's tests wrote %s into the source tree.\n"+
				"A check test must pass --cwd; without it the suite gates on the repository's own\n"+
				"baseline and leaves state behind that changes later runs. Delete the file and add --cwd.\n",
			baseline.Root("."))
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

// execMainEnv makes the test binary run ynh's main instead of the tests.
const execMainEnv = "YNH_TEST_EXEC_MAIN"

// isolateTelemetry clears every variable that would send `ynh agent run`
// telemetry anywhere, and points the laptop default spool into a temporary
// state home, so no test writes to this machine's real spool or to an
// operator's collector. A test that wants telemetry sets its own with
// t.Setenv.
func isolateTelemetry() (string, error) {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "OTEL_") {
			if err := os.Unsetenv(name); err != nil {
				return "", fmt.Errorf("clearing %s: %w", name, err)
			}
		}
	}
	for _, name := range []string{"YNR_SPOOL", "TRACEPARENT", "TRACESTATE"} {
		if err := os.Unsetenv(name); err != nil {
			return "", fmt.Errorf("clearing %s: %w", name, err)
		}
	}
	state, err := os.MkdirTemp("", "ynh-test-state-")
	if err != nil {
		return "", fmt.Errorf("creating a test state home: %w", err)
	}
	if err := os.Setenv("XDG_STATE_HOME", state); err != nil {
		return "", fmt.Errorf("setting XDG_STATE_HOME: %w", err)
	}
	return state, nil
}
