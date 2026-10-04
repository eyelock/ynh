package agent

import (
	"io"
	"strings"
	"testing"
)

// A resume continues the checkpoint's backend: its resume token is a codex
// thread id or a claude or cursor session id, meaningless to any other.
func TestResumeBackend(t *testing.T) {
	tests := []struct {
		name     string
		flag     string
		recorded string
		want     string
		wantErr  string
	}{
		{name: "no flag takes the checkpoint's", recorded: "codex", want: "codex"},
		{name: "no flag takes cursor too", recorded: "cursor", want: "cursor"},
		{name: "a checkpoint without a backend is claude's", want: "claude"},
		{name: "the same backend again is fine", flag: "codex", recorded: "codex", want: "codex"},
		{name: "claude named on a claude checkpoint", flag: "claude", recorded: "claude", want: "claude"},
		{name: "claude named on a checkpoint without a backend", flag: "claude", want: "claude"},
		{name: "another backend is refused", flag: "claude", recorded: "codex", wantErr: `started on the "codex" backend`},
		{name: "codex on an old claude checkpoint is refused", flag: "codex", wantErr: `started on the "claude" backend`},
		{name: "an unknown flag is refused as before", flag: "claude-code", recorded: "claude", wantErr: `did you mean "claude"`},
		{name: "an unknown recorded backend is refused", recorded: "bogus", wantErr: `checkpoint records unknown backend "bogus"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resumeBackend(tt.flag, tt.recorded)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("backend = %q, want %q", got, tt.want)
			}
		})
	}
}

// The task and focus given on a resume must be the session's own: a resumed
// conversation carrying on with a different task is not a resume.
func TestResumeTaskConflict(t *testing.T) {
	taskRun := &Checkpoint{Task: "fix the tests"}
	focusRun := &Checkpoint{Task: "the focus prompt", Focus: "review"}
	noTask := &Checkpoint{}
	tests := []struct {
		name  string
		cp    *Checkpoint
		focus string
		task  string
		want  string
	}{
		{name: "same task", cp: taskRun, task: "fix the tests"},
		{name: "different task", cp: taskRun, task: "write docs", want: "differs from the task this session was started with"},
		{name: "focus on a task run whose prompt differs", cp: taskRun, focus: "review", task: "the focus prompt", want: "differs from the task"},
		{name: "same focus", cp: focusRun, focus: "review", task: "the focus prompt"},
		{name: "other focus", cp: focusRun, focus: "tidy", task: "the focus prompt", want: `ran focus "review"`},
		{name: "a task on a focus run", cp: focusRun, task: "the focus prompt", want: `ran focus "review"`},
		{name: "a checkpoint with no task accepts one", cp: noTask, task: "anything"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resumeTaskConflict(tt.focus, tt.task, tt.cp)
			if (tt.want == "") != (got == "") || !strings.Contains(got, tt.want) {
				t.Errorf("conflict = %q, want %q", got, tt.want)
			}
		})
	}
}

// writePlanCheckpoint hand-authors a checkpoint taken during the plan phase,
// which is what a run interrupted while planning leaves behind.
func writePlanCheckpoint(t *testing.T, dir string, cp Checkpoint) {
	t.Helper()
	cp.SessionID = "s-plan"
	cp.Phase = PhasePlan
	if err := writeCheckpoint(dir, &cp); err != nil {
		t.Fatal(err)
	}
}

// A run interrupted while planning re-runs the plan on resume, and the plan
// needs the task. It used to be saved and never read back, so the resumed plan
// was asked for with an empty task.
func TestRunLoop_ResumeDuringPlanRestoresTask(t *testing.T) {
	dir := t.TempDir()
	writePlanCheckpoint(t, dir, Checkpoint{Backend: "claude", Task: "rename the widget", ResumeToken: "tok"})

	passSensor(t)
	mb := &mockBackend{name: "mock", resumeToken: "tok", turns: []Turn{{Content: "the plan"}, {Content: "done"}}}
	opts := resumeOpts(mb, dir)
	opts.Resume = dir
	opts.Task = ""
	opts.NoPlan = false
	if _, err := RunLoop(opts); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if len(mb.sends) == 0 || !strings.Contains(mb.sends[0], "Task: rename the widget") {
		t.Fatalf("plan request = %q, want it to carry the checkpoint's task", mb.sends)
	}
	cp, err := readCheckpoint(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cp.Task != "rename the widget" {
		t.Errorf("checkpoint task after the resume = %q, want it kept", cp.Task)
	}
}

// The real interruption, not a hand-made checkpoint: cut off during the plan
// turn, then resumed with no --task.
func TestRunLoop_InterruptDuringPlanResumesWithTask(t *testing.T) {
	dir := t.TempDir()
	failSensor(t)
	pr, pw := io.Pipe()
	mb1 := &mockBackend{name: "mock", resumeToken: "tok", interruptAtCall: 1, interruptWriter: pw}
	opts1 := resumeOpts(mb1, dir)
	opts1.Task = "rename the widget"
	opts1.NoPlan = false
	opts1.Stdin = pr
	if _, err := RunLoop(opts1); err == nil {
		t.Fatal("run 1 should be interrupted during the plan")
	}
	if cp, err := readCheckpoint(dir); err != nil || cp.Phase != PhasePlan {
		t.Fatalf("checkpoint after the plan interrupt = %+v, %v; want phase plan", cp, err)
	}

	passSensor(t)
	mb2 := &mockBackend{name: "mock", resumeToken: "tok", turns: []Turn{{Content: "the plan"}, {Content: "done"}}}
	opts2 := resumeOpts(mb2, dir)
	opts2.Resume = dir
	opts2.Task = ""
	opts2.NoPlan = false
	if _, err := RunLoop(opts2); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if len(mb2.sends) == 0 || !strings.Contains(mb2.sends[0], "Task: rename the widget") {
		t.Fatalf("resumed plan request = %q, want the original task", mb2.sends)
	}
}

// Passing a different --task on a resume is refused before any worker starts.
// Passing the same one, as the docs used to tell operators to, still works.
func TestRunLoop_ResumeTaskFlag(t *testing.T) {
	tests := []struct {
		name    string
		phase   CheckpointPhase
		task    string
		wantErr bool
	}{
		{name: "plan phase, same task", phase: PhasePlan, task: "rename the widget"},
		{name: "plan phase, different task", phase: PhasePlan, task: "delete the widget", wantErr: true},
		{name: "act phase, same task", phase: PhaseAct, task: "rename the widget"},
		{name: "act phase, different task", phase: PhaseAct, task: "delete the widget", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cp := &Checkpoint{
				SessionID: "s", Backend: "claude", Task: "rename the widget", Phase: tt.phase,
				PlanFinalized: tt.phase == PhaseAct, PendingMessage: "continue", ResumeToken: "tok",
			}
			if err := writeCheckpoint(dir, cp); err != nil {
				t.Fatal(err)
			}
			passSensor(t)
			mb := &mockBackend{name: "mock", resumeToken: "tok", turns: []Turn{{Content: "the plan"}, {Content: "done"}}}
			opts := resumeOpts(mb, dir)
			opts.Resume = dir
			opts.Task = tt.task
			opts.NoPlan = false
			_, err := RunLoop(opts)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("resume: %v", err)
				}
				return
			}
			var ee *ExitError
			if !asExitError(err, &ee) || ee.Code != ExitResumeError || !strings.Contains(ee.Message, "differs from the task") {
				t.Fatalf("err = %v, want a resume error naming the task conflict", err)
			}
			if len(mb.startOpts) != 0 {
				t.Errorf("worker started %d times for a refused resume", len(mb.startOpts))
			}
		})
	}
}

// A run interrupted after planning resumes from its pending message, exactly
// as before: the task is not re-sent.
func TestRunLoop_ResumeAfterPlanSendsPendingMessage(t *testing.T) {
	dir := t.TempDir()
	if err := writeCheckpoint(dir, &Checkpoint{
		SessionID: "s", Backend: "claude", Task: "rename the widget", Phase: PhaseAct,
		PlanFinalized: true, LastCompletedTurn: 1, PendingMessage: "sensor feedback", ResumeToken: "tok",
	}); err != nil {
		t.Fatal(err)
	}
	passSensor(t)
	mb := &mockBackend{name: "mock", resumeToken: "tok", turns: []Turn{{Content: "done"}}}
	opts := resumeOpts(mb, dir)
	opts.Resume = dir
	opts.Task = ""
	if _, err := RunLoop(opts); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if len(mb.sends) != 1 || mb.sends[0] != "sensor feedback" {
		t.Errorf("sends = %q, want only the pending message", mb.sends)
	}
}

// A checkpoint that records no task cannot re-run its plan: say so rather than
// asking the worker to plan nothing.
func TestRunLoop_ResumeDuringPlanWithoutTaskFails(t *testing.T) {
	dir := t.TempDir()
	writePlanCheckpoint(t, dir, Checkpoint{Backend: "claude"})
	mb := &mockBackend{name: "mock", turns: []Turn{{Content: "the plan"}}}
	opts := resumeOpts(mb, dir)
	opts.Resume = dir
	opts.Task = ""
	opts.NoPlan = false
	_, err := RunLoop(opts)
	var ee *ExitError
	if !asExitError(err, &ee) || ee.Code != ExitResumeError || !strings.Contains(ee.Message, "--task") {
		t.Fatalf("err = %v, want a resume error asking for --task", err)
	}
	if len(mb.sends) != 0 {
		t.Errorf("worker was asked %q", mb.sends)
	}
}

// The resumed run drives the checkpoint's backend. The sandbox check proves
// which one was resolved: srt is claude-only, so on a codex checkpoint it is
// refused as codex's, where a resume used to fall back to claude and accept it.
func TestRunLoop_ResumeUsesCheckpointBackend(t *testing.T) {
	dir := t.TempDir()
	if err := writeCheckpoint(dir, &Checkpoint{
		SessionID: "s", Backend: "codex", Task: "t", Phase: PhaseAct,
		PlanFinalized: true, PendingMessage: "continue", ResumeToken: "thread-1",
	}); err != nil {
		t.Fatal(err)
	}
	mb := &mockBackend{name: "mock"}
	opts := resumeOpts(mb, dir)
	opts.Resume = dir
	opts.Sandbox = "srt"
	_, err := RunLoop(opts)
	if err == nil || !strings.Contains(err.Error(), "not supported by the codex backend") {
		t.Fatalf("err = %v, want srt refused as the codex backend's", err)
	}
}

// Naming another backend on a resume is refused: the token would be handed to
// a backend that cannot read it.
func TestRunLoop_ResumeRefusesOtherBackend(t *testing.T) {
	dir := t.TempDir()
	if err := writeCheckpoint(dir, &Checkpoint{
		SessionID: "s", Backend: "codex", Task: "t", Phase: PhaseAct,
		PlanFinalized: true, PendingMessage: "continue", ResumeToken: "thread-1",
	}); err != nil {
		t.Fatal(err)
	}
	mb := &mockBackend{name: "mock"}
	opts := resumeOpts(mb, dir)
	opts.Resume = dir
	opts.Backend = "claude"
	_, err := RunLoop(opts)
	var ee *ExitError
	if !asExitError(err, &ee) || ee.Code != ExitResumeError || !strings.Contains(ee.Message, `"codex"`) {
		t.Fatalf("err = %v, want a resume error naming the checkpoint's backend", err)
	}
	if len(mb.startOpts) != 0 {
		t.Errorf("worker started %d times for a refused resume", len(mb.startOpts))
	}
}

// The checkpoint records the backend by its canonical name, the one a resume
// selects, not whatever the session object calls itself.
func TestRunLoop_CheckpointRecordsBackend(t *testing.T) {
	dir := t.TempDir()
	failSensor(t)
	mb := &mockBackend{name: "mock", turns: []Turn{{Content: "r1"}}, resumeToken: "tok"}
	opts := resumeOpts(mb, dir)
	opts.Backend = "codex"
	opts.MaxTurns = 1
	_, _ = RunLoop(opts)
	cp, err := readCheckpoint(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cp.Backend != "codex" {
		t.Errorf("checkpoint backend = %q, want codex", cp.Backend)
	}
}
