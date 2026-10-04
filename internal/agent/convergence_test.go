package agent

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
)

// installTestHarness writes a harness with the given sensors into a temporary
// YNH_HOME and returns its id.
func installTestHarness(t *testing.T, sensors map[string]any) string {
	t.Helper()
	t.Setenv("YNH_HOME", t.TempDir())
	const id = "local/verifier-test"
	dir := harness.InstalledDirByID(id)
	if err := os.MkdirAll(filepath.Join(dir, plugin.PluginDir), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{
		"name":           "verifier-test",
		"version":        "0.1.0",
		"default_vendor": "claude",
		"focuses":        map[string]any{"reviewer": map[string]any{"prompt": "review it"}},
		"sensors":        sensors,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return id
}

// A convergence verifier that can never return pass makes a run go to its
// turn cap without saying why (#447). The loop refuses it before any worker
// starts, and a verifier that can decide still reaches the worker.
func TestRunLoop_RefusesVerifierThatCannotPass(t *testing.T) {
	out := map[string]any{"format": "text"}
	focusSrc := map[string]any{"focus": "reviewer"}
	filesSrc := map[string]any{"files": []any{"done.txt"}}
	cmdSrc := map[string]any{"command": "true"}

	cases := []struct {
		name     string
		sensors  map[string]any
		flag     string // --convergence-sensor
		refusal  string // substring of the refusal; "" means the worker starts
		verifier string
	}{
		{
			name:     "focus sensor with the role",
			sensors:  map[string]any{"review": map[string]any{"role": "convergence-verifier", "source": focusSrc, "output": out}},
			refusal:  "a focus sensor is never resolved",
			verifier: "review",
		},
		{
			name:     "focus sensor named by --convergence-sensor",
			sensors:  map[string]any{"review": map[string]any{"source": focusSrc, "output": out}},
			flag:     "review",
			refusal:  "a focus sensor is never resolved",
			verifier: "review",
		},
		{
			name:     "files sensor with the role",
			sensors:  map[string]any{"done": map[string]any{"role": "convergence-verifier", "source": filesSrc, "output": out}},
			refusal:  "a files sensor's freshness",
			verifier: "done",
		},
		{
			name:    "command sensor with the role",
			sensors: map[string]any{"verify": map[string]any{"role": "convergence-verifier", "source": cmdSrc, "output": out}},
		},
		{
			name:    "a focus sensor that is not the verifier",
			sensors: map[string]any{"review": map[string]any{"source": focusSrc, "output": out}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id := installTestHarness(t, c.sensors)
			// No turns: a worker that starts exits at once, so the loop never
			// reaches the gate and never invokes a ynh binary.
			mb := &mockBackend{name: "mock"}
			var stdout, stderr bytes.Buffer
			opts := baseOpts(mb, &stdout, &stderr, strings.NewReader(""))
			opts.HarnessName = id
			opts.ConvergenceSensor = c.flag
			opts.WorktreeDir = t.TempDir()
			opts.YNHBinary = filepath.Join(t.TempDir(), "no-such-ynh")

			_, err := RunLoop(opts)
			if c.refusal == "" {
				if len(mb.startOpts) != 1 {
					t.Fatalf("worker started %d times, want 1; err: %v", len(mb.startOpts), err)
				}
				if err != nil && strings.Contains(err.Error(), "convergence verifier") {
					t.Fatalf("a verifier that can decide was refused: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected a refusal, got none")
			}
			if len(mb.startOpts) != 0 {
				t.Errorf("worker started %d times before the refusal; want 0", len(mb.startOpts))
			}
			for _, want := range []string{`sensor "` + c.verifier + `"`, "requires a command source", c.refusal} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal %q does not contain %q", err, want)
				}
			}
		})
	}
}
