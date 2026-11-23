//go:build windows

package presence

import "syscall"

const processQueryLimitedInformation = 0x1000

// ProcessStart returns the process's creation time (a FILETIME, 100ns ticks),
// or an error when no such process exists.
func ProcessStart(pid int) (int64, error) {
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return 0, err
	}
	defer func() { _ = syscall.CloseHandle(h) }()
	var creation, exit, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return 0, err
	}
	// A process that has exited keeps a handle valid until closed: it is dead.
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return 0, err
	}
	if code != 259 { // STILL_ACTIVE
		return 0, syscall.ERROR_NOT_FOUND
	}
	return creation.Nanoseconds() / 100, nil
}
