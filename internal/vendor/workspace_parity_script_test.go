package vendor

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runWorkspaceParity(root, script string) (string, error) {
	out, err := exec.Command("bash", script, root, "claude", "codex", "cursor", "copilot").CombinedOutput()
	return string(out), err
}

// TestWorkspaceParityScript: the script passes on the real sources and fails
// when a vendor loses its declaration, declares neither a flag nor a reason, or
// declares both.
func TestWorkspaceParityScript(t *testing.T) {
	root, hookScript := parityRoot(t)
	script := filepath.Join(filepath.Dir(hookScript), "workspace-parity.sh")
	if out, err := runWorkspaceParity(root, script); err != nil {
		t.Fatalf("unmodified sources must pass: %v\n%s", err, out)
	}
	tests := []struct {
		name    string
		edit    func(root string)
		wantMsg string
	}{
		{"declaration removed", func(r string) {
			editCopy(t, r, "internal/vendor/claude.go", "var claudeWorkspaceFlag = \"--add-dir\"\n", "")
		}, "claude: must declare <x>WorkspaceFlag"},
		{"neither flag nor reason", func(r string) {
			editCopy(t, r, "internal/vendor/codex.go", "var codexWorkspaceFlag = \"--add-dir\"", "var codexWorkspaceFlag = \"\"")
		}, "codex: no workspace flag and no reason"},
		{"both", func(r string) {
			editCopy(t, r, "internal/vendor/cursor.go", "var cursorWorkspaceFlag = \"\"", "var cursorWorkspaceFlag = \"--add-dir\"")
		}, "CONFLICT cursor"},
		{"a reason instead of a flag passes", func(r string) {
			editCopy(t, r, "internal/vendor/copilot.go", "var copilotWorkspaceFlag = \"--add-dir\"", "var copilotWorkspaceFlag = \"\"")
			editCopy(t, r, "internal/vendor/copilot.go", "var copilotWorkspaceUnsupported = \"\"", "var copilotWorkspaceUnsupported = \"no flag\"")
		}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, s := parityRoot(t)
			tc.edit(r)
			out, err := runWorkspaceParity(r, filepath.Join(filepath.Dir(s), "workspace-parity.sh"))
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
