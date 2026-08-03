//go:build e2e

package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Resume e2e: drives the production binary with fake vendor CLIs on PATH that
// record their argv, so the whole `--resume` path can be asserted without
// invoking a real LLM.
//
// The invariant these tests exist to protect: ynh must never emit a bare
// resume flag. On every vendor a bare --resume opens an interactive session
// *picker*, which would hang an unattended relaunch waiting for a keypress.
// Every resume launch is either an explicit id or the vendor's continue-last
// form. A regression here is invisible in unit tests and painful in practice.

// resumeEnv is a sandbox plus a fake HOME (for vendor session stores) and a
// shim directory prepended to PATH (for fake vendor CLIs).
type resumeEnv struct {
	sandbox  *sandbox
	home     string
	shimDir  string
	argvFile string
	project  string
}

// newResumeEnv builds the fake vendor CLIs, an isolated HOME, and a local
// harness installed into the sandbox.
func newResumeEnv(t *testing.T) *resumeEnv {
	t.Helper()

	s := newSandbox(t)
	root := t.TempDir()
	env := &resumeEnv{
		sandbox:  s,
		home:     filepath.Join(root, "home"),
		shimDir:  filepath.Join(root, "shims"),
		argvFile: filepath.Join(root, "argv.txt"),
		project:  filepath.Join(root, "project"),
	}

	for _, dir := range []string{env.home, env.shimDir, env.project} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("creating %s: %v", dir, err)
		}
	}

	// t.TempDir() hands back a symlinked path on macOS (/var/... -> /private/var/...).
	// ynh resolves cwd via os.Getwd(), which returns the real path — and so does
	// the vendor CLI when it records the session. Store fixtures must therefore be
	// keyed on the resolved path, or the lookup misses for reasons that have
	// nothing to do with the code under test.
	if resolved, err := filepath.EvalSymlinks(env.project); err == nil {
		env.project = resolved
	}

	// One shim per vendor binary. "agent" is Cursor's CLI name.
	shim := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ARGV_OUT\"\nexit 0\n"
	for _, bin := range []string{"claude", "copilot", "codex", "agent"} {
		path := filepath.Join(env.shimDir, bin)
		if err := os.WriteFile(path, []byte(shim), 0o755); err != nil {
			t.Fatalf("writing shim %s: %v", bin, err)
		}
	}

	// Minimal local harness — no network, no fixtures.
	harnessDir := filepath.Join(root, "harness")
	pluginDir := filepath.Join(harnessDir, ".ynh-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("creating harness: %v", err)
	}
	manifest := `{"name":"resume-test","version":"1.0.0","description":"resume e2e","default_vendor":"claude"}`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("writing plugin.json: %v", err)
	}
	s.mustRunYnh(t, "install", harnessDir)

	return env
}

// run executes `ynh run` from the project directory with the shims on PATH,
// returning the vendor's recorded argv and ynh's stderr.
func (e *resumeEnv) run(t *testing.T, args ...string) (argv []string, stderr string) {
	t.Helper()

	_ = os.Remove(e.argvFile)

	full := append([]string{"run", "local/resume-test"}, args...)
	cmd := exec.Command(ynhBinary(t), full...)
	cmd.Dir = e.project

	// Build the environment explicitly: HOME and PATH must be *replaced*, not
	// appended to, so the fake store and shims are the only ones visible.
	var environ []string
	for _, kv := range os.Environ() {
		switch {
		case strings.HasPrefix(kv, "HOME="),
			strings.HasPrefix(kv, "PATH="),
			strings.HasPrefix(kv, "YNH_HOME="),
			strings.HasPrefix(kv, "ARGV_OUT="):
			continue
		}
		environ = append(environ, kv)
	}
	environ = append(environ,
		"HOME="+e.home,
		"PATH="+e.shimDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"YNH_HOME="+e.sandbox.home,
		"ARGV_OUT="+e.argvFile,
	)
	cmd.Env = environ

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		t.Fatalf("ynh run %s failed: %v\nstdout:\n%s\nstderr:\n%s",
			strings.Join(full, " "), err, outBuf.String(), errBuf.String())
	}

	data, err := os.ReadFile(e.argvFile)
	if err != nil {
		t.Fatalf("vendor shim recorded no argv (it was never launched): %v", err)
	}
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line != "" {
			argv = append(argv, line)
		}
	}
	return argv, errBuf.String()
}

// claudeProjectSlug mirrors the adapter's slug rule: "/" and "." both become
// "-". Duplicated rather than exported, so a change to the real rule has to be
// made deliberately in both places.
func claudeProjectSlug(dir string) string {
	return strings.NewReplacer("/", "-", ".", "-").Replace(dir)
}

func (e *resumeEnv) writeClaudeSession(t *testing.T, cwd, id string, old bool) {
	t.Helper()
	dir := filepath.Join(e.home, ".claude", "projects", claudeProjectSlug(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating claude store: %v", err)
	}
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("writing claude session: %v", err)
	}
	if old {
		// Stamp it into the past so newest-first ordering is deterministic
		// rather than dependent on how fast the test writes both files.
		stamp := time.Now().Add(-24 * time.Hour)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatalf("stamping claude session: %v", err)
		}
	}
}

func (e *resumeEnv) writeCopilotSession(t *testing.T, cwd, id string) {
	t.Helper()
	dir := filepath.Join(e.home, ".copilot", "session-state", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating copilot store: %v", err)
	}
	body := "id: " + id + "\ncwd: " + cwd + "\nrepository: eyelock/ynh\n"
	if err := os.WriteFile(filepath.Join(dir, "workspace.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("writing copilot workspace.yaml: %v", err)
	}
}

func TestResume_NoStoreLaunchesColdWithWarning(t *testing.T) {
	env := newResumeEnv(t)

	argv, stderr := env.run(t, "-v", "claude", "--resume")

	if hasAnyResumeFlag(argv) {
		t.Errorf("cold launch must carry no resume flag, got argv %v", argv)
	}
	if !strings.Contains(stderr, "no previous session found") {
		t.Errorf("expected a warning about no previous session, got stderr:\n%s", stderr)
	}
}

func TestResume_ResolvesNewestClaudeSessionForCwd(t *testing.T) {
	env := newResumeEnv(t)
	env.writeClaudeSession(t, env.project, "session-old", true)
	env.writeClaudeSession(t, env.project, "session-new", false)

	argv, _ := env.run(t, "-v", "claude", "--resume")

	if !containsPair(argv, "--resume", "session-new") {
		t.Errorf("expected --resume session-new, got argv %v", argv)
	}
	if contains(argv, "session-old") {
		t.Errorf("resumed the older session: argv %v", argv)
	}
}

// The regression this guards: Copilot's own --continue is not directory-scoped,
// so a session belonging to a different worktree must never be selected.
func TestResume_CopilotIgnoresSessionsFromOtherDirectories(t *testing.T) {
	env := newResumeEnv(t)
	env.writeCopilotSession(t, env.project, "mine")
	env.writeCopilotSession(t, "/some/other/project", "elsewhere")

	argv, _ := env.run(t, "-v", "copilot", "--resume")

	if !contains(argv, "--resume=mine") {
		t.Errorf("expected --resume=mine, got argv %v", argv)
	}
	if contains(argv, "--resume=elsewhere") {
		t.Errorf("resumed another directory's session: argv %v", argv)
	}
}

func TestResume_ExplicitIDBypassesTheStore(t *testing.T) {
	env := newResumeEnv(t)
	env.writeClaudeSession(t, env.project, "from-store", false)

	argv, _ := env.run(t, "-v", "claude", "--resume=explicit-id")

	if !containsPair(argv, "--resume", "explicit-id") {
		t.Errorf("expected --resume explicit-id, got argv %v", argv)
	}
	if contains(argv, "from-store") {
		t.Errorf("explicit id was overridden by the store: argv %v", argv)
	}
}

// Codex and Cursor cannot resolve ids locally, but must still resume via their
// continue-last form rather than falling back to a cold launch.
func TestResume_LookupUnavailableVendorsUseContinueLast(t *testing.T) {
	tests := []struct {
		vendor   string
		wantArgv []string
	}{
		{vendor: "codex", wantArgv: []string{"resume", "--last"}},
		{vendor: "cursor", wantArgv: []string{"--continue"}},
	}

	for _, tt := range tests {
		t.Run(tt.vendor, func(t *testing.T) {
			env := newResumeEnv(t)

			argv, _ := env.run(t, "-v", tt.vendor, "--resume")

			for _, want := range tt.wantArgv {
				if !contains(argv, want) {
					t.Errorf("expected %q in argv, got %v", want, argv)
				}
			}
			if contains(argv, "--resume") {
				t.Errorf("bare --resume would open a picker and hang: argv %v", argv)
			}
		})
	}
}

func TestResume_AbsentFlagLaunchesNormally(t *testing.T) {
	env := newResumeEnv(t)
	env.writeClaudeSession(t, env.project, "available-session", false)

	argv, _ := env.run(t, "-v", "claude")

	if hasAnyResumeFlag(argv) {
		t.Errorf("launch without --resume must not resume, got argv %v", argv)
	}
}

func hasAnyResumeFlag(argv []string) bool {
	for _, a := range argv {
		if a == "--continue" || a == "resume" || strings.HasPrefix(a, "--resume") {
			return true
		}
	}
	return false
}

func contains(argv []string, want string) bool {
	for _, a := range argv {
		if a == want {
			return true
		}
	}
	return false
}

func containsPair(argv []string, first, second string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == first && argv[i+1] == second {
			return true
		}
	}
	return false
}
