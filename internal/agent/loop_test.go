package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/eyelock/ynh/internal/gate"
)

// mockBackend is a test WorkerBackend that returns scripted turns.
// delays and errs are optional parallel slices: when set at position N,
// Next() applies that delay/error before returning turns[N]. Both are
// zero-value compatible — tests that don't need them can leave them nil.
type mockBackend struct {
	name        string
	turns       []Turn
	delays      []time.Duration
	errs        []error
	pos         int
	sends       []string
	resumeToken string         // returned by the session's ResumeToken()
	startOpts   []StartOptions // captures each Start call for resume assertions

	// Stop hooks (1-indexed Next() call number). When the matching call is
	// reached, the mock triggers a stop and blocks until the loop's context is
	// cancelled, then reports the cancellation so the loop takes the interrupt
	// path deterministically (turn N-1 stays the last completed turn).
	interruptAtCall int       // writes {"action":"interrupt"} to interruptWriter
	interruptWriter io.Writer // sink wired to the loop's control stdin
	sigtermAtCall   int       // raises SIGTERM at the process

	// onTurn runs as the worker produces turn N, for tests that need the
	// worktree to change while a turn is in flight.
	onTurn func(call int)
}

func (m *mockBackend) Name() string { return m.name }

func (m *mockBackend) Start(ctx context.Context, opts StartOptions) (WorkerSession, error) {
	m.startOpts = append(m.startOpts, opts)
	return &mockSession{backend: m, ctx: ctx}, nil
}

type mockSession struct {
	backend *mockBackend
	ctx     context.Context
}

func (s *mockSession) ResumeToken() string { return s.backend.resumeToken }

func (s *mockSession) Send(msg string) error {
	s.backend.sends = append(s.backend.sends, msg)
	return nil
}

func (s *mockSession) Next() (Turn, error) {
	mb := s.backend
	if mb.pos >= len(mb.turns) {
		return Turn{}, io.EOF
	}
	pos := mb.pos
	mb.pos++
	call := pos + 1 // 1-indexed Next() call number

	if mb.onTurn != nil {
		mb.onTurn(call)
	}

	if mb.interruptAtCall == call && mb.interruptWriter != nil {
		_, _ = mb.interruptWriter.Write([]byte(`{"action":"interrupt"}` + "\n"))
		<-s.ctx.Done()
		return Turn{}, s.ctx.Err()
	}
	if mb.sigtermAtCall == call {
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
		<-s.ctx.Done()
		return Turn{}, s.ctx.Err()
	}

	if pos < len(mb.delays) && mb.delays[pos] > 0 {
		time.Sleep(mb.delays[pos])
	}
	if pos < len(mb.errs) && mb.errs[pos] != nil {
		return Turn{}, mb.errs[pos]
	}
	return mb.turns[pos], nil
}

func (s *mockSession) Close() error { return nil }

// baseOpts returns a minimal RunOptions for testing the loop without harness or sensors.
func baseOpts(backend WorkerBackend, stdout, stderr io.Writer, stdin io.Reader) RunOptions {
	return RunOptions{
		HarnessName:     "", // no harness — sensors are skipped
		Task:            "test task",
		NoPlan:          true, // skip plan phase
		MaxTurns:        5,
		Stdout:          stdout,
		Stderr:          stderr,
		Stdin:           stdin,
		YNHBinary:       "ynh",
		backendOverride: backend,
	}
}

func TestRunLoop_ConvergesWhenNoSensors(t *testing.T) {
	// With no sensors declared and a single response from the worker,
	// the loop should immediately converge (all zero sensors = all passed).
	mb := &mockBackend{
		name:  "mock",
		turns: []Turn{{Content: "done"}},
	}
	var stdout, stderr bytes.Buffer
	opts := baseOpts(mb, &stdout, &stderr, strings.NewReader(""))

	err := RunLoop(opts)
	if err != nil {
		t.Fatalf("expected convergence, got: %v", err)
	}
}

func TestRunLoop_TurnCapExceeded(t *testing.T) {
	// Worker keeps responding, sensors always fail → no convergence.
	// MaxTurns=1 should stop after the first act turn is recorded.
	stubCheck(t, func(_ int, name string) gate.Result {
		return gate.Result{Name: name, Kind: "command", Tolerance: "blocking", Status: gate.StatusFail, ExitCode: 1}
	})

	mb := &mockBackend{
		name: "mock",
		turns: []Turn{
			{Content: "response 1"},
			{Content: "response 2"},
		},
	}
	var stdout, stderr bytes.Buffer
	opts := baseOpts(mb, &stdout, &stderr, strings.NewReader(""))
	opts.MaxTurns = 1
	opts.testSensorNames = []string{"build"}

	err := RunLoop(opts)
	var exitErr *ExitError
	if err == nil {
		t.Fatal("expected ExitError, got nil")
	}
	if !asExitError(err, &exitErr) || exitErr.Code != ExitIterationCap {
		t.Errorf("expected ExitIterationCap (%d), got: %v", ExitIterationCap, err)
	}
}

func TestRunLoop_WorkerEOFBeforeConvergence(t *testing.T) {
	// Worker exits after one turn but sensors (if any) didn't all pass.
	// With sensors mocked to fail, worker EOF is a worker error.
	mb := &mockBackend{
		name:  "mock",
		turns: []Turn{{Content: "partial work"}},
	}
	var stdout, stderr bytes.Buffer
	opts := baseOpts(mb, &stdout, &stderr, strings.NewReader(""))

	// Worker returns EOF after 1 turn; since there are no sensors, loop converges.
	err := RunLoop(opts)
	if err != nil {
		t.Fatalf("no sensors → should converge on first turn, got: %v", err)
	}
}

func TestRunLoop_SensorFailureSendsFeedback(t *testing.T) {
	// Sensor fails on turn 1, passes on turn 2. Loop should converge on turn 2.
	stubCheck(t, func(call int, name string) gate.Result {
		r := gate.Result{Name: name, Kind: "command", Tolerance: "blocking", Status: gate.StatusPass}
		if call == 1 {
			r.Status, r.ExitCode = gate.StatusFail, 1
		}
		return r
	})

	mb := &mockBackend{
		name: "mock",
		turns: []Turn{
			{Content: "first attempt"},
			{Content: "second attempt (fixed)"},
		},
	}
	var stdout, stderr bytes.Buffer
	opts := baseOpts(mb, &stdout, &stderr, strings.NewReader(""))
	opts.testSensorNames = []string{"build"} // inject sensor without real harness

	err := RunLoop(opts)
	if err != nil {
		t.Fatalf("expected convergence after retry, got: %v", err)
	}
	// Worker should have received the initial task + sensor feedback.
	if len(mb.sends) < 2 {
		t.Errorf("expected at least 2 sends (task + feedback), got %d", len(mb.sends))
	}
}

func TestRunLoop_TrajectoryContainsSessionStart(t *testing.T) {
	mb := &mockBackend{
		name:  "mock",
		turns: []Turn{{Content: "done"}},
	}
	var traj, stderr bytes.Buffer
	opts := baseOpts(mb, &bytes.Buffer{}, &stderr, strings.NewReader(""))
	opts.EmitJSONL = "-"
	opts.Stdout = &traj

	if err := RunLoop(opts); err != nil {
		t.Fatalf("RunLoop: %v", err)
	}

	lines := bytes.Split(bytes.TrimRight(traj.Bytes(), "\n"), []byte("\n"))
	if len(lines) == 0 {
		t.Fatal("no trajectory events emitted")
	}

	var firstEvent Event
	if err := json.Unmarshal(lines[0], &firstEvent); err != nil {
		t.Fatalf("parsing first event: %v", err)
	}
	if firstEvent.Kind != KindSessionStart {
		t.Errorf("expected first event to be session_start, got %q", firstEvent.Kind)
	}
}

func TestRunLoop_TrajectoryEndsWithSessionEnd(t *testing.T) {
	mb := &mockBackend{
		name:  "mock",
		turns: []Turn{{Content: "done"}},
	}
	var traj, stderr bytes.Buffer
	opts := baseOpts(mb, &bytes.Buffer{}, &stderr, strings.NewReader(""))
	opts.EmitJSONL = "-"
	opts.Stdout = &traj

	if err := RunLoop(opts); err != nil {
		t.Fatalf("RunLoop: %v", err)
	}

	lines := bytes.Split(bytes.TrimRight(traj.Bytes(), "\n"), []byte("\n"))
	last := lines[len(lines)-1]

	var lastEvent Event
	if err := json.Unmarshal(last, &lastEvent); err != nil {
		t.Fatalf("parsing last event: %v", err)
	}
	if lastEvent.Kind != KindSessionEnd {
		t.Errorf("expected last event to be session_end, got %q", lastEvent.Kind)
	}
}

func TestRunLoop_TaskRequired(t *testing.T) {
	mb := &mockBackend{name: "mock"}
	var stdout, stderr bytes.Buffer
	opts := baseOpts(mb, &stdout, &stderr, strings.NewReader(""))
	opts.Task = "" // missing

	// Should not crash — missing task is detected by the CLI layer.
	// But if somehow called with empty task, the loop will send "" as first message.
	// This test verifies no panic occurs.
	err := RunLoop(opts)
	// With no turns and empty task, worker returns EOF immediately.
	// That's a worker error since convergence wasn't reached.
	if err == nil {
		// No sensors → convergence (valid outcome with 0 turns + 0 sensors).
		return
	}
}

func TestSelectBackend(t *testing.T) {
	wb, err := selectBackend("claude")
	if err != nil {
		t.Fatalf("selectBackend claude: %v", err)
	}
	if wb.Name() != "claude" {
		t.Errorf("expected claude, got %q", wb.Name())
	}

	_, err = selectBackend("unknown")
	if err == nil {
		t.Error("expected error for unknown backend")
	}
}

func TestSynthesizeFeedback(t *testing.T) {
	fb := synthesizeFeedback(env(
		gate.Result{Name: "build", Kind: "command", Tolerance: "blocking",
			Status: gate.StatusPass, DurationMS: 800},
		gate.Result{Name: "test", Kind: "command", Tolerance: "blocking",
			Status: gate.StatusFail, ExitCode: 1, DurationMS: 2100, Stdout: "FAIL: TestFoo"},
	))
	if !strings.Contains(fb, "<sensor-results>") {
		t.Error("feedback should contain <sensor-results>")
	}
	if !strings.Contains(fb, `status="fail"`) {
		t.Error("feedback should mark test as failed")
	}
	if !strings.Contains(fb, `status="pass"`) {
		t.Error("feedback should mark build as passed")
	}
	if !strings.Contains(fb, "FAIL: TestFoo") {
		t.Error("feedback should carry the failing output the worker must act on")
	}
}

func TestExitError(t *testing.T) {
	e := &ExitError{Code: ExitStuck, Message: "stuck in a loop"}
	if e.Error() != "stuck in a loop" {
		t.Errorf("unexpected Error() string: %q", e.Error())
	}

	e2 := &ExitError{Code: ExitWorkerError}
	if e2.Error() == "" {
		t.Error("ExitError with no message should still return non-empty string")
	}
}

// asExitError type-asserts err to *ExitError and stores it in out.
func asExitError(err error, out **ExitError) bool {
	if exitErr, ok := err.(*ExitError); ok {
		*out = exitErr
		return true
	}
	return false
}

func TestValidateBackend(t *testing.T) {
	t.Run("defaults empty to claude", func(t *testing.T) {
		got, err := validateBackend("")
		if err != nil || got != "claude" {
			t.Errorf("validateBackend(\"\") = (%q, %v), want (\"claude\", nil)", got, err)
		}
	})
	t.Run("accepts canonical names", func(t *testing.T) {
		for _, name := range []string{"claude", "codex", "cursor"} {
			got, err := validateBackend(name)
			if err != nil || got != name {
				t.Errorf("validateBackend(%q) = (%q, %v); want (%q, nil)", name, got, err, name)
			}
		}
	})
	t.Run("rejects claude-code with hint", func(t *testing.T) {
		_, err := validateBackend("claude-code")
		if err == nil || !strings.Contains(err.Error(), "did you mean \"claude\"") {
			t.Errorf("expected hinted rejection for claude-code, got: %v", err)
		}
	})
	t.Run("rejects unknown", func(t *testing.T) {
		_, err := validateBackend("nope")
		if err == nil {
			t.Error("expected error for unknown backend")
		}
	})
}
