package agent

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/gate"
	"github.com/eyelock/ynh/internal/harness"
)

// The point of C1: a pipeline gets one object instead of tailing NDJSON and
// reconstructing the run by inference across events.
func TestRunLoop_ReturnsAResultOnConvergence(t *testing.T) {
	mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
	var stdout, stderr bytes.Buffer
	opts := baseOpts(mb, &stdout, &stderr, strings.NewReader(""))
	opts.testSensorNames = []string{"build"}
	opts.MaxTurns = 5
	stubCheck(t, func(_ int, name string) gate.Result {
		return gate.Result{Name: name, Kind: "command", Tolerance: "blocking", Status: gate.StatusPass}
	})

	res, err := RunLoop(opts)
	if err != nil {
		t.Fatalf("expected convergence: %v", err)
	}
	if res == nil {
		t.Fatal("a result must be returned")
	}
	if !res.Converged || res.ExitCode != ExitConverged {
		t.Errorf("converged=%v exit=%d, want true/0", res.Converged, res.ExitCode)
	}
	if res.Consumed.Turns == 0 {
		t.Error("a run that took a turn must report it")
	}
	if res.Budgets.MaxTurns != opts.MaxTurns {
		t.Errorf("budgets not reported: %+v", res.Budgets)
	}
	if len(res.Sensors) == 0 {
		t.Error("the gate result is the evidence behind Converged and must be carried")
	}
	if res.Capabilities == "" || res.YnhVersion == "" {
		t.Error("the envelope must identify the contract version it speaks")
	}
}

// The case that actually matters. A run that did not converge is the one worth
// investigating, so it must still report what it consumed and what it touched
// — returning nothing on failure would leave a pipeline back to tailing NDJSON
// for exactly the runs it cares about.
func TestRunLoop_ReturnsAResultOnFailure(t *testing.T) {
	mb := &mockBackend{name: "mock", turns: []Turn{{Content: "1"}, {Content: "2"}, {Content: "3"}}}
	var stdout, stderr bytes.Buffer
	opts := baseOpts(mb, &stdout, &stderr, strings.NewReader(""))
	opts.testSensorNames = []string{"lint"}
	opts.MaxTurns = 2
	stubCheck(t, func(_ int, name string) gate.Result {
		return gate.Result{Name: name, Kind: "command", Tolerance: "blocking", Status: gate.StatusFail, ExitCode: 1}
	})

	res, err := RunLoop(opts)
	if err == nil {
		t.Fatal("a failing blocking sensor must not converge")
	}
	if res == nil {
		t.Fatal("a result must be returned on failure too — that is the run worth reading")
	}
	if res.Converged {
		t.Error("Converged must be false when the run did not converge")
	}
	if res.ExitCode != ExitIterationCap {
		t.Errorf("exit = %d, want ExitIterationCap (%d)", res.ExitCode, ExitIterationCap)
	}
	if res.Reason == "" {
		t.Error("a non-zero exit must say why")
	}
	// A cap nobody chose that fires is noise in a batch; a chosen cap that
	// fires is a finding. BoundBy plus BudgetSources is what tells them apart.
	if res.BoundBy != string(BudgetTurns) {
		t.Errorf("bound_by = %q, want %q", res.BoundBy, BudgetTurns)
	}
	if res.Consumed.Turns != 2 {
		t.Errorf("consumed turns = %d, want 2", res.Consumed.Turns)
	}
}

// The exit code in the JSON and the process exit code must never disagree —
// a consumer reading one and branching on the other would be silently wrong.
func TestRunResult_ExitCodeMatchesTheError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
		conv bool
	}{
		{"converged", nil, ExitConverged, true},
		{"stuck", &ExitError{Code: ExitStuck, Message: "stuck: no progress"}, ExitStuck, false},
		{"tamper", &ExitError{Code: ExitTamper, Message: "baseline changed"}, ExitTamper, false},
		{"gate broken", &ExitError{Code: ExitGateError, Message: "harness missing"}, ExitGateError, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &RunResult{}
			r.finalise(c.err)
			if r.ExitCode != c.want || r.Converged != c.conv {
				t.Errorf("exit=%d converged=%v, want %d/%v", r.ExitCode, r.Converged, c.want, c.conv)
			}
		})
	}
}

// A plain error means the loop failed before it could classify itself.
// Reporting 0 there would say "converged" about a run that did not — the one
// lie this contract exists to prevent.
func TestRunResult_UnclassifiedErrorIsNotConverged(t *testing.T) {
	r := &RunResult{}
	r.finalise(os.ErrNotExist)
	if r.Converged || r.ExitCode == ExitConverged {
		t.Errorf("an unclassified error must never read as converged: exit=%d converged=%v",
			r.ExitCode, r.Converged)
	}
	if r.Reason == "" {
		t.Error("the reason must carry the error")
	}
}

// "What did this run actually do to the tree" has to include new files —
// which is most of what an agent produces. Tracked-only would report nothing
// for a run whose entire output is new.
func TestChangedFiles_SeesNewAndModifiedAndIgnoresClean(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "kept.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-qm", "base")

	base := baseCommit(dir)
	if got := changedFiles(dir, base); len(got) != 0 {
		t.Errorf("a clean tree changed nothing, got %v", got)
	}

	if err := os.WriteFile(filepath.Join(dir, "kept.txt"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "brand-new.go"), []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := changedFiles(dir, base)
	has := func(p string) bool {
		for _, g := range got {
			if g == p {
				return true
			}
		}
		return false
	}
	if !has("kept.txt") {
		t.Errorf("modified file missing from %v", got)
	}
	if !has("brand-new.go") {
		t.Errorf("untracked new file missing from %v — most agent output is new files", got)
	}
}

// A worktree that is not a git repository must report nothing rather than
// failing the run. Provenance is worth having, not worth dying for.
func TestChangedFiles_NonRepoIsNotFatal(t *testing.T) {
	if got := changedFiles(t.TempDir(), ""); len(got) != 0 {
		t.Errorf("a non-repo should report no changes, got %v", got)
	}
	if got := changedFiles("", ""); got == nil || len(got) != 0 {
		t.Errorf("an empty dir should report an empty list, not nil: %v", got)
	}
}

// A run with no harness verified nothing, and the field must be absent rather
// than carrying the "(none)" placeholder the trajectory uses for display.
// A consumer reading a harness literally named "(none)" is worse served than
// one reading no harness at all.
func TestRunLoop_NoHarnessReportsNoHarness(t *testing.T) {
	mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
	var stdout, stderr bytes.Buffer
	opts := baseOpts(mb, &stdout, &stderr, strings.NewReader(""))
	opts.HarnessName = ""

	res, err := RunLoop(opts)
	if err != nil {
		t.Fatalf("a run with no harness converges on worker completion: %v", err)
	}
	if res.Harness != nil {
		t.Errorf("harness must be absent when none was named, got %+v", res.Harness)
	}
}

// A run cannot observe its own image digest, so it is passed in. Absent must
// mean "not recorded" rather than a guess — a wrong digest in a graded corpus
// is indistinguishable from a right one until someone tries to reproduce the
// run and cannot.
func TestImageDigest_ComesFromTheLauncherOrNotAtAll(t *testing.T) {
	t.Run("absent when unset", func(t *testing.T) {
		t.Setenv(imageDigestEnv, "")
		if got := imageDigest(); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
	t.Run("recorded when the launcher passes it", func(t *testing.T) {
		const d = "sha256:9f2c1ab4e7d3c0518b6a2f4d9e8c7b1a3f5d6e2077aa11bb22cc33dd44ee5566"
		t.Setenv(imageDigestEnv, "  "+d+"\n")
		if got := imageDigest(); got != d {
			t.Errorf("got %q, want %q trimmed", got, d)
		}
	})
}

// Version alone is an author-declared string that can be reused across
// different content. The SHA is what actually pins a run to one set of
// sensors, so a result that has provenance must carry it.
func TestRunLoop_CarriesHarnessSHAAndImageDigest(t *testing.T) {
	const digest = "sha256:abc123def456"
	t.Setenv(imageDigestEnv, digest)

	mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
	var stdout, stderr bytes.Buffer
	opts := baseOpts(mb, &stdout, &stderr, strings.NewReader(""))

	res, err := RunLoop(opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.ImageDigest != digest {
		t.Errorf("image_digest = %q, want %q", res.ImageDigest, digest)
	}
}

// Version is author-declared and can be reused across different content; the
// SHA is what pins a run to one set of sensors. An untested provenance field
// is worse than none — it looks like evidence.
func TestHarnessProvenance(t *testing.T) {
	cases := []struct {
		name     string
		hName    string
		h        *harness.Harness
		wantNil  bool
		wantVer  string
		wantSHA  string
		wantName string
	}{
		{
			name:    "no harness named — the run verified nothing",
			hName:   "",
			h:       &harness.Harness{Version: "1.0"},
			wantNil: true,
		},
		{
			name:     "installed from git — SHA pins the sensors",
			hName:    "eyelock/demo",
			h:        &harness.Harness{Version: "0.2.0", InstalledFrom: &harness.Provenance{SHA: "9f2c1ab"}},
			wantName: "eyelock/demo",
			wantVer:  "0.2.0",
			wantSHA:  "9f2c1ab",
		},
		{
			name:     "local harness with no recorded SHA",
			hName:    "local/demo",
			h:        &harness.Harness{Version: "0.1.0", InstalledFrom: &harness.Provenance{SourceType: "local"}},
			wantName: "local/demo",
			wantVer:  "0.1.0",
		},
		{
			name:     "named but unloadable — the name is still worth recording",
			hName:    "local/demo",
			h:        nil,
			wantName: "local/demo",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := harnessProvenance(c.hName, c.h)
			if c.wantNil {
				if got != nil {
					t.Fatalf("want nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("want a harness, got nil")
			}
			if got.Name != c.wantName || got.Version != c.wantVer || got.SHA != c.wantSHA {
				t.Errorf("got %+v, want name=%q version=%q sha=%q",
					got, c.wantName, c.wantVer, c.wantSHA)
			}
		})
	}
}
