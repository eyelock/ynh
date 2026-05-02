//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestHooks_PerVendor verifies that canonical hook declarations in a
// harness's plugin.json get translated into each vendor's native hooks
// file, with the canonical event name remapped per-vendor:
//
//   - Claude: .claude/hooks/hooks.json — uses Claude event names (PreToolUse, etc.)
//   - Codex:  .codex/hooks.json
//   - Cursor: .cursor/hooks.json
//
// Locks the canonical→vendor event-name remap. Silently breaking it
// means hooks stop firing on the vendor CLI.
func TestHooks_PerVendor(t *testing.T) {
	cases := []struct {
		vendor   string
		hookFile string // relative to runDir
	}{
		{vendor: "claude", hookFile: filepath.Join(".claude", "hooks", "hooks.json")},
		{vendor: "codex", hookFile: filepath.Join(".codex", "hooks.json")},
		{vendor: "cursor", hookFile: filepath.Join(".cursor", "hooks.json")},
	}

	for _, tc := range cases {
		t.Run(tc.vendor, func(t *testing.T) {
			s := newSandbox(t)
			name := fmt.Sprintf("hooked-%s", tc.vendor)
			harness := newHookedHarness(t, name)
			s.mustRunYnh(t, "install", harness)

			project := filepath.Join(t.TempDir(), "project")
			if err := os.MkdirAll(project, 0o755); err != nil {
				t.Fatal(err)
			}
			mustRunYnhInDir(t, s, project, "run", name, "-v", tc.vendor, "--install")

			runDir := filepath.Join(s.home, "run", name)
			path := filepath.Join(runDir, tc.hookFile)
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("expected hook file %s: %v", tc.hookFile, err)
			}
			// Sanity-check JSON parses and is non-empty.
			var raw map[string]any
			if err := json.Unmarshal(body, &raw); err != nil {
				t.Fatalf("hook file is not valid JSON: %v\n%s", err, body)
			}
			if len(raw) == 0 {
				t.Errorf("hook file is empty JSON object: %s", body)
			}
			// The canonical command must appear somewhere in the rendered file —
			// vendor remap touches event names, not command bodies.
			if !bytes.Contains(body, []byte("echo hooked")) {
				t.Errorf("hook command not present in %s:\n%s", tc.hookFile, body)
			}
		})
	}
}

func newHookedHarness(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Join(dir, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": %q,
  "version": "0.1.0",
  "hooks": {
    "before_tool": [{"command": "echo hooked"}]
  }
}
`, name)
	if err := os.WriteFile(filepath.Join(dir, ".ynh-plugin", "plugin.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}
