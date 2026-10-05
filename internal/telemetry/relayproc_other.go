//go:build !linux

package telemetry

import "syscall"

// relaySysProcAttr puts the relay in its own process group, so a Ctrl-C at
// the terminal reaches ynh, which stops the relay only after the vendor has
// exited and sent what it had, rather than the relay too early. Unlike Linux,
// macOS has no parent-death signal: a ynh killed with SIGKILL leaves its
// relay running.
func relaySysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
