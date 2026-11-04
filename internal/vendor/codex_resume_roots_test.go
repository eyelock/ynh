package vendor

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCodexConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCodexExecResumeTranslatesAddDir(t *testing.T) {
	cfg := writeCodexConfig(t, "sandbox_mode = \"workspace-write\"\n")
	var warn bytes.Buffer
	extra := []string{"--add-dir", "/a", "-c", "x=1", "--add-dir", "/b c"}
	got := codexExecResumeExtraArgs(extra, cfg, &warn)
	want := []string{"-c", "x=1", "-c", `sandbox_workspace_write.writable_roots=["/a","/b c"]`}
	if !equalArgs(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
	if warn.Len() != 0 {
		t.Errorf("unexpected warning under workspace-write: %q", warn.String())
	}
	// Without a prompt it is the interactive `codex resume`, which keeps --add-dir.
	args, _ := codexResumeArgs("abc", "", false, []string{"--add-dir", "/a"}, &warn)
	if w := []string{"resume", "abc", "--add-dir", "/a"}; !equalArgs(args, w) {
		t.Errorf("interactive args = %v, want %v", args, w)
	}
}

func TestCodexExecResumeNoFoldersLeavesArgs(t *testing.T) {
	got := codexExecResumeExtraArgs([]string{"-c", "x=1"}, "", nil)
	if !equalArgs(got, []string{"-c", "x=1"}) {
		t.Errorf("args = %v", got)
	}
}

func TestCodexExecResumeMergesUserRoots(t *testing.T) {
	cfg := writeCodexConfig(t, "sandbox_mode = \"workspace-write\"\n[sandbox_workspace_write]\nwritable_roots = [\n  \"/u1\", # mine\n  '/u 2',\n]\nnetwork_access = true\n")
	got := codexExecResumeExtraArgs([]string{"--add-dir", "/g", "-c", `sandbox_workspace_write.writable_roots=["/c1"]`}, cfg, nil)
	want := []string{"-c", `sandbox_workspace_write.writable_roots=["/u1","/u 2","/c1","/g"]`}
	if !equalArgs(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
}

func TestCodexExecResumeWarnsOutsideWorkspaceWrite(t *testing.T) {
	tests := []struct {
		name string
		cfg  string
		args []string
		warn string
	}{
		{"unset", "", nil, "defaults to read-only"},
		{"config read-only", "sandbox_mode = \"read-only\"\n", nil, "sandbox is read-only"},
		{"flag read-only", "sandbox_mode = \"workspace-write\"\n", []string{"-s", "read-only"}, "sandbox is read-only"},
		{"-c read-only", "", []string{"-c", `sandbox_mode="read-only"`}, "sandbox is read-only"},
		{"full-auto is fine", "", []string{"--full-auto"}, ""},
		{"full access is fine", "sandbox_mode = \"danger-full-access\"\n", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var warn bytes.Buffer
			codexExecResumeExtraArgs(append([]string{"--add-dir", "/g"}, tt.args...), writeCodexConfig(t, tt.cfg), &warn)
			if tt.warn == "" && warn.Len() != 0 {
				t.Errorf("unexpected warning %q", warn.String())
			}
			if tt.warn != "" && !strings.Contains(warn.String(), tt.warn) {
				t.Errorf("warning %q lacks %q", warn.String(), tt.warn)
			}
		})
	}
}

func TestTOMLBasicStringEscapes(t *testing.T) {
	in := "a\x07b\x0bc\x00d\x7f\"e\\f\tg\nh é"
	got := tomlBasicString(in)
	want := `"a\u0007b\u000Bc\u0000d\u007F\"e\\f\tg\nh é"`
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	// It round-trips through the reader.
	back, n, ok := readTOMLString(got)
	if !ok || n != len(got) || back != in {
		t.Errorf("round trip = %q ok=%v n=%d", back, ok, n)
	}
}
