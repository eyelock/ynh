package vendor

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runSubagentParity(root, script string) (string, error) {
	out, err := exec.Command("bash", script, root, "claude", "codex", "cursor", "copilot").CombinedOutput()
	return string(out), err
}

// TestSubagentParityScript: the script passes on the real sources and fails
// when a vendor loses its declaration or hides the answer behind logic.
func TestSubagentParityScript(t *testing.T) {
	root, hookScript := parityRoot(t)
	script := filepath.Join(filepath.Dir(hookScript), "subagent-parity.sh")
	if out, err := runSubagentParity(root, script); err != nil {
		t.Fatalf("unmodified sources must pass: %v\n%s", err, out)
	}
	tests := []struct {
		name    string
		edit    func(root string)
		wantMsg string
	}{
		{"declaration removed", func(r string) {
			editCopy(t, r, "internal/vendor/claude.go", "func (c *Claude) SupportsNativeSubagents() bool { return true }", "")
		}, "claude: must declare SupportsNativeSubagents exactly once"},
		{"declared twice", func(r string) {
			editCopy(t, r, "internal/vendor/codex.go", "func (c *Codex) SupportsNativeSubagents() bool { return true }",
				"func (c *Codex) SupportsNativeSubagents() bool { return true }\nfunc (c *Codex) SupportsNativeSubagents() bool { return true }")
		}, "codex: must declare SupportsNativeSubagents exactly once"},
		{"not a bare answer", func(r string) {
			editCopy(t, r, "internal/vendor/cursor.go", "func (c *Cursor) SupportsNativeSubagents() bool { return false }",
				"func (c *Cursor) SupportsNativeSubagents() bool { return cursorHasThem() }")
		}, "cursor: SupportsNativeSubagents must return a bare true or false"},
		{"an answer changed still passes", func(r string) {
			editCopy(t, r, "internal/vendor/cursor.go", "SupportsNativeSubagents() bool { return false }", "SupportsNativeSubagents() bool { return true }")
		}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, s := parityRoot(t)
			tc.edit(r)
			out, err := runSubagentParity(r, filepath.Join(filepath.Dir(s), "subagent-parity.sh"))
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
				t.Errorf("want %q in:\n%s", tc.wantMsg, out)
			}
		})
	}
}
