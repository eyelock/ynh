package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/eyelock/ynh/internal/config"
)

// #920: the settings file left denyRead empty, so under --sandbox srt a worker
// with --auto-approve all could read ~/.ssh, cloud credentials and ynh's own
// trust record.
func TestSrtReadPolicy_DefaultsNameCredentialPaths(t *testing.T) {
	home := t.TempDir()
	ynhHome := t.TempDir()
	t.Setenv("YNH_HOME", ynhHome)
	got := SrtReadPolicy(&config.Config{}, []string{"HOME=" + home})
	for _, want := range []string{
		filepath.Join(home, ".ssh"),
		filepath.Join(home, ".aws"),
		filepath.Join(home, ".config", "gh"),
		filepath.Join(home, "Library", "Keychains"),
		filepath.Join(ynhHome, "trust.json"),
	} {
		if !slices.Contains(got, want) {
			t.Errorf("default deny list lacks %s: %q", want, got)
		}
	}
}

func TestSrtReadPolicy_ConfigExtendsAndOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", t.TempDir())
	env := []string{"HOME=" + home}
	cfg := &config.Config{
		SrtDenyRead:  []string{"~/.kube", "/srv/secrets", "~/.kube"},
		SrtAllowRead: []string{"~/.ssh", "~/.config/gh/"},
	}
	got := SrtReadPolicy(cfg, env)
	for _, want := range []string{filepath.Join(home, ".kube"), "/srv/secrets", filepath.Join(home, ".aws")} {
		if !slices.Contains(got, want) {
			t.Errorf("deny list lacks %s: %q", want, got)
		}
	}
	for _, gone := range []string{filepath.Join(home, ".ssh"), filepath.Join(home, ".config", "gh")} {
		if slices.Contains(got, gone) {
			t.Errorf("srt_allow_read did not remove %s: %q", gone, got)
		}
	}
	if n := countOf(got, filepath.Join(home, ".kube")); n != 1 {
		t.Errorf("~/.kube listed %d times, want 1", n)
	}
	if got := SrtReadPolicy(nil, env); got == nil || len(got) == 0 {
		t.Errorf("nil config must still give the defaults, got %q", got)
	}
}

func countOf(s []string, v string) int {
	n := 0
	for _, x := range s {
		if x == v {
			n++
		}
	}
	return n
}

// The settings file srt is given carries the deny list, the loop's when it
// resolved one and ynh's defaults when it did not.
func TestSrtCommand_WritesDenyRead(t *testing.T) {
	fakeProgram(t, "srt", "", "", 0)
	fakeVendor(t, "claude", "", "", 0)
	home := resolvedTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", resolvedTempDir(t))

	run := func(deny []string) srtSettings {
		session := t.TempDir()
		sess, err := (&ClaudeBackend{}).Start(context.Background(), StartOptions{
			Sandbox: "srt", WorktreeDir: t.TempDir(), SessionDir: session, SrtDenyRead: deny,
		})
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		_ = sess.Close()
		return readSrtSettings(t, filepath.Join(session, srtSettingsFile))
	}

	if s := run(nil); !slices.Contains(s.Filesystem.DenyRead, filepath.Join(home, ".ssh")) {
		t.Errorf("default denyRead lacks ~/.ssh: %q", s.Filesystem.DenyRead)
	}
	if s := run([]string{"/srv/only"}); !slices.Equal(s.Filesystem.DenyRead, []string{"/srv/only"}) {
		t.Errorf("denyRead = %q, want exactly the loop's list", s.Filesystem.DenyRead)
	}
	if s := run([]string{}); len(s.Filesystem.DenyRead) != 0 {
		t.Errorf("an empty list must deny nothing, got %q", s.Filesystem.DenyRead)
	}
}

// With srt installed, a worker under the generated settings cannot read a
// fixture credential and can still read the worktree. Skipped where srt is
// not installed (macOS and Linux CI runners that have it run it).
func TestSrt_DeniesCredentialRead(t *testing.T) {
	srtBin, err := exec.LookPath("srt")
	if err != nil {
		t.Skip("srt is not installed")
	}
	home := resolvedTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", filepath.Join(home, ".ynh"))
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(home, ".ssh", "id_fixture")
	if err := os.WriteFile(key, []byte("not a key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	worktree := resolvedTempDir(t)
	ok := filepath.Join(worktree, "readme")
	if err := os.WriteFile(ok, []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureSrtTmpdir(srtTmpdir); err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	path, err := writeSrtSettings(dir, buildSrtSettings(worktree, filepath.Join(dir, srtSettingsFile),
		srtPolicy{denyRead: SrtReadPolicy(&config.Config{}, []string{"HOME=" + home})}))
	if err != nil {
		t.Fatal(err)
	}
	cat := func(file string) error {
		c := exec.Command(srtBin, "--settings", path, "--", "cat", file)
		c.Dir = worktree
		return c.Run()
	}
	if err := cat(ok); err != nil {
		t.Skipf("srt cannot run a command here: %v", err)
	}
	if err := cat(key); err == nil {
		t.Errorf("srt let the worker read %s", key)
	}
}
