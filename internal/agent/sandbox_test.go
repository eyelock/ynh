package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

// `--sandbox srt` was read only by the Claude backend; codex and cursor
// contained no reference to it. `--sandbox srt --backend codex` therefore ran
// completely unsandboxed and reported nothing — the operator believed a
// containment control was in force that was never applied.
func TestValidateSandbox(t *testing.T) {
	cases := []struct {
		sandbox, backend string
		wantErr          bool
		why              string
	}{
		{"", "claude", false, "unset means none"},
		{"none", "codex", false, "explicitly unsandboxed is a choice, not a failure"},
		{"srt", "claude", false, "claude implements srt"},
		{"srt", "codex", false, "codex runs under srt"},
		{"srt", "cursor", false, "cursor runs under srt"},
		{"srt", "opencode", runtime.GOOS != "linux", "opencode runs under srt on Linux and stays refused on macOS (L18)"},
		{"docker", "claude", true, "an unknown sandbox is not silently ignored"},
	}
	for _, c := range cases {
		err := validateSandbox(c.sandbox, c.backend)
		if c.wantErr && err == nil {
			t.Errorf("validateSandbox(%q, %q) = nil, want error — %s", c.sandbox, c.backend, c.why)
		}
		if !c.wantErr && err != nil {
			t.Errorf("validateSandbox(%q, %q) = %v, want nil — %s", c.sandbox, c.backend, err, c.why)
		}
	}
}

// The error has to tell the operator what to do instead, or it just blocks.
func TestValidateSandbox_ErrorIsActionable(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("opencode is allowed under srt on Linux")
	}
	err := validateSandbox("srt", "opencode")
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"opencode", "container", "--sandbox none"} {
		if !contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func TestEnsureSrtTmpdir_CreatesPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "claude")
	if err := ensureSrtTmpdir(dir); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
		t.Fatalf("want a 0700 directory, got %v %v", fi, err)
	}
	if err := ensureSrtTmpdir(dir); err != nil {
		t.Errorf("an owned, private directory that exists must be accepted: %v", err)
	}
}

func TestEnsureSrtTmpdir_RefusesWritableByOthers(t *testing.T) {
	for _, mode := range []os.FileMode{0o770, 0o707, 0o777, 0o720} {
		dir := filepath.Join(t.TempDir(), "claude")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		err := ensureSrtTmpdir(dir)
		if err == nil || !strings.Contains(err.Error(), "written by other users") {
			t.Errorf("mode %04o: want a refusal, got %v", mode, err)
		}
		if fi, _ := os.Stat(dir); fi.Mode().Perm() != mode {
			t.Errorf("mode %04o: the directory must not be changed, now %04o", mode, fi.Mode().Perm())
		}
	}
}

func TestEnsureSrtTmpdir_RefusesLinkAndFile(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "elsewhere")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{link, file} {
		if err := ensureSrtTmpdir(p); err == nil || !strings.Contains(err.Error(), "not a directory") {
			t.Errorf("%s: want a refusal, got %v", p, err)
		}
	}
}

// Another user's directory is refused. Only root can make one, so this runs
// where the tests run as root (a container), and is skipped elsewhere.
func TestEnsureSrtTmpdir_RefusesOtherOwner(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("needs root to create a directory owned by another user")
	}
	dir := filepath.Join(t.TempDir(), "claude")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(dir, 12345, 12345); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(dir); fi.Sys().(*syscall.Stat_t).Uid != 12345 {
		t.Skip("chown did not take")
	}
	if err := ensureSrtTmpdir(dir); err == nil || !strings.Contains(err.Error(), "owned by another user") {
		t.Errorf("want a refusal, got %v", err)
	}
}

// srtCommand is every backend's way into srt, so it is where an unsafe
// /tmp/claude stops a run, before srt starts.
func TestSrtCommand_RefusesUnsafeTmpdir(t *testing.T) {
	fakeProgram(t, "srt", "", "", 0)
	dir := filepath.Join(t.TempDir(), "claude")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	old := srtTmpdir
	srtTmpdir = dir
	t.Cleanup(func() { srtTmpdir = old })
	_, _, err := srtCommand(t.Context(), StartOptions{WorktreeDir: t.TempDir()}, srtPolicy{}, "claude", nil)
	if err == nil || !strings.Contains(err.Error(), dir) {
		t.Fatalf("want a refusal naming %s, got %v", dir, err)
	}
}
