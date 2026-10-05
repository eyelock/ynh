package telemetry

import "syscall"

// relaySysProcAttr puts the relay in its own process group, so a Ctrl-C at
// the terminal reaches ynh, which stops the relay only after the vendor has
// exited and sent what it had, rather than the relay too early.
//
// No parent-death signal: the stdin pipe already ends the relay when ynh
// dies, on every platform, and Linux's Pdeathsig fires when the thread that
// started the child exits, which in a Go program is not ynh exiting.
func relaySysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
