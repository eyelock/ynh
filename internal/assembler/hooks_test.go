package assembler

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/vendor"
)

// sessionHookHarness writes a harness directory with one executable hook
// script, scripts/guard.sh, and returns it with hooks that run it alongside
// commands that are not harness scripts.
func sessionHookHarness(t *testing.T) (string, map[string][]plugin.HookEntry) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "scripts", "guard.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	hooks := map[string][]plugin.HookEntry{
		"on_stop": {
			{Command: "./scripts/guard.sh --strict"},
			{Command: "/usr/local/bin/lint.sh"},
			{Command: "make check"},
			{Command: "$CLAUDE_PROJECT_DIR/tools/x.sh"},
		},
	}
	return dir, hooks
}

// sessionHookCommands returns every "command" value in a hook document.
func sessionHookCommands(t *testing.T, data []byte) []string {
	t.Helper()
	var cmds []string
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if c, ok := x["command"].(string); ok {
				cmds = append(cmds, c)
			}
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(x[k])
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parsing hook file: %v", err)
	}
	walk(doc)
	return cmds
}

// filesUnder lists every regular file under root, relative and slash-separated.
func filesUnder(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		found = append(found, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	sort.Strings(found)
	return found
}

// TestWriteSessionHooks: a "./" hook command names a script the harness ships
// (#495). Session assembly copies it into the run directory, keeping its mode,
// and the session hook command resolves to the copy: Claude reads the session
// hooks as a --plugin-dir plugin rooted at .claude/, so the script goes there
// under ${CLAUDE_PLUGIN_ROOT}; Cursor and Codex run hooks from the run
// directory, where the session starts, so the bare "./" reaches the copy at
// the run directory's root. Other commands are not harness scripts and are
// written exactly as declared.
func TestWriteSessionHooks(t *testing.T) {
	untouched := []string{"/usr/local/bin/lint.sh", "make check", "$CLAUDE_PROJECT_DIR/tools/x.sh"}
	tests := []struct {
		vendor   string
		hookFile string
		guard    string // the session command for ./scripts/guard.sh --strict
		root     string // where its "./" or variable resolves, relative to the run dir
	}{
		{"claude", ".claude/hooks/hooks.json", `"${CLAUDE_PLUGIN_ROOT}"/scripts/guard.sh --strict`, ".claude"},
		{"codex", ".codex/hooks.json", "./scripts/guard.sh --strict", "."},
		{"cursor", ".cursor/hooks.json", "./scripts/guard.sh --strict", "."},
	}
	for _, tt := range tests {
		t.Run(tt.vendor, func(t *testing.T) {
			harnessDir, hooks := sessionHookHarness(t)
			runDir := t.TempDir()
			adapter, err := vendor.Get(tt.vendor)
			if err != nil {
				t.Fatal(err)
			}

			warnings, err := WriteSessionHooks(runDir, adapter, harnessDir, HookSet{Hooks: hooks})
			if err != nil {
				t.Fatal(err)
			}
			if len(warnings) != 0 {
				t.Errorf("warnings = %q, want none", warnings)
			}
			data, err := os.ReadFile(filepath.Join(runDir, filepath.FromSlash(tt.hookFile)))
			if err != nil {
				t.Fatalf("session hook file: %v", err)
			}

			want := append([]string{tt.guard}, untouched...)
			got := sessionHookCommands(t, data)
			sort.Strings(got)
			sort.Strings(want)
			if !slices.Equal(got, want) {
				t.Errorf("commands = %q, want %q", got, want)
			}

			// The command's script resolves to an executable copy.
			script := strings.Fields(tt.guard)[0]
			script = strings.TrimPrefix(script, `"${CLAUDE_PLUGIN_ROOT}"/`)
			script = strings.TrimPrefix(script, "./")
			copied := filepath.Join(runDir, tt.root, filepath.FromSlash(script))
			info, err := os.Stat(copied)
			if err != nil {
				t.Fatalf("hook script not in run dir: %v", err)
			}
			if info.Mode().Perm()&0o111 == 0 {
				t.Errorf("copied script mode = %v, want executable", info.Mode())
			}

			// Exactly the hook file and the script, nothing else.
			wantFiles := []string{tt.hookFile, filepath.ToSlash(filepath.Join(tt.root, "scripts", "guard.sh"))}
			sort.Strings(wantFiles)
			if got := filesUnder(t, runDir); !slices.Equal(got, wantFiles) {
				t.Errorf("run dir files = %q, want %q", got, wantFiles)
			}
		})
	}
}

// TestWriteSessionHooks_Copilot: Copilot emits no session hooks, so it needs
// none of their scripts.
func TestWriteSessionHooks_Copilot(t *testing.T) {
	harnessDir, hooks := sessionHookHarness(t)
	runDir := t.TempDir()
	adapter, err := vendor.Get("copilot")
	if err != nil {
		t.Fatal(err)
	}
	warnings, err := WriteSessionHooks(runDir, adapter, harnessDir, HookSet{Hooks: hooks})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %q, want none", warnings)
	}
	if got := filesUnder(t, runDir); len(got) != 0 {
		t.Errorf("run dir files = %q, want none", got)
	}
}

// TestWriteSessionHooks_Warnings: a "./" script that is not a file in the
// harness, or climbs out of it, is not copied, and the session warns, naming
// it. The hook itself is still written.
func TestWriteSessionHooks_Warnings(t *testing.T) {
	for _, v := range []string{"claude", "codex", "cursor"} {
		t.Run(v, func(t *testing.T) {
			base := t.TempDir()
			harnessDir := filepath.Join(base, "harness")
			// A script outside the harness, beside it, must not be copied.
			outside := filepath.Join(base, "escape.sh")
			if err := os.WriteFile(outside, []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(harnessDir, "scripts", "dir.sh"), 0o755); err != nil {
				t.Fatal(err)
			}
			hooks := map[string][]plugin.HookEntry{
				"on_stop": {
					{Command: "./scripts/missing.sh"},
					{Command: "./scripts/dir.sh"},
					{Command: "./../" + filepath.Base(outside)},
				},
			}
			runDir := t.TempDir()
			adapter, err := vendor.Get(v)
			if err != nil {
				t.Fatal(err)
			}
			warnings, err := WriteSessionHooks(runDir, adapter, harnessDir, HookSet{Hooks: hooks})
			if err != nil {
				t.Fatal(err)
			}
			if got := filesUnder(t, runDir); len(got) != 1 {
				t.Errorf("run dir files = %q, want only the hook file", got)
			}
			wantWarnings := []string{
				"hook script ./scripts/missing.sh is not a file in the harness, so the session does not carry it",
				"hook script ./scripts/dir.sh is not a file in the harness, so the session does not carry it",
				"hook script ./../" + filepath.Base(outside) + " is outside the harness, so the session cannot carry it",
			}
			sort.Strings(wantWarnings)
			sort.Strings(warnings)
			if !slices.Equal(warnings, wantWarnings) {
				t.Errorf("warnings = %q, want %q", warnings, wantWarnings)
			}
			for _, f := range filesUnder(t, runDir) {
				if strings.Contains(f, "scripts/") || strings.Contains(f, "escape") {
					t.Errorf("unexpected file in run dir: %s", f)
				}
			}
		})
	}
}

// TestCopyHookScripts_NoWriteThroughSymlink: the copy never writes through a
// symlink already at the destination, which could point anywhere. Every path
// here is under t.TempDir().
func TestCopyHookScripts_NoWriteThroughSymlink(t *testing.T) {
	harnessDir, hooks := sessionHookHarness(t)
	destDir := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim.sh")
	if err := os.WriteFile(victim, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(destDir, "scripts", "guard.sh")
	if !strings.HasPrefix(link, destDir) {
		t.Fatalf("link %s escapes the test directory", link)
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, link); err != nil {
		t.Fatal(err)
	}

	warnings, err := CopyHookScripts(harnessDir, destDir, HookSet{Hooks: hooks}, "the session")
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "./scripts/guard.sh") {
		t.Errorf("warnings = %q, want one naming ./scripts/guard.sh", warnings)
	}
	data, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "original\n" {
		t.Errorf("symlink target was overwritten: %q", data)
	}
}
