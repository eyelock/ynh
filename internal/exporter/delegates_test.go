package exporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
)

// No delegates must write nothing at all, not an empty agents/ directory. An
// export that creates a stray directory changes what ships.
func TestExportDelegates_NoneWritesNothing(t *testing.T) {
	out := t.TempDir()
	if err := ExportDelegates(out, nil); err != nil {
		t.Fatalf("ExportDelegates: %v", err)
	}
	if err := ExportDelegates(out, []harness.Delegate{}); err != nil {
		t.Fatalf("ExportDelegates: %v", err)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("no delegates should create no directories, got %v", entries)
	}
	if _, err := os.Stat(filepath.Join(out, "agents")); err == nil {
		t.Error("agents/ was created for zero delegates")
	}
}

// A delegate that cannot be resolved must fail loudly and name itself. Export
// silently dropping a delegate would ship a harness missing a hand-off the
// manifest promised.
func TestExportDelegates_UnresolvableIsAnError(t *testing.T) {
	out := t.TempDir()
	err := ExportDelegates(out, []harness.Delegate{
		{GitSource: harness.GitSource{Git: "example.invalid/nope/nothing-here"}},
	})
	if err == nil {
		t.Fatal("an unresolvable delegate must be an error, not a silent skip")
	}
	if !strings.Contains(err.Error(), "delegate") {
		t.Errorf("error should identify what failed, got: %v", err)
	}
}

// ExportDelegates writes to <outputDir>/agents/, deliberately not inside the
// vendor ConfigDir the way assembler.AssembleDelegates does. The two are easy
// to confuse and the plugin layout depends on the difference.
func TestExportDelegates_TargetsPluginRootNotConfigDir(t *testing.T) {
	out := t.TempDir()
	// Resolution fails, but only after the directory decision is made, so the
	// created path still tells us where it intended to write.
	_ = ExportDelegates(out, []harness.Delegate{
		{GitSource: harness.GitSource{Git: "example.invalid/x/y"}},
	})
	if _, err := os.Stat(filepath.Join(out, "agents")); err != nil {
		t.Errorf("delegates should target <output>/agents/: %v", err)
	}
	for _, wrong := range []string{".claude", ".cursor", ".codex"} {
		if _, err := os.Stat(filepath.Join(out, wrong)); err == nil {
			t.Errorf("delegates must not be written inside %s on export", wrong)
		}
	}
}
