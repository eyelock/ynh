package presence

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
)

// processStart is ProcessStart, replaceable by a test that changes who owns a
// pid between the two moments Stop reads it.
var processStart = ProcessStart

// afterTerm is called by Stop after it has signalled and before it waits; nil
// outside tests.
var afterTerm func()

// ErrNotLive is returned by Stop for a session that is not running.
var ErrNotLive = errors.New("the session is not live")

// Find returns the entry for a session named by its id or its address
// (ynh:<id>), live or not.
func Find(name string) (Entry, error) {
	id := strings.TrimPrefix(name, "ynh:")
	all, err := List(true)
	if err != nil {
		return Entry{}, err
	}
	for _, e := range all {
		if e.SessionID == id {
			return e, nil
		}
	}
	return Entry{}, fmt.Errorf("no session %q (see `ynh ps --all`)", name)
}

// Stop ends a live session: it asks the vendor process to terminate, so the
// wrapper that is its parent sees it exit and ends the record cleanly, and
// signals the wrapper itself when no vendor process was recorded. The record
// is checked against the process table first (Alive), so a pid that now
// belongs to another process is never signalled. It waits up to wait for the
// wrapper to go, then kills the vendor and says so if it did not.
func Stop(e Entry, wait time.Duration) error {
	if e.Ended || !Alive(e.Record) {
		return fmt.Errorf("%s: %w", e.Address, ErrNotLive)
	}
	pid := StopTarget(e.Record)
	if err := signalTerm(pid); err != nil {
		return fmt.Errorf("stopping %s: %w", e.Address, err)
	}
	if afterTerm != nil {
		afterTerm()
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if !Alive(e.Record) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if Alive(e.Record) {
		// The target is worked out again: the vendor's pid may have gone, or
		// been reused, during the wait, and then the wrapper (verified just
		// above) is what is killed, never whatever now has the vendor's pid.
		if p, err := os.FindProcess(StopTarget(e.Record)); err == nil {
			_ = p.Kill()
		}
		time.Sleep(200 * time.Millisecond)
		if Alive(e.Record) {
			return fmt.Errorf("stopping %s: the wrapper (pid %d) is still running after a kill of pid %d", e.Address, e.WrapperPID, pid)
		}
	}
	return nil
}

// StopTarget is the process Stop signals: the vendor, but only while the
// process at its pid is still the one recorded (it started when the record
// says). A vendor pid that was never given a start time, is gone, or now
// belongs to another process is not signalled; the wrapper, whose identity the
// caller has just verified, is, and it ends its session when its child exits.
func StopTarget(r Record) int {
	if r.VendorPID > 1 && r.VendorStart != 0 {
		if start, err := processStart(r.VendorPID); err == nil && start == r.VendorStart {
			return r.VendorPID
		}
	}
	return r.WrapperPID
}

// signalTerm asks the process to terminate, where the OS has a way to ask, and
// ends it where it has not.
func signalTerm(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return p.Kill()
	}
	return nil
}

// Remove deletes the record of a session that is not live, which is how
// history is cleared. A live session's record is never removed.
func Remove(e Entry) error {
	if !e.Ended && Alive(e.Record) {
		return fmt.Errorf("%s is live: stop it first", e.Address)
	}
	if err := os.Remove(path(e.SessionID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
