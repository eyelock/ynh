//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVendorInstructions_Propagation verifies that a harness's AGENTS.md
// content is copied to each vendor's project instructions file (the value
// of adapter.InstructionsFile()):
//
//   - Claude: CLAUDE.md
//   - Codex:  codex.md
//   - Cursor: .cursorrules
//
// This is the "documentation == code" backstop: silently breaking the
// per-vendor file name would mean a harness's instructions stop reaching
// the vendor CLI.
func TestVendorInstructions_Propagation(t *testing.T) {
	const sentinel = "Reach the vendor"
	agents := "# E2E test harness\n\n" + sentinel + ".\n"

	cases := []struct {
		vendor           string
		instructionsFile string
	}{
		{vendor: "claude", instructionsFile: "CLAUDE.md"},
		{vendor: "codex", instructionsFile: "codex.md"},
		{vendor: "cursor", instructionsFile: ".cursorrules"},
	}

	for _, tc := range cases {
		t.Run(tc.vendor, func(t *testing.T) {
			s := newSandbox(t)
			name := fmt.Sprintf("instr-%s", tc.vendor)
			harness := newAgentsMdHarness(t, name, agents)
			s.mustRunYnh(t, "install", harness)

			project := filepath.Join(t.TempDir(), "project")
			if err := os.MkdirAll(project, 0o755); err != nil {
				t.Fatal(err)
			}
			mustRunYnhInDir(t, s, project, "run", name, "-v", tc.vendor, "--install")

			runDir := filepath.Join(s.home, "run", name)
			body, err := os.ReadFile(filepath.Join(runDir, tc.instructionsFile))
			if err != nil {
				t.Fatalf("expected %s to exist: %v", tc.instructionsFile, err)
			}
			if !strings.Contains(string(body), sentinel) {
				t.Errorf("%s missing harness content (%q):\n%s", tc.instructionsFile, sentinel, body)
			}
		})
	}
}

// newAgentsMdHarness builds a synthetic harness directory with a
// plugin.json and an AGENTS.md containing the given instructions.
func newAgentsMdHarness(t *testing.T, name, agents string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Join(dir, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	plugin := fmt.Sprintf(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":%q,"version":"0.1.0"}`, name)
	if err := os.WriteFile(filepath.Join(dir, ".ynh-plugin", "plugin.json"), []byte(plugin), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(agents), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}
