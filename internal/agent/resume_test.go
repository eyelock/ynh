package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/gate"
)

// resumeOpts builds RunOptions wired to a real session directory so the
// checkpoint sidecar is active (EmitJSONL points at a file, not "-").
func resumeOpts(backend WorkerBackend, dir string) RunOptions {
	return RunOptions{
		Task:            "test task",
		NoPlan:          true, // exercise the act loop directly
		Stdout:          io.Discard,
		Stderr:          io.Discard,
		Stdin:           strings.NewReader(""),
		YNHBinary:       "ynh",
		EmitJSONL:       filepath.Join(dir, "trajectory.jsonl"),
		testSensorNames: []string{"build"},
		backendOverride: backend,
	}
}

// failSensor / passSensor swap the package gate hook for the duration of a test.
func failSensor(t *testing.T) {
	t.Helper()
	stubCheck(t, func(_ int, name string) gate.Result {
		return gate.Result{Name: name, Kind: "command", Tolerance: "blocking", Status: gate.StatusFail, ExitCode: 1}
	})
}

func passSensor(t *testing.T) {
	t.Helper()
	stubCheck(t, func(_ int, name string) gate.Result {
		return gate.Result{Name: name, Kind: "command", Tolerance: "blocking", Status: gate.StatusPass}
	})
}

func readTrajectoryFile(t *testing.T, path string) []Event {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading trajectory: %v", err)
	}
	var events []Event
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("trajectory line %q: %v", sc.Text(), err)
		}
		events = append(events, e)
	}
	return events
}

func decodeData[T any](t *testing.T, e Event) T {
	t.Helper()
	raw, _ := json.Marshal(e.Data)
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decoding %s data: %v", e.Kind, err)
	}
	return out
}

// Resume restores budget counters, re-supplies the backend resume token, and
// continues the act loop at turn N+1 — without re-running completed turns.
func TestRunLoop_ResumeRestoresBudgetAndContinues(t *testing.T) {
	dir := t.TempDir()

	// ── Run 1: sensors fail, capped at 2 turns. ──────────────────────────────
	failSensor(t)
	mb1 := &mockBackend{
		name:        "mock",
		turns:       []Turn{{Content: "r1"}, {Content: "r2"}},
		resumeToken: "tok-1",
	}
	opts1 := resumeOpts(mb1, dir)
	opts1.MaxTurns = 2
	_, err1 := RunLoop(opts1)
	var ee *ExitError
	if !asExitError(err1, &ee) || ee.Code != ExitIterationCap {
		t.Fatalf("run 1: expected ExitIterationCap, got %v", err1)
	}

	cp, err := readCheckpoint(dir)
	if err != nil {
		t.Fatalf("readCheckpoint after run 1: %v", err)
	}
	if cp.LastCompletedTurn != 2 {
		t.Errorf("LastCompletedTurn = %d, want 2", cp.LastCompletedTurn)
	}
	if cp.Budget.Turns != 2 {
		t.Errorf("Budget.Turns = %d, want 2", cp.Budget.Turns)
	}
	if cp.ResumeToken != "tok-1" {
		t.Errorf("ResumeToken = %q, want tok-1", cp.ResumeToken)
	}
	if cp.Phase != PhaseAct {
		t.Errorf("Phase = %q, want act", cp.Phase)
	}

	// ── Run 2: --resume, sensors now pass → converge on turn 3. ──────────────
	passSensor(t)
	mb2 := &mockBackend{
		name:        "mock",
		turns:       []Turn{{Content: "done"}},
		resumeToken: "tok-1",
	}
	opts2 := resumeOpts(mb2, dir)
	opts2.Resume = dir
	opts2.MaxTurns = 5
	if _, err := RunLoop(opts2); err != nil {
		t.Fatalf("run 2 (resume): expected convergence, got %v", err)
	}

	// The reconstructed worker must be started with the persisted token.
	if len(mb2.startOpts) != 1 {
		t.Fatalf("resume should Start the worker once, got %d", len(mb2.startOpts))
	}
	if mb2.startOpts[0].ResumeToken != "tok-1" {
		t.Errorf("resume StartOptions.ResumeToken = %q, want tok-1", mb2.startOpts[0].ResumeToken)
	}

	// Exactly one turn ran after resume (the next turn, not a replay of 1–2).
	if len(mb2.sends) != 1 {
		t.Errorf("post-resume sends = %d, want 1 (only the continuation turn)", len(mb2.sends))
	}

	events := readTrajectoryFile(t, opts2.EmitJSONL)
	// Trajectory is appended: it still holds run 1's session_start AND run 2's
	// session_resumed.
	if kindCount(events, KindSessionStart) != 1 {
		t.Errorf("session_start count = %d, want 1 (appended, not truncated)", kindCount(events, KindSessionStart))
	}
	if kindCount(events, KindSessionResumed) != 1 {
		t.Fatalf("session_resumed count = %d, want 1", kindCount(events, KindSessionResumed))
	}
	for _, e := range events {
		if e.Kind != KindSessionResumed {
			continue
		}
		d := decodeData[SessionResumedData](t, e)
		if d.ResumedAtTurn != 3 {
			t.Errorf("ResumedAtTurn = %d, want 3", d.ResumedAtTurn)
		}
		if d.RestoredTurns != 2 {
			t.Errorf("RestoredTurns = %d, want 2", d.RestoredTurns)
		}
	}
	// The continuation turn is numbered 3 (N+1), proving no replay of 1–2.
	var sawTurn3 bool
	for _, e := range events {
		if e.Kind == KindTurnStart && e.Turn == 3 {
			sawTurn3 = true
		}
	}
	if !sawTurn3 {
		t.Error("expected a turn_start at turn 3 after resume")
	}
}

// A fresh (non-resume) run truncates the trajectory: a second fresh run into
// the same path leaves exactly one session_start, from the second run.
func TestRunLoop_FreshRunTruncatesTrajectory(t *testing.T) {
	dir := t.TempDir()
	passSensor(t) // converge immediately each run

	run := func(task string) {
		mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
		opts := resumeOpts(mb, dir)
		opts.Task = task
		if _, err := RunLoop(opts); err != nil {
			t.Fatalf("run %q: %v", task, err)
		}
	}
	run("first")
	run("second")

	events := readTrajectoryFile(t, filepath.Join(dir, "trajectory.jsonl"))
	if got := kindCount(events, KindSessionStart); got != 1 {
		t.Fatalf("session_start count = %d, want 1 (fresh run must truncate)", got)
	}
	for _, e := range events {
		if e.Kind == KindSessionStart {
			d := decodeData[SessionStartData](t, e)
			if d.Task != "second" {
				t.Errorf("session_start task = %q, want second (stale run-1 events should be gone)", d.Task)
			}
		}
	}
}

// An interrupt mid-turn leaves a checkpoint at the last completed turn, and a
// subsequent --resume redoes only the incomplete turn.
func TestRunLoop_InterruptLeavesResumableCheckpoint(t *testing.T) {
	dir := t.TempDir()
	failSensor(t)

	pr, pw := io.Pipe()
	mb1 := &mockBackend{
		name:            "mock",
		turns:           []Turn{{Content: "r1"}, {Content: "r2"}},
		resumeToken:     "tok-int",
		interruptAtCall: 2, // interrupt during turn 2 (turn 1 already completed)
		interruptWriter: pw,
	}
	opts1 := resumeOpts(mb1, dir)
	opts1.Stdin = pr
	opts1.MaxTurns = 10
	_, err1 := RunLoop(opts1)
	var ee *ExitError
	if !asExitError(err1, &ee) || ee.Code != ExitInterrupted {
		t.Fatalf("expected ExitInterrupted, got %v", err1)
	}

	cp, err := readCheckpoint(dir)
	if err != nil {
		t.Fatalf("readCheckpoint after interrupt: %v", err)
	}
	if cp.LastCompletedTurn != 1 {
		t.Errorf("LastCompletedTurn = %d, want 1 (turn 2 was interrupted)", cp.LastCompletedTurn)
	}

	// Resume redoes only the incomplete turn (turn 2) and converges.
	passSensor(t)
	mb2 := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}, resumeToken: "tok-int"}
	opts2 := resumeOpts(mb2, dir)
	opts2.Resume = dir
	opts2.MaxTurns = 10
	if _, err := RunLoop(opts2); err != nil {
		t.Fatalf("resume after interrupt: %v", err)
	}
	if len(mb2.sends) != 1 {
		t.Errorf("post-resume sends = %d, want 1 (only the redone turn)", len(mb2.sends))
	}
	events := readTrajectoryFile(t, opts2.EmitJSONL)
	for _, e := range events {
		if e.Kind == KindSessionResumed {
			if d := decodeData[SessionResumedData](t, e); d.ResumedAtTurn != 2 {
				t.Errorf("ResumedAtTurn = %d, want 2", d.ResumedAtTurn)
			}
		}
	}
}

// A run can be interrupted, resumed, interrupted again, and resumed again —
// the checkpoint advances each time and the final resume converges.
func TestRunLoop_DoubleInterruptResumes(t *testing.T) {
	dir := t.TempDir()

	// Run 1: interrupt during turn 2 → checkpoint last=1.
	failSensor(t)
	pr1, pw1 := io.Pipe()
	mb1 := &mockBackend{
		name: "mock", turns: []Turn{{Content: "r1"}, {Content: "r2"}},
		resumeToken: "tok-multi", interruptAtCall: 2, interruptWriter: pw1,
	}
	o1 := resumeOpts(mb1, dir)
	o1.Stdin = pr1
	o1.MaxTurns = 20
	if _, err := RunLoop(o1); err == nil {
		t.Fatal("run 1 should interrupt")
	}
	if cp, _ := readCheckpoint(dir); cp.LastCompletedTurn != 1 {
		t.Fatalf("after run 1: LastCompletedTurn = %d, want 1", cp.LastCompletedTurn)
	}

	// Run 2: resume, complete one more turn, then interrupt → checkpoint last=2.
	pr2, pw2 := io.Pipe()
	mb2 := &mockBackend{
		name: "mock", turns: []Turn{{Content: "r2b"}, {Content: "r3"}},
		resumeToken: "tok-multi", interruptAtCall: 2, interruptWriter: pw2,
	}
	o2 := resumeOpts(mb2, dir)
	o2.Resume = dir
	o2.Stdin = pr2
	o2.MaxTurns = 20
	if _, err := RunLoop(o2); err == nil {
		t.Fatal("run 2 should interrupt")
	}
	cp2, _ := readCheckpoint(dir)
	if cp2.LastCompletedTurn != 2 {
		t.Fatalf("after run 2: LastCompletedTurn = %d, want 2", cp2.LastCompletedTurn)
	}
	if cp2.ResumeToken != "tok-multi" {
		t.Errorf("after run 2: ResumeToken = %q, want tok-multi", cp2.ResumeToken)
	}

	// Run 3: resume and converge.
	passSensor(t)
	mb3 := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}, resumeToken: "tok-multi"}
	o3 := resumeOpts(mb3, dir)
	o3.Resume = dir
	o3.MaxTurns = 20
	if _, err := RunLoop(o3); err != nil {
		t.Fatalf("run 3 should converge, got %v", err)
	}
	if len(mb3.sends) != 1 {
		t.Errorf("final resume sends = %d, want 1", len(mb3.sends))
	}
}

// SIGTERM shares the interrupt cancel path: it leaves a resumable checkpoint
// at the last completed turn.
func TestRunLoop_SIGTERMLeavesResumableCheckpoint(t *testing.T) {
	dir := t.TempDir()
	failSensor(t)

	mb := &mockBackend{
		name:          "mock",
		turns:         []Turn{{Content: "r1"}, {Content: "r2"}},
		resumeToken:   "tok-sig",
		sigtermAtCall: 2, // SIGTERM during turn 2
	}
	opts := resumeOpts(mb, dir)
	opts.MaxTurns = 10
	_, err := RunLoop(opts)
	var ee *ExitError
	if !asExitError(err, &ee) || ee.Code != ExitInterrupted {
		t.Fatalf("expected ExitInterrupted from SIGTERM, got %v", err)
	}

	cp, rerr := readCheckpoint(dir)
	if rerr != nil {
		t.Fatalf("readCheckpoint after SIGTERM: %v", rerr)
	}
	if cp.LastCompletedTurn != 1 {
		t.Errorf("LastCompletedTurn = %d, want 1", cp.LastCompletedTurn)
	}
	if cp.ResumeToken != "tok-sig" {
		t.Errorf("ResumeToken = %q, want tok-sig", cp.ResumeToken)
	}
}

// Resuming past an already-exceeded budget honors the cap immediately: it emits
// budget_exceeded and exits without starting a worker or a new turn.
func TestRunLoop_ResumePastExceededBudgetExits(t *testing.T) {
	dir := t.TempDir()
	// Hand-author a checkpoint whose budget already exceeds the cap.
	if err := writeCheckpoint(dir, &Checkpoint{
		SessionID:         "s",
		Backend:           "mock",
		Phase:             PhaseAct,
		PlanFinalized:     true,
		LastCompletedTurn: 5,
		PendingMessage:    "continue",
		Budget:            CheckpointBudget{Turns: 5},
	}); err != nil {
		t.Fatal(err)
	}

	mb := &mockBackend{name: "mock"} // no turns; must never be asked for one
	opts := resumeOpts(mb, dir)
	opts.Resume = dir
	opts.MaxTurns = 3 // already exceeded by the restored 5

	_, err := RunLoop(opts)
	var ee *ExitError
	if !asExitError(err, &ee) || ee.Code != ExitIterationCap {
		t.Fatalf("expected ExitIterationCap, got %v", err)
	}
	if len(mb.startOpts) != 0 {
		t.Errorf("worker must not be started when budget is already exceeded; Start calls = %d", len(mb.startOpts))
	}

	events := readTrajectoryFile(t, opts.EmitJSONL)
	if kindCount(events, KindBudgetExceeded) != 1 {
		t.Errorf("budget_exceeded count = %d, want 1", kindCount(events, KindBudgetExceeded))
	}
}

func TestRunLoop_ResumeMissingCheckpointFails(t *testing.T) {
	mb := &mockBackend{name: "mock"}
	opts := resumeOpts(mb, t.TempDir())
	opts.Resume = t.TempDir() // empty dir, no checkpoint.json
	_, err := RunLoop(opts)
	var ee *ExitError
	if !asExitError(err, &ee) || ee.Code != ExitResumeError {
		t.Fatalf("expected ExitResumeError for missing checkpoint, got %v", err)
	}
}

// ── Backend resume-token unit coverage ───────────────────────────────────────

func TestNewClaudeSessionID_IsUUIDv4(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	for i := 0; i < 50; i++ {
		id := newClaudeSessionID()
		if !re.MatchString(id) {
			t.Fatalf("not a v4 UUID: %q", id)
		}
	}
}

func TestSessions_ResumeTokenAccessors(t *testing.T) {
	if got := (&claudeSession{sessionID: "abc"}).ResumeToken(); got != "abc" {
		t.Errorf("claude ResumeToken = %q, want abc", got)
	}
	if got := (&cursorSession{chatID: "cid"}).ResumeToken(); got != "cid" {
		t.Errorf("cursor ResumeToken = %q, want cid", got)
	}
	if got := (&codexSession{sessionID: "sid"}).ResumeToken(); got != "sid" {
		t.Errorf("codex ResumeToken = %q, want sid", got)
	}
}

// codex learns its session id from the event stream; ResumeToken surfaces it.
func TestCodexSession_CapturesSessionID(t *testing.T) {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	_ = enc.Encode(map[string]any{"type": "session.created", "session_id": "sess-xyz"})
	_ = enc.Encode(codexOutputEvent{
		Type:    "message",
		Message: &codexMsg{Role: "assistant", Content: []codexContent{{Type: "text", Text: "hi"}}},
	})
	_ = enc.Encode(codexOutputEvent{Type: "result"})

	sess := &codexSession{scanner: bufio.NewScanner(strings.NewReader(sb.String()))}
	if _, err := sess.Next(); err != nil {
		t.Fatalf("Next: %v", err)
	}
	if sess.ResumeToken() != "sess-xyz" {
		t.Errorf("captured session id = %q, want sess-xyz", sess.ResumeToken())
	}
}
