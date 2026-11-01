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

func readOpenCodeFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// fakeOpenCode is a stub opencode on PATH that answers --version with version
// and otherwise behaves like fakeVendor's. Start gates on --version, so a stub
// that printed turn events for it would be refused as a 1.x.
func fakeOpenCode(t *testing.T, version, stdout, stderrText string, code int) (argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo '" + version + "'; exit 0; fi\n" +
		"for a in \"$@\"; do printf '%s\\n' \"$a\"; done > " + argsFile + "\n"
	if stdout != "" {
		script += "cat <<'OUTEOF'\n" + stdout + "\nOUTEOF\n"
	}
	if stderrText != "" {
		script += "echo '" + stderrText + "' >&2\n"
	}
	script += "exit " + itoa(code) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "opencode"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	shadowVendorCLIs(t, dir)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argsFile
}

func TestOpenCodeBackend_Name(t *testing.T) {
	if got := (&OpenCodeBackend{}).Name(); got != "opencode" {
		t.Errorf("Name = %q, want opencode", got)
	}
}

func TestBuildOpenCodeArgs(t *testing.T) {
	tests := []struct {
		name      string
		opts      StartOptions
		sessionID string
		want      []string
	}{
		{
			name: "fresh turn, defaults",
			want: []string{"run", "--standalone", "--format", "json", "--", "hi"},
		},
		{
			name:      "resumed turn with model and auto",
			opts:      StartOptions{Model: "opencode/m#high", AutoApprove: AutoApproveAll},
			sessionID: "ses_1",
			want:      []string{"run", "--standalone", "--format", "json", "-m", "opencode/m#high", "--auto", "-s", "ses_1", "--", "hi"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildOpenCodeArgs(tt.opts, tt.sessionID, "hi")
			if !slices.Equal(got, tt.want) {
				t.Errorf("args = %q, want %q", got, tt.want)
			}
		})
	}
}

// A message that starts with a dash must not be read as a flag.
func TestBuildOpenCodeArgs_MessageIsAfterDoubleDash(t *testing.T) {
	args := buildOpenCodeArgs(StartOptions{}, "", "--help me")
	n := len(args)
	if args[n-2] != "--" || args[n-1] != "--help me" {
		t.Errorf("tail of args = %q, want -- then the message", args[n-2:])
	}
}

func TestParseOpenCodeOutput_ToolTurn(t *testing.T) {
	run, err := parseOpenCodeOutput(strings.NewReader(readOpenCodeFixture(t, "opencode-tool-turn.jsonl")))
	if err != nil {
		t.Fatal(err)
	}
	if run.sessionID != "ses_ecd8be2acffexDYOr6rgWf85Tb" {
		t.Errorf("sessionID = %q", run.sessionID)
	}
	if run.turn.Content != "DONE" {
		t.Errorf("Content = %q, want DONE", run.turn.Content)
	}
	// Two step_finish events: 8303 + 8376 in, (27+27) + (2+18) out.
	if !run.turn.UsageReported || !run.turn.CacheReported || !run.turn.CacheCreationReported {
		t.Errorf("usage flags not set: %+v", run.turn)
	}
	want := Usage{InputTokens: 16679, OutputTokens: 74}
	if run.turn.Usage != want {
		t.Errorf("Usage = %+v, want %+v", run.turn.Usage, want)
	}
	if !run.turn.CostReported || run.turn.CostUSD != 0 {
		t.Errorf("cost = %v reported=%v, want a reported zero", run.turn.CostUSD, run.turn.CostReported)
	}
	if run.failure != "" {
		t.Errorf("failure = %q", run.failure)
	}
}

// A prompt piped on stdin ends before the final step_finish, so no usage is
// reported: not a zero, an absence.
func TestParseOpenCodeOutput_NoStepFinishReportsNoUsage(t *testing.T) {
	run, err := parseOpenCodeOutput(strings.NewReader(readOpenCodeFixture(t, "opencode-stdin-no-step-finish.jsonl")))
	if err != nil {
		t.Fatal(err)
	}
	if run.turn.Content != "PONG" {
		t.Errorf("Content = %q, want PONG", run.turn.Content)
	}
	if run.turn.UsageReported {
		t.Error("UsageReported = true with no step_finish")
	}
}

func TestParseOpenCodeOutput_ErrorEvent(t *testing.T) {
	run, err := parseOpenCodeOutput(strings.NewReader(readOpenCodeFixture(t, "opencode-error.jsonl")))
	if err != nil {
		t.Fatal(err)
	}
	if run.failure != "Model unavailable: nope/nothing" {
		t.Errorf("failure = %q", run.failure)
	}
	if run.sessionID == "" {
		t.Error("an error event still carries the session id")
	}
	if run.turn.Content != "" {
		t.Errorf("Content = %q, want empty", run.turn.Content)
	}
}

func TestParseOpenCodeOutput_EmptyAndGarbage(t *testing.T) {
	run, err := parseOpenCodeOutput(strings.NewReader("\nnot json\n"))
	if err != nil {
		t.Fatal(err)
	}
	if run.turn.Content != "" || run.sessionID != "" {
		t.Errorf("run = %+v, want empty", run)
	}
}

func TestOpenCodeConfigDir(t *testing.T) {
	if got := openCodeConfigDir(""); got != "" {
		t.Errorf("no config path: got %q, want empty", got)
	}
	got := openCodeConfigDir("/run/h")
	if got != filepath.Join("/run/h", ".opencode") {
		t.Errorf("got %q, want /run/h/.opencode", got)
	}
}

func TestOpenCodeSession_NextErrorsWithoutSend(t *testing.T) {
	if _, err := (&openCodeSession{}).Next(); err == nil {
		t.Error("expected an error when Next is called without Send")
	}
}

func TestOpenCodeSession_ResumeTokenSeededAndLearned(t *testing.T) {
	out := readOpenCodeFixture(t, "opencode-tool-turn.jsonl")
	argsFile := fakeOpenCode(t, "opencode v2.0.27", out, "", 0)

	sess, err := (&OpenCodeBackend{}).Start(context.Background(), StartOptions{WorktreeDir: t.TempDir()})
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
	if turn.Content != "DONE" {
		t.Errorf("Content = %q", turn.Content)
	}
	if sess.ResumeToken() != "ses_ecd8be2acffexDYOr6rgWf85Tb" {
		t.Errorf("token = %q", sess.ResumeToken())
	}
	first := readLines(t, argsFile)
	if slices.Contains(first, "-s") {
		t.Errorf("first turn resumed a session: %q", first)
	}

	// The second turn resumes the session the first one reported.
	if err := sess.Send("again"); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Next(); err != nil {
		t.Fatal(err)
	}
	second := readLines(t, argsFile)
	i := slices.Index(second, "-s")
	if i < 0 || second[i+1] != "ses_ecd8be2acffexDYOr6rgWf85Tb" {
		t.Errorf("second turn args = %q, want -s <session>", second)
	}

	// A seeded token resumes from the very first turn.
	seeded, err := (&OpenCodeBackend{}).Start(context.Background(), StartOptions{ResumeToken: "ses_seed", WorktreeDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := seeded.Send("x"); err != nil {
		t.Fatal(err)
	}
	if _, err := seeded.Next(); err != nil {
		t.Fatal(err)
	}
	if got := readLines(t, argsFile); !slices.Contains(got, "ses_seed") {
		t.Errorf("seeded first turn args = %q, want -s ses_seed", got)
	}
}

func TestOpenCodeSession_ErrorEventIsWorkerError(t *testing.T) {
	out := readOpenCodeFixture(t, "opencode-error.jsonl")
	fakeOpenCode(t, "opencode v2.0.27", out, "", 1)
	sess, err := (&OpenCodeBackend{}).Start(context.Background(), StartOptions{WorktreeDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Send("x"); err != nil {
		t.Fatal(err)
	}
	_, err = sess.Next()
	var we *WorkerError
	if !errors.As(err, &we) || we.Backend != "opencode" || !strings.Contains(we.Message, "Model unavailable") {
		t.Fatalf("err = %v, want a WorkerError carrying the event's message", err)
	}
}

func TestOpenCodeSession_NonZeroExitWithoutEventsUsesStderr(t *testing.T) {
	fakeOpenCode(t, "opencode v2.0.27", "", "boom on stderr", 2)
	sess, err := (&OpenCodeBackend{}).Start(context.Background(), StartOptions{WorktreeDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Send("x"); err != nil {
		t.Fatal(err)
	}
	_, err = sess.Next()
	var we *WorkerError
	if !errors.As(err, &we) || !strings.Contains(we.Message, "boom on stderr") {
		t.Fatalf("err = %v, want a WorkerError with stderr", err)
	}
}

func TestOpenCodeSession_CleanExitWithNoOutputIsEOF(t *testing.T) {
	fakeOpenCode(t, "opencode v2.0.27", "", "", 0)
	sess, err := (&OpenCodeBackend{}).Start(context.Background(), StartOptions{WorktreeDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Send("x"); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Next(); err != io.EOF {
		t.Errorf("err = %v, want io.EOF", err)
	}
	if err := sess.Close(); err != nil {
		t.Errorf("Close = %v", err)
	}
}

// Start prepares the harness the way a launch does: the composed
// .opencode/AGENTS.md carries the instructions and the rules.
func TestOpenCodeBackend_StartComposesInstructionsAndRules(t *testing.T) {
	fakeOpenCode(t, "opencode v2.0.27", "", "", 0)
	cfg := t.TempDir()
	rules := filepath.Join(cfg, ".opencode", "rules")
	if err := os.MkdirAll(rules, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "AGENTS.md"), []byte("Base.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rules, "r.md"), []byte("---\nx: y\n---\nRule R.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (&OpenCodeBackend{}).Start(context.Background(), StartOptions{ConfigPath: cfg}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(cfg, ".opencode", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "Base.\n\nRule R.\n" {
		t.Errorf("composed AGENTS.md = %q", got)
	}
}

func TestOpenCodeBackend_StartRefusesOneX(t *testing.T) {
	fakeOpenCode(t, "opencode v1.14.3", "", "", 0)
	_, err := (&OpenCodeBackend{}).Start(context.Background(), StartOptions{ConfigPath: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "2.x only") {
		t.Fatalf("err = %v, want the 2.x refusal", err)
	}
}

// A process that dies mid-turn after streaming some text did not finish the
// turn; its text is a fragment and must not come back as an answer.
func TestOpenCodeSession_CrashMidTurnIsAnError(t *testing.T) {
	partial := `{"type":"text","timestamp":1,"sessionID":"ses_p","part":{"type":"text","text":"half an ans"}}`
	fakeOpenCode(t, "opencode v2.0.27", partial, "killed", 1)
	sess, err := (&OpenCodeBackend{}).Start(context.Background(), StartOptions{WorktreeDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Send("x"); err != nil {
		t.Fatal(err)
	}
	turn, err := sess.Next()
	var we *WorkerError
	if !errors.As(err, &we) || !strings.Contains(we.Message, "killed") || turn.Content != "" {
		t.Fatalf("turn = %+v, err = %v, want a WorkerError and no content", turn, err)
	}
}
