package runsnap

import (
	"os"
	"path/filepath"
	"testing"
)

func mkSnap(t *testing.T, files map[string]string) string {
	t.Helper()
	d := t.TempDir()
	for p, c := range files {
		full := filepath.Join(d, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func TestWorkDirStablePathFollowsNewestSnapshot(t *testing.T) {
	root := t.TempDir()
	s1 := mkSnap(t, map[string]string{"AGENTS.md": "one", ".codex/hooks.json": "h1", ".codex/config.toml": "c"})
	w1, err := WorkDir(root, "local--x", s1, ".codex")
	if err != nil {
		t.Fatal(err)
	}
	s2 := mkSnap(t, map[string]string{"AGENTS.md": "two", ".codex/hooks.json": "h2", "extra.md": "e"})
	w2, err := WorkDir(root, "local--x", s2, ".codex")
	if err != nil {
		t.Fatal(err)
	}
	if w1 != w2 {
		t.Fatalf("working folder moved: %s vs %s", w1, w2)
	}
	if fi, _ := os.Lstat(w2); fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("working folder must be a real folder")
	}
	if b, _ := os.ReadFile(filepath.Join(w2, "AGENTS.md")); string(b) != "two" {
		t.Errorf("AGENTS.md = %q, want two", b)
	}
	if fi, _ := os.Lstat(filepath.Join(w2, ".codex")); !fi.IsDir() {
		t.Error(".codex must be a real folder of links")
	}
	if fi, _ := os.Lstat(filepath.Join(w2, ".codex", "hooks.json")); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("hooks.json must be a link")
	}
	if b, _ := os.ReadFile(filepath.Join(w2, ".codex", "hooks.json")); string(b) != "h2" {
		t.Errorf("hooks.json = %q, want h2", b)
	}
	if _, err := os.Lstat(filepath.Join(w2, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Error("entry gone from the new snapshot must be gone from the working folder")
	}
	if _, err := os.Lstat(filepath.Join(w2, "extra.md")); err != nil {
		t.Error("new entry missing")
	}
	// The old snapshot is untouched.
	if b, _ := os.ReadFile(filepath.Join(s1, "AGENTS.md")); string(b) != "one" {
		t.Error("old snapshot changed")
	}
}
