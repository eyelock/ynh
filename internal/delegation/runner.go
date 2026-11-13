package delegation

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Grace periods: how long a cancelled session gets after SIGTERM before its
// group is killed. Variables so tests can shorten them.
var termGrace = 5 * time.Second

// resultLimit bounds the result text kept in the record. The whole output is
// in the log.
const resultLimit = 256 << 10

// StartOptions describe one delegation.
type StartOptions struct {
	// Parent is the canonical id of the harness whose delegates_to named the
	// delegate. Delegate is the canonical id of the installed harness the
	// entry resolved to, the one that runs.
	Parent   string
	Delegate string
	// Task is the prompt, Model an optional model, Profile and Focus the
	// entry's selection (at most one).
	Task    string
	Model   string
	Profile string
	Focus   string
	// Folder is the working folder: the parent's, and the delegate's.
	Folder string
	// Supervisor is the command that starts the session process. The record's
	// path is appended to it as its last argument. For ynh itself it is
	// {<ynh>, "delegate-run", "supervise"}.
	Supervisor []string
}

// Start writes a new record and launches the session that runs the delegate,
// detached: in its own session and process group, with no terminal and no
// standard streams, so it outlives the process that called Start, and that
// process's MCP server. Start returns as soon as the session is launched; the
// record then moves on its own.
func Start(o StartOptions) (*Record, string, error) {
	if len(o.Supervisor) == 0 {
		return nil, "", errors.New("starting a delegation: no supervisor command")
	}
	if o.Profile != "" && o.Focus != "" {
		return nil, "", errors.New("starting a delegation: an entry names a profile or a focus, not both")
	}
	folder, err := filepath.Abs(o.Folder)
	if err != nil {
		return nil, "", fmt.Errorf("starting a delegation: %w", err)
	}
	handle, err := NewHandle()
	if err != nil {
		return nil, "", err
	}
	path := RecordPath(o.Parent, folder, handle)
	rec := &Record{
		Handle: handle, Parent: o.Parent, Delegate: o.Delegate, Folder: folder,
		Task: o.Task, Model: o.Model, Profile: o.Profile, Focus: o.Focus,
		Log: strings.TrimSuffix(path, ".json") + ".log",
	}
	if err := create(path, rec); err != nil {
		return nil, "", err
	}
	argv := append(append([]string{}, o.Supervisor...), path)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = folder
	// The session never inherits its parent's A2A endpoints or their tokens:
	// those are the parent's, and a delegate with delegates of its own gets its own.
	for _, kv := range os.Environ() {
		if !ParentOnlyEnv(kv) {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	// Setsid makes the session its own session and process group leader, so a
	// signal to the caller's group, or the caller's terminal closing, never
	// reaches it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		msg := fmt.Sprintf("starting the session process: %v", err)
		_, _ = Update(path, func(r *Record) { r.Status, r.Error = StatusFailed, msg })
		return nil, path, errors.New(msg)
	}
	pid := cmd.Process.Pid
	// Reap the session if this process outlives it.
	go func() { _ = cmd.Wait() }()
	r, err := Update(path, func(r *Record) {
		if r.Pid == 0 {
			r.Pid, r.PidStart = pid, procStart(pid)
		}
	})
	if err != nil {
		return nil, path, err
	}
	return r, path, nil
}

// Launcher builds the command that is the delegate's ynh run for rec. Supervise
// runs it in rec's folder, in a process group of its own.
type Launcher func(rec *Record) *exec.Cmd

// Supervise is the session process: it runs the delegate's ynh run as its
// child, moves the record from started to running, and records how the run
// ended.
func Supervise(path string, launch Launcher) error {
	// A cancel signals the delegate's group, not this process; a stray signal
	// to this one is a cancel too.
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	// A hangup (the starter's terminal closing) is not a cancel: the session
	// is meant to outlive it. Taking it on a channel, rather than ignoring it,
	// keeps the run from inheriting an ignored SIGHUP.
	hups := make(chan os.Signal, 1)
	signal.Notify(hups, syscall.SIGHUP)
	defer signal.Stop(sigs)
	defer signal.Stop(hups)

	rec, err := Load(path)
	if err != nil {
		return err
	}
	if rec.Status.Final() {
		return nil // cancelled before it began
	}

	logf, err := os.OpenFile(rec.Log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fail(path, fmt.Sprintf("opening the output log: %v", err))
	}
	defer func() { _ = logf.Close() }()

	cmd := launch(rec)
	cmd.Dir = rec.Folder
	cmd.Stdin = nil
	tail := &tailBuffer{limit: resultLimit}
	cmd.Stdout = io.MultiWriter(logf, tail)
	cmd.Stderr = logf
	// The run is the leader of a process group of its own, which holds the
	// vendor CLI and everything it starts: cancel stops that group.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// A vendor's grandchild can hold the output pipe after the run is gone;
	// do not wait on it for ever.
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		return fail(path, fmt.Sprintf("starting ynh run: %v", err))
	}
	pgid := cmd.Process.Pid

	var cancelledEarly bool
	if _, err := Update(path, func(r *Record) {
		r.Pid, r.PidStart = os.Getpid(), procStart(os.Getpid())
		r.Pgid, r.PgidStart = pgid, procStart(pgid)
		if r.Status.Final() { // cancelled between the check above and now
			cancelledEarly = true
			return
		}
		r.Status = StatusRunning
	}); err != nil {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_ = cmd.Wait()
		return err
	}
	if cancelledEarly {
		stopGroup(pgid)
	}

	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	var werr error
loop:
	for {
		select {
		case werr = <-waited:
			break loop
		case <-hups:
		case <-sigs:
			// Mark it cancelled first, so the exit below cannot be read as a
			// failure, then stop the group.
			_, _ = Update(path, func(r *Record) {
				if !r.Status.Final() {
					r.Status = StatusCancelled
				}
			})
			go stopGroup(pgid)
		}
	}
	// Whatever the vendor left behind in the group goes with it.
	_ = syscall.Kill(-pgid, syscall.SIGKILL)

	code, sigMsg := exitOf(werr)
	_, err = Update(path, func(r *Record) {
		r.Result = tail.String()
		r.ExitCode = code
		r.Error = sigMsg
		if r.Status.Final() { // a cancel got there first and stays
			return
		}
		if code != nil && *code == 0 {
			r.Status = StatusSucceeded
		} else {
			r.Status = StatusFailed
		}
	})
	return err
}

// fail records a delegation that could not run.
func fail(path, msg string) error {
	_, err := Update(path, func(r *Record) {
		r.Error = msg
		if !r.Status.Final() {
			r.Status = StatusFailed
		}
	})
	return err
}

// exitOf turns what cmd.Wait returned into the exit code to record, passed
// through exactly, or no code and a message when a signal ended the run.
func exitOf(err error) (*int, string) {
	if err == nil {
		zero := 0
		return &zero, ""
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if c := ee.ExitCode(); c >= 0 {
			return &c, ""
		}
		return nil, "ynh run ended by a signal: " + ee.String()
	}
	return nil, "waiting for ynh run: " + err.Error()
}

// stopGroup asks the group to stop and kills what is left after termGrace.
func stopGroup(pgid int) {
	if pgid <= 1 {
		return
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	deadline := time.Now().Add(termGrace)
	for time.Now().Before(deadline) {
		if !groupAlive(pgid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}

func groupAlive(pgid int) bool {
	return syscall.Kill(-pgid, 0) == nil
}

// Cancel stops the delegation at path: it records cancelled, then stops the
// delegate's process group (SIGTERM, then SIGKILL after a grace). The record is
// final from the moment Cancel marks it, so a session that ends a moment later
// cannot turn it into a failure. A delegation already final is left as it is.
func Cancel(path string) (*Record, error) {
	var pgid int
	var already, safe bool
	rec, err := Update(path, func(r *Record) {
		if r.Status.Final() {
			already = true
			return
		}
		// The group is signalled only while its leader is still the run this
		// record was written for; after that the id may be anyone's.
		pgid, safe = r.Pgid, runAlive(r)
		if runUnverified(r) {
			r.Error = "cancelled, but the run was not signalled: its process could not be verified as the one recorded (no start time to compare), so it may still be running"
		}
		r.Status = StatusCancelled
	})
	if err != nil || already {
		return rec, err
	}
	// With no group yet, the session has not started the run; it sees the final
	// status when it does and stops the run itself.
	if pgid > 1 && safe {
		stopGroup(pgid)
	}
	return rec, nil
}

// tailBuffer keeps the last limit bytes written to it.
type tailBuffer struct {
	mu    sync.Mutex
	limit int
	buf   bytes.Buffer
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf.Write(p)
	if over := t.buf.Len() - t.limit; over > 0 {
		t.buf.Next(over)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.buf.String()
}

// ParentOnlyEnv reports whether the environment entry kv belongs to the parent
// session: its A2A delegate endpoints (YNH_DELEGATES, YNH_DELEGATE_*) and the
// a2a CLI's own settings (A2ACLI_*).
func ParentOnlyEnv(kv string) bool {
	k, _, _ := strings.Cut(kv, "=")
	return k == "YNH_DELEGATES" || strings.HasPrefix(k, "YNH_DELEGATE_") || strings.HasPrefix(k, "A2ACLI_")
}
