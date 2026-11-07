//go:build !linux

package vendor

import (
	"fmt"
	"os/exec"
	"syscall"
)

func withParentDeathSignal(attr *syscall.SysProcAttr) *syscall.SysProcAttr { return attr }

func lockForParentDeathSignal() func() { return func() {} }

// watchdogScript ends the child when the parent is gone, and quits when the
// child is. It is POSIX sh, with the two pids as its arguments. It polls once
// a second: macOS has no Pdeathsig.
const watchdogScript = `while kill -0 "$2" 2>/dev/null; do
  if ! kill -0 "$1" 2>/dev/null; then
    kill -TERM "$2" 2>/dev/null
    sleep 2
    kill -KILL "$2" 2>/dev/null
    exit 0
  fi
  sleep 1
done`

// startWatchdog starts a small detached shell that ends the child if ynh is
// killed without a chance to forward anything (SIGKILL, a crash), so no vendor
// CLI is left running without its parent. It runs in its own session, so the
// terminal's Ctrl-C does not reach it. The returned function stops it when the
// child has exited normally. If the shell cannot start, the child still runs,
// unwatched: that costs only the cleanup after a SIGKILL.
func startWatchdog(parent, child int) func() {
	w := exec.Command("sh", "-c", watchdogScript, "ynh-watchdog", fmt.Sprint(parent), fmt.Sprint(child))
	w.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := w.Start(); err != nil {
		return func() {}
	}
	return func() {
		_ = w.Process.Kill()
		_ = w.Wait()
	}
}
