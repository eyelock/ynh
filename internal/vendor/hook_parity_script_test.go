package vendor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// parityRoot copies what scripts/hook-event-parity.sh reads into a temp
// directory, so a test can break a mapping there. The real sources and the real
// script are never modified.
func parityRoot(t *testing.T) (root, script string) {
	t.Helper()
	for _, tool := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	root = t.TempDir()
	files := []string{
		"docs/schema/plugin.schema.json",
		"docs/schema/harness.schema.json",
		"internal/plugin/plugin.go",
	}
	for _, v := range []string{"claude", "codex", "cursor", "copilot"} {
		files = append(files, "internal/vendor/"+v+".go")
	}
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(repo, f))
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, filepath.Join(repo, "scripts", "hook-event-parity.sh")
}

func runParity(root, script string) (string, error) {
	out, err := exec.Command("bash", script, root, "claude", "codex", "cursor", "copilot").CombinedOutput()
	return string(out), err
}

// editCopy rewrites one file of the temp copy.
func editCopy(t *testing.T, root, rel, old, repl string) {
	t.Helper()
	path := filepath.Join(root, rel)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s does not contain %q", rel, old)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), old, repl, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestHookEventParityScript: the script passes on the real sources and fails
// when a vendor loses a mapping, when a declaration has no reason, and when the
// schema gains an event no vendor knows.
func TestHookEventParityScript(t *testing.T) {
	root, script := parityRoot(t)
	if out, err := runParity(root, script); err != nil {
		t.Fatalf("unmodified sources must pass: %v\n%s", err, out)
	}

	tests := []struct {
		name    string
		edit    func()
		wantMsg string
	}{
		{"mapping removed", func() {
			editCopy(t, root, "internal/vendor/cursor.go", "\t\"on_session_end\":   \"sessionEnd\",\n", "")
		}, "MISSING  cursor: on_session_end is neither mapped nor declared unsupported"},
		{"declared without a reason", func() {
			editCopy(t, root, "internal/vendor/copilot.go", "\t\"on_session_end\":   \"SessionEnd\",\n", "")
			editCopy(t, root, "internal/vendor/copilot.go", "var copilotUnsupportedHookEvents = map[string]string{}", "var copilotUnsupportedHookEvents = map[string]string{\n\t\"on_session_end\": \"\",\n}")
		}, "copilot: on_session_end is declared unsupported without a reason"},
		{"declared with a reason passes", func() {
			editCopy(t, root, "internal/vendor/copilot.go", "\t\"on_session_end\":   \"SessionEnd\",\n", "")
			editCopy(t, root, "internal/vendor/copilot.go", "var copilotUnsupportedHookEvents = map[string]string{}", "var copilotUnsupportedHookEvents = map[string]string{\n\t\"on_session_end\": \"no such event\",\n}")
		}, ""},
		{"timeout declaration removed", func() {
			editCopy(t, root, "internal/vendor/cursor.go", "var cursorHookTimeoutField = \"timeout\"\n", "")
		}, "cursor: must declare <x>HookTimeoutField"},
		{"no timeout field and no reason", func() {
			editCopy(t, root, "internal/vendor/codex.go", "var codexHookTimeoutField = \"timeout\"", "var codexHookTimeoutField = \"\"")
		}, "codex: no timeout field and no reason"},
		{"unsupported timeout with a reason passes", func() {
			editCopy(t, root, "internal/vendor/codex.go", "var codexHookTimeoutField = \"timeout\"", "var codexHookTimeoutField = \"\"")
			editCopy(t, root, "internal/vendor/codex.go", "var codexHookTimeoutUnsupported = \"\"", "const codexHookTimeoutUnsupported = \"no such field\"")
		}, ""},
		{"new event in the schema only", func() {
			editCopy(t, root, "docs/schema/plugin.schema.json", "\"on_session_end\"", "\"on_session_end\", \"on_compact\"")
		}, "on_compact is neither mapped nor declared unsupported"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, s := parityRoot(t)
			root = r // edit closures act on the fresh copy
			tc.edit()
			out, err := runParity(r, s)
			if tc.wantMsg == "" {
				if err != nil {
					t.Fatalf("want pass, got %v\n%s", err, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("want failure, got success\n%s", out)
			}
			if !strings.Contains(out, tc.wantMsg) {
				t.Errorf("output lacks %q:\n%s", tc.wantMsg, out)
			}
		})
	}
}
