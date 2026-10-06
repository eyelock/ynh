package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/vendor"
)

// vendorCLINames lists every program a vendor CLI may be installed as: each
// vendor adapter's CLIName, plus "cursor", the Cursor editor's launcher, and
// "cursor-agent", the Cursor CLI's older alias.
func vendorCLINames(t *testing.T) []string {
	t.Helper()
	names := []string{"cursor", "cursor-agent"}
	for _, v := range vendor.Available() {
		adapter, err := vendor.Get(v)
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, adapter.CLIName())
	}
	return names
}

// shadowVendorCLIs writes, into dir, a failing stub for every vendor CLI that
// dir does not already hold a stub for, so a test that puts dir first on PATH
// cannot fall through to a real install, which would run on the developer's
// account. A stub named for the wrong binary is otherwise silently skipped
// (#524).
func shadowVendorCLIs(t *testing.T, dir string) {
	t.Helper()
	for _, name := range vendorCLINames(t) {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		script := "#!/bin/sh\necho 'test stub: " + name + " is a vendor CLI and must not run here' >&2\nexit 97\n"
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// vendorShims puts a fake of every vendor CLI first on PATH. Each one records
// that it was started, writes failure to stderr and exits 1, which is what a
// vendor that refuses to start looks like. It returns the file the starts are
// recorded in.
func vendorShims(t *testing.T, failure string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX-only")
	}
	dir := t.TempDir()
	starts := filepath.Join(dir, "starts")
	for _, name := range vendorCLINames(t) {
		script := "#!/bin/sh\necho " + name + " >> '" + starts + "'\necho '" + failure + "' >&2\nexit 1\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return starts
}

// writeAgentCheckpoint writes a checkpoint.json a resume reads.
func writeAgentCheckpoint(t *testing.T, dir string, cp map[string]any) {
	t.Helper()
	cp["version"] = 1
	cp["session_id"] = "s-exit"
	data, err := json.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "checkpoint.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// processExitCode is the status the ynh process ends with when a command
// returns err, as main decides it for `ynh agent run`.
func processExitCode(err error) int {
	if err == nil {
		return 0
	}
	var coded *exitCodeError
	if errors.As(err, &coded) {
		return coded.code
	}
	return 1
}

// A run refused before any worker starts exits with the code its JSON result
// reports, and starts nothing (#509). It used to exit 1 while the result said
// 20, a worker error, about a worker that never ran.
func TestCmdAgentRun_RefusalExitCodeMatchesResult(t *testing.T) {
	const (
		plain    = `{"name":"plain","version":"0.1.0","default_vendor":"claude","focuses":{"reviewer":{"prompt":"review it"}}}`
		verifier = `{"name":"verifier","version":"0.1.0","default_vendor":"claude","sensors":{"review":{"role":"convergence-verifier","source":{"focus":"reviewer"},"output":{"format":"text"}}},"focuses":{"reviewer":{"prompt":"review it"}}}`
		effort   = `{"name":"effort","version":"0.1.0","default_vendor":"claude","agent":{"effort":"low"}}`
	)
	tests := []struct {
		name     string
		args     []string
		files    map[string]string // written into the worktree
		harness  string            // manifest installed as local/<name>
		resume   map[string]any    // checkpoint written into the --resume dir
		wantCode int
		wantSub  string
	}{
		{name: "unknown backend", args: []string{"--task", "t", "--backend", "gemini"},
			wantCode: 1, wantSub: `unknown backend "gemini"`},
		{name: "sandbox the backend cannot use", args: []string{"--task", "t", "--backend", "codex", "--sandbox", "srt"},
			wantCode: 1, wantSub: "not supported by the codex backend"},
		{name: "unknown auto-approve level", args: []string{"--task", "t", "--auto-approve", "sometimes"},
			wantCode: 1, wantSub: "unknown --auto-approve level"},
		{name: "auto-approve edits on codex", args: []string{"--task", "t", "--backend", "codex", "--auto-approve", "edits"},
			wantCode: 1, wantSub: "codex cannot auto-approve edits only"},
		{name: "auto-approve edits on cursor", args: []string{"--task", "t", "--backend", "cursor", "--auto-approve", "edits"},
			wantCode: 1, wantSub: "cursor cannot auto-approve edits only"},
		{name: "unknown effort level", args: []string{"--task", "t", "--effort", "max"},
			wantCode: 1, wantSub: `unknown effort level "max"`},
		{name: "effort on cursor", args: []string{"--task", "t", "--backend", "cursor", "--effort", "high"},
			wantCode: 1, wantSub: "cursor has no reasoning effort setting"},
		{name: "claude project permission mode", args: []string{"--task", "t", "--auto-approve", "all"},
			files:    map[string]string{".claude/settings.json": `{"permissions":{"defaultMode":"plan"}}`},
			wantCode: 1, wantSub: "the project's choice wins"},
		{name: "codex project approval policy", args: []string{"--task", "t", "--backend", "codex", "--auto-approve", "all"},
			files:    map[string]string{".codex/config.toml": "approval_policy = \"on-request\"\n"},
			wantCode: 1, wantSub: "approval_policy"},
		{name: "trajectory cannot be opened", args: []string{"--task", "t", "--emit-jsonl", "{missing}/trajectory.jsonl"},
			wantCode: 1, wantSub: "opening trajectory file"},
		{name: "harness not installed", args: []string{"--task", "t", "--harness", "local/absent"},
			wantCode: 1, wantSub: `loading harness "local/absent"`},
		{name: "focus not in the harness", args: []string{"--harness", "local/plain", "--focus", "absent"},
			harness: plain, wantCode: 1, wantSub: `focus "absent" not defined`},
		{name: "profile not in the harness", args: []string{"--task", "t", "--harness", "local/plain", "--profile", "absent"},
			harness: plain, wantCode: 1, wantSub: `resolving profile "absent"`},
		{name: "profile without a harness", args: []string{"--task", "t", "--profile", "p"},
			wantCode: 1, wantSub: "--focus and --profile require --harness"},
		{name: "convergence verifier that can never pass", args: []string{"--task", "t", "--harness", "local/verifier"},
			harness: verifier, wantCode: 1, wantSub: "cannot be the convergence verifier"},
		{name: "harness effort on cursor", args: []string{"--task", "t", "--harness", "local/effort", "--backend", "cursor"},
			harness: effort, wantCode: 1, wantSub: "harness agent.effort: cursor has no reasoning effort setting"},
		{name: "resume with no checkpoint", args: []string{"--resume", "{resume}"},
			wantCode: 21, wantSub: "no checkpoint found"},
		{name: "resume on another backend", args: []string{"--resume", "{resume}", "--backend", "claude"},
			resume:   map[string]any{"backend": "codex", "task": "t", "phase": "act", "plan_finalized": true, "pending_message": "go"},
			wantCode: 21, wantSub: `"codex"`},
		{name: "resume with another task", args: []string{"--resume", "{resume}", "--task", "something else"},
			resume:   map[string]any{"backend": "claude", "task": "t", "phase": "act", "plan_finalized": true, "pending_message": "go"},
			wantCode: 21, wantSub: "task"},
		{name: "resume during planning with no task", args: []string{"--resume", "{resume}"},
			resume:   map[string]any{"backend": "claude", "phase": "plan"},
			wantCode: 21, wantSub: "pass --task"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("YNH_HOME", home)
			t.Setenv("YNH_AGENT_SESSION", "")
			starts := vendorShims(t, "a refused run must not start a worker")
			if tt.harness != "" {
				var m struct {
					Name string `json:"name"`
				}
				if err := json.Unmarshal([]byte(tt.harness), &m); err != nil {
					t.Fatal(err)
				}
				installListTestHarness(t, home, m.Name, tt.harness)
			}
			worktree := t.TempDir()
			for rel, content := range tt.files {
				p := filepath.Join(worktree, rel)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			resumeDir := t.TempDir()
			if tt.resume != nil {
				writeAgentCheckpoint(t, resumeDir, tt.resume)
			}
			args := []string{"--format", "json", "--worktree", worktree}
			for _, a := range tt.args {
				a = strings.ReplaceAll(a, "{resume}", resumeDir)
				a = strings.ReplaceAll(a, "{missing}", filepath.Join(t.TempDir(), "missing"))
				args = append(args, a)
			}

			var stdout, stderr bytes.Buffer
			err := cmdAgentRun(args, &stdout, &stderr, strings.NewReader(""))
			if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantSub)
			}
			var result struct {
				ExitCode  int    `json:"exit_code"`
				Converged bool   `json:"converged"`
				Reason    string `json:"reason"`
			}
			if jerr := json.Unmarshal(stdout.Bytes(), &result); jerr != nil {
				t.Fatalf("--format json printed no result: %v\nstdout: %s", jerr, stdout.String())
			}
			if got := processExitCode(err); got != result.ExitCode {
				t.Errorf("process exits %d but the result says exit_code %d", got, result.ExitCode)
			}
			if result.ExitCode != tt.wantCode {
				t.Errorf("exit_code = %d, want %d", result.ExitCode, tt.wantCode)
			}
			if result.Converged || !strings.Contains(result.Reason, tt.wantSub) {
				t.Errorf("converged=%v reason=%q, want an unconverged result giving the refusal", result.Converged, result.Reason)
			}
			if data, rerr := os.ReadFile(starts); rerr == nil {
				t.Errorf("a refused run started a worker: %s", data)
			}
		})
	}
}

// A worker that starts and then fails is still a worker error, 20, in the
// process exit and the result alike.
func TestCmdAgentRun_WorkerErrorExitCodeMatchesResult(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	t.Setenv("YNH_AGENT_SESSION", "")
	starts := vendorShims(t, "Not logged in")

	var stdout, stderr bytes.Buffer
	err := cmdAgentRun([]string{"--task", "t", "--no-plan", "--format", "json", "--worktree", t.TempDir()},
		&stdout, &stderr, strings.NewReader(""))
	var result struct {
		ExitCode int `json:"exit_code"`
	}
	if jerr := json.Unmarshal(stdout.Bytes(), &result); jerr != nil {
		t.Fatalf("--format json printed no result: %v\nstdout: %s", jerr, stdout.String())
	}
	if got := processExitCode(err); got != 20 || result.ExitCode != 20 {
		t.Errorf("process exits %d, result says %d, want 20 for both (err: %v)", got, result.ExitCode, err)
	}
	if _, rerr := os.Stat(starts); rerr != nil {
		t.Errorf("the worker never started, so this proves nothing: %v", rerr)
	}
}

// Text output reports the error once on stderr; JSON output leaves stderr
// clean, as the result carries the reason.
func TestCmdAgentRun_RefusalReportedOnce(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	vendorShims(t, "unused")
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := cmdAgentRun([]string{"--task", "t", "--effort", "max", "--format", format, "--worktree", t.TempDir()},
				&stdout, &stderr, strings.NewReader(""))
			if processExitCode(err) != 1 {
				t.Fatalf("exit = %d, want 1 (err: %v)", processExitCode(err), err)
			}
			want := ""
			if format == "text" {
				want = "Error: " + err.Error() + "\n"
			}
			if stderr.String() != want {
				t.Errorf("stderr = %q, want %q", stderr.String(), want)
			}
		})
	}
}
