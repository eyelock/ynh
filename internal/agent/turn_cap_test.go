package agent

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/eyelock/ynh/internal/gate"
)

// redSensors makes every gate run fail, so each turn ends in feedback.
func redSensors(t *testing.T) {
	t.Helper()
	stubCheck(t, func(_ int, name string) gate.Result {
		return gate.Result{Name: name, Kind: "command", Tolerance: "blocking", Status: gate.StatusFail, ExitCode: 1}
	})
}

// A message sent to the worker is a turn taken: it acts on it as soon as it
// reads it, whatever the loop does next (#567). A run that ends at a cap must
// therefore never have sent the turn beyond it, and must not claim in the
// trajectory to have asked for one.
func TestRunLoop_NeverSendsATurnBeyondTheCap(t *testing.T) {
	big := Usage{InputTokens: 400, OutputTokens: 400}
	tests := []struct {
		name      string
		configure func(*RunOptions)
		turns     []Turn
		delays    []time.Duration
		wantCode  int
		wantSends int
		wantNexts int
	}{
		{
			name:      "turn cap",
			configure: func(o *RunOptions) { o.MaxTurns = 1 },
			turns:     []Turn{{Content: "one"}, {Content: "two"}},
			wantCode:  ExitIterationCap, wantSends: 1, wantNexts: 1,
		},
		{
			name:      "turn cap after several turns",
			configure: func(o *RunOptions) { o.MaxTurns = 3 },
			turns:     []Turn{{Content: "one"}, {Content: "two"}, {Content: "three"}, {Content: "four"}},
			wantCode:  ExitIterationCap, wantSends: 3, wantNexts: 3,
		},
		{
			name:      "token cap",
			configure: func(o *RunOptions) { o.MaxTokens = 500 },
			turns:     []Turn{{Content: "one", Usage: big}, {Content: "two", Usage: big}},
			wantCode:  ExitTokenBudget, wantSends: 1, wantNexts: 1,
		},
		{
			name:      "wall-clock cap",
			configure: func(o *RunOptions) { o.MaxWall = 300 * time.Millisecond },
			turns:     []Turn{{Content: "one"}, {Content: "two"}},
			delays:    []time.Duration{500 * time.Millisecond},
			wantCode:  ExitWallClock, wantSends: 1, wantNexts: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redSensors(t)
			mb := &mockBackend{name: "mock", turns: tt.turns, delays: tt.delays}
			var traj bytes.Buffer
			opts := baseOpts(mb, &traj, &bytes.Buffer{}, strings.NewReader(""))
			opts.EmitJSONL = "-"
			opts.testSensorNames = []string{"build"}
			tt.configure(&opts)

			_, err := RunLoop(opts)
			var exitErr *ExitError
			if !asExitError(err, &exitErr) || exitErr.Code != tt.wantCode {
				t.Fatalf("want exit %d, got %v", tt.wantCode, err)
			}
			// The first message is the task; each further one is a turn.
			if len(mb.sends) != tt.wantSends {
				t.Errorf("worker was sent %d messages, want %d: %q", len(mb.sends), tt.wantSends, mb.sends)
			}
			if mb.pos != tt.wantNexts {
				t.Errorf("worker turns read = %d, want %d", mb.pos, tt.wantNexts)
			}
			events := parseTrajectory(t, &traj)
			if got := kindCount(events, KindFeedbackSent); got != tt.wantSends-1 {
				t.Errorf("feedback_sent events = %d, want %d (one per turn actually taken)", got, tt.wantSends-1)
			}
			if kindCount(events, KindBudgetExceeded) != 1 {
				t.Errorf("want one budget_exceeded event")
			}
			last := events[len(events)-1]
			if last.Kind != KindSessionEnd {
				t.Errorf("trajectory ends with %q, want session_end", last.Kind)
			}
		})
	}
}

// Stuck detection already ended the run before feedback; this keeps it so.
func TestRunLoop_StuckRunSendsNoFurtherTurn(t *testing.T) {
	redSensors(t)
	var turns []Turn
	for range 10 {
		turns = append(turns, Turn{Content: "same again"})
	}
	mb := &mockBackend{name: "mock", turns: turns}
	opts := baseOpts(mb, &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
	opts.MaxTurns = 10
	opts.testSensorNames = []string{"build"}

	_, err := RunLoop(opts)
	var exitErr *ExitError
	if !asExitError(err, &exitErr) || exitErr.Code != ExitStuck {
		t.Fatalf("want ExitStuck, got %v", err)
	}
	// The task, then feedback for every turn but the last, which was stuck.
	if want := mb.pos; len(mb.sends) != want {
		t.Errorf("sends = %d for %d turns read; the stuck turn must not be answered", len(mb.sends), want)
	}
}

// The feedback a capped run did not send is kept, so --resume with room left
// sends it instead of re-deriving it.
func TestRunLoop_CapKeepsUnsentFeedbackInCheckpoint(t *testing.T) {
	redSensors(t)
	mb := &mockBackend{name: "mock", turns: []Turn{{Content: "one"}, {Content: "two"}}}
	dir := t.TempDir()
	opts := baseOpts(mb, &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
	opts.EmitJSONL = filepath.Join(dir, "traj.jsonl")
	opts.MaxTurns = 1
	opts.testSensorNames = []string{"build"}

	if _, err := RunLoop(opts); err == nil {
		t.Fatal("want the cap to end the run")
	}
	cp, err := readCheckpoint(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cp.LastCompletedTurn != 1 || !strings.Contains(cp.PendingMessage, "build") {
		t.Errorf("checkpoint = turn %d pending %q, want turn 1 and the unsent feedback", cp.LastCompletedTurn, cp.PendingMessage)
	}
	if len(mb.sends) != 1 {
		t.Errorf("sends = %d, want 1", len(mb.sends))
	}
}

// startShell starts a stand-in for claude: a shell running script, wired the
// way Start wires claude.
func startShell(t *testing.T, script string) *claudeSession {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX-only")
	}
	cmd := exec.CommandContext(context.Background(), "sh", "-c", script)
	confineWorker(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return &claudeSession{cmd: cmd, stdin: stdin}
}

func shortGraces(t *testing.T, exit, term time.Duration) {
	t.Helper()
	oe, ot := workerExitGrace, workerTermGrace
	workerExitGrace, workerTermGrace = exit, term
	t.Cleanup(func() { workerExitGrace, workerTermGrace = oe, ot })
}

func closeWithin(t *testing.T, s *claudeSession, limit time.Duration) time.Duration {
	t.Helper()
	start := time.Now()
	done := make(chan struct{})
	go func() { _ = s.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(limit):
		t.Fatalf("Close did not return within %s", limit)
	}
	return time.Since(start)
}

// A worker still processing a message is terminated, not waited for: claude
// would otherwise finish the turn before it read the EOF.
func TestClaudeSession_CloseTerminatesWorkerWithATurnOpen(t *testing.T) {
	shortGraces(t, time.Hour, 2*time.Second)
	// Ignores stdin closing, as claude does while it works.
	s := startShell(t, "sleep 30")
	s.turnOpen = true
	if d := closeWithin(t, s, 5*time.Second); d > 3*time.Second {
		t.Errorf("Close took %s, want it to terminate the worker at once", d)
	}
}

// With nothing in flight the worker is let go gracefully: it exits on EOF.
func TestClaudeSession_CloseIsGracefulWhenIdle(t *testing.T) {
	shortGraces(t, 5*time.Second, time.Second)
	s := startShell(t, "cat >/dev/null; exit 0")
	if err := s.Close(); err != nil {
		t.Errorf("an idle worker exiting on EOF should close cleanly, got %v", err)
	}
}

// An idle worker that will not leave on EOF is terminated after the grace,
// and one that ignores SIGTERM is killed after that.
func TestClaudeSession_CloseEscalates(t *testing.T) {
	tests := []struct {
		name   string
		script string
	}{
		{"ignores EOF", "sleep 30"},
		{"ignores EOF and SIGTERM", `trap '' TERM; while :; do :; done`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shortGraces(t, 100*time.Millisecond, 200*time.Millisecond)
			s := startShell(t, tt.script)
			d := closeWithin(t, s, 5*time.Second)
			if d < 100*time.Millisecond {
				t.Errorf("Close returned after %s, before the exit grace", d)
			}
		})
	}
}

// Closing twice, or after Next reaped the process, is harmless.
func TestClaudeSession_CloseAfterReap(t *testing.T) {
	s := startShell(t, "exit 0")
	_ = s.wait()
	if err := s.Close(); err != nil {
		t.Errorf("Close after the process was reaped: %v", err)
	}
}
