package agent

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/gate"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
)

// installProfileHarness installs a harness with a regular sensor "build", a
// convergence verifier "verify", a profile "factory" and a focus "fac" bound
// to it, with extra merged into the profile.
func installProfileHarness(t *testing.T, profile map[string]any) string {
	t.Helper()
	t.Setenv("YNH_HOME", t.TempDir())
	const id = "local/profile-gate"
	dir := harness.InstalledDirByID(id)
	if err := os.MkdirAll(filepath.Join(dir, plugin.PluginDir), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{
		"name":           "profile-gate",
		"version":        "0.1.0",
		"default_vendor": "claude",
		"profiles":       map[string]any{"factory": profile},
		"focuses":        map[string]any{"fac": map[string]any{"profile": "factory", "prompt": "go"}},
		"sensors": map[string]any{
			"build":  commandSensor(""),
			"verify": commandSensor("convergence-verifier"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return id
}

// The gate is handed the run's profile, from --profile or from a focus's, on
// the per-turn call and on the verifiers' call alike.
func TestRunLoop_GatePassesProfile(t *testing.T) {
	soften := map[string]any{"sensor_overrides": map[string]any{"build": map[string]any{"tolerance": "advisory"}}}
	for _, tc := range []struct {
		name  string
		tweak func(*RunOptions)
		want  string
	}{
		{"profile flag", func(o *RunOptions) { o.Profile = "factory" }, "factory"},
		{"focus bound to a profile", func(o *RunOptions) { o.Focus = "fac" }, "factory"},
		{"neither", func(*RunOptions) {}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := installProfileHarness(t, soften)
			orig := runCheckFn
			t.Cleanup(func() { runCheckFn = orig })
			var profiles []string
			runCheckFn = func(_, _, _, profile string, only []string, _ map[string]json.RawMessage, _ []string) (*gate.Envelope, error) {
				profiles = append(profiles, profile)
				return env(cmdResult(only[0], gate.StatusPass)), nil
			}
			mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
			var stdout, stderr bytes.Buffer
			opts := baseOpts(mb, &stdout, &stderr, strings.NewReader(""))
			opts.HarnessName = id
			opts.WorktreeDir = t.TempDir()
			tc.tweak(&opts)
			if _, err := RunLoop(opts); err != nil {
				t.Fatalf("RunLoop: %v", err)
			}
			// One per-turn call and one verifier call, both with the profile.
			if len(profiles) < 2 {
				t.Fatalf("gate called %d times, want the turn gate and the verifiers", len(profiles))
			}
			for i, p := range profiles {
				if p != tc.want {
					t.Errorf("call %d got profile %q, want %q", i, p, tc.want)
				}
			}
		})
	}
}

// A profile may not touch a verifier, whether the manifest or
// --convergence-sensor makes it one, and the refusal comes before any worker.
func TestRunLoop_RefusesProfileOverrideOfVerifier(t *testing.T) {
	for _, tc := range []struct {
		name  string
		over  map[string]any
		tweak func(*RunOptions)
	}{
		{"role in the manifest", map[string]any{"verify": map[string]any{"tolerance": "advisory"}}, func(*RunOptions) {}},
		{"role by flag", map[string]any{"build": map[string]any{"tolerance": "advisory"}},
			func(o *RunOptions) { o.ConvergenceSensor = "build" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := installProfileHarness(t, map[string]any{"sensor_overrides": tc.over})
			mb := &mockBackend{name: "mock"}
			var stdout, stderr bytes.Buffer
			opts := baseOpts(mb, &stdout, &stderr, strings.NewReader(""))
			opts.HarnessName = id
			opts.Profile = "factory"
			opts.WorktreeDir = t.TempDir()
			tc.tweak(&opts)
			_, err := RunLoop(opts)
			if err == nil || !strings.Contains(err.Error(), `profile "factory"`) || !strings.Contains(err.Error(), "convergence verifier") {
				t.Fatalf("want a refusal naming the profile and the verifier, got %v", err)
			}
			if len(mb.startOpts) != 0 {
				t.Error("a worker started")
			}
		})
	}
}

// A profile naming a sensor nothing declares stops the run too.
func TestRunLoop_RefusesProfileOverrideOfUnknownSensor(t *testing.T) {
	id := installProfileHarness(t, map[string]any{"sensor_overrides": map[string]any{"nope": map[string]any{"tolerance": "advisory"}}})
	mb := &mockBackend{name: "mock"}
	var stdout, stderr bytes.Buffer
	opts := baseOpts(mb, &stdout, &stderr, strings.NewReader(""))
	opts.HarnessName = id
	opts.Profile = "factory"
	opts.WorktreeDir = t.TempDir()
	_, err := RunLoop(opts)
	if err == nil || !strings.Contains(err.Error(), `"nope"`) || !strings.Contains(err.Error(), `profile "factory"`) {
		t.Fatalf("want a refusal naming the profile and sensor, got %v", err)
	}
}
