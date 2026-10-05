package telemetry

import "syscall"

// relaySysProcAttr puts the relay in its own process group, so a Ctrl-C at
// the terminal reaches ynh, which stops the relay only after the vendor has
// exited and sent what it had, rather than the relay too early. Pdeathsig
// asks Linux to send it SIGTERM if ynh dies without stopping it, such as on
// SIGKILL, so a relay is not left running.
func relaySysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
}
