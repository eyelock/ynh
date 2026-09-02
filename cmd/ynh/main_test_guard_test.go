package main

import (
	"fmt"
	"os"
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
func TestMain(m *testing.M) {
	code := m.Run()
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
