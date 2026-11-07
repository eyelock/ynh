package agent

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func readCopilotFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCopilotBackend_Name(t *testing.T) {
	if got := (&CopilotBackend{}).Name(); got != "copilot" {
		t.Errorf("Name = %q, want copilot", got)
	}
}

// The backend can be kept dark: with the switch off, copilot is an unknown
// backend and appears in no list. This runs with the switch as built.
func TestCopilotBackend_SelectableFollowsTheSwitch(t *testing.T) {
	got, err := validateBackend("copilot")
	wb, selErr := selectBackend("copilot")
	if copilotWorkerSelectable {
		if err != nil || got != "copilot" || selErr != nil || wb.Name() != "copilot" {
			t.Errorf("switch on: validate = (%q, %v), select = (%v, %v)", got, err, wb, selErr)
		}
		if !strings.Contains(supportedBackends(), "copilot") {
			t.Errorf("switch on but supportedBackends = %q", supportedBackends())
		}
		return
	}
	if err == nil || selErr == nil {
		t.Errorf("switch off but copilot is selectable: validate err = %v, select err = %v", err, selErr)
	}
	if strings.Contains(supportedBackends(), "copilot") {
		t.Errorf("switch off but supportedBackends = %q", supportedBackends())
	}
}

func TestBuildCopilotWorkerArgs(t *testing.T) {
	tests := []struct {
		name      string
		opts      StartOptions
		sessionID string
		want      []string
	}{
		{
			name: "fresh turn, defaults",
			want: []string{"--output-format", "json", "--no-ask-user", "--no-auto-update", "--prompt=hi"},
		},
		{
			name:      "resumed turn with model, effort and allow-all",
			opts:      StartOptions{Model: "gpt-5", Effort: EffortHigh, AutoApprove: AutoApproveAll, Workspace: []string{"/w/a"}},
			sessionID: "be461914-d466-4905-b39e-1d02f0806516",
			want: []string{
				"--output-format", "json", "--no-ask-user", "--no-auto-update",
				"--add-dir", "/w/a", "--model", "gpt-5", "--reasoning-effort", "high",
				"--allow-all-tools", "--resume=be461914-d466-4905-b39e-1d02f0806516", "--prompt=hi",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildCopilotWorkerArgs(tt.opts, tt.sessionID, "hi")
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("args = %q, want %q", got, tt.want)
			}
		})
	}
}

// The harness arrives by plugin directory and MCP file, and its instructions
// are projected into the worktree.
func TestBuildCopilotWorkerArgs_Harness(t *testing.T) {
	cfg, wt := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(cfg, "AGENTS.md"), []byte("be kind\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	args, err := buildCopilotWorkerArgs(StartOptions{ConfigPath: cfg, WorktreeDir: wt}, "", "go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--plugin-dir", filepath.Join(cfg, ".copilot"), "--add-dir", cfg} {
		if !slices.Contains(args, want) {
			t.Errorf("args %q lack %q", args, want)
		}
	}
	data, err := os.ReadFile(filepath.Join(wt, ".github", "instructions", "ynh-harness.instructions.md"))
	if err != nil {
		t.Fatalf("instructions not projected into the worktree: %v", err)
	}
	if !strings.Contains(string(data), "be kind") {
		t.Errorf("projected instructions = %q", data)
	}
}

// A message that starts with a dash must not be read as a flag.
func TestBuildCopilotWorkerArgs_MessageIsTheValueOfPrompt(t *testing.T) {
	args, err := buildCopilotWorkerArgs(StartOptions{}, "", "--help me")
	if err != nil {
		t.Fatal(err)
	}
	if last := args[len(args)-1]; last != "--prompt=--help me" {
		t.Errorf("last arg = %q", last)
	}
}

func TestParseCopilotOutput_OneWord(t *testing.T) {
	run, err := parseCopilotOutput(strings.NewReader(readCopilotFixture(t, "copilot-one-word.jsonl")))
	if err != nil {
		t.Fatal(err)
	}
	if run.sessionID != "2120d876-c9c2-4571-9b70-fcb8a86d1d04" {
		t.Errorf("sessionID = %q", run.sessionID)
	}
	if run.turn.Content != "ok" {
		t.Errorf("Content = %q, want ok", run.turn.Content)
	}
	if run.turn.Model != "mai-code-1.1-flash" {
		t.Errorf("Model = %q", run.turn.Model)
	}
	if !run.finished || run.exitCode != 0 {
		t.Errorf("finished = %v, exitCode = %d", run.finished, run.exitCode)
	}
	if !run.turn.UsageReported || !run.turn.CacheReported || !run.turn.CacheCreationReported {
		t.Errorf("usage flags not set: %+v", run.turn)
	}
	if want := (Usage{InputTokens: 565, OutputTokens: 5, CacheTokens: 12672}); run.turn.Usage != want {
		t.Errorf("Usage = %+v, want %+v", run.turn.Usage, want)
	}
	// Copilot reports premium requests, not money: absent, not zero.
	if run.turn.CostReported {
		t.Error("CostReported = true, but Copilot reports no cost")
	}
}

// Two model calls (a shell tool call, then the answer) make one ynh turn: the
// empty tool-call message adds no text, and the usage is the process's total.
func TestParseCopilotOutput_ToolTurn(t *testing.T) {
	run, err := parseCopilotOutput(strings.NewReader(readCopilotFixture(t, "copilot-tool-turn.jsonl")))
	if err != nil {
		t.Fatal(err)
	}
	if run.sessionID != "be461914-d466-4905-b39e-1d02f0806516" {
		t.Errorf("sessionID = %q", run.sessionID)
	}
	if want := "Files here:\n- No files or directories found in the current workspace.\n\ndone."; run.turn.Content != want {
		t.Errorf("Content = %q, want %q", run.turn.Content, want)
	}
	if want := (Usage{InputTokens: 12156, OutputTokens: 89, CacheTokens: 14464}); run.turn.Usage != want {
		t.Errorf("Usage = %+v, want %+v", run.turn.Usage, want)
	}
	if !run.turn.UsageReported {
		t.Error("UsageReported = false")
	}
}

func TestParseCopilotOutput_NoCheckpointReportsNoUsage(t *testing.T) {
	var kept []string
	for _, l := range strings.Split(readCopilotFixture(t, "copilot-one-word.jsonl"), "\n") {
		if !strings.Contains(l, `"session.usage_checkpoint"`) {
			kept = append(kept, l)
		}
	}
	run, err := parseCopilotOutput(strings.NewReader(strings.Join(kept, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	if run.turn.Content != "ok" {
		t.Errorf("Content = %q", run.turn.Content)
	}
	if run.turn.UsageReported || run.turn.CacheReported || run.turn.CacheCreationReported {
		t.Errorf("usage reported without a checkpoint: %+v", run.turn)
	}
}

func TestParseCopilotOutput_EmptyAndGarbage(t *testing.T) {
	run, err := parseCopilotOutput(strings.NewReader("\nnot json\n"))
	if err != nil {
		t.Fatal(err)
	}
	if run.turn.Content != "" || run.sessionID != "" || run.finished {
		t.Errorf("run = %+v, want empty", run)
	}
}

func TestCopilotSession_NextErrorsWithoutSend(t *testing.T) {
	if _, err := (&copilotSession{}).Next(); err == nil {
		t.Error("expected an error when Next is called without Send")
	}
}

func TestCopilotSession_ResumeTokenSeededAndLearned(t *testing.T) {
	argsFile := fakeVendor(t, "copilot", readCopilotFixture(t, "copilot-tool-turn.jsonl"), "", 0)

	sess, err := (&CopilotBackend{}).Start(context.Background(), StartOptions{WorktreeDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if sess.ResumeToken() != "" {
		t.Errorf("token before the first turn = %q, want empty", sess.ResumeToken())
	}
	if err := sess.Send("go"); err != nil {
		t.Fatal(err)
	}
	turn, err := sess.Next()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(turn.Content, "done.") {
		t.Errorf("Content = %q", turn.Content)
	}
	const id = "be461914-d466-4905-b39e-1d02f0806516"
	if sess.ResumeToken() != id {
		t.Errorf("token = %q, want %q", sess.ResumeToken(), id)
	}
	first := readLines(t, argsFile)
	for _, a := range first {
		if strings.HasPrefix(a, "--resume") {
			t.Errorf("first turn resumed a session: %q", first)
		}
	}

	if err := sess.Send("again"); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Next(); err != nil {
		t.Fatal(err)
	}
	if second := readLines(t, argsFile); !slices.Contains(second, "--resume="+id) {
		t.Errorf("second turn did not resume %s: %q", id, second)
	}

	// A resumed session starts on its token.
	resumed, err := (&CopilotBackend{}).Start(context.Background(), StartOptions{ResumeToken: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.ResumeToken() != "abc" {
		t.Errorf("seeded token = %q", resumed.ResumeToken())
	}
	_ = resumed.Close()
}

// A process that fails says why on stderr; a clean exit with no answer is EOF.
func TestCopilotSession_FailureAndEmpty(t *testing.T) {
	fakeVendor(t, "copilot", "", "Error: not signed in", 1)
	sess, err := (&CopilotBackend{}).Start(context.Background(), StartOptions{WorktreeDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	_ = sess.Send("go")
	_, err = sess.Next()
	var we *WorkerError
	if !errors.As(err, &we) || !strings.Contains(we.Message, "not signed in") {
		t.Errorf("err = %v, want a WorkerError carrying stderr", err)
	}

	fakeVendor(t, "copilot", "", "", 0)
	sess, err = (&CopilotBackend{}).Start(context.Background(), StartOptions{WorktreeDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	_ = sess.Send("go")
	if _, err := sess.Next(); err != io.EOF {
		t.Errorf("err = %v, want io.EOF", err)
	}
}
