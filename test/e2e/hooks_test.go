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
		absent   string // relative to runDir; a hook file the vendor never reads in a project
	}{
		{vendor: "claude", hookFile: filepath.Join(".claude", "hooks", "hooks.json")},
		{vendor: "codex", hookFile: filepath.Join(".codex", "hooks.json")},
		// hooks/hooks.json is the Cursor plugin path; a project session reads
		// only .cursor/hooks.json, so the run assembly must not carry it (#454).
		{vendor: "cursor", hookFile: filepath.Join(".cursor", "hooks.json"), absent: "hooks"},
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
			mustRunYnhInDir(t, s, project, "run", "local/"+name, "-v", tc.vendor, "--install")

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
			if tc.absent != "" {
				if _, err := os.Stat(filepath.Join(runDir, tc.absent)); !os.IsNotExist(err) {
					t.Errorf("%s must not be assembled for %s, stat err = %v", tc.absent, tc.vendor, err)
				}
			}
		})
	}
}

// TestHooks_SessionScripts verifies that `ynh run` carries the scripts a hook
// runs by a "./" path into the run directory, executable, and that each
// vendor's session hook command reaches the copy (#495): Claude through
// ${CLAUDE_PLUGIN_ROOT}, the .claude/ plugin root; Codex and Cursor through
// the bare "./", since they run hooks from the run directory.
func TestHooks_SessionScripts(t *testing.T) {
	cases := []struct {
		vendor   string
		hookFile string
		command  string
		script   string
	}{
		{"claude", ".claude/hooks/hooks.json", `"${CLAUDE_PLUGIN_ROOT}"/scripts/guard.sh --strict`, ".claude/scripts/guard.sh"},
		{"codex", ".codex/hooks.json", "./scripts/guard.sh --strict", "scripts/guard.sh"},
		{"cursor", ".cursor/hooks.json", "./scripts/guard.sh --strict", "scripts/guard.sh"},
	}
	for _, tc := range cases {
		t.Run(tc.vendor, func(t *testing.T) {
			s := newSandbox(t)
			name := fmt.Sprintf("scripted-%s", tc.vendor)
			harness := filepath.Join(t.TempDir(), name)
			if err := os.MkdirAll(filepath.Join(harness, ".agents/harness"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(harness, "scripts"), 0o755); err != nil {
				t.Fatal(err)
			}
			body := fmt.Sprintf(`{"name": %q, "version": "0.1.0", "hooks": {"on_stop": [{"command": "./scripts/guard.sh --strict"}]}}`, name)
			if err := os.WriteFile(filepath.Join(harness, ".agents/harness", "plugin.json"), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(harness, "scripts", "guard.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			s.mustRunYnh(t, "install", harness)

			project := filepath.Join(t.TempDir(), "project")
			if err := os.MkdirAll(project, 0o755); err != nil {
				t.Fatal(err)
			}
			mustRunYnhInDir(t, s, project, "run", "local/"+name, "-v", tc.vendor, "--install")

			runDir := filepath.Join(s.home, "run", name)
			hooks, err := os.ReadFile(filepath.Join(runDir, filepath.FromSlash(tc.hookFile)))
			if err != nil {
				t.Fatalf("expected hook file %s: %v", tc.hookFile, err)
			}
			var cmds []string
			collectCommands(t, hooks, &cmds)
			if len(cmds) != 1 || cmds[0] != tc.command {
				t.Errorf("session hook commands = %q, want [%q]", cmds, tc.command)
			}
			info, err := os.Stat(filepath.Join(runDir, filepath.FromSlash(tc.script)))
			if err != nil {
				t.Fatalf("hook script not in the run dir: %v", err)
			}
			if info.Mode().Perm()&0o111 == 0 {
				t.Errorf("hook script mode = %v, want executable", info.Mode())
			}
		})
	}
}

// collectCommands appends every "command" string in a hook document.
func collectCommands(t *testing.T, data []byte, out *[]string) {
	t.Helper()
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("hook file is not valid JSON: %v\n%s", err, data)
	}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if c, ok := x["command"].(string); ok {
				*out = append(*out, c)
			}
			for _, e := range x {
				walk(e)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(doc)
}

func newHookedHarness(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Join(dir, ".agents/harness"), 0o755); err != nil {
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
	if err := os.WriteFile(filepath.Join(dir, ".agents/harness", "plugin.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestHooks_Export verifies that an exported plugin carries each vendor's hooks
// where that vendor's plugin loader reads them: hooks/<vendor>.json, named by
// the vendor's manifest "hooks" field, in a per-vendor or a merged export.
// No hooks/hooks.json (Claude Code always loads it from a plugin root, so it
// could not hold another vendor's format in a shared root) and no session
// paths, which a plugin never reads (#468, #469).
func TestHooks_Export(t *testing.T) {
	harness := newHookedHarness(t, "hooked-export")
	manifests := map[string]string{
		"claude": ".claude-plugin/plugin.json",
		"codex":  ".codex-plugin/plugin.json",
		"cursor": ".cursor-plugin/plugin.json",
	}
	sessionPaths := []string{"hooks/hooks.json", ".claude/hooks", ".codex/hooks.json", ".cursor/hooks.json"}

	check := func(t *testing.T, root string, vendors []string) {
		t.Helper()
		for _, v := range vendors {
			var m struct {
				Hooks string `json:"hooks"`
			}
			body, err := os.ReadFile(filepath.Join(root, manifests[v]))
			if err != nil {
				t.Fatalf("%s manifest: %v", v, err)
			}
			if err := json.Unmarshal(body, &m); err != nil {
				t.Fatalf("%s manifest: %v", v, err)
			}
			if want := "./hooks/" + v + ".json"; m.Hooks != want {
				t.Errorf("%s manifest hooks = %q, want %q", v, m.Hooks, want)
			}
			hooks, err := os.ReadFile(filepath.Join(root, "hooks", v+".json"))
			if err != nil {
				t.Fatalf("%s hooks file: %v", v, err)
			}
			if !bytes.Contains(hooks, []byte("echo hooked")) {
				t.Errorf("hook command not present in hooks/%s.json:\n%s", v, hooks)
			}
		}
		for _, p := range sessionPaths {
			if _, err := os.Stat(filepath.Join(root, p)); !os.IsNotExist(err) {
				t.Errorf("%s must not be in a plugin export, stat err = %v", p, err)
			}
		}
	}

	t.Run("per vendor", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "out")
		mustRunYnd(t, "export", harness, "-v", "claude,codex,cursor", "-o", out)
		for _, v := range []string{"claude", "codex", "cursor"} {
			check(t, filepath.Join(out, v), []string{v})
		}
	})
	t.Run("merged", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "out")
		mustRunYnd(t, "export", harness, "--merged", "-o", out)
		check(t, out, []string{"claude", "codex", "cursor"})
	})
}
