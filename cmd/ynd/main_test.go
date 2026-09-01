package main

import (
	"os"
	"testing"
)

// TestMain clears the skip-confirm environment for the whole package.
//
// skipConfirmEnv() honours YNH_YES and CI, so any test that drives a
// confirmation prompt behaves differently depending on where it runs. Of the
// 23 prompt-driven inspect tests, exactly one cleared CI for itself; the rest
// inherited whatever the machine had. Three of them passed locally and failed
// on GitHub Actions, which exports CI=true, because under skip-confirm the
// prompt they stub is never reached at all.
//
// The same ambient dependency put TestYnd_Migrate in the opposite position:
// green in CI, red on a developer machine.
//
// Clearing it once here means a test that wants skip-confirm has to ask for it
// with t.Setenv, which is visible in the test, rather than inheriting it from
// the environment, which is not. A test's result should not depend on who ran
// it.
func TestMain(m *testing.M) {
	if err := os.Unsetenv("CI"); err != nil {
		panic(err)
	}
	if err := os.Unsetenv("YNH_YES"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
