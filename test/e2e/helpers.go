//go:build e2e

// Package e2e contains the ynh end-to-end test suite.
//
// These tests exercise real install/update/fork/delegate flows against
// hand-crafted fixtures pinned by SHA in eyelock/assistants:e2e-fixtures/.
// They build the production binary via `make build` and assert deep
// filesystem state in temp directories. They never touch the developer's
// real ~/.ynh.
//
// Run with `make e2e`. They are gated behind the `e2e` build tag and do
// not run as part of `make test`.
package e2e

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// AssistantsRepo is the upstream repository hosting the E2E fixtures.
const AssistantsRepo = "https://github.com/eyelock/assistants.git"

// AssistantsFixturesSHA pins the commit of eyelock/assistants whose
// e2e-fixtures/ tree the suite tests against. Bump intentionally when
// fixtures evolve (see eyelock/assistants:e2e-fixtures/README.md).
//
// Pinned to the tip of eyelock/assistants:feat/e2e-fixtures-extended
// (containing all Phases 2–4 fixtures). Update to the merge SHA on
// `develop` once that branch is merged.
const AssistantsFixturesSHA = "69baa731b5a1ed67ff0c5362f7806f398ee62815"

// AssistantsFixturesV1Tag is a stable git tag in eyelock/assistants used
// by the with-tag-include fixture to verify tag-to-SHA resolution.
// Currently points at the initial fixture commit (8713efa).
const AssistantsFixturesV1Tag = "e2e-fixtures-v1"

// repoRoot resolves the ynh repo root from this source file's location.
// Stable across test working directories and CI runners.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// helpers.go lives at <repo>/test/e2e/helpers.go
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// ynhBinary returns the absolute path of the ynh binary built by
// `make build`. The Makefile's `e2e` target depends on `build`, so
// the binary is guaranteed to exist when the suite runs.
func ynhBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(repoRoot(t), "bin", "ynh")
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("ynh binary not found at %s — run `make build` first: %v", bin, err)
	}
	return bin
}

// sandbox is a fully isolated ynh environment for one test.
// home is set as YNH_HOME for the duration of the test.
type sandbox struct {
	home string
}

// newSandbox creates a per-test ynh home under t.TempDir() and points
// YNH_HOME at it. Cleanup is automatic via t.TempDir().
func newSandbox(t *testing.T) *sandbox {
	t.Helper()
	home := filepath.Join(t.TempDir(), "ynh-home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("creating ynh home: %v", err)
	}
	t.Setenv("YNH_HOME", home)
	return &sandbox{home: home}
}

// runYnh executes the ynh binary with args inside the sandbox.
// It returns stdout, stderr, and the resulting error (non-nil on non-zero exit).
func (s *sandbox) runYnh(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := exec.Command(ynhBinary(t), args...)
	cmd.Env = append(os.Environ(), "YNH_HOME="+s.home)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

// mustRunYnh is runYnh that fails the test on non-zero exit.
func (s *sandbox) mustRunYnh(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()
	out, errOut, err := s.runYnh(t, args...)
	if err != nil {
		t.Fatalf("ynh %s failed: %v\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), err, out, errOut)
	}
	return out, errOut
}

// runYnhInDirRaw executes the ynh binary with cwd=dir inside sandbox s and
// returns stdout, stderr, and the resulting error. Used by tests that need
// to assert behaviour from a project directory rather than a default cwd.
func runYnhInDirRaw(t *testing.T, s *sandbox, dir string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := exec.Command(ynhBinary(t), args...)
	cmd.Env = append(os.Environ(), "YNH_HOME="+s.home)
	cmd.Dir = dir
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

// mustRunYnhInDir is runYnhInDirRaw that fails the test on non-zero exit.
func mustRunYnhInDir(t *testing.T, s *sandbox, dir string, args ...string) (stdout, stderr string) {
	t.Helper()
	out, errOut, err := runYnhInDirRaw(t, s, dir, args...)
	if err != nil {
		t.Fatalf("ynh %s in %s failed: %v\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), dir, err, out, errOut)
	}
	return out, errOut
}

// cloneAssistantsAtSHA clones eyelock/assistants into a tempdir and
// checks out the pinned fixture SHA. Returns the absolute path of the
// working tree. Tests install fixtures from <clone>/e2e-fixtures/<name>
// via `ynh install <local-path>` (source_type=local) — the local-path
// installer is what gives reproducibility against the pinned SHA.
// Git-source coverage is deferred to Phase 2 (needs `--ref` on
// `ynh install`, or live-github structural-only assertions).
func cloneAssistantsAtSHA(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "assistants")
	mustGit(t, "", "clone", "--quiet", AssistantsRepo, dir)
	mustGit(t, dir, "checkout", "--quiet", AssistantsFixturesSHA)
	return dir
}

// mustGit runs `git <args...>` in dir (or cwd if dir is empty) and
// fails the test on non-zero exit.
func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// assertFileExists fails the test if path is not a regular file.
func assertFileExists(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected file at %s: %v", path, err)
	}
	if info.IsDir() {
		t.Fatalf("expected file at %s, found directory", path)
	}
}

// assertDirExists fails the test if path is not a directory.
func assertDirExists(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected directory at %s: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("expected directory at %s, found regular file", path)
	}
}

// assertEqual compares got and want using reflect.DeepEqual via fmt
// formatting and reports field-level context on mismatch. Zero-deps —
// no go-cmp.
func assertEqual[T any](t *testing.T, field string, got, want T) {
	t.Helper()
	gs, ws := fmt.Sprintf("%+v", got), fmt.Sprintf("%+v", want)
	if gs != ws {
		t.Errorf("%s mismatch:\n  got:  %s\n  want: %s", field, gs, ws)
	}
}
