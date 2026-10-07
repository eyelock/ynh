package agent

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// gitIn runs git in dir for a test fixture and fails the test on error.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// sensorRepo builds a git repo whose manifest declares one sensor and returns
// its file:// URL.
func sensorRepo(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, plugin.PluginDir), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"up","version":"1.0.0","sensors":{"review":{
  "role":"convergence-verifier","source":{"focus":"reviewer"},"output":{"format":"text"}}}}`
	if err := os.WriteFile(filepath.Join(src, plugin.PluginDir, plugin.PluginFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, src, "init", "-b", "main")
	gitIn(t, src, "add", ".")
	gitIn(t, src, "commit", "-m", "up")
	return "file://" + src
}

func writeGitIncludeHarness(t *testing.T, url string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "consumer")
	if err := os.MkdirAll(filepath.Join(dir, plugin.PluginDir), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"consumer","version":"0.1.0","default_vendor":"claude",
  "includes":[{"git":"` + url + `"}]}`
	if err := os.WriteFile(filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A run is where the network is allowed, so it fetches the includes an empty
// cache lacks before it reads their sensors (ynf #130). The included focus
// sensor is then seen, and refused as a convergence verifier, which proves the
// sensor was read from the fetched include.
func TestRunLoop_FetchesUncachedGitIncludeBeforeMergingSensors(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	t.Setenv("GIT_ALLOW_PROTOCOL", "file")
	dir := writeGitIncludeHarness(t, sensorRepo(t))

	mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
	opts := baseOpts(mb, io.Discard, io.Discard, strings.NewReader(""))
	opts.HarnessName = dir
	_, err := RunLoop(opts)
	if err == nil || !strings.Contains(err.Error(), "a focus sensor is never resolved") {
		t.Fatalf("want the included sensor seen after the fetch, got %v", err)
	}
	if len(mb.startOpts) != 0 {
		t.Error("a worker started under a verifier that can never pass")
	}
}

// With the remote unreachable the run stops before any worker starts, as a
// refusal, and the error says which include could not be fetched.
func TestRunLoop_UnreachableGitIncludeRefusesBeforeWorker(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	t.Setenv("GIT_ALLOW_PROTOCOL", "file")
	missing := "file://" + filepath.Join(t.TempDir(), "gone.git")
	dir := writeGitIncludeHarness(t, missing)

	mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
	opts := baseOpts(mb, io.Discard, io.Discard, strings.NewReader(""))
	opts.HarnessName = dir
	_, err := RunLoop(opts)
	if err == nil || !strings.Contains(err.Error(), "fetching includes") || !strings.Contains(err.Error(), "gone.git") {
		t.Fatalf("want a clear fetch error naming the include, got %v", err)
	}
	var ee *ExitError
	if asExitError(err, &ee) && ee.Code != ExitRefused {
		t.Errorf("exit code = %d, want a refusal", ee.Code)
	}
	if len(mb.startOpts) != 0 {
		t.Error("a worker started although the includes could not be fetched")
	}
}
