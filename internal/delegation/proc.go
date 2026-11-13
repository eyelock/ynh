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
)

// procStart is a token for when process pid started, or "" when pid is not a
// live process (or its start cannot be read). A pid alone cannot say whether it
// is still the process a record was written for, since ids are reused; the pid
// with its start time can.
func procStart(pid int) string {
	if pid <= 1 {
		return ""
	}
	cmd := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid))
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.Join(strings.Fields(string(out)), " ")
}

// sameProc reports whether pid is still the process whose start token is
// start. An empty token (a record written without one) is never the same
// process: nothing signals on a guess.
func sameProc(pid int, start string) bool {
	if start == "" {
		return false
	}
	return procStart(pid) == start
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

// supervisorAlive reports whether the session process r names is still that
// process. A record with no session process yet (Start has not recorded one) is
// treated as alive: it is about to have one.
func supervisorAlive(r *Record) bool {
	if r.Pid == 0 {
		return true
	}
	return sameProc(r.Pid, r.PidStart)
}

// runAlive reports whether the delegate's run, the leader of r.Pgid, is still
// the recorded process, so that signalling its group cannot reach another.
func runAlive(r *Record) bool {
	return r.Pgid > 1 && sameProc(r.Pgid, r.PgidStart)
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
