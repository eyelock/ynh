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

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/permission"
	"github.com/eyelock/ynh/internal/plugin"
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
		{"", "opencode", ""},
		{"edits", "claude", ""},
		{"all", "claude", ""},
		{"all", "codex", ""},
		{"all", "cursor", ""},
		{"all", "opencode", ""},
		{"none", "claude", ""},
		{"none", "codex", ""},
		{"none", "opencode", ""},
		{"edits", "opencode", ""},
		{"none", "cursor", "cursor cannot hold auto-approve none exactly"},
		{"edits", "codex", "codex cannot hold auto-approve edits exactly"},
		{"edits", "cursor", "cursor cannot hold auto-approve edits exactly"},
		{"all", "copilot", "not supported by the copilot backend"},
		{"yes", "claude", `unknown auto-approve level "yes"`},
		{"Edits", "claude", "unknown auto-approve level"},
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
	"--permission-mode", "--disallowedTools", "--dangerously-skip-permissions", "--allow-dangerously-skip-permissions",
	"--dangerously-bypass-approvals-and-sandbox", "--yolo", "--full-auto", "--ask-for-approval",
	"-a", "--sandbox", "-s", "--force", "-f", "--auto",
}

// backendArgs returns the argument list each backend builds for opts.
func backendArgs(backend string, opts StartOptions) []string {
	switch backend {
	case "claude":
		return buildClaudeStreamArgs(opts)
	case "codex":
		return buildCodexArgs(opts, "")
	case "opencode":
		return buildOpenCodeArgs(opts, "", "do the task")
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
		{"claude", "none", []string{"--permission-mode", "default"}},
		{"codex", "none", []string{"--sandbox", "read-only"}},
		{"codex", "", nil},
		{"cursor", "", nil},
		{"claude", "edits", []string{"--permission-mode", "acceptEdits", "--disallowedTools"}},
		{"claude", "all", []string{"--permission-mode", "bypassPermissions"}},
		{"codex", "all", []string{"--dangerously-bypass-approvals-and-sandbox"}},
		{"cursor", "all", []string{"--force"}},
		{"opencode", "", nil},
		{"opencode", "all", []string{"--auto"}},
	}
	for _, tt := range tests {
		t.Run(tt.backend+"/"+tt.level, func(t *testing.T) {
			args := backendArgs(tt.backend, StartOptions{AutoApprove: tt.level, Model: "m"})
			var got []string
			for i, a := range args {
				if slices.Contains(permissionFlags, a) {
					got = append(got, a)
					if (a == "--permission-mode" || a == "--sandbox") && i+1 < len(args) {
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

func TestRunLoop_ProjectModeNarrowsAndNeverRefuses(t *testing.T) {
	tests := []struct {
		name, backend, level, file, content string
		wantLevel                           string
		wantLine                            bool
		wantFromProject                     bool
	}{
		{"narrower claude project mode wins", "claude", "all", ".claude/settings.json", `{"permissions":{"defaultMode":"plan"}}`, "none", true, true},
		{"narrower local mode wins", "claude", "all", ".claude/settings.local.json", `{"permissions":{"defaultMode":"acceptEdits"}}`, "edits", true, true},
		{"wider project mode loses to the flag", "claude", "edits", ".claude/settings.json", `{"permissions":{"defaultMode":"bypassPermissions"}}`, "edits", false, false},
		{"equal mode is the flag's", "claude", "edits", ".claude/settings.json", `{"permissions":{"defaultMode":"acceptEdits"}}`, "edits", false, false},
		{"codex read-only wins over all", "codex", "all", ".codex/config.toml", "sandbox_mode = \"read-only\"\n", "none", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, tt.file), tt.content)
			mb := &mockBackend{name: tt.backend, turns: []Turn{{Content: "done"}}}
			var stdout, stderr bytes.Buffer
			opts := baseOpts(mb, &stdout, &stderr, strings.NewReader(""))
			opts.Backend = tt.backend
			opts.AutoApprove = tt.level
			opts.WorktreeDir = dir
			if _, err := RunLoop(opts); err != nil {
				t.Fatalf("a project mode must never refuse the run: %v", err)
			}
			if len(mb.startOpts) != 1 {
				t.Fatalf("worker starts = %d, want 1", len(mb.startOpts))
			}
			got := mb.startOpts[0]
			if got.AutoApprove != tt.wantLevel || (got.ProjectArgs != nil) != tt.wantFromProject {
				t.Errorf("opened at %q (from project %v), want %q (%v)", got.AutoApprove, got.ProjectArgs != nil, tt.wantLevel, tt.wantFromProject)
			}
			if has := strings.Contains(stderr.String(), "opening at the project's mode"); has != tt.wantLine {
				t.Errorf("line printed = %v, want %v (stderr %q)", has, tt.wantLine, stderr.String())
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
		{name: "edits on codex", backend: "codex", level: "edits", wantSub: "cannot hold auto-approve edits exactly"},
		{name: "edits on cursor", backend: "cursor", level: "edits", wantSub: "cannot hold auto-approve edits exactly"},
		{name: "unknown level", backend: "claude", level: "everything", wantSub: "unknown auto-approve level"},
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
		{&OpenCodeBackend{}, "Error: --auto is disabled by your administrator"},
	}
	for _, tt := range tests {
		t.Run(tt.backend.Name(), func(t *testing.T) {
			if tt.backend.Name() == "opencode" {
				fakeOpenCode(t, "opencode v2.0.27", "", tt.stderr, 1) // Start gates on --version
			} else {
				fakeVendor(t, tt.backend.Name(), "", tt.stderr, 1)
			}
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

// A project-narrowed worker is passed the project's own mode explicitly, so a
// wider user-level setting (Codex danger-full-access, say) cannot widen it; and
// Claude's opened-mode check compares against that mode.
func TestBackendArgs_ProjectModeIsExplicit(t *testing.T) {
	codex := backendArgs("codex", StartOptions{AutoApprove: "none", ProjectArgs: []string{"--sandbox", "read-only"}, Model: "m"})
	if !slices.Contains(codex, "--sandbox") || slices.Contains(codex, "--dangerously-bypass-approvals-and-sandbox") {
		t.Errorf("codex args = %q", codex)
	}
	claude := backendArgs("claude", StartOptions{AutoApprove: "none", ProjectArgs: []string{"--permission-mode", "plan"}, ProjectMode: "plan"})
	i := slices.Index(claude, "--permission-mode")
	if i < 0 || claude[i+1] != "plan" {
		t.Errorf("claude args = %q", claude)
	}
	opts := StartOptions{AutoApprove: "none", ProjectMode: "plan"}
	if got := claudePermissionMode(opts); got != "plan" {
		t.Errorf("expected opened mode = %q, want plan", got)
	}
	if err := claudeModeMismatch(claudeOutputEvent{Subtype: "init", PermissionMode: "bypassPermissions"}, claudePermissionMode(opts)); err == nil {
		t.Error("a session that opened wider than the project's mode must be stopped")
	}
}

// OpenCode's permission block is merged into the MCP isolation's one
// OPENCODE_CONFIG_CONTENT: the servers it turned off stay off.
func TestOpenCodeEnv_PermissionKeepsMCPIsolation(t *testing.T) {
	isolation := []string{"OPENCODE_CONFIG_CONTENT=" + `{"mcp":{"servers":{"web":{"enabled":false}}}}`}
	env, err := openCodeEnv(StartOptions{AutoApprove: "none"}, isolation)
	if err != nil {
		t.Fatal(err)
	}
	var copies []string
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "OPENCODE_CONFIG_CONTENT="); ok {
			copies = append(copies, v)
		}
	}
	if len(copies) != 1 || !strings.Contains(copies[0], `"web":{"enabled":false}`) || !strings.Contains(copies[0], `"edit":"deny"`) {
		t.Errorf("config content = %q", copies)
	}
}

// An accepted harness's worker opens at the harness's level (D7): its
// declared one, else all, and --auto-approve may only narrow it.
func TestRunLoop_AcceptedHarnessOpensAtItsLevel(t *testing.T) {
	install := func(t *testing.T, level string) string {
		t.Helper()
		t.Setenv("YNH_HOME", t.TempDir())
		t.Setenv(permission.CapEnv, "")
		const id = "local/level-test"
		dir := harness.InstalledDirByID(id)
		if err := os.MkdirAll(filepath.Join(dir, plugin.PluginDir), 0o755); err != nil {
			t.Fatal(err)
		}
		m := `{"name":"level-test","version":"0.1.0","default_vendor":"claude"`
		if level != "" {
			m += `,"auto_approve":"` + level + `"`
		}
		writeFile(t, filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), m+"}")
		return id
	}
	tests := []struct {
		name, declared, flag, want, wantErr string
	}{
		{name: "undeclared opens at all", want: "all"},
		{name: "declared level", declared: "edits", want: "edits"},
		{name: "flag narrows", declared: "edits", flag: "none", want: "none"},
		{name: "flag cannot widen", declared: "edits", flag: "all", wantErr: "wider than the ceiling"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := install(t, tt.declared)
			mb := &mockBackend{name: "claude", turns: []Turn{{Content: "done"}}}
			var stderr bytes.Buffer
			opts := baseOpts(mb, io.Discard, &stderr, strings.NewReader(""))
			opts.Backend = "claude"
			opts.HarnessName = id
			opts.AutoApprove = tt.flag
			opts.WorktreeDir = t.TempDir()
			opts.YNHBinary = filepath.Join(t.TempDir(), "no-such-ynh")
			opts.TrustGate = func(*harness.Harness, string) error { return nil }
			opts.Trusted = func(*harness.Harness, string) bool { return true }
			_, err := RunLoop(opts)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) || len(mb.startOpts) != 0 {
					t.Fatalf("err = %v, starts = %d", err, len(mb.startOpts))
				}
				return
			}
			if len(mb.startOpts) != 1 {
				t.Fatalf("worker started %d times; err: %v", len(mb.startOpts), err)
			}
			if got := mb.startOpts[0].AutoApprove; got != tt.want {
				t.Errorf("opened at %q, want %q", got, tt.want)
			}
		})
	}
}
