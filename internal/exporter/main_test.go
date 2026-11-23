package exporter

import (
	"os"
	"testing"
)

// TestMain clears YNH_HOME: it takes precedence over the HOME these tests set, so an exported
// value would send their cache writes into the developer's real ynh home. A test that wants
// YNH_HOME sets its own with t.Setenv.
func TestMain(m *testing.M) {
	if err := os.Unsetenv("YNH_HOME"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
