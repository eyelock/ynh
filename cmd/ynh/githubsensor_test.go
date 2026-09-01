package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/gate"
	"github.com/eyelock/ynh/internal/plugin"
)

// fakeGH puts a stub `gh` on PATH that echoes a canned payload, and a git repo
// on disk for the ref to resolve against. Tests never reach the network, which
// is the point: a sensor that can only be tested against live GitHub is a
// sensor nobody can test.
func fakeGH(t *testing.T, payload string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	payloadFile := filepath.Join(dir, "payload.json")
	if err := os.WriteFile(payloadFile, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncase \"$*\" in\n  *\"repo view\"*) echo eyelock/demo ;;\n  *) cat " + payloadFile + " ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	mustGit(t, work, "init")
	mustGit(t, work, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "x")
	return work
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := runGitForTest(dir, args...); err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, out)
	}
}

func TestObserveGitHubStatus(t *testing.T) {
	cases := []struct {
		name     string
		payload  string
		src      plugin.GitHubStatusSource
		wantCode int
		wantIn   string
	}{
		{
			name:     "required state present",
			payload:  `{"statuses":[{"context":"security/snyk","state":"success"}]}`,
			src:      plugin.GitHubStatusSource{Context: "security/snyk"},
			wantCode: 0,
		},
		{
			name:     "wrong state is a finding",
			payload:  `{"statuses":[{"context":"security/snyk","state":"failure","description":"2 high"}]}`,
			src:      plugin.GitHubStatusSource{Context: "security/snyk"},
			wantCode: 1,
			wantIn:   "2 high",
		},
		{
			// The distinction that matters: no verdict is not a bad verdict.
			name:     "absent context cannot be observed",
			payload:  `{"statuses":[{"context":"other","state":"success"}]}`,
			src:      plugin.GitHubStatusSource{Context: "security/snyk"},
			wantCode: gate.ExitObservationUnavailable,
			wantIn:   "seen: other",
		},
		{
			name:     "pending is not failure",
			payload:  `{"statuses":[{"context":"security/snyk","state":"pending"}]}`,
			src:      plugin.GitHubStatusSource{Context: "security/snyk"},
			wantCode: gate.ExitObservationUnavailable,
			wantIn:   "still pending",
		},
		{
			name:     "on_missing pass opts out",
			payload:  `{"statuses":[]}`,
			src:      plugin.GitHubStatusSource{Context: "security/snyk", OnMissing: "pass"},
			wantCode: 0,
		},
		{
			name:     "on_missing fail blocks instead of breaking",
			payload:  `{"statuses":[]}`,
			src:      plugin.GitHubStatusSource{Context: "security/snyk", OnMissing: "fail"},
			wantCode: 1,
		},
		{
			name:     "require can select a non-success state",
			payload:  `{"statuses":[{"context":"c","state":"pending"}]}`,
			src:      plugin.GitHubStatusSource{Context: "c", Require: "pending"},
			wantCode: 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			work := fakeGH(t, c.payload)
			code, out := observeGitHubStatus(work, &c.src)
			if code != c.wantCode {
				t.Errorf("exit = %d, want %d\noutput: %s", code, c.wantCode, out)
			}
			if c.wantIn != "" && !strings.Contains(out, c.wantIn) {
				t.Errorf("output missing %q\ngot: %s", c.wantIn, out)
			}
		})
	}
}

func TestObserveGitHubCheck(t *testing.T) {
	cases := []struct {
		name     string
		payload  string
		src      plugin.GitHubCheckSource
		wantCode int
		wantIn   string
	}{
		{
			name:     "completed success",
			payload:  `{"check_runs":[{"name":"CodeQL","status":"completed","conclusion":"success","app":{"slug":"github"}}]}`,
			src:      plugin.GitHubCheckSource{Name: "CodeQL"},
			wantCode: 0,
		},
		{
			name:     "completed failure is a finding",
			payload:  `{"check_runs":[{"name":"CodeQL","status":"completed","conclusion":"failure","app":{"slug":"github"}}]}`,
			src:      plugin.GitHubCheckSource{Name: "CodeQL"},
			wantCode: 1,
		},
		{
			// A run with no conclusion has not answered, which is not "no".
			name:     "in_progress has no conclusion to read",
			payload:  `{"check_runs":[{"name":"CodeQL","status":"in_progress","app":{"slug":"github"}}]}`,
			src:      plugin.GitHubCheckSource{Name: "CodeQL"},
			wantCode: gate.ExitObservationUnavailable,
			wantIn:   "not completed",
		},
		{
			name:     "app filter excludes a same-named run",
			payload:  `{"check_runs":[{"name":"CodeQL","status":"completed","conclusion":"success","app":{"slug":"impostor"}}]}`,
			src:      plugin.GitHubCheckSource{Name: "CodeQL", App: "github"},
			wantCode: gate.ExitObservationUnavailable,
			wantIn:   `from app "github"`,
		},
		{
			name:     "app filter admits the right one",
			payload:  `{"check_runs":[{"name":"CodeQL","status":"completed","conclusion":"success","app":{"slug":"github"}}]}`,
			src:      plugin.GitHubCheckSource{Name: "CodeQL", App: "github"},
			wantCode: 0,
		},
		{
			name:     "skipped can be the required conclusion",
			payload:  `{"check_runs":[{"name":"CodeQL","status":"completed","conclusion":"skipped","app":{"slug":"github"}}]}`,
			src:      plugin.GitHubCheckSource{Name: "CodeQL", Require: "skipped"},
			wantCode: 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			work := fakeGH(t, c.payload)
			code, out := observeGitHubCheck(work, &c.src)
			if code != c.wantCode {
				t.Errorf("exit = %d, want %d\noutput: %s", code, c.wantCode, out)
			}
			if c.wantIn != "" && !strings.Contains(out, c.wantIn) {
				t.Errorf("output missing %q\ngot: %s", c.wantIn, out)
			}
		})
	}
}

// A malformed payload must break the gate, never pass it. Reaching the network
// and getting nonsense is the same epistemic position as not reaching it.
func TestGitHubSensorRejectsUnparseablePayload(t *testing.T) {
	work := fakeGH(t, `not json at all`)
	code, out := observeGitHubStatus(work, &plugin.GitHubStatusSource{Context: "c"})
	if code != gate.ExitObservationUnavailable {
		t.Errorf("exit = %d, want %d (broken), output: %s", code, gate.ExitObservationUnavailable, out)
	}
}

func TestApplyOnMissingDefaultsToBroken(t *testing.T) {
	for policy, want := range map[string]int{
		"":       gate.ExitObservationUnavailable,
		"broken": gate.ExitObservationUnavailable,
		"fail":   1,
		"pass":   0,
		"typo":   gate.ExitObservationUnavailable,
	} {
		if got := applyOnMissing(policy); got != want {
			t.Errorf("applyOnMissing(%q) = %d, want %d", policy, got, want)
		}
	}
}

// runGitForTest runs git in dir, for building fixtures.
func runGitForTest(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// A ref is attacker-controlled in the sense that matters: it comes from a
// harness manifest, which may have been composed from a remote source. git
// reads a leading "-" as an option, so "--upload-pack=..." would run a command
// of the declaration's choosing. ynh#358 closed this for ls-remote.
func TestGitHubSensorRejectsOptionLikeRef(t *testing.T) {
	work := fakeGH(t, `{"statuses":[{"context":"c","state":"success"}]}`)
	for _, ref := range []string{"--upload-pack=touch /tmp/pwned", "-x", "--help"} {
		code, out := observeGitHubStatus(work, &plugin.GitHubStatusSource{Context: "c", Ref: ref})
		if code != gate.ExitObservationUnavailable {
			t.Errorf("ref %q: exit = %d, want broken", ref, code)
		}
		if !strings.Contains(out, "may not begin with") {
			t.Errorf("ref %q: expected a refusal naming the reason, got: %s", ref, out)
		}
	}
}

// A ref that is merely unknown is a different failure from one that is
// dangerous, and both must stop the gate rather than pass it.
func TestGitHubSensorUnknownRefIsBroken(t *testing.T) {
	work := fakeGH(t, `{"statuses":[{"context":"c","state":"success"}]}`)
	code, out := observeGitHubStatus(work, &plugin.GitHubStatusSource{Context: "c", Ref: "no-such-ref"})
	if code != gate.ExitObservationUnavailable {
		t.Errorf("exit = %d, want broken; output: %s", code, out)
	}
}
