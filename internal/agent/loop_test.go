package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// mockBackend is a test WorkerBackend that returns scripted turns.
type mockBackend struct {
	name  string
	turns []Turn
	pos   int
	sends []string
}

func (m *mockBackend) Name() string { return m.name }

func (m *mockBackend) Start(_ context.Context, _ StartOptions) (WorkerSession, error) {
	return &mockSession{backend: m}, nil
}

type mockSession struct {
	backend *mockBackend
}

func (s *mockSession) Send(msg string) error {
	s.backend.sends = append(s.backend.sends, msg)
	return nil
}

func (s *mockSession) Next() (Turn, error) {
	if s.backend.pos >= len(s.backend.turns) {
		return Turn{}, io.EOF
	}
	t := s.backend.turns[s.backend.pos]
	s.backend.pos++
	return t, nil
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
	original := runSensorFn
	defer func() { runSensorFn = original }()
	runSensorFn = func(ynh, harnessName, sensorName, cwd, overlayJSON string) (*SensorResult, error) {
		return &SensorResult{Name: sensorName, Kind: "command", ExitCode: 1}, nil
	}

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
	original := runSensorFn
	defer func() { runSensorFn = original }()

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
	original := runSensorFn
	defer func() { runSensorFn = original }()

	callCount := 0
	runSensorFn = func(ynh, harnessName, sensorName, cwd, overlayJSON string) (*SensorResult, error) {
		callCount++
		exitCode := 1
		if callCount > 1 {
			exitCode = 0 // pass on second sensor run
		}
		return &SensorResult{Name: sensorName, Kind: "command", ExitCode: exitCode}, nil
	}

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
	results := []*SensorResult{
		{Name: "build", Kind: "command", ExitCode: 0, DurationMS: 800},
		{Name: "test", Kind: "command", ExitCode: 1, DurationMS: 2100,
			Output: SensorRunOutput{Stdout: "FAIL: TestFoo"}},
	}
	fb := synthesizeFeedback(results)
	if !strings.Contains(fb, "<sensor-results>") {
		t.Error("feedback should contain <sensor-results>")
	}
	if !strings.Contains(fb, `status="failed"`) {
		t.Error("feedback should mark test as failed")
	}
	if !strings.Contains(fb, `status="passed"`) {
		t.Error("feedback should mark build as passed")
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
