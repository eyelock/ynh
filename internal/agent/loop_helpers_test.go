package agent

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/vendor"
)

func TestDirOf(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/abs/path/file.txt", "/abs/path"},
		{"rel/path/file", "rel/path"},
		{"file", "."},
		{"", "."},
		{`win\path\file`, `win\path`},
	}
	for _, c := range cases {
		if got := dirOf(c.in); got != c.want {
			t.Errorf("dirOf(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestResolveYNHBinary_OverrideWins(t *testing.T) {
	got, err := resolveYNHBinary("/custom/ynh")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/custom/ynh" {
		t.Errorf("expected /custom/ynh, got %q", got)
	}
}

func TestResolveYNHBinary_FallsBackToExecutable(t *testing.T) {
	got, err := resolveYNHBinary("")
	if err != nil {
		t.Fatal(err)
	}
	if got == "" {
		t.Error("expected non-empty path")
	}
}

func TestOpenTrajectory_Stdout(t *testing.T) {
	var buf bytes.Buffer
	w, cleanup, err := openTrajectory("-", &buf, false)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if w == nil {
		t.Fatal("expected non-nil writer")
	}
}

func TestOpenTrajectory_File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "traj.jsonl")
	w, cleanup, err := openTrajectory(path, io.Discard, false)
	if err != nil {
		t.Fatalf("openTrajectory: %v", err)
	}
	defer cleanup()
	if w == nil {
		t.Fatal("expected non-nil writer")
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file created: %v", err)
	}
}

func TestOpenTrajectory_BadPath(t *testing.T) {
	_, _, err := openTrajectory("/no/such/dir/traj.jsonl", io.Discard, false)
	if err == nil {
		t.Error("expected error opening file in nonexistent dir")
	}
}

func TestNewSessionID_NonEmptyAndUnique(t *testing.T) {
	a := newSessionID()
	b := newSessionID()
	if a == "" || b == "" {
		t.Fatal("session id should be non-empty")
	}
	if a == b {
		t.Errorf("session ids should be unique, got %q twice", a)
	}
	if len(a) != 16 {
		t.Errorf("expected 16-char hex, got %d chars: %q", len(a), a)
	}
}

func TestAssembleHarness_UnknownBackend(t *testing.T) {
	h := &harness.Harness{Name: "x", Dir: t.TempDir()}
	_, _, err := assembleHarness(h, "no-such-vendor", harness.Selection{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "vendor") {
		t.Errorf("error should mention vendor: %v", err)
	}
}

// gitAutoCommit is FS- and exec-heavy but cheap to exercise end-to-end.
func TestGitAutoCommit_NothingToCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	// Configure committer to avoid `commit` failing in CI environments.
	_ = exec.Command("git", "-C", dir, "config", "user.email", "x@example.com").Run()
	_ = exec.Command("git", "-C", dir, "config", "user.name", "x").Run()

	// First call: nothing to commit → silently succeeds.
	if err := gitAutoCommit(dir, 1); err != nil {
		t.Errorf("expected nil on empty repo, got %v", err)
	}
}

func TestGitAutoCommit_CommitsChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "x@example.com"},
		{"config", "user.name", "x"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := gitAutoCommit(dir, 2); err != nil {
		t.Fatalf("gitAutoCommit: %v", err)
	}
	// Verify a commit landed.
	out, err := exec.Command("git", "-C", dir, "log", "--oneline").CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v: %s", err, out)
	}
	if !strings.Contains(string(out), "turn 2") {
		t.Errorf("expected 'turn 2' in log, got: %s", out)
	}
}

// The loop assembles a harness's delegates the way `ynh run` does: an agent
// file for each, carrying the delegate's own MCP servers.
func TestAssembleHarness_AssemblesDelegates(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	t.Setenv("YNH_HOME", t.TempDir())
	t.Setenv("PROBE_TOKEN", "loop-s3cret")

	del := t.TempDir()
	if err := os.MkdirAll(filepath.Join(del, ".agents", "harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"probe","version":"0.1.0","env_passthrough":["PROBE_TOKEN"],"mcp_servers":{"probe-srv":{"command":"/bin/probe","env":{"PROBE_TOKEN":"${PROBE_TOKEN}"}}}}`
	if err := os.WriteFile(filepath.Join(del, ".agents", "harness", "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}, {"add", "."}, {"commit", "-m", "init"}} {
		if out, err := exec.Command("git", append([]string{"-C", del}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	h := &harness.Harness{
		Name:        "root",
		Dir:         t.TempDir(),
		DelegatesTo: []harness.Delegate{{GitSource: harness.GitSource{Git: del}}},
	}
	dir, _, err := assembleHarness(h, "claude", harness.Selection{})
	if err != nil {
		t.Fatalf("assembleHarness: %v", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "agents", "probe.md"))
	if err != nil {
		t.Fatalf("delegate agent not assembled: %v", err)
	}
	if !strings.Contains(string(data), `mcpServers: [{"probe-srv":{"command":"/bin/probe","env":{"PROBE_TOKEN":"${PROBE_TOKEN}"}}}]`) {
		t.Errorf("agent should carry the delegate's server, unexpanded:\n%s", data)
	}

	// The run directory is the loop's config path: the launch file is read
	// from it, and the secret is in the env file beside it, never in the
	// --agents argument.
	args := vendor.ClaudeDelegateAgentArgs(dir)
	if len(args) != 2 || strings.Contains(args[1], "loop-s3cret") || !strings.Contains(args[1], "mcp-exec") {
		t.Errorf("--agents should go through the launcher without the secret: %v", args)
	}
	env, err := os.ReadFile(filepath.Join(dir, "delegates", "probe", ".env.ynh"))
	if err != nil || string(env) != "PROBE_TOKEN=\"loop-s3cret\"\n" {
		t.Errorf("env file = %q, %v", env, err)
	}
}
