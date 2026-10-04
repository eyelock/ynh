package agent

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
)

// Each neutral level is accepted where the backend has a control for it, and
// refused, before anything runs, where it has none.
func TestValidateEffort(t *testing.T) {
	tests := []struct {
		level, backend string
		wantSub        string // "" means accepted
	}{
		{"", "claude", ""},
		{"", "codex", ""},
		{"", "cursor", ""},
		{"low", "claude", ""},
		{"medium", "claude", ""},
		{"high", "claude", ""},
		{"low", "codex", ""},
		{"medium", "codex", ""},
		{"high", "codex", ""},
		{"low", "cursor", "cursor has no reasoning effort setting"},
		{"medium", "cursor", "cursor has no reasoning effort setting"},
		{"high", "cursor", "cursor has no reasoning effort setting"},
		{"max", "claude", `unknown effort level "max" (supported: low, medium, high)`},
		{"minimal", "codex", `unknown effort level "minimal"`},
		{"HIGH", "claude", `unknown effort level "HIGH"`},
	}
	for _, tt := range tests {
		t.Run(tt.level+"/"+tt.backend, func(t *testing.T) {
			err := validateEffort(tt.level, tt.backend)
			if tt.wantSub == "" {
				if err != nil {
					t.Fatalf("validateEffort(%q, %q) = %v, want nil", tt.level, tt.backend, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("validateEffort(%q, %q) = %v, want it to contain %q", tt.level, tt.backend, err, tt.wantSub)
			}
		})
	}
}

// The worker receives each level in its own CLI's form, and no effort
// argument at all when none was asked for.
func TestEffortArgs(t *testing.T) {
	for _, level := range []string{"", "low", "medium", "high"} {
		t.Run("level="+level, func(t *testing.T) {
			opts := StartOptions{Effort: level}

			claude := buildClaudeStreamArgs(opts)
			i := slices.Index(claude, "--effort")
			if level == "" {
				if i >= 0 {
					t.Errorf("claude args %v carry --effort with nothing requested", claude)
				}
			} else if i < 0 || i+1 >= len(claude) || claude[i+1] != level {
				t.Errorf("claude args %v, want --effort %s", claude, level)
			}

			for _, thread := range []string{"", "thread-1"} {
				codex := buildCodexArgs(opts, thread)
				want := `model_reasoning_effort="` + level + `"`
				j := slices.Index(codex, "-c")
				if level == "" {
					if j >= 0 {
						t.Errorf("codex args %v carry -c with nothing requested", codex)
					}
					continue
				}
				if j < 0 || j+1 >= len(codex) || codex[j+1] != want {
					t.Errorf("codex args (thread %q) %v, want -c %s", thread, codex, want)
				}
				// The prompt is read from stdin, so "-" stays last.
				if codex[len(codex)-1] != "-" {
					t.Errorf("codex args %v must end with -", codex)
				}
			}
		})
	}
}

// installEffortHarness writes a harness whose agent block sets effort (or
// leaves it out when empty) into a temporary YNH_HOME and returns its id.
func installEffortHarness(t *testing.T, effort string) string {
	t.Helper()
	t.Setenv("YNH_HOME", t.TempDir())
	const id = "local/effort-test"
	dir := harness.InstalledDirByID(id)
	if err := os.MkdirAll(filepath.Join(dir, plugin.PluginDir), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"name":           "effort-test",
		"version":        "0.1.0",
		"default_vendor": "claude",
	}
	if effort != "" {
		manifest["agent"] = map[string]any{"effort": effort}
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return id
}

// The level requested reaches the worker and the result's effort_requested,
// and the trajectory header records it. The harness supplies a default the
// flag overrides. Nothing requested leaves the key out entirely.
func TestRunLoop_EffortRequested(t *testing.T) {
	tests := []struct {
		name          string
		flag          string
		harnessEffort string // "" with useHarness: a harness without agent.effort
		useHarness    bool
		want          string // "" means the key must be absent
	}{
		{name: "nothing requested"},
		{name: "flag", flag: "high", want: "high"},
		{name: "harness default", useHarness: true, harnessEffort: "low", want: "low"},
		{name: "flag wins over the harness", useHarness: true, harnessEffort: "low", flag: "medium", want: "medium"},
		{name: "harness without effort", useHarness: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
			opts := baseOpts(mb, io.Discard, io.Discard, strings.NewReader(""))
			opts.Effort = tt.flag
			opts.EmitJSONL = filepath.Join(t.TempDir(), "trajectory.jsonl")
			if tt.useHarness {
				opts.HarnessName = installEffortHarness(t, tt.harnessEffort)
				opts.WorktreeDir = t.TempDir()
				opts.YNHBinary = filepath.Join(t.TempDir(), "no-such-ynh")
			}
			res, err := RunLoop(opts)
			if len(mb.startOpts) != 1 {
				t.Fatalf("worker started %d times, want 1; err: %v", len(mb.startOpts), err)
			}
			if got := mb.startOpts[0].Effort; got != tt.want {
				t.Errorf("worker asked for effort %q, want %q", got, tt.want)
			}
			top := resultJSON(t, res)
			got, present := top["effort_requested"]
			if tt.want == "" && present {
				t.Errorf("effort_requested = %v, want the key absent", got)
			}
			if tt.want != "" && got != tt.want {
				t.Errorf("effort_requested = %v, want %q", got, tt.want)
			}
			start := readSessionStart(t, opts.EmitJSONL)
			got, present = start["effort_requested"]
			if tt.want == "" && present {
				t.Errorf("session_start effort_requested = %v, want the key absent", got)
			}
			if tt.want != "" && got != tt.want {
				t.Errorf("session_start effort_requested = %v, want %q", got, tt.want)
			}
		})
	}
}

// A level the backend cannot honour, from the flag or the harness, stops the
// run before a worker starts.
func TestRunLoop_EffortRefusedBeforeWorkerStarts(t *testing.T) {
	tests := []struct {
		name          string
		backend       string
		flag          string
		harnessEffort string
		wantSub       string
	}{
		{name: "cursor flag", backend: "cursor", flag: "high", wantSub: "cursor has no reasoning effort setting"},
		{name: "unknown level", backend: "claude", flag: "max", wantSub: `unknown effort level "max"`},
		{name: "cursor harness default", backend: "cursor", harnessEffort: "low",
			wantSub: "harness agent.effort: cursor has no reasoning effort setting"},
		{name: "unknown harness level", backend: "claude", harnessEffort: "max",
			wantSub: `harness agent.effort: unknown effort level "max"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
			opts := baseOpts(mb, io.Discard, io.Discard, strings.NewReader(""))
			opts.Backend = tt.backend
			opts.Effort = tt.flag
			if tt.harnessEffort != "" {
				opts.HarnessName = installEffortHarness(t, tt.harnessEffort)
				opts.WorktreeDir = t.TempDir()
				opts.YNHBinary = filepath.Join(t.TempDir(), "no-such-ynh")
			}
			_, err := RunLoop(opts)
			if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantSub)
			}
			if len(mb.startOpts) != 0 {
				t.Errorf("worker started %d times before the refusal, want 0", len(mb.startOpts))
			}
		})
	}
}

// --effort is not restored on resume, like --model: the resumed process asks
// for what it is given, and records that in session_resumed.
func TestRunLoop_ResumeDoesNotRestoreEffort(t *testing.T) {
	for _, resumeEffort := range []string{"", "low"} {
		t.Run("resume effort="+resumeEffort, func(t *testing.T) {
			dir := t.TempDir()
			failSensor(t)
			mb1 := &mockBackend{name: "mock", resumeToken: "tok-1", turns: []Turn{{Content: "r1"}}}
			opts1 := resumeOpts(mb1, dir)
			opts1.Effort = "high"
			opts1.MaxTurns = 1
			if _, err := RunLoop(opts1); err == nil {
				t.Fatal("run 1 should stop at the turn cap")
			}
			before := len(readTrajectoryFile(t, filepath.Join(dir, "trajectory.jsonl")))

			passSensor(t)
			mb2 := &mockBackend{name: "mock", resumeToken: "tok-1", turns: []Turn{{Content: "done"}}}
			opts2 := resumeOpts(mb2, dir)
			opts2.Resume = dir
			opts2.Effort = resumeEffort
			opts2.MaxTurns = 5
			res, err := RunLoop(opts2)
			if err != nil {
				t.Fatalf("resume: %v", err)
			}
			if got := mb2.startOpts[0].Effort; got != resumeEffort {
				t.Errorf("resumed worker asked for effort %q, want %q: --effort is not restored", got, resumeEffort)
			}
			got, present := resultJSON(t, res)["effort_requested"]
			if resumeEffort == "" && present {
				t.Errorf("effort_requested = %v after a resume without --effort, want the key absent", got)
			}
			if resumeEffort != "" && got != resumeEffort {
				t.Errorf("effort_requested = %v, want %q", got, resumeEffort)
			}
			var seen bool
			for _, e := range readTrajectoryFile(t, filepath.Join(dir, "trajectory.jsonl"))[before:] {
				if e.Kind == KindSessionResumed {
					seen = true
					if got := decodeData[SessionResumedData](t, e).EffortRequested; got != resumeEffort {
						t.Errorf("session_resumed effort_requested = %q, want %q", got, resumeEffort)
					}
				}
			}
			if !seen {
				t.Error("no session_resumed event after the resume")
			}
		})
	}
}
