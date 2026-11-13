//go:build unix

package delegation

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

// procStartFunc reads when process pid started; a variable so tests can stand
// in for a platform where it cannot be read.
var procStartFunc = readProcStart

// procStart is a token for when process pid started, or "" when it cannot be
// read: the process is gone, or this platform will not say. A pid alone cannot
// say whether it is still the process a record was written for, since ids are
// reused; the pid with its start time can. "" means "cannot tell", never "gone":
// use procState to learn whether the process exists.
func procStart(pid int) string {
	if pid <= 1 {
		return ""
	}
	return procStartFunc(pid)
}

// readProcStart reads the start time from /proc/<pid>/stat where there is a
// /proc (Linux, including BusyBox images with no procps), and falls back to
// `ps -o lstart=` (macOS and other BSDs).
func readProcStart(pid int) string {
	if data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil {
		return parseStatStart(string(data))
	}
	cmd := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid))
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.Join(strings.Fields(string(out)), " ")
}

// parseStatStart returns the start-time field (22nd, in clock ticks since boot)
// of a /proc/<pid>/stat line as a token, or "" if the line is not that shape.
// The command name, field 2, sits in parentheses and may hold spaces and
// parentheses, so the fields are counted from the last ")".
func parseStatStart(line string) string {
	i := strings.LastIndexByte(line, ')')
	if i < 0 {
		return ""
	}
	f := strings.Fields(line[i+1:])
	// f[0] is field 3 (state), so field 22 is f[19].
	if len(f) < 20 {
		return ""
	}
	return "proc:" + f[19]
}

type procStatus int

const (
	procGone    procStatus = iota // no such process
	procSame                      // the process the record names
	procOther                     // a different process now holds the pid
	procUnknown                   // it exists, but whether it is the same cannot be told
)

// procState says what pid is now, given the start token recorded for it. A
// start time that cannot be read, now or when the record was written, is
// procUnknown: not a reason to call the process dead.
func procState(pid int, recorded string) procStatus {
	if pid <= 1 {
		return procGone
	}
	if err := syscall.Kill(pid, 0); err != nil && !errors.Is(err, syscall.EPERM) {
		return procGone
	}
	cur := procStart(pid)
	if recorded == "" || cur == "" {
		return procUnknown
	}
	if cur == recorded {
		return procSame
	}
	return procOther
}

// Reconcile settles a record whose session is gone. A delegation not final
// whose supervisor process is no longer the one recorded has no one left to
// write its end: it is marked failed, with an error that says so. If the
// delegate's run is still verifiably the recorded process, its group is
// stopped too, since nothing will record what it does. A record still held by
// a live supervisor, and a final one, are returned as they are.
func Reconcile(path string) (*Record, error) {
	rec, err := Load(path)
	if err != nil {
		return nil, err
	}
	if rec.Status.Final() || supervisorAlive(rec) {
		return rec, nil
	}
	var pgid int
	var runLive bool
	rec, err = Update(path, func(r *Record) {
		if r.Status.Final() || supervisorAlive(r) {
			return
		}
		pgid, runLive = r.Pgid, runAlive(r)
		msg := "the session process (pid " + strconv.Itoa(r.Pid) + ") ended without recording how the run ended"
		if runLive {
			msg += "; the run was still going and was stopped"
		}
		r.Status, r.Error = StatusFailed, msg
	})
	if err == nil && runLive {
		stopGroup(pgid)
	}
	return rec, err
}

// supervisorAlive reports whether the session process r names may still be
// running: it is, or it exists and its start cannot be compared. Only a process
// that is gone, or is verifiably another one, is dead. A record with no session
// process yet (Start has not recorded one) is about to have one.
func supervisorAlive(r *Record) bool {
	if r.Pid == 0 {
		return true
	}
	st := procState(r.Pid, r.PidStart)
	return st == procSame || st == procUnknown
}

// runAlive reports whether the group of r.Pgid is safe to signal: only when
// something is verified. Either the group's leader, the delegate's run, is
// verifiably the recorded process (same start token), or the supervisor, its
// parent, is (same start token), which means the run is its child and its id
// has not been reused. Where neither start time can be compared, nothing is
// verified, so nothing is signalled: a pid that merely exists proves nothing.
func runAlive(r *Record) bool {
	if r.Pgid <= 1 {
		return false
	}
	switch procState(r.Pgid, r.PgidStart) {
	case procSame:
		return true
	case procUnknown:
		return r.Pid > 1 && procState(r.Pid, r.PidStart) == procSame
	}
	return false
}

// runUnverified reports whether the run may still exist but cannot be verified
// as the recorded one, so a signal was withheld (as opposed to the run being
// gone or another process's).
func runUnverified(r *Record) bool {
	return r.Pgid > 1 && !runAlive(r) && procState(r.Pgid, r.PgidStart) == procUnknown
}

var handleRE = regexp.MustCompile(`^dlg-[0-9a-f]{16}$`)

// FindHandle returns the path of the record for handle, found under Dir. A
// handle is all a caller outside this package may name a delegation by: it is
// validated, never joined into a path as given.
func FindHandle(handle string) (string, error) {
	if !handleRE.MatchString(handle) {
		return "", fmt.Errorf("%q is not a delegation handle", handle)
	}
	matches, err := filepath.Glob(filepath.Join(Dir(), "*", "*", handle+".json"))
	if err != nil {
		return "", err
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no delegation with handle %s", handle)
	case 1:
		return matches[0], nil
	}
	return "", errors.New("more than one delegation with handle " + handle)
}
