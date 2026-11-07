package vendor

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
)

var (
	childStartMu   sync.Mutex
	childStartHook func()
)

// OnChildStart registers fn to run in the launch of every vendor child, just
// before the child starts and after the launch has finished reading ynh's own
// files. `ynh run` uses it to drop the run directory's assembly lock at that
// point, so a session that lasts hours does not hold up the next run of the
// same harness. fn must be safe to call more than once. The returned function
// removes the registration.
//
// There is one hook slot, so this assumes a single launch per process, as
// `ynh run` has: a second registration replaces the first.
func OnChildStart(fn func()) (remove func()) {
	childStartMu.Lock()
	childStartHook = fn
	childStartMu.Unlock()
	return func() {
		childStartMu.Lock()
		childStartHook = nil
		childStartMu.Unlock()
	}
}

func fireChildStart() {
	childStartMu.Lock()
	fn := childStartHook
	childStartMu.Unlock()
	if fn != nil {
		fn()
	}
}

// ExitError reports that the vendor CLI ended with a non-zero exit code. `ynh
// run` stays the vendor's parent for the whole session (no vendor is exec'd
// into), so it must end with exactly the code the vendor ended with. The
// caller maps this error to os.Exit; returning it, rather than exiting here,
// lets deferred cleanup run first.
type ExitError struct {
	Code int
	// Signal is the signal that ended the child, or 0 when it exited by
	// itself. Code is then 128+Signal, as a shell reports it.
	Signal syscall.Signal
}

func (e *ExitError) Error() string { return fmt.Sprintf("vendor CLI exited with code %d", e.Code) }

// execVendor runs bin with argv and env as a child of this process. argv[0] is
// the name the vendor sees for itself, as it was for syscall.Exec.
func execVendor(bin string, argv []string, env []string) error {
	cmd := exec.Command(bin, argv[1:]...)
	cmd.Args = argv
	cmd.Env = env
	return runChildProcess(cmd)
}

// runChildProcess runs cmd as a child of this process and waits for it. Every
// vendor CLI is launched this way: ynh stays alive as the parent for the whole
// session, with or without telemetry.
//
// The child inherits stdin, stdout and stderr and stays in ynh's process group,
// so it keeps the terminal and receives the terminal's signals itself:
//
//   - SIGINT and SIGQUIT are not forwarded. Ctrl-C and Ctrl-\ reach the child
//     from the terminal; a forwarded copy would deliver them twice. ynh must
//     also not die from them, or the session would lose its parent (Go's
//     default for SIGQUIT is to dump every goroutine's stack and exit 2). The
//     signals are received and dropped rather than set to ignored, because an
//     ignored disposition is inherited across exec and would make the vendor
//     deaf to them.
//   - SIGTERM and SIGHUP are forwarded: they are sent to ynh alone (a kill),
//     and the child would not otherwise hear of them. A closed terminal's
//     SIGHUP reaches both from the terminal and the child hears it twice; a
//     hangup is a request to end the session, so the repeat is harmless.
//   - If ynh is killed outright (SIGKILL), nothing can be forwarded; the child
//     is then ended by the kernel (Linux) or by a watchdog (see watchdog_*.go),
//     so no vendor CLI is left running without its parent.
//
// It returns nil when the child exits 0, an *ExitError carrying the child's
// exit code otherwise (128+n when a signal ended it, as a shell reports), and
// any other error when the child could not be started.
func runChildProcess(cmd *exec.Cmd) error {
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = withParentDeathSignal(cmd.SysProcAttr)

	// Subscribe before the child starts so no signal is missed in between.
	intCh := make(chan os.Signal, 1)
	signal.Notify(intCh, syscall.SIGINT, syscall.SIGQUIT)
	fwdCh := make(chan os.Signal, 4)
	signal.Notify(fwdCh, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(intCh)
	defer signal.Stop(fwdCh)

	// Held until the child is gone: the kernel's parent-death signal follows the
	// thread that forked it.
	defer lockForParentDeathSignal()()
	fireChildStart()
	if err := cmd.Start(); err != nil {
		return err
	}
	stopWatchdog := startWatchdog(os.Getpid(), cmd.Process.Pid)
	defer stopWatchdog()

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-intCh:
				// Dropped on purpose: the child gets it from the terminal.
			case sig := <-fwdCh:
				_ = cmd.Process.Signal(sig)
			case <-done:
				return
			}
		}
	}()

	err := cmd.Wait()
	close(done)
	return childResult(err)
}

// childResult turns cmd.Wait's error into runChildProcess's result.
func childResult(err error) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return err
	}
	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return &ExitError{Code: 128 + int(ws.Signal()), Signal: ws.Signal()}
	}
	return &ExitError{Code: exitErr.ExitCode()}
}
