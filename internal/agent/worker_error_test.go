package agent

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/gate"
)

// claudeNotLoggedIn is what Claude Code 2.1.288 wrote to stdout for one turn in
// `--input-format stream-json --output-format stream-json --print --verbose`
// mode with no credentials (empty HOME, no ANTHROPIC_API_KEY). The system init
// event, which carries local paths, is left out.
func claudeNotLoggedIn(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("testdata/claude-not-logged-in.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func claudeSessionOver(raw string) *claudeSession {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 2<<20), 2<<20)
	return &claudeSession{scanner: scanner, stdin: &nopWriteCloser{new(bytes.Buffer)}}
}

func TestClaudeSession_NextClassifiesTurn(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantErr     bool
		wantAuth    bool
		wantMessage string
		wantContent string
	}{
		{
			name:        "recorded not-logged-in turn",
			raw:         "", // filled from the fixture below
			wantErr:     true,
			wantAuth:    true,
			wantMessage: "Not logged in · Please run /login",
		},
		{
			name: "normal turn",
			raw: `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"fixed it"}],"usage":{"input_tokens":12,"output_tokens":4}}}
{"type":"result","subtype":"success","is_error":false,"result":"fixed it","usage":{"input_tokens":12,"output_tokens":4}}
`,
			wantContent: "fixed it",
		},
		{
			name: "error result that is not an auth failure",
			raw: `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"API Error: 529 overloaded"}],"usage":{"input_tokens":0,"output_tokens":0}},"error":"overloaded","is_api_error_message":true}
{"type":"result","subtype":"success","is_error":true,"result":"API Error: 529 overloaded","usage":{"input_tokens":0,"output_tokens":0}}
`,
			wantErr:     true,
			wantMessage: "API Error: 529 overloaded",
		},
		{
			name: "error result with no text falls back to the error code",
			raw: `{"type":"assistant","message":{"role":"assistant","content":[]},"error":"authentication_failed"}
{"type":"result","is_error":true}
`,
			wantErr:     true,
			wantAuth:    true,
			wantMessage: "authentication_failed",
		},
		{
			name: "unmarked not-logged-in text falls back to the marker",
			raw: `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Not logged in · Please run /login"}]}}
{"type":"result","subtype":"success","result":"Not logged in · Please run /login"}
`,
			wantErr:     true,
			wantAuth:    true,
			wantMessage: "Not logged in · Please run /login",
		},
		{
			name: "the model quoting the marker is still agent output",
			raw: `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Not logged in is what the CLI says when the key is missing."}],"usage":{"input_tokens":30,"output_tokens":11}}}
{"type":"result","subtype":"success","is_error":false}
`,
			wantContent: "Not logged in is what the CLI says when the key is missing.",
		},
		{
			name: "an error field of another type does not drop the event",
			raw: `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":3,"output_tokens":1}},"error":{"unexpected":true}}
{"type":"result","is_error":false,"result":{"unexpected":true}}
`,
			wantContent: "ok",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := tt.raw
			if raw == "" {
				raw = claudeNotLoggedIn(t)
			}
			turn, err := claudeSessionOver(raw).Next()
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("Next: %v", err)
				}
				if turn.Content != tt.wantContent {
					t.Errorf("content = %q, want %q", turn.Content, tt.wantContent)
				}
				if !turn.UsageReported {
					t.Error("usage was in the stream; UsageReported should be set")
				}
				return
			}
			var we *WorkerError
			if !errors.As(err, &we) {
				t.Fatalf("want *WorkerError, got %v", err)
			}
			if we.Backend != "claude" || we.Message != tt.wantMessage || we.Auth != tt.wantAuth {
				t.Errorf("got %+v, want backend claude, message %q, auth %v", we, tt.wantMessage, tt.wantAuth)
			}
			if hint := strings.Contains(we.Error(), "env_passthrough"); hint != tt.wantAuth {
				t.Errorf("env_passthrough hint present = %v, want %v: %q", hint, tt.wantAuth, we.Error())
			}
		})
	}
}

func TestParseCursorOutput_ErrorResultIsWorkerError(t *testing.T) {
	raw := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"partial"}]}}
{"type":"result","subtype":"error","is_error":true,"result":"Authentication required"}
`
	_, err := parseCursorOutput(strings.NewReader(raw))
	var we *WorkerError
	if !errors.As(err, &we) || we.Backend != "cursor" || we.Message != "Authentication required" {
		t.Fatalf("want cursor WorkerError with the result text, got %v", err)
	}
}

func TestCodexSession_TurnFailedIsWorkerError(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "with message",
			raw:  `{"type":"turn.failed","error":{"message":"unexpected status 401 Unauthorized"}}` + "\n",
			want: "unexpected status 401 Unauthorized",
		},
		{
			name: "without message",
			raw:  `{"type":"turn.failed","error":"not an object"}` + "\n",
			want: "turn failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := &codexSession{
				scanner: bufio.NewScanner(strings.NewReader(tt.raw)),
				stdin:   &nopWriteCloser{new(bytes.Buffer)},
			}
			_, err := sess.Next()
			var we *WorkerError
			if !errors.As(err, &we) || we.Backend != "codex" || we.Message != tt.want {
				t.Fatalf("want codex WorkerError %q, got %v", tt.want, err)
			}
		})
	}
}

func TestUnmeteredTurn(t *testing.T) {
	long := strings.Repeat("é", 250)
	tests := []struct {
		name    string
		turn    Turn
		wantErr bool
		wantSub string
	}{
		{name: "usage not reported", turn: Turn{Content: "hello"}},
		{name: "usage reported and non-zero", turn: Turn{Content: "hello", UsageReported: true, Usage: Usage{OutputTokens: 1}}},
		{name: "usage reported, zero, empty response", turn: Turn{Content: "  ", UsageReported: true}},
		{
			name:    "usage reported, zero, a response",
			turn:    Turn{Content: "Not logged in\nsecond line", UsageReported: true},
			wantErr: true,
			wantSub: `consumed no tokens, so the model did not write it: "Not logged in"`,
		},
		{
			name:    "long response is cut on a rune boundary",
			turn:    Turn{Content: long, UsageReported: true},
			wantErr: true,
			wantSub: strings.Repeat("é", 200) + `..."`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := unmeteredTurn("mock", tt.turn)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("%q does not contain %q", err.Error(), tt.wantSub)
			}
		})
	}
}

// fixtureBackend replays recorded claude stream-json through the real claude
// parser, so the loop test exercises the same code a live worker would.
type fixtureBackend struct {
	raw   string
	sends int
}

func (b *fixtureBackend) Name() string { return "claude" }

func (b *fixtureBackend) Start(context.Context, StartOptions) (WorkerSession, error) {
	return &fixtureSession{claudeSession: claudeSessionOver(b.raw), backend: b}, nil
}

type fixtureSession struct {
	*claudeSession
	backend *fixtureBackend
}

func (s *fixtureSession) Send(string) error { s.backend.sends++; return nil }
func (s *fixtureSession) Close() error      { return nil }

// A worker that cannot authenticate answers every turn the same way. Before
// #414 the loop fed that back as agent output, spent three turns and the
// sensors on each, and ended the run as stuck. It is a worker error, and the
// first turn is enough to know.
func TestRunLoop_UnauthenticatedWorkerIsWorkerErrorOnFirstTurn(t *testing.T) {
	stubCheck(t, func(_ int, name string) gate.Result {
		return gate.Result{Name: name, Kind: "command", Tolerance: "blocking", Status: gate.StatusFail, ExitCode: 1}
	})
	fixture := claudeNotLoggedIn(t)
	fb := &fixtureBackend{raw: strings.Repeat(fixture, 3)}

	var stdout, stderr bytes.Buffer
	opts := baseOpts(fb, &stdout, &stderr, strings.NewReader(""))
	opts.testSensorNames = []string{"build"}

	result, err := RunLoop(opts)
	var exitErr *ExitError
	if !asExitError(err, &exitErr) || exitErr.Code != ExitWorkerError {
		t.Fatalf("want ExitWorkerError (%d), got %v", ExitWorkerError, err)
	}
	want := "worker error: claude: Not logged in · Please run /login"
	if !strings.HasPrefix(exitErr.Message, want) || !strings.Contains(exitErr.Message, "env_passthrough") {
		t.Errorf("reason = %q, want it to open with %q and point at env_passthrough", exitErr.Message, want)
	}
	if fb.sends != 1 {
		t.Errorf("the run should end on the first turn, but %d messages were sent", fb.sends)
	}
	if result.Reason != exitErr.Message || result.ExitCode != ExitWorkerError {
		t.Errorf("result = exit %d %q, want it to mirror the exit error", result.ExitCode, result.Reason)
	}
}

// The vendor-neutral safety net, and where it must not fire.
func TestRunLoop_ZeroTokenResponses(t *testing.T) {
	stubCheck(t, func(_ int, name string) gate.Result {
		return gate.Result{Name: name, Kind: "command", Tolerance: "blocking", Status: gate.StatusFail, ExitCode: 1}
	})
	repeat := func(turn Turn) []Turn { return []Turn{turn, turn, turn, turn} }
	tests := []struct {
		name      string
		turns     []Turn
		wantCode  int
		wantSub   string
		wantNexts int
	}{
		{
			name:      "usage-reporting backend, zero tokens: worker error on turn 1",
			turns:     repeat(Turn{Content: "Please log in", UsageReported: true}),
			wantCode:  ExitWorkerError,
			wantSub:   "worker error: mock: the response consumed no tokens",
			wantNexts: 1,
		},
		{
			name:      "backend that reports no usage: zero is not failure, repetition is still stuck",
			turns:     repeat(Turn{Content: "same again"}),
			wantCode:  ExitStuck,
			wantSub:   "edit-loop",
			wantNexts: 3,
		},
		{
			name:      "metered identical responses are still stuck",
			turns:     repeat(Turn{Content: "same again", UsageReported: true, Usage: Usage{InputTokens: 9, OutputTokens: 2}}),
			wantCode:  ExitStuck,
			wantSub:   "edit-loop",
			wantNexts: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mb := &mockBackend{name: "mock", turns: tt.turns}
			var stdout, stderr bytes.Buffer
			opts := baseOpts(mb, &stdout, &stderr, strings.NewReader(""))
			opts.testSensorNames = []string{"build"}

			_, err := RunLoop(opts)
			var exitErr *ExitError
			if !asExitError(err, &exitErr) || exitErr.Code != tt.wantCode {
				t.Fatalf("want exit %d, got %v", tt.wantCode, err)
			}
			if !strings.Contains(exitErr.Message, tt.wantSub) {
				t.Errorf("reason %q does not contain %q", exitErr.Message, tt.wantSub)
			}
			if mb.pos != tt.wantNexts {
				t.Errorf("worker turns read = %d, want %d", mb.pos, tt.wantNexts)
			}
		})
	}
}

// The plan phase reads worker turns too, and must classify them the same way.
func TestRunLoop_PlanPhaseWorkerErrorKeepsVendorMessage(t *testing.T) {
	mb := &mockBackend{
		name:  "claude",
		turns: []Turn{{}},
		errs:  []error{&WorkerError{Backend: "claude", Message: "Not logged in · Please run /login", Auth: true}},
	}
	var stdout, stderr bytes.Buffer
	opts := baseOpts(mb, &stdout, &stderr, strings.NewReader(""))
	opts.NoPlan = false

	_, err := RunLoop(opts)
	var exitErr *ExitError
	if !asExitError(err, &exitErr) || exitErr.Code != ExitWorkerError {
		t.Fatalf("want ExitWorkerError, got %v", err)
	}
	if !strings.HasPrefix(exitErr.Message, "worker error: claude: Not logged in") {
		t.Errorf("reason = %q", exitErr.Message)
	}
}

// The run ynf saw (#434): the same test failing every turn, only its timings
// changing. The loop must end it as stuck, not spend the whole turn budget.
func TestRunLoop_IdenticalFailureWithVaryingTimingsIsStuck(t *testing.T) {
	stubCheck(t, func(call int, name string) gate.Result {
		out := fmt.Sprintf("--- FAIL: TestParse (0.%02ds)\n    parse_test.go:12: got 3, want 4\nFAIL\nFAIL\tgithub.com/x/y/pkg\t%d.%03ds\n", call, call, call*37)
		return gate.Result{Name: name, Kind: "command", Tolerance: "blocking", Status: gate.StatusFail, ExitCode: 1, Stdout: out}
	})
	var turns []Turn
	for i := 1; i <= 12; i++ {
		turns = append(turns, Turn{Content: fmt.Sprintf("attempt %d", i)})
	}
	mb := &mockBackend{name: "mock", turns: turns}
	var stdout, stderr bytes.Buffer
	opts := baseOpts(mb, &stdout, &stderr, strings.NewReader(""))
	opts.MaxTurns = 12
	opts.testSensorNames = []string{"test"}

	_, err := RunLoop(opts)
	var exitErr *ExitError
	if !asExitError(err, &exitErr) || exitErr.Code != ExitStuck {
		t.Fatalf("want ExitStuck (%d), got %v", ExitStuck, err)
	}
	if !strings.Contains(exitErr.Message, "no sensor progress for 5 consecutive turns") {
		t.Errorf("reason = %q, want the no-progress signal", exitErr.Message)
	}
	if mb.pos != 6 {
		t.Errorf("worker turns read = %d, want 6 (one reference turn, five unchanged)", mb.pos)
	}
}
