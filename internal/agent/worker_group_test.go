package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A worker is often a wrapper (srt around claude), so stopping it means
// stopping the tree. These tests start only processes they write themselves
// under t.TempDir(), and signal only the pids those processes report.

// treeScript is a fake worker that starts a child and waits for it. The child
// is /bin/sleep, since the test PATH holds nothing else. With ignoreTerm both
// ignore SIGTERM, so only SIGKILL stops them.
func treeScript(dir string, ignoreTerm bool) (script, pidFile string) {
	pidFile = filepath.Join(dir, "child.pid")
	trap := ""
	if ignoreTerm {
		trap = "trap '' TERM\n"
	}
	return trap + "/bin/sleep 30 &\necho $! > " + pidFile + "\nwait\n", pidFile
}

func waitPid(t *testing.T, file string) int {
	t.Helper()
	for range 100 {
		if data, err := os.ReadFile(file); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 1 {
				return pid
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no pid in %s", file)
	return 0
}

func requireGone(t *testing.T, name string, pid int) {
	t.Helper()
	for range 100 {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Leave nothing running behind a failed test: it is a process this test started.
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Errorf("%s (pid %d) is still running", name, pid)
}

func TestClaudeSession_CloseStopsTheWholeProcessTree(t *testing.T) {
	for _, ignoreTerm := range []bool{false, true} {
		name := "terminates"
		if ignoreTerm {
			name = "kills what ignores SIGTERM"
		}
		t.Run(name, func(t *testing.T) {
			shortGraces(t, time.Hour, 300*time.Millisecond)
			script, pidFile := treeScript(t.TempDir(), ignoreTerm)
			s := startShell(t, script)
			leader := s.cmd.Process.Pid
			child := waitPid(t, pidFile)
			s.turnOpen = true
			closeWithin(t, s, 5*time.Second)
			requireGone(t, "wrapper", leader)
			requireGone(t, "child", child)
		})
	}
}

// A worker that exits on its own still must not leave its children behind.
func TestClaudeSession_CloseKillsChildrenOfAnExitedWorker(t *testing.T) {
	shortGraces(t, 5*time.Second, time.Second)
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	s := startShell(t, "/bin/sleep 30 &\necho $! > "+pidFile+"\ncat >/dev/null\n")
	child := waitPid(t, pidFile)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	requireGone(t, "child", child)
}

// An interrupt stops the turn in flight of the per-turn backends, tree and
// all, and the turn returns promptly.
func TestPerTurnBackends_InterruptStopsTheTurnTree(t *testing.T) {
	for _, backend := range []string{"cursor", "codex"} {
		for _, ignoreTerm := range []bool{false, true} {
			name := backend + "/terminates"
			if ignoreTerm {
				name = backend + "/kills what ignores SIGTERM"
			}
			t.Run(name, func(t *testing.T) {
				shortGraces(t, time.Hour, 300*time.Millisecond)
				dir := t.TempDir()
				script, childFile := treeScript(dir, ignoreTerm)
				leaderFile := filepath.Join(dir, "leader.pid")
				cli := map[string]string{"cursor": "agent", "codex": "codex"}[backend]
				onlyStubOnPath(t, cli, "echo $$ > "+leaderFile+"\n"+script)

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				var wb WorkerBackend = &CursorBackend{}
				if backend == "codex" {
					wb = &CodexBackend{}
				}
				sess, err := wb.Start(ctx, StartOptions{WorktreeDir: dir})
				if err != nil {
					t.Fatal(err)
				}
				if err := sess.Send("work"); err != nil {
					t.Fatal(err)
				}
				done := make(chan struct{})
				go func() { _, _ = sess.Next(); close(done) }()
				leader, child := waitPid(t, leaderFile), waitPid(t, childFile)

				start := time.Now()
				cancel()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					_ = syscall.Kill(-leader, syscall.SIGKILL)
					t.Fatal("the turn in flight was not stopped by the interrupt")
				}
				if d := time.Since(start); d > 3*time.Second {
					t.Errorf("interrupt took %s to stop the turn", d)
				}
				requireGone(t, "worker", leader)
				requireGone(t, "child", child)
			})
		}
	}
}

// A worktree that does not exist is named, not the program (the tutorial
// shows this message).
func TestStartWorker_NamesAMissingWorkingDirectory(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "sh", "-c", "exit 0")
	confineWorker(cmd)
	cmd.Dir = filepath.Join(t.TempDir(), "missing")
	err := startWorker(cmd)
	if err == nil || !strings.Contains(err.Error(), "chdir "+cmd.Dir+": no such file or directory") {
		t.Errorf("err = %v, want it to name the missing directory", err)
	}
}
