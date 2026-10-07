package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/vendor"
)

func TestValidateAutoApprove(t *testing.T) {
	tests := []struct {
		level, backend string
		wantSub        string // "" means accepted
	}{
		{"", "claude", ""},
		{"", "codex", ""},
		{"", "cursor", ""},
		{"edits", "claude", ""},
		{"all", "claude", ""},
		{"all", "codex", ""},
		{"all", "cursor", ""},
		{"edits", "codex", "codex cannot auto-approve edits only"},
		{"edits", "cursor", "cursor cannot auto-approve edits only"},
		{"all", "copilot", "not supported by the copilot backend"},
		{"yes", "claude", `unknown --auto-approve level "yes"`},
		{"Edits", "claude", "unknown --auto-approve level"},
	}
	for _, tt := range tests {
		t.Run(tt.level+"/"+tt.backend, func(t *testing.T) {
			err := validateAutoApprove(tt.level, tt.backend)
			if tt.wantSub == "" {
				if err != nil {
					t.Fatalf("want accepted, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantSub)
			}
		})
	}
}

// permissionFlags is every vendor flag that widens what a worker may do
// without asking. None may appear unless --auto-approve asked for it.
var permissionFlags = []string{
	"--permission-mode", "--dangerously-skip-permissions", "--allow-dangerously-skip-permissions",
	"--dangerously-bypass-approvals-and-sandbox", "--yolo", "--full-auto", "--ask-for-approval",
	"-a", "--sandbox", "-s", "--force", "-f",
}

// backendArgs returns the argument list each backend builds for opts.
func backendArgs(backend string, opts StartOptions) []string {
	switch backend {
	case "claude":
		return buildClaudeStreamArgs(opts)
	case "codex":
		return buildCodexArgs(opts, "")
	default:
		return buildCursorArgs(opts, "chat", true, "do the task")
	}
}

func TestBackendArgs_AutoApproveMapping(t *testing.T) {
	tests := []struct {
		backend, level string
		want           []string // the permission flags expected, in order
	}{
		// The old doctrine, kept as a regression guard: by default ynh
		// passes no permission flag to any backend.
		{"claude", "", nil},
		{"codex", "", nil},
		{"cursor", "", nil},
		{"claude", "edits", []string{"--permission-mode", "acceptEdits"}},
		{"claude", "all", []string{"--permission-mode", "bypassPermissions"}},
		{"codex", "all", []string{"--dangerously-bypass-approvals-and-sandbox"}},
		{"cursor", "all", []string{"--force"}},
	}
	for _, tt := range tests {
		t.Run(tt.backend+"/"+tt.level, func(t *testing.T) {
			args := backendArgs(tt.backend, StartOptions{AutoApprove: tt.level, Model: "m"})
			var got []string
			for i, a := range args {
				if slices.Contains(permissionFlags, a) {
					got = append(got, a)
					if a == "--permission-mode" && i+1 < len(args) {
						got = append(got, args[i+1])
					}
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("permission flags = %q, want %q (args %q)", got, tt.want, args)
			}
		})
	}
}

// The user message stays the last argument cursor receives, after --force.
func TestBuildCursorArgs_MessageStaysLast(t *testing.T) {
	args := buildCursorArgs(StartOptions{AutoApprove: AutoApproveAll}, "chat", false, "fix it")
	if args[len(args)-1] != "fix it" {
		t.Errorf("last arg = %q, want the message (args %q)", args[len(args)-1], args)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProjectPermissionSetting(t *testing.T) {
	tests := []struct {
		name    string
		backend string
		files   map[string]string
		wantSub string // "" means no project setting
		wantErr bool
	}{
		{name: "claude, no settings", backend: "claude"},
		{
			name:    "claude settings.json sets defaultMode",
			backend: "claude",
			files:   map[string]string{".claude/settings.json": `{"permissions":{"defaultMode":"plan"}}`},
			wantSub: `settings.json sets permissions.defaultMode "plan"`,
		},
		{
			name:    "claude settings.local.json sets defaultMode",
			backend: "claude",
			files:   map[string]string{".claude/settings.local.json": `{"permissions":{"defaultMode":"acceptEdits"}}`},
			wantSub: `settings.local.json sets permissions.defaultMode "acceptEdits"`,
		},
		{
			name:    "claude settings without a mode",
			backend: "claude",
			files:   map[string]string{".claude/settings.json": `{"permissions":{"allow":["Bash(go test:*)"]}}`},
		},
		{
			name:    "claude settings that do not parse",
			backend: "claude",
			files:   map[string]string{".claude/settings.json": `{`},
			wantErr: true,
		},
		{
			name:    "codex top-level approval_policy",
			backend: "codex",
			files:   map[string]string{".codex/config.toml": "model = \"x\"\napproval_policy = \"on-request\"\n"},
			wantSub: `config.toml sets approval_policy = "on-request"`,
		},
		{
			name:    "codex top-level sandbox_mode",
			backend: "codex",
			files:   map[string]string{".codex/config.toml": "sandbox_mode = \"workspace-write\"\n"},
			wantSub: `sets sandbox_mode = "workspace-write"`,
		},
		{
			name:    "codex keys under a table or commented out do not count",
			backend: "codex",
			files: map[string]string{".codex/config.toml": "# approval_policy = \"never\"\n" +
				"[profiles.ci]\napproval_policy = \"never\"\n"},
		},
		{
			name:    "cursor project file holds only allow and deny lists",
			backend: "cursor",
			files:   map[string]string{".cursor/cli.json": `{"permissions":{"allow":[],"deny":["Shell(rm)"]}}`},
		},
		{
			name:    "another vendor's settings are not this backend's",
			backend: "codex",
			files:   map[string]string{".claude/settings.json": `{"permissions":{"defaultMode":"plan"}}`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, content := range tt.files {
				writeFile(t, filepath.Join(dir, rel), content)
			}
			got, err := projectPermissionSetting(dir, tt.backend)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantSub == "" && got != "" {
				t.Errorf("reason = %q, want none", got)
			}
			if tt.wantSub != "" && !strings.Contains(got, tt.wantSub) {
				t.Errorf("reason = %q, want it to contain %q", got, tt.wantSub)
			}
		})
	}
}

// User-level settings are the operator's own defaults; the flag overrides
// them for one run, so they are never consulted.
func TestProjectPermissionSetting_IgnoresUserSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(home, ".claude", "settings.json"), `{"permissions":{"defaultMode":"plan"}}`)
	writeFile(t, filepath.Join(home, ".codex", "config.toml"), "approval_policy = \"never\"\n")
	project := t.TempDir()
	for _, backend := range []string{"claude", "codex", "cursor"} {
		got, err := projectPermissionSetting(project, backend)
		if err != nil || got != "" {
			t.Errorf("%s: got %q, %v; want no setting", backend, got, err)
		}
	}
}

func readSessionStart(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		var ev struct {
			Type string         `json:"type"`
			Data map[string]any `json:"data"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) == nil && ev.Type == string(KindSessionStart) {
			return ev.Data
		}
	}
	t.Fatal("no session_start event in trajectory")
	return nil
}

func TestRunLoop_AutoApproveIsRecordedAndPassed(t *testing.T) {
	for _, level := range []string{"", "edits", "all"} {
		t.Run("level="+level, func(t *testing.T) {
			mb := &mockBackend{name: "claude", turns: []Turn{{Content: "done"}}}
			var stdout, stderr bytes.Buffer
			opts := baseOpts(mb, &stdout, &stderr, strings.NewReader(""))
			opts.AutoApprove = level
			opts.WorktreeDir = t.TempDir()
			opts.EmitJSONL = filepath.Join(t.TempDir(), "trajectory.jsonl")

			result, err := RunLoop(opts)
			if err != nil {
				t.Fatalf("RunLoop: %v", err)
			}
			if len(mb.startOpts) != 1 || mb.startOpts[0].AutoApprove != level {
				t.Fatalf("worker StartOptions.AutoApprove = %+v, want %q", mb.startOpts, level)
			}
			if result.AutoApprove != level {
				t.Errorf("result.AutoApprove = %q, want %q", result.AutoApprove, level)
			}
			start := readSessionStart(t, opts.EmitJSONL)
			got, present := start["auto_approve"]
			if level == "" && present {
				t.Errorf("session_start carries auto_approve %v on a run without the flag", got)
			}
			if level != "" && got != level {
				t.Errorf("session_start auto_approve = %v, want %q", got, level)
			}
		})
	}
}

func TestRunLoop_AutoApproveRefusedBeforeTheWorkerStarts(t *testing.T) {
	tests := []struct {
		name    string
		backend string
		level   string
		files   map[string]string
		wantSub string
	}{
		{
			name:    "project settings.json chooses a mode",
			backend: "claude",
			level:   "edits",
			files:   map[string]string{".claude/settings.json": `{"permissions":{"defaultMode":"default"}}`},
			wantSub: "the project's choice wins",
		},
		{
			name:    "project settings.local.json chooses a mode",
			backend: "claude",
			level:   "all",
			files:   map[string]string{".claude/settings.local.json": `{"permissions":{"defaultMode":"plan"}}`},
			wantSub: "settings.local.json sets permissions.defaultMode",
		},
		{
			name:    "codex project config chooses a policy",
			backend: "codex",
			level:   "all",
			files:   map[string]string{".codex/config.toml": "approval_policy = \"on-request\"\n"},
			wantSub: "approval_policy",
		},
		{name: "edits on codex", backend: "codex", level: "edits", wantSub: "cannot auto-approve edits only"},
		{name: "edits on cursor", backend: "cursor", level: "edits", wantSub: "cannot auto-approve edits only"},
		{name: "unknown level", backend: "claude", level: "everything", wantSub: "unknown --auto-approve level"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, content := range tt.files {
				writeFile(t, filepath.Join(dir, rel), content)
			}
			mb := &mockBackend{name: tt.backend, turns: []Turn{{Content: "done"}}}
			var stdout, stderr bytes.Buffer
			opts := baseOpts(mb, &stdout, &stderr, strings.NewReader(""))
			opts.Backend = tt.backend
			opts.AutoApprove = tt.level
			opts.WorktreeDir = dir

			_, err := RunLoop(opts)
			if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantSub)
			}
			if len(mb.startOpts) != 0 {
				t.Errorf("the worker was started %d times; a refused run must not start one", len(mb.startOpts))
			}
		})
	}
}

// fakeVendor puts the CLI of backend's vendor on PATH, as a stub that records
// its arguments, writes stderrText to stderr, prints stdout, and exits with
// code. It never reads stdin, so a worker that writes to it may find it gone:
// the same race a real vendor refusing to start produces. The binary's name
// is the vendor adapter's, and every other vendor CLI is shadowed.
func fakeVendor(t *testing.T, backend, stdout, stderrText string, code int) (argsFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX-only")
	}
	adapter, err := vendor.Get(backend)
	if err != nil {
		t.Fatal(err)
	}
	return fakeProgram(t, adapter.CLIName(), stdout, stderrText, code)
}

// fakeProgram puts a stub named name first on PATH, in front of failing stubs
// for every vendor CLI, and returns the file its arguments are written to. Use
// it for a program that is not a vendor CLI, such as srt; fakeVendor resolves a
// vendor's binary through its adapter and calls it.
func fakeProgram(t *testing.T, name, stdout, stderrText string, code int) (argsFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX-only")
	}
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > " + argsFile + "\n"
	if stdout != "" {
		script += "cat <<'OUTEOF'\n" + stdout + "\nOUTEOF\n"
	}
	if stderrText != "" {
		script += "echo '" + stderrText + "' >&2\n"
	}
	script += "exit " + itoa(code) + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	shadowVendorCLIs(t, dir)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argsFile
}

// claudeRootRefusal is what Claude Code 2.1.288 writes to stderr, before
// exiting 1, when bypassPermissions is requested by root outside a sandbox
// (found in the shipped binary; not reproducible without root).
const claudeRootRefusal = "--dangerously-skip-permissions cannot be used with root/sudo privileges for security reasons"

// A vendor that refuses to start ends the run as a worker error carrying the
// vendor's own words, whichever of Send or Next notices first.
func TestBackends_VendorRefusalIsWorkerError(t *testing.T) {
	tests := []struct {
		backend WorkerBackend
		stderr  string
	}{
		{&ClaudeBackend{}, claudeRootRefusal},
		{&CodexBackend{}, "error: approval policy disabled by managed configuration"},
		{&CursorBackend{}, "Error: --force is disabled by your administrator"},
	}
	for _, tt := range tests {
		t.Run(tt.backend.Name(), func(t *testing.T) {
			fakeVendor(t, tt.backend.Name(), "", tt.stderr, 1)
			var operator bytes.Buffer
			sess, err := tt.backend.Start(context.Background(), StartOptions{
				WorktreeDir: t.TempDir(),
				AutoApprove: AutoApproveAll,
				Stderr:      &operator,
			})
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			defer func() { _ = sess.Close() }()
			err = sess.Send("do the task")
			if err == nil {
				_, err = sess.Next()
			}
			var we *WorkerError
			if !errors.As(err, &we) {
				t.Fatalf("want *WorkerError, got %T %v", err, err)
			}
			if we.Message != tt.stderr || we.Backend != tt.backend.Name() {
				t.Errorf("WorkerError = %s/%q, want %s/%q", we.Backend, we.Message, tt.backend.Name(), tt.stderr)
			}
			if !strings.Contains(operator.String(), tt.stderr) {
				t.Errorf("stderr was not passed through to the operator: %q", operator.String())
			}
		})
	}
}

// A clean exit with no output is still io.EOF, not a worker error.
func TestBackends_CleanExitIsEOF(t *testing.T) {
	fakeVendor(t, "cursor", "", "", 0)
	sess, err := (&CursorBackend{}).Start(context.Background(), StartOptions{WorktreeDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Send("x"); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Next(); err != io.EOF {
		t.Errorf("err = %v, want io.EOF", err)
	}
}

func TestRunLoop_ClaudeRootRefusalIsExit20WithTheVendorMessage(t *testing.T) {
	argsFile := fakeVendor(t, "claude", "", claudeRootRefusal, 1)
	var stdout, stderr bytes.Buffer
	opts := baseOpts(&ClaudeBackend{}, &stdout, &stderr, strings.NewReader(""))
	opts.AutoApprove = AutoApproveAll
	opts.WorktreeDir = t.TempDir()

	result, err := RunLoop(opts)
	var exitErr *ExitError
	if !asExitError(err, &exitErr) || exitErr.Code != ExitWorkerError {
		t.Fatalf("want ExitWorkerError (%d), got %v", ExitWorkerError, err)
	}
	if want := "worker error: claude: " + claudeRootRefusal; exitErr.Message != want {
		t.Errorf("reason = %q, want %q", exitErr.Message, want)
	}
	if result.ExitCode != ExitWorkerError || result.Reason != exitErr.Message {
		t.Errorf("result = exit %d %q, want it to mirror the exit error", result.ExitCode, result.Reason)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "--permission-mode\nbypassPermissions\n") {
		t.Errorf("claude was not asked for bypassPermissions; args:\n%s", args)
	}
}

// Claude does not fail when a managed setting disables the requested mode: it
// downgrades the session and carries on. The init event says which mode it
// actually runs in, and a mismatch is a worker error rather than a grant that
// silently does not apply.
func TestClaudeSession_PermissionModeMismatch(t *testing.T) {
	initEv := func(mode string) string {
		return `{"type":"system","subtype":"init","permissionMode":"` + mode + `"}` + "\n"
	}
	result := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":3,"output_tokens":1}}}
{"type":"result","subtype":"success","is_error":false,"result":"ok","usage":{"input_tokens":3,"output_tokens":1}}
`
	tests := []struct {
		name    string
		want    string
		raw     string
		wantErr string
	}{
		{"granted mode honoured", "bypassPermissions", initEv("bypassPermissions") + result, ""},
		{"downgraded by settings", "bypassPermissions", initEv("auto") + result, `started in "auto"`},
		{"edits downgraded", "acceptEdits", initEv("default") + result, "--permission-mode acceptEdits"},
		{"no grant, any mode", "", initEv("plan") + result, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := claudeSessionOver(tt.raw)
			s.wantMode = tt.want
			_, err := s.Next()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Next: %v", err)
				}
				return
			}
			var we *WorkerError
			if !errors.As(err, &we) || !strings.Contains(we.Message, tt.wantErr) {
				t.Fatalf("err = %v, want a WorkerError containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestStderrTail_KeepsTheEnd(t *testing.T) {
	var tail stderrTail
	_, _ = tail.Write([]byte(strings.Repeat("noise\n", 2000)))
	_, _ = tail.Write([]byte("the reason\n"))
	got := tail.String()
	if !strings.HasSuffix(got, "the reason") || len(got) > stderrTailMax {
		t.Errorf("tail = %d bytes ending %q", len(got), got[max(0, len(got)-20):])
	}
	if strings.HasPrefix(got, "oise") || !strings.HasPrefix(got, "noise") {
		t.Errorf("tail should start on a line boundary, starts %q", got[:10])
	}
}
