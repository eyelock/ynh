//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestVendors_TextOutput asserts the human-readable form of `ynh vendors`
// lists all three supported adapters. We test the JSON shape elsewhere;
// this locks the table format documented for shell users.
func TestVendors_TextOutput(t *testing.T) {
	s := newSandbox(t)
	out, _ := s.mustRunYnh(t, "vendors")

	for _, want := range []string{"claude", "codex", "cursor"} {
		if !strings.Contains(out, want) {
			t.Errorf("vendors text output missing %q\n%s", want, out)
		}
	}
}

// TestInfo_WithIncludes asserts `ynh info --format json` of a harness
// with one include surfaces the include in the includes[] array. Existing
// TestInfo_JSON_Shape covers only the minimal harness; this one locks the
// includes shape used by IDE plugins to render dependency trees.
func TestInfo_WithIncludes(t *testing.T) {
	s := newSandbox(t)

	upstream := filepath.Join(t.TempDir(), "upstream", "skills", "thing")
	if err := os.MkdirAll(upstream, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(upstream, "SKILL.md"),
		[]byte("---\nname: thing\ndescription: marker.\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	harness := filepath.Join(t.TempDir(), "with-include")
	if err := os.MkdirAll(filepath.Join(harness, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "with-include",
  "version": "0.1.0",
  "includes": [{"local": %q}]
}
`, filepath.Dir(filepath.Dir(upstream)))
	if err := os.WriteFile(filepath.Join(harness, ".ynh-plugin", "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	s.mustRunYnh(t, "install", harness)

	out, _ := s.mustRunYnh(t, "info", "with-include", "--format", "json")
	var got envelopeInfo
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parsing info JSON: %v\n%s", err, out)
	}
	if len(got.Harness.Includes) != 1 {
		t.Fatalf("expected 1 include in info, got %d: %+v", len(got.Harness.Includes), got.Harness.Includes)
	}
}

// TestInstall_ReservedNameYnh covers the documented carve-out for the
// reserved name "ynh": the harness installs but no launcher script is
// written to ~/.ynh/bin/ (which would otherwise overwrite the ynh binary
// itself). Users invoke the harness via `ynh run ynh`.
func TestInstall_ReservedNameYnh(t *testing.T) {
	s := newSandbox(t)

	harness := filepath.Join(t.TempDir(), "ynh-named")
	if err := os.MkdirAll(filepath.Join(harness, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	plugin := `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"ynh","version":"0.1.0"}`
	if err := os.WriteFile(filepath.Join(harness, ".ynh-plugin", "plugin.json"), []byte(plugin), 0o644); err != nil {
		t.Fatal(err)
	}

	s.mustRunYnh(t, "install", harness)

	// Harness dir exists.
	assertDirExists(t, filepath.Join(s.home, "harnesses", "ynh"))

	// Launcher must NOT exist — would overwrite ynh itself.
	if _, err := os.Stat(filepath.Join(s.home, "bin", "ynh")); err == nil {
		t.Errorf("reserved name 'ynh' should not get a launcher script")
	}
}

// TestInstall_GitWithRefAndPath exercises the full git-source install path:
// a file:// upstream with --ref pointing at a specific SHA and --path
// scoping into a subdirectory. The combination is documented for
// reproducible monorepo installs.
func TestInstall_GitWithRefAndPath(t *testing.T) {
	s := newSandbox(t)

	// Build a local git upstream with a harness in subdir/harness-a/.
	upstream := filepath.Join(t.TempDir(), "upstream")
	harnessSub := filepath.Join("subdir", "harness-a")
	dir := filepath.Join(upstream, harnessSub, ".ynh-plugin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	plugin := `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"harness-a","version":"0.1.0"}`
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(plugin), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, upstream, "init", "--quiet", "--initial-branch=main")
	mustGit(t, upstream, "config", "user.email", "e2e@example.invalid")
	mustGit(t, upstream, "config", "user.name", "e2e")
	mustGit(t, upstream, "config", "uploadpack.allowReachableSHA1InWant", "true")
	mustGit(t, upstream, "add", "-A")
	mustGit(t, upstream, "commit", "--quiet", "-m", "init")

	// Capture the SHA for --ref pinning.
	sha, _, err := runGit(t, upstream, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	sha = strings.TrimSpace(sha)

	gitURL := "file://" + upstream
	s.mustRunYnh(t, "install", gitURL, "--ref", sha, "--path", harnessSub)

	// Installed at ~/.ynh/harnesses/harness-a/, with installed.json carrying
	// the resolved SHA and the --path subdir.
	installDir := filepath.Join(s.home, "harnesses", "harness-a")
	assertFileExists(t, filepath.Join(installDir, ".ynh-plugin", "plugin.json"))

	got := readInstalledJSON(t, installDir)
	assertEqual(t, "source_type", got.SourceType, "git")
	assertEqual(t, "ref", got.Ref, sha)
	assertEqual(t, "sha", got.SHA, sha)
	assertEqual(t, "path", got.Path, harnessSub)
}

// runGit is a non-fatal wrapper around git for tests that need to capture
// output without aborting. Use mustGit when failure should fail the test.
func runGit(t *testing.T, dir string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.String(), errBuf.String(), err
}
