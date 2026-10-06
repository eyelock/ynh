package agent

import (
	"context"
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

// The codex fixtures in testdata:
//   - codex-not-logged-in.jsonl is captured from codex-cli 0.160.0
//     (`codex exec --json -` with no credentials), trimmed of repeated
//     reconnect lines.
//   - codex-turn.jsonl, codex-resumed-turn.jsonl, codex-zero-usage.jsonl,
//     codex-cache-write-turn.jsonl and codex-no-cache-write-turn.jsonl are
//     derived: the event and item shapes from codex's ThreadEvent
//     (codex-rs/exec/src/exec_events.rs in openai/codex), the usage figures
//     from the sample in OpenAI's `codex exec --json` documentation. The
//     last has the usage shape of a codex from before cache_write_input_tokens
//     (openai/codex#33454).
//     A successful turn cannot be captured without credentials.

func TestCodexBackend_Name(t *testing.T) {
	b := &CodexBackend{}
	if b.Name() != "codex" {
		t.Errorf("expected 'codex', got %q", b.Name())
	}
}

func readCodexFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// codexTurnFromFixture parses one process's output and makes the turn the
// session would, as a codex exec process that exited cleanly.
func codexTurnFromFixture(t *testing.T, s *codexSession, raw string) (Turn, error) {
	t.Helper()
	run, err := parseCodexOutput(strings.NewReader(raw))
	return s.turnFrom(run, err, nil, &stderrTail{})
}

// A turn with reasoning, commands, a plan and file changes yields the agent's
// messages as its text and its usage from turn.completed.
func TestCodexSession_MultiItemTurn(t *testing.T) {
	s := &codexSession{totalsKnown: true}
	turn, err := codexTurnFromFixture(t, s, readCodexFixture(t, "codex-turn.jsonl"))
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	if want := "I will fix the parser.\n\nDone: the parser is fixed."; turn.Content != want {
		t.Errorf("content = %q, want %q", turn.Content, want)
	}
	// input_tokens 24763 includes cached_input_tokens 24448; output_tokens
	// 122 includes reasoning_output_tokens 64, which is not added again.
	want := Usage{InputTokens: 315, OutputTokens: 122, CacheTokens: 24448}
	if turn.Usage != want || !turn.UsageReported || !turn.CacheReported || !turn.CacheCreationReported {
		t.Errorf("usage = %+v reported=%v cache=%v, want %+v reported and cache", turn.Usage, turn.UsageReported, turn.CacheReported, want)
	}
	if turn.CostReported || turn.Effort != "" {
		t.Errorf("codex reports no cost or effort, got cost=%v effort=%q", turn.CostReported, turn.Effort)
	}
	if s.ResumeToken() != "0199a213-81c0-7800-8aa1-bbab2a035a53" {
		t.Errorf("thread id = %q, want the one from thread.started", s.ResumeToken())
	}
}

// turn.completed carries the thread's running totals, so each turn counts
// the difference from what was already counted, once.
func TestCodexSession_UsageFromRunningTotals(t *testing.T) {
	first := readCodexFixture(t, "codex-turn.jsonl")
	second := readCodexFixture(t, "codex-resumed-turn.jsonl")
	tests := []struct {
		name         string
		opts         StartOptions
		fixtures     []string
		want         []Usage
		wantReported []bool
	}{
		{
			name:         "fresh thread: the first total is the first turn",
			fixtures:     []string{first},
			want:         []Usage{{InputTokens: 315, OutputTokens: 122, CacheTokens: 24448}},
			wantReported: []bool{true},
		},
		{
			name:     "two turns on one thread: the second counts only its own",
			fixtures: []string{first, second},
			want: []Usage{
				{InputTokens: 315, OutputTokens: 122, CacheTokens: 24448},
				// 50000-24763 input of which 49000-24448 cached; 200-122 output.
				{InputTokens: 685, OutputTokens: 78, CacheTokens: 24552},
			},
			wantReported: []bool{true, true},
		},
		{
			name: "resumed with the checkpoint's record: earlier turns are not counted again",
			opts: StartOptions{
				ResumeToken: "0199a213-81c0-7800-8aa1-bbab2a035a53",
				UsageBase:   &Usage{InputTokens: 315, OutputTokens: 122, CacheTokens: 24448},
			},
			fixtures:     []string{second},
			want:         []Usage{{InputTokens: 685, OutputTokens: 78, CacheTokens: 24552}},
			wantReported: []bool{true},
		},
		{
			name:         "resumed without a record: the first turn's share is unknown, the next is not",
			opts:         StartOptions{ResumeToken: "0199a213-81c0-7800-8aa1-bbab2a035a53"},
			fixtures:     []string{first, second},
			want:         []Usage{{}, {InputTokens: 685, OutputTokens: 78, CacheTokens: 24552}},
			wantReported: []bool{false, true},
		},
		{
			name: "a total below the record is not this thread's: unknown, not negative",
			opts: StartOptions{
				ResumeToken: "other",
				UsageBase:   &Usage{InputTokens: 99999, OutputTokens: 99999, CacheTokens: 99999},
			},
			fixtures:     []string{first},
			want:         []Usage{{}},
			wantReported: []bool{false},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess, err := (&CodexBackend{}).startForTest(t, tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			for i, raw := range tt.fixtures {
				turn, err := codexTurnFromFixture(t, sess, raw)
				if err != nil {
					t.Fatalf("turn %d: %v", i+1, err)
				}
				if turn.Usage != tt.want[i] || turn.UsageReported != tt.wantReported[i] || turn.CacheReported != tt.wantReported[i] {
					t.Errorf("turn %d: usage = %+v reported=%v cache=%v, want %+v reported=%v",
						i+1, turn.Usage, turn.UsageReported, turn.CacheReported, tt.want[i], tt.wantReported[i])
				}
			}
		})
	}
}

// Cache writes are inside codex's input_tokens; they come out of input and
// into CacheCreationTokens, reported only when codex reports them.
func TestCodexSession_CacheWrites(t *testing.T) {
	tests := []struct {
		name         string
		opts         StartOptions
		fixture      string
		want         Usage
		wantReported bool
	}{
		{
			name:    "reported: writes leave input",
			fixture: "codex-cache-write-turn.jsonl",
			// 24763 input less 20000 cached and 4000 written.
			want:         Usage{InputTokens: 763, OutputTokens: 122, CacheTokens: 20000, CacheCreationTokens: 4000},
			wantReported: true,
		},
		{
			name:    "an older codex without the field: absent, input as before",
			fixture: "codex-no-cache-write-turn.jsonl",
			want:    Usage{InputTokens: 315, OutputTokens: 122, CacheTokens: 24448},
		},
		{
			name: "resumed: the base's cache writes are not counted again",
			opts: StartOptions{
				ResumeToken: "0199a213-81c0-7800-8aa1-bbab2a035a53",
				UsageBase:   &Usage{InputTokens: 263, OutputTokens: 100, CacheTokens: 20000, CacheCreationTokens: 3500},
			},
			fixture: "codex-cache-write-turn.jsonl",
			// Base totals: input 23763, cached 20000, written 3500, output 100.
			want:         Usage{InputTokens: 500, OutputTokens: 22, CacheTokens: 0, CacheCreationTokens: 500},
			wantReported: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess, err := (&CodexBackend{}).startForTest(t, tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			turn, err := codexTurnFromFixture(t, sess, readCodexFixture(t, tt.fixture))
			if err != nil {
				t.Fatalf("turn: %v", err)
			}
			if turn.Usage != tt.want || !turn.UsageReported || turn.CacheCreationReported != tt.wantReported {
				t.Errorf("usage = %+v cache writes reported=%v, want %+v reported=%v",
					turn.Usage, turn.CacheCreationReported, tt.want, tt.wantReported)
			}
		})
	}
}

// startForTest starts a codex session with a codex stub on PATH, since Start
// requires the binary to exist.
func (b *CodexBackend) startForTest(t *testing.T, opts StartOptions) (*codexSession, error) {
	t.Helper()
	codexStub(t, "")
	sess, err := b.Start(context.Background(), opts)
	if err != nil {
		return nil, err
	}
	return sess.(*codexSession), nil
}

// A turn codex could not complete is a worker error carrying codex's own
// words, whether it ends in turn.failed or only in an error event.
func TestCodexSession_FailureIsWorkerError(t *testing.T) {
	notLoggedIn := "unexpected status 401 Unauthorized: Missing bearer or basic authentication in header, " +
		"url: https://api.openai.com/v1/responses, cf-ray: a45444665d15b107-MAN, request id: req_4d0bca3823424476a6781156145e5935"
	tests := []struct {
		name     string
		raw      string
		want     string
		wantAuth bool
	}{
		{
			name:     "captured: not logged in ends in turn.failed after retried errors",
			raw:      readCodexFixture(t, "codex-not-logged-in.jsonl"),
			want:     notLoggedIn,
			wantAuth: true,
		},
		{
			name: "turn.failed with a message",
			raw:  `{"type":"turn.failed","error":{"message":"stream disconnected before completion"}}` + "\n",
			want: "stream disconnected before completion",
		},
		{
			name: "turn.failed without a message falls back to the last error event",
			raw: `{"type":"error","message":"quota exceeded"}` + "\n" +
				`{"type":"turn.failed","error":"not an object"}` + "\n",
			want: "quota exceeded",
		},
		{
			name: "turn.failed with nothing to say",
			raw:  `{"type":"turn.failed","error":"not an object"}` + "\n",
			want: "turn failed",
		},
		{
			name: "an error event and then the stream ends",
			raw: `{"type":"thread.started","thread_id":"t1"}` + "\n" +
				`{"type":"turn.started"}` + "\n" +
				`{"type":"error","message":"unexpected status 401 Unauthorized"}` + "\n",
			want:     "unexpected status 401 Unauthorized",
			wantAuth: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := codexTurnFromFixture(t, &codexSession{totalsKnown: true}, tt.raw)
			var we *WorkerError
			if !errors.As(err, &we) || we.Backend != "codex" || we.Message != tt.want || we.Auth != tt.wantAuth {
				t.Fatalf("want codex WorkerError %q auth=%v, got %#v", tt.want, tt.wantAuth, err)
			}
		})
	}
}

// A retried error does not end a turn that then completes.
func TestCodexSession_RetriedErrorThenCompletes(t *testing.T) {
	raw := `{"type":"error","message":"Reconnecting... 1/5 (stream disconnected)"}` + "\n" +
		readCodexFixture(t, "codex-turn.jsonl")
	turn, err := codexTurnFromFixture(t, &codexSession{totalsKnown: true}, raw)
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	if !turn.UsageReported || turn.Content == "" {
		t.Errorf("turn = %+v, want the completed turn", turn)
	}
}

// #416: a codex answer that consumed no tokens was not written by the model.
func TestCodexSession_ZeroUsageIsUnmetered(t *testing.T) {
	turn, err := codexTurnFromFixture(t, &codexSession{totalsKnown: true}, readCodexFixture(t, "codex-zero-usage.jsonl"))
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	if !turn.UsageReported {
		t.Fatal("a reported zero usage must be reported, or #416's rule cannot apply")
	}
	var we *WorkerError
	if err := unmeteredTurn("codex", turn); !errors.As(err, &we) || we.Backend != "codex" {
		t.Fatalf("want codex WorkerError for the unmetered turn, got %v", err)
	}
}

func TestCodexSession_CleanExitWithoutTurnIsEOF(t *testing.T) {
	_, err := codexTurnFromFixture(t, &codexSession{totalsKnown: true}, `{"type":"thread.started","thread_id":"t1"}`+"\n")
	if err != io.EOF {
		t.Errorf("err = %v, want io.EOF", err)
	}
}

func TestBuildCodexArgs(t *testing.T) {
	tests := []struct {
		name     string
		opts     StartOptions
		threadID string
		want     []string
	}{
		{name: "fresh", want: []string{"exec", "--json", "-"}},
		{name: "resume", threadID: "t1", want: []string{"exec", "resume", "t1", "--json", "-"}},
		{
			name:     "model and auto-approve",
			opts:     StartOptions{Model: "gpt-5", AutoApprove: AutoApproveAll},
			threadID: "t1",
			want:     []string{"exec", "resume", "t1", "--json", "--model", "gpt-5", "--dangerously-bypass-approvals-and-sandbox", "-"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildCodexArgs(tt.opts, tt.threadID); !slices.Equal(got, tt.want) {
				t.Errorf("args = %q, want %q", got, tt.want)
			}
		})
	}
}

// codexStub puts a codex on PATH that records each run's arguments and stdin
// (one numbered pair of files per run) and replays a fixture: the resumed
// one when its arguments include "resume", the first turn otherwise. It
// returns the directory the records go to.
func codexStub(t *testing.T, fixtureDir string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX-only")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"n=$(ls " + dir + " | grep -c '^args')\n" +
		"for a in \"$@\"; do printf '%s\\n' \"$a\"; done > " + dir + "/args$n\n" +
		"cat > " + dir + "/stdin$n\n"
	if fixtureDir != "" {
		script += "case \" $* \" in *' resume '*) cat " + fixtureDir + "/codex-resumed-turn.jsonl ;; " +
			"*) cat " + fixtureDir + "/codex-turn.jsonl ;; esac\n"
	}
	adapter, err := vendor.Get("codex")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, adapter.CLIName()), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	shadowVendorCLIs(t, bin)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

// Each turn is its own codex exec process with the prompt on stdin; the
// second resumes the thread the first one's thread.started named.
func TestCodexSession_PerTurnProcessesResumeTheThread(t *testing.T) {
	fixtures, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	records := codexStub(t, fixtures)
	sess, err := (&CodexBackend{}).Start(context.Background(), StartOptions{WorktreeDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()

	if _, err := sess.Next(); err == nil {
		t.Fatal("Next without Send must fail")
	}
	var turns []Turn
	for _, msg := range []string{"first task", "--second message\nwith lines"} {
		if err := sess.Send(msg); err != nil {
			t.Fatal(err)
		}
		turn, err := sess.Next()
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		turns = append(turns, turn)
	}

	const thread = "0199a213-81c0-7800-8aa1-bbab2a035a53"
	if got := readLines(t, filepath.Join(records, "args0")); !slices.Equal(got, []string{"exec", "--json", "-"}) {
		t.Errorf("first turn args = %q", got)
	}
	if got := readLines(t, filepath.Join(records, "args1")); !slices.Equal(got, []string{"exec", "resume", thread, "--json", "-"}) {
		t.Errorf("second turn args = %q", got)
	}
	for i, want := range []string{"first task", "--second message\nwith lines"} {
		data, err := os.ReadFile(filepath.Join(records, "stdin"+string(rune('0'+i))))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Errorf("turn %d stdin = %q, want %q", i+1, data, want)
		}
	}
	if sess.ResumeToken() != thread {
		t.Errorf("ResumeToken = %q, want %q", sess.ResumeToken(), thread)
	}
	if turns[1].Content != "Second turn done." || turns[1].Usage != (Usage{InputTokens: 685, OutputTokens: 78, CacheTokens: 24552}) {
		t.Errorf("second turn = %+v", turns[1])
	}
}

// A session started from a resume token resumes that thread from turn one.
func TestCodexSession_ResumeTokenResumesFromTheFirstTurn(t *testing.T) {
	fixtures, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	records := codexStub(t, fixtures)
	sess, err := (&CodexBackend{}).Start(context.Background(), StartOptions{ResumeToken: "t-prior"})
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Send("continue"); err != nil {
		t.Fatal(err)
	}
	turn, err := sess.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if got := readLines(t, filepath.Join(records, "args0")); !slices.Equal(got, []string{"exec", "resume", "t-prior", "--json", "-"}) {
		t.Errorf("args = %q", got)
	}
	// No record of what the thread consumed before: unknown, not counted twice.
	if turn.UsageReported {
		t.Errorf("usage = %+v reported, want unreported without a base", turn.Usage)
	}
}

// A resume hands the backend what the checkpoint recorded as consumed, so a
// backend reporting running totals does not count earlier turns again.
func TestRunLoop_ResumePassesUsageBase(t *testing.T) {
	tests := []struct {
		name  string
		turns []Turn
		want  *Usage
	}{
		{
			name: "usage and cache reads recorded",
			turns: []Turn{
				{Content: "r1", Usage: Usage{InputTokens: 10, OutputTokens: 5, CacheTokens: 100, CacheCreationTokens: 40}, UsageReported: true, CacheReported: true, CacheCreationReported: true},
				{Content: "r2", Usage: Usage{InputTokens: 20, OutputTokens: 7, CacheTokens: 200, CacheCreationTokens: 2}, UsageReported: true, CacheReported: true, CacheCreationReported: true},
			},
			want: &Usage{InputTokens: 30, OutputTokens: 12, CacheTokens: 300, CacheCreationTokens: 42},
		},
		{
			name: "usage and cache reads without cache writes",
			turns: []Turn{
				{Content: "r1", Usage: Usage{InputTokens: 10, OutputTokens: 5, CacheTokens: 100}, UsageReported: true, CacheReported: true},
				{Content: "r2", Usage: Usage{InputTokens: 20, OutputTokens: 7, CacheTokens: 200}, UsageReported: true, CacheReported: true},
			},
			want: &Usage{InputTokens: 30, OutputTokens: 12, CacheTokens: 300},
		},
		{
			name:  "no usage recorded",
			turns: []Turn{{Content: "r1"}, {Content: "r2"}},
		},
		{
			name: "usage without cache reads is not a complete record",
			turns: []Turn{
				{Content: "r1", Usage: Usage{InputTokens: 10, OutputTokens: 5}, UsageReported: true},
				{Content: "r2", Usage: Usage{InputTokens: 20, OutputTokens: 7}, UsageReported: true},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			failSensor(t)
			mb1 := &mockBackend{name: "mock", turns: tt.turns, resumeToken: "tok"}
			opts1 := resumeOpts(mb1, dir)
			opts1.MaxTurns = 2
			if _, err := RunLoop(opts1); err == nil {
				t.Fatal("run 1 should stop at the turn cap")
			}

			mb2 := &mockBackend{name: "mock", turns: []Turn{{Content: "r3"}}, resumeToken: "tok"}
			opts2 := resumeOpts(mb2, dir)
			opts2.Resume = dir
			opts2.MaxTurns = 3
			_, _ = RunLoop(opts2)
			if len(mb2.startOpts) != 1 {
				t.Fatalf("resume started the worker %d times, want 1", len(mb2.startOpts))
			}
			got := mb2.startOpts[0].UsageBase
			if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
				t.Errorf("UsageBase = %+v, want %+v", got, tt.want)
			}
		})
	}
}
