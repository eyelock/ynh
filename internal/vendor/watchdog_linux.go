//go:build linux

package vendor

import (
	"runtime"
	"syscall"
)

// withParentDeathSignal asks the kernel to SIGKILL the child when ynh dies, so
// even a SIGKILL of ynh leaves no vendor CLI behind.
func withParentDeathSignal(attr *syscall.SysProcAttr) *syscall.SysProcAttr {
	if attr == nil {
		attr = &syscall.SysProcAttr{}
	}
	attr.Pdeathsig = syscall.SIGKILL
	return attr
}

// lockForParentDeathSignal pins the goroutine to its thread while the child
// starts: Pdeathsig fires when the thread that forked the child exits, not the
// process. The returned function unpins it.
func lockForParentDeathSignal() func() {
	runtime.LockOSThread()
	return runtime.UnlockOSThread
}

// startWatchdog is not needed on Linux: Pdeathsig does the job.
func startWatchdog(parent, child int) func() { return func() {} }
