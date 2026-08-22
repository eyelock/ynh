package vendor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeFileAt writes content to path (creating parents) and stamps its mtime,
// so newest-first ordering is deterministic rather than dependent on how fast
// the test runs.
func writeFileAt(t *testing.T, path, content string, mod time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeProjectSlug(t *testing.T) {
	tests := []struct {
		dir  string
		want string
	}{
		{"/Users/david/Storage/Workspace/eyelock/TermQ", "-Users-david-Storage-Workspace-eyelock-TermQ"},
		// "/." produces a double dash — verified against real store directories.
		{"/Users/david/TermQ/.worktrees/feat/resume", "-Users-david-TermQ--worktrees-feat-resume"},
		{"/tmp/a.b.c", "-tmp-a-b-c"},
		// Case is preserved.
		{"/Users/David/MyProject", "-Users-David-MyProject"},
	}

	for _, tt := range tests {
		if got := claudeProjectSlug(tt.dir); got != tt.want {
			t.Errorf("claudeProjectSlug(%q) = %q, want %q", tt.dir, got, tt.want)
		}
	}
}

func TestClaudeResolveLastSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := "/Users/david/project"
	projectDir := filepath.Join(home, ".claude", "projects", claudeProjectSlug(cwd))

	base := time.Now().Add(-time.Hour)
	writeFileAt(t, filepath.Join(projectDir, "older-session.jsonl"), "{}", base)
	writeFileAt(t, filepath.Join(projectDir, "newest-session.jsonl"), "{}", base.Add(30*time.Minute))
	// Non-session files must be ignored even when they are the newest thing there.
	writeFileAt(t, filepath.Join(projectDir, "notes.txt"), "x", base.Add(45*time.Minute))

	c := &Claude{}

	got, err := c.ResolveLastSession(cwd, time.Time{})
	if err != nil {
		t.Fatalf("ResolveLastSession: %v", err)
	}
	if got != "newest-session" {
		t.Errorf("session id = %q, want %q", got, "newest-session")
	}

	t.Run("notBefore excludes older sessions", func(t *testing.T) {
		_, err := c.ResolveLastSession(cwd, base.Add(time.Hour))
		if !errors.Is(err, ErrNoResumableSession) {
			t.Errorf("err = %v, want ErrNoResumableSession", err)
		}
	})

	t.Run("unknown directory has no session", func(t *testing.T) {
		_, err := c.ResolveLastSession("/Users/david/somewhere-else", time.Time{})
		if !errors.Is(err, ErrNoResumableSession) {
			t.Errorf("err = %v, want ErrNoResumableSession", err)
		}
	})
}

func TestClaudeLaunchResumeArgs(t *testing.T) {
	// Exercised through buildClaudeArgs, which is what LaunchResume feeds.
	configPath := t.TempDir()

	withID := buildClaudeArgs(configPath, "", []string{"--resume", "abc123"})
	if !containsPair(withID, "--resume", "abc123") {
		t.Errorf("args %v missing --resume abc123", withID)
	}

	continueLast := buildClaudeArgs(configPath, "", []string{"--continue"})
	if !contains(continueLast, "--continue") {
		t.Errorf("args %v missing --continue", continueLast)
	}
	// A bare --resume would open the session picker and hang an unattended run.
	if contains(continueLast, "--resume") {
		t.Errorf("args %v must not contain a bare --resume", continueLast)
	}
}

func TestCopilotResolveLastSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	stateDir := filepath.Join(home, ".copilot", "session-state")
	cwd := "/Users/david/project"
	base := time.Now().Add(-time.Hour)

	workspace := func(id, dir string) string {
		return "id: " + id + "\ncwd: " + dir + "\nrepository: eyelock/ynh\n"
	}

	writeFileAt(t, filepath.Join(stateDir, "old-id", "workspace.yaml"), workspace("old-id", cwd), base)
	writeFileAt(t, filepath.Join(stateDir, "new-id", "workspace.yaml"), workspace("new-id", cwd), base.Add(20*time.Minute))
	// A newer session in a *different* directory must not win.
	writeFileAt(t, filepath.Join(stateDir, "other-dir-id", "workspace.yaml"),
		workspace("other-dir-id", "/Users/david/other"), base.Add(40*time.Minute))

	// Directory mtimes drive the newest-first walk.
	for name, mod := range map[string]time.Time{
		"old-id":       base,
		"new-id":       base.Add(20 * time.Minute),
		"other-dir-id": base.Add(40 * time.Minute),
	} {
		if err := os.Chtimes(filepath.Join(stateDir, name), mod, mod); err != nil {
			t.Fatal(err)
		}
	}

	c := &Copilot{}

	got, err := c.ResolveLastSession(cwd, time.Time{})
	if err != nil {
		t.Fatalf("ResolveLastSession: %v", err)
	}
	if got != "new-id" {
		t.Errorf("session id = %q, want %q", got, "new-id")
	}

	t.Run("directory with no sessions", func(t *testing.T) {
		_, err := c.ResolveLastSession("/Users/david/untouched", time.Time{})
		if !errors.Is(err, ErrNoResumableSession) {
			t.Errorf("err = %v, want ErrNoResumableSession", err)
		}
	})

	t.Run("empty store", func(t *testing.T) {
		emptyHome := t.TempDir()
		t.Setenv("HOME", emptyHome)
		_, err := c.ResolveLastSession(cwd, time.Time{})
		if !errors.Is(err, ErrNoResumableSession) {
			t.Errorf("err = %v, want ErrNoResumableSession", err)
		}
	})
}

func TestReadCopilotWorkspace(t *testing.T) {
	dir := t.TempDir()

	t.Run("quoted values and nested keys", func(t *testing.T) {
		path := filepath.Join(dir, "quoted.yaml")
		writeFileAt(t, path, "id: \"abc\"\ncwd: '/Users/david/p'\nnested:\n  cwd: /wrong\n", time.Now())

		id, cwd, err := readCopilotWorkspace(path)
		if err != nil {
			t.Fatalf("readCopilotWorkspace: %v", err)
		}
		if id != "abc" {
			t.Errorf("id = %q, want %q", id, "abc")
		}
		// An indented key belongs to a nested mapping and must not be read.
		if cwd != "/Users/david/p" {
			t.Errorf("cwd = %q, want %q", cwd, "/Users/david/p")
		}
	})

	t.Run("missing cwd is an error", func(t *testing.T) {
		path := filepath.Join(dir, "no-cwd.yaml")
		writeFileAt(t, path, "id: abc\n", time.Now())

		if _, _, err := readCopilotWorkspace(path); err == nil {
			t.Error("expected an error when cwd is absent")
		}
	})

	t.Run("missing file is an error", func(t *testing.T) {
		if _, _, err := readCopilotWorkspace(filepath.Join(dir, "absent.yaml")); err == nil {
			t.Error("expected an error for a missing file")
		}
	})
}

// Codex and Cursor cannot resolve ids locally, but must report that distinctly
// from "the store is empty" — the two lead to different launch fallbacks.
func TestLookupUnavailableVendors(t *testing.T) {
	for _, a := range []Adapter{&Codex{}, &Cursor{}} {
		t.Run(a.Name(), func(t *testing.T) {
			if !a.SupportsResume() {
				t.Error("SupportsResume() = false, want true")
			}
			_, err := a.ResolveLastSession("/anywhere", time.Time{})
			if !errors.Is(err, ErrSessionLookupUnavailable) {
				t.Errorf("err = %v, want ErrSessionLookupUnavailable", err)
			}
		})
	}
}

func TestAllVendorsSupportResume(t *testing.T) {
	for _, name := range Available() {
		a, err := Get(name)
		if err != nil {
			t.Fatalf("Get(%q): %v", name, err)
		}
		if !a.SupportsResume() {
			t.Errorf("%s: SupportsResume() = false, want true", name)
		}
	}
}

func TestNewestCandidate(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	candidates := []sessionCandidate{
		{id: "old", modTime: base},
		{id: "new", modTime: base.Add(time.Minute)},
	}

	got, err := newestCandidate(candidates, time.Time{})
	if err != nil {
		t.Fatalf("newestCandidate: %v", err)
	}
	if got != "new" {
		t.Errorf("id = %q, want %q", got, "new")
	}

	if _, err := newestCandidate(nil, time.Time{}); !errors.Is(err, ErrNoResumableSession) {
		t.Errorf("err = %v, want ErrNoResumableSession", err)
	}

	if _, err := newestCandidate(candidates, base.Add(time.Hour)); !errors.Is(err, ErrNoResumableSession) {
		t.Errorf("err = %v, want ErrNoResumableSession for an exhausted notBefore", err)
	}
}

func TestSameDir(t *testing.T) {
	if !sameDir("/a/b", "/a/b/") {
		t.Error("trailing slash should not change the directory")
	}
	if sameDir("/a/b", "/a/c") {
		t.Error("different directories compared equal")
	}
}

func contains(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func containsPair(args []string, first, second string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == first && args[i+1] == second {
			return true
		}
	}
	return false
}

// Regression: a shell reports the logical path (/tmp/x) while the vendor CLI
// resolves it before recording (/private/tmp/x on macOS). Resolution must find
// the session through that mismatch — this is the failure that reached manual
// testing, where ynh looked under "-tmp-resume-test" for a session Claude had
// filed under "-private-tmp-resume-test".
func TestClaudeResolveLastSession_ThroughSymlinkedCwd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	realDir := filepath.Join(t.TempDir(), "real-project")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(t.TempDir(), "linked-project")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	// The vendor records the resolved path...
	resolved, err := filepath.EvalSymlinks(realDir)
	if err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(home, ".claude", "projects", claudeProjectSlug(resolved))
	writeFileAt(t, filepath.Join(store, "recorded-session.jsonl"), "{}", time.Now())

	// ...while ynh is handed the symlinked one.
	got, err := (&Claude{}).ResolveLastSession(linkDir, time.Time{})
	if err != nil {
		t.Fatalf("ResolveLastSession through a symlinked cwd: %v", err)
	}
	if got != "recorded-session" {
		t.Errorf("session id = %q, want %q", got, "recorded-session")
	}
}

func TestDirCandidates(t *testing.T) {
	// A path with no symlink component yields exactly one candidate. Resolve
	// first: t.TempDir() itself sits under a symlink on macOS (/var ->
	// /private/var), so the raw value would legitimately produce two.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := dirCandidates(root); len(got) != 1 {
		t.Errorf("dirCandidates(%q) = %v, want a single entry", root, got)
	}

	// A symlinked path yields the resolved form first, then the literal one.
	target := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	got := dirCandidates(link)
	if len(got) != 2 {
		t.Fatalf("dirCandidates(%q) = %v, want two entries", link, got)
	}
	resolved, _ := filepath.EvalSymlinks(target)
	if got[0] != resolved {
		t.Errorf("first candidate = %q, want the resolved path %q", got[0], resolved)
	}
	if got[1] != link {
		t.Errorf("second candidate = %q, want the literal path %q", got[1], link)
	}
}
