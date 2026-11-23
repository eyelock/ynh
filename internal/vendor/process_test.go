package vendor

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The supervisor tests run runChildProcess in a helper process (this test
// binary, re-executed), so that signals can be sent to ynh's stand-in and its
// exit code observed from outside.
const (
	supervisorEnv       = "YNH_TEST_SUPERVISOR_SCRIPT"
	supervisorHelperRun = "TestSupervisorHelperProcess"
)

func TestSupervisorHelperProcess(t *testing.T) {
	script := os.Getenv(supervisorEnv)
	if script == "" {
		t.Skip("helper process only")
	}
	err := runChildProcess(exec.Command("sh", "-c", script))
	var ee *ExitError
	if errors.As(err, &ee) {
		os.Exit(ee.Code)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(99)
	}
	os.Exit(0)
}

// startSupervisor starts the helper (the stand-in for ynh) running script as
// its vendor child.
func startSupervisor(t *testing.T, script string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^"+supervisorHelperRun+"$")
	cmd.Env = append(os.Environ(), supervisorEnv+"="+script)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd
}

// waitExit waits for cmd to end and returns its exit code, failing after d.
func waitExit(t *testing.T, cmd *exec.Cmd, d time.Duration) int {
	t.Helper()
	ch := make(chan error, 1)
	go func() { ch <- cmd.Wait() }()
	select {
	case err := <-ch:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		if err != nil {
			t.Fatal(err)
		}
		return 0
	case <-time.After(d):
		t.Fatalf("process %d did not exit within %s", cmd.Process.Pid, d)
		return -1
	}
}

// waitPid waits for the child to write its pid to file and returns it.
func waitPid(t *testing.T, file string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(file)
		if err == nil {
			if pid, perr := strconv.Atoi(strings.TrimSpace(string(b))); perr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("child never wrote %s", file)
	return 0
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func waitGone(t *testing.T, pid int, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("process %d is still running after %s", pid, d)
}

func TestRunChildProcessPassesExitCodeThrough(t *testing.T) {
	for _, code := range []int{0, 1, 2, 42, 255} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			cmd := startSupervisor(t, fmt.Sprintf("exit %d", code))
			if got := waitExit(t, cmd, 10*time.Second); got != code {
				t.Fatalf("exit code = %d, want %d", got, code)
			}
		})
	}
}

func TestRunChildProcessReportsSignalDeathAsShellDoes(t *testing.T) {
	cmd := startSupervisor(t, "kill -KILL $$")
	if got := waitExit(t, cmd, 10*time.Second); got != 128+int(syscall.SIGKILL) {
		t.Fatalf("exit code = %d, want %d", got, 128+int(syscall.SIGKILL))
	}
}

func TestChildResult(t *testing.T) {
	if err := childResult(nil); err != nil {
		t.Fatalf("nil in, got %v", err)
	}
	boom := errors.New("fork failed")
	if err := childResult(boom); !errors.Is(err, boom) {
		t.Fatalf("a start failure must pass through, got %v", err)
	}
	err := exec.Command("sh", "-c", "exit 255").Run()
	var ee *ExitError
	if got := childResult(err); !errors.As(got, &ee) || ee.Code != 255 {
		t.Fatalf("childResult(exit 255) = %v, want ExitError 255", got)
	}
}

// Ctrl-C and Ctrl-\ reach the vendor from the terminal. ynh must neither die
// from them (Go's default for SIGQUIT dumps every goroutine and exits 2) nor
// forward a second copy, and must not leave the vendor deaf to them.
func TestRunChildProcessSurvivesAndDoesNotForwardTerminalSignals(t *testing.T) {
	for _, tc := range []struct {
		sig  syscall.Signal
		trap string
	}{{syscall.SIGINT, "INT"}, {syscall.SIGQUIT, "QUIT"}} {
		t.Run(tc.sig.String(), func(t *testing.T) {
			dir := t.TempDir()
			pidFile, got := filepath.Join(dir, "pid"), filepath.Join(dir, "got")
			script := fmt.Sprintf(`trap 'echo x >> %s; exit 7' %s
echo $$ > %s
while :; do sleep 0.05; done`, got, tc.trap, pidFile)
			ynh := startSupervisor(t, script)
			child := waitPid(t, pidFile)

			// The signal to ynh alone: it must live, and the child must not hear of it.
			if err := ynh.Process.Signal(tc.sig); err != nil {
				t.Fatal(err)
			}
			time.Sleep(500 * time.Millisecond)
			if !alive(ynh.Process.Pid) {
				t.Fatalf("ynh died from %s: the session would lose its parent", tc.sig)
			}
			if _, err := os.Stat(got); err == nil {
				t.Fatalf("ynh forwarded %s to the vendor: it would receive it twice", tc.sig)
			}

			// The terminal's copy, which only the vendor gets: its handler must
			// run, proving ynh did not hand it an ignored disposition.
			if err := syscall.Kill(child, tc.sig); err != nil {
				t.Fatal(err)
			}
			if code := waitExit(t, ynh, 10*time.Second); code != 7 {
				t.Fatalf("ynh exit code = %d, want the vendor's 7", code)
			}
			if b, _ := os.ReadFile(got); strings.Count(string(b), "x") != 1 {
				t.Fatalf("vendor handled %s %d times, want once", tc.sig, strings.Count(string(b), "x"))
			}
		})
	}
}

// The hook registered with OnChildStart runs before the child does, and not
// after it has been started.
func TestRunChildProcessFiresOnChildStartBeforeTheChild(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	remove := OnChildStart(func() { _ = os.WriteFile(marker, []byte("x"), 0o644) })
	defer remove()
	script := fmt.Sprintf(`test -f %s`, marker)
	if err := runChildProcess(exec.Command("sh", "-c", script)); err != nil {
		t.Fatalf("the hook had not run when the child started: %v", err)
	}
}

// A SIGTERM or SIGHUP sent to ynh alone (a kill, a closed terminal) is for the
// session, so it is forwarded.
func TestRunChildProcessForwardsSIGTERMAndSIGHUP(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			pidFile := filepath.Join(t.TempDir(), "pid")
			script := fmt.Sprintf(`trap 'exit 9' TERM HUP
echo $$ > %s
while :; do sleep 0.05; done`, pidFile)
			ynh := startSupervisor(t, script)
			waitPid(t, pidFile)
			if err := ynh.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			if code := waitExit(t, ynh, 10*time.Second); code != 9 {
				t.Fatalf("ynh exit code = %d, want the vendor's 9", code)
			}
		})
	}
}

// If ynh dies without a chance to forward anything, no vendor CLI is left
// running: the kernel (Linux) or the watchdog (elsewhere) ends it.
func TestRunChildProcessLeavesNoOrphanWhenYnhIsKilled(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	script := fmt.Sprintf(`echo $$ > %s
while :; do sleep 0.05; done`, pidFile)
	ynh := startSupervisor(t, script)
	child := waitPid(t, pidFile)

	if err := ynh.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = ynh.Wait()
	waitGone(t, child, 10*time.Second)
}

// fakeVendor puts an executable named name on PATH that records its parent pid,
// working directory, argv and the variable FAKE_SEEN, then exits with
// $FAKE_EXIT.
func fakeVendor(t *testing.T, name string) (record string) {
	t.Helper()
	dir := t.TempDir()
	record = filepath.Join(dir, "record")
	script := "#!/bin/sh\n" +
		"{ echo \"ppid=$PPID\"; echo \"argv0=$0\"; echo \"args=$*\"; echo \"seen=$FAKE_SEEN\"; } > '" + record + "'\n" +
		"exit \"${FAKE_EXIT:-0}\"\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_SEEN", "yes")
	return record
}

// Every vendor is a child of ynh and its exit code comes back exactly. Before
// #745 Claude, Copilot and OpenCode were exec'd and ynh was gone.
func TestEveryVendorRunsAsChildOfYnh(t *testing.T) {
	launches := map[string]struct {
		bin    string
		launch func(cfg string) error
	}{
		"claude":  {claudeCLI, func(cfg string) error { return launchClaude(cfg, "", nil) }},
		"copilot": {copilotCLI, func(cfg string) error { return launchCopilot(cfg, "", nil) }},
		"codex":   {codexCLI, func(cfg string) error { return launchCodex(cfg, nil) }},
		"cursor":  {cursorCLI, func(cfg string) error { return launchCursor(cfg, nil) }},
		"opencode": {opencodeCLI, func(cfg string) error {
			old := opencodeVersionProbe
			t.Cleanup(func() { opencodeVersionProbe = old })
			opencodeVersionProbe = func(string) (string, error) { return "v2.2.3", nil }
			return launchOpencodeArgs(cfg, []string{"--standalone"})
		}},
	}
	for name, l := range launches {
		for _, code := range []int{0, 1, 255} {
			t.Run(fmt.Sprintf("%s/exit %d", name, code), func(t *testing.T) {
				record := fakeVendor(t, l.bin)
				t.Setenv("FAKE_EXIT", strconv.Itoa(code))
				cfg := t.TempDir()
				if err := os.MkdirAll(filepath.Join(cfg, ".opencode"), 0o755); err != nil {
					t.Fatal(err)
				}

				err := l.launch(cfg)

				if code == 0 {
					if err != nil {
						t.Fatalf("exit 0 returned %v", err)
					}
				} else {
					var ee *ExitError
					if !errors.As(err, &ee) || ee.Code != code {
						t.Fatalf("exit %d returned %v, want ExitError{%d}", code, err, code)
					}
				}
				b, rerr := os.ReadFile(record)
				if rerr != nil {
					t.Fatalf("the vendor never ran: %v", rerr)
				}
				if want := "ppid=" + strconv.Itoa(os.Getpid()); !strings.Contains(string(b), want+"\n") {
					t.Errorf("the vendor's parent is not ynh (want %s):\n%s", want, b)
				}
				if !strings.Contains(string(b), "seen=yes") {
					t.Errorf("the vendor did not inherit the environment:\n%s", b)
				}
			})
		}
	}
}
