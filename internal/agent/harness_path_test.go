package agent

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/gate"
	"github.com/eyelock/ynh/internal/plugin"
)

// writePathHarness writes a minimal harness into dir, which need not be
// installed anywhere, and returns dir.
func writePathHarness(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, plugin.PluginDir), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{
		"name":           "authoring",
		"version":        "0.2.0",
		"default_vendor": "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// recordCheck replaces the gate and records the harness argument each `ynh
// check` would have been handed, so a test can see what the loop passes down.
func recordCheck(t *testing.T, result string) *[]string {
	t.Helper()
	orig := runCheckFn
	t.Cleanup(func() { runCheckFn = orig })
	var seen []string
	runCheckFn = func(_, harnessName, _ string, only []string, _ map[string]json.RawMessage, _ []string) (*gate.Envelope, error) {
		seen = append(seen, harnessName)
		rs := make([]gate.Result, 0, len(only))
		for _, n := range only {
			rs = append(rs, gate.Result{Name: n, Kind: "command", Tolerance: "blocking", Status: result})
		}
		return env(rs...), nil
	}
	return &seen
}

func sameDir(t *testing.T, got, want string) {
	t.Helper()
	g, gErr := filepath.EvalSymlinks(got)
	w, wErr := filepath.EvalSymlinks(want)
	if gErr != nil || wErr != nil {
		t.Fatalf("resolving %q (%v) or %q (%v)", got, gErr, want, wErr)
	}
	if g != w {
		t.Errorf("got %q, want %q", got, want)
	}
}

// --harness takes an installed id or a path, as `ynh run` and `ynh check` do
// (#560). A path needs no install, reaches the sensor gate as an absolute
// path, and is reported by the manifest name: the trajectory and the result
// never carry a filesystem path.
func TestRunLoop_HarnessGivenAsPath(t *testing.T) {
	tests := []struct {
		name string
		// ref receives the harness directory and returns the --harness value;
		// it may change into a directory first.
		ref func(t *testing.T, dir string) string
	}{
		{"dot", func(t *testing.T, dir string) string { t.Chdir(dir); return "." }},
		{"relative", func(t *testing.T, dir string) string {
			t.Chdir(filepath.Dir(dir))
			return "./" + filepath.Base(dir)
		}},
		{"absolute", func(_ *testing.T, dir string) string { return dir }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("YNH_HOME", t.TempDir())
			dir := writePathHarness(t, filepath.Join(t.TempDir(), "my-harness"))
			ref := tt.ref(t, dir)
			seen := recordCheck(t, gate.StatusPass)

			mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
			opts := resumeOpts(mb, t.TempDir())
			opts.HarnessName = ref
			res, err := RunLoop(opts)
			if err != nil {
				t.Fatalf("a path is a valid --harness: %v", err)
			}
			if len(*seen) == 0 {
				t.Fatal("the sensor gate never ran")
			}
			for _, got := range *seen {
				if !filepath.IsAbs(got) {
					t.Errorf("gate was handed %q, want an absolute path", got)
				}
				sameDir(t, got, dir)
			}
			if res.Harness == nil || res.Harness.Name != "authoring" || res.Harness.Version != "0.2.0" {
				t.Errorf("result harness = %+v, want name authoring, version 0.2.0", res.Harness)
			}
			for _, e := range readTrajectoryFile(t, opts.EmitJSONL) {
				if e.Kind != KindSessionStart {
					continue
				}
				if d := decodeData[SessionStartData](t, e); d.Harness != "authoring" {
					t.Errorf("session_start harness = %q, want the manifest name", d.Harness)
				}
			}
		})
	}
}

// An installed id still resolves, and what is neither an id nor a harness
// directory is refused with a message that says a path is accepted too.
func TestRunLoop_HarnessIDAndRefusals(t *testing.T) {
	t.Run("installed id", func(t *testing.T) {
		mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
		opts := baseOpts(mb, io.Discard, io.Discard, strings.NewReader(""))
		opts.HarnessName = installEffortHarness(t, "")
		opts.WorktreeDir = t.TempDir()
		opts.YNHBinary = filepath.Join(t.TempDir(), "no-such-ynh")
		res, err := RunLoop(opts)
		if err != nil && len(mb.startOpts) != 1 {
			t.Fatalf("an installed id must load: %v", err)
		}
		if res.Harness == nil || res.Harness.Name != "local/effort-test" {
			t.Errorf("result harness = %+v, want the id", res.Harness)
		}
	})

	t.Run("directory that is not a harness", func(t *testing.T) {
		empty := t.TempDir()
		mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
		opts := baseOpts(mb, io.Discard, io.Discard, strings.NewReader(""))
		opts.HarnessName = empty
		_, err := RunLoop(opts)
		if err == nil || !strings.Contains(err.Error(), "no harness at") {
			t.Fatalf("want a no-harness refusal, got %v", err)
		}
		if len(mb.startOpts) != 0 {
			t.Error("a worker started under a harness that does not exist")
		}
	})

	t.Run("neither id nor path", func(t *testing.T) {
		mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
		opts := baseOpts(mb, io.Discard, io.Discard, strings.NewReader(""))
		opts.HarnessName = "demo"
		_, err := RunLoop(opts)
		if err == nil || !strings.Contains(err.Error(), `"demo" is not a valid harness id`) ||
			!strings.Contains(err.Error(), "local harness directory") {
			t.Fatalf("want the id-or-path refusal, got %v", err)
		}
	})
}

// A run started with a path checkpoints its absolute path, so --resume finds
// the same harness from a different directory, with no --harness given.
func TestRunLoop_ResumeOfPathStartedRun(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	dir := writePathHarness(t, filepath.Join(t.TempDir(), "my-harness"))
	session := t.TempDir()

	t.Chdir(dir)
	recordCheck(t, gate.StatusFail)
	mb1 := &mockBackend{name: "mock", turns: []Turn{{Content: "r1"}, {Content: "r2"}}, resumeToken: "tok"}
	opts1 := resumeOpts(mb1, session)
	opts1.HarnessName = "."
	opts1.MaxTurns = 2
	_, err1 := RunLoop(opts1)
	var ee *ExitError
	if !asExitError(err1, &ee) || ee.Code != ExitIterationCap {
		t.Fatalf("run 1: want ExitIterationCap, got %v", err1)
	}
	cp, err := readCheckpoint(session)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(cp.HarnessName) {
		t.Fatalf("checkpoint stored %q, want an absolute path", cp.HarnessName)
	}
	sameDir(t, cp.HarnessName, dir)

	t.Chdir(t.TempDir())
	seen2 := recordCheck(t, gate.StatusPass)
	mb2 := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}, resumeToken: "tok"}
	opts2 := resumeOpts(mb2, session)
	opts2.Resume = session
	opts2.MaxTurns = 10
	res, err := RunLoop(opts2)
	if err != nil {
		t.Fatalf("resume from another directory: %v", err)
	}
	if len(*seen2) == 0 {
		t.Fatal("the resumed run never ran the gate")
	}
	for _, got := range *seen2 {
		sameDir(t, got, dir)
	}
	if res.Harness == nil || res.Harness.Name != "authoring" {
		t.Errorf("resumed result harness = %+v, want the same harness", res.Harness)
	}
}

// The convergence verifier is checked against the sensors `ynh check` will
// run, included ones among them: a focus sensor an include brings in can never
// decide, and the run is refused before a worker starts.
func TestRunLoop_VerifierCheckSeesIncludedSensors(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	root := t.TempDir()
	dir := filepath.Join(root, "consumer")
	up := filepath.Join(dir, "vendored", "up")
	if err := os.MkdirAll(filepath.Join(up, plugin.PluginDir), 0o755); err != nil {
		t.Fatal(err)
	}
	upManifest := `{"name":"up","version":"1.0.0","sensors":{"review":{
  "role":"convergence-verifier","source":{"focus":"reviewer"},"output":{"format":"text"}}}}`
	if err := os.WriteFile(filepath.Join(up, plugin.PluginDir, plugin.PluginFile), []byte(upManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, plugin.PluginDir), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"consumer","version":"0.1.0","default_vendor":"claude",
  "includes":[{"local":"vendored/up"}]}`
	if err := os.WriteFile(filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
	opts := baseOpts(mb, io.Discard, io.Discard, strings.NewReader(""))
	opts.HarnessName = dir
	_, err := RunLoop(opts)
	if err == nil || !strings.Contains(err.Error(), "a focus sensor is never resolved") {
		t.Fatalf("want the verifier refused for an included focus sensor, got %v", err)
	}
	if len(mb.startOpts) != 0 {
		t.Error("a worker started under a verifier that can never pass")
	}
}
