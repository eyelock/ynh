package delegation

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/namespace"
	"github.com/eyelock/ynh/internal/presence"
)

// DefaultTakeBy is how long a queued task may wait for the live session to
// take it before the sender is told it is still waiting (and may choose).
// AutoApproveInteractive is the auto_approve_from of a task queued to a live
// interactive session (permission.SourceInteractive): a person approves each
// action there, so the record shows no level.
const AutoApproveInteractive = "interactive"

const DefaultTakeBy = 5 * time.Minute

// ErrNotQueued is what Take returns for a task another session already took,
// or that was cancelled, fell back to headless or ended.
var ErrNotQueued = errors.New("the task is no longer waiting in the inbox")

// The mailbox of a harness is a folder of small pointer files, one per task
// queued for a live session of it, at $YNH_HOME/inbox/<harness-fs>/<handle>.json.
// The task itself is an ordinary delegation record (status queued, route
// live), so everything that reads delegations reads it. A mailbox belongs to
// the installed harness, not to a session: every live interactive session of
// that harness reads it, and Take is atomic, so exactly one of them owns a
// task.
type pointer struct {
	Record string `json:"record"`
}

// MailboxDir is the mailbox folder of the installed harness id.
func MailboxDir(delegate string) string {
	return filepath.Join(config.HomeDir(), "inbox", namespace.IDToFSName(delegate))
}

// QueueOptions describe one task for a live session.
type QueueOptions struct {
	Parent, Delegate string
	Task             string
	Model            string
	Profile, Focus   string
	AutoApprove      string
	AutoApproveFrom  string
	Folder           string
	Supervisor       []string
	Session          presence.Entry
	TakeBy           time.Duration
}

// LiveSessionsOf lists the live interactive sessions of the installed harness id
// that accept tasks, oldest first.
func LiveSessionsOf(delegate string) []presence.Entry {
	live, err := presence.List(false)
	if err != nil {
		return nil
	}
	run := namespace.IDToFSName(delegate)
	var out []presence.Entry
	for _, e := range live {
		if e.Status == presence.StatusLive && e.Mode == presence.ModeInteractive && e.AcceptsTasks && e.Harness == run {
			out = append(out, e)
		}
	}
	return out
}

// Queue writes the task as a queued delegation addressed to a live session of
// the delegate and puts it in the harness's mailbox. It starts nothing.
func Queue(o QueueOptions) (*Record, string, error) {
	anc, err := Guard(o.Parent, o.Delegate, Chain(os.Getenv), os.Getenv)
	if err != nil {
		return nil, "", err
	}
	folder, err := filepath.Abs(o.Folder)
	if err != nil {
		return nil, "", fmt.Errorf("queueing a task: %w", err)
	}
	handle, err := NewHandle()
	if err != nil {
		return nil, "", err
	}
	if o.TakeBy <= 0 {
		o.TakeBy = DefaultTakeBy
	}
	now := time.Now().UTC()
	by := now.Add(o.TakeBy)
	path := RecordPath(o.Parent, folder, handle)
	rec := &Record{
		Handle: handle, Parent: o.Parent, Delegate: o.Delegate, Folder: folder,
		Task: o.Task, Model: o.Model, Profile: o.Profile, Focus: o.Focus, Chain: anc,
<<<<<<< HEAD
		AutoApproveFrom:     AutoApproveInteractive,
		FallbackAutoApprove: o.AutoApprove, FallbackAutoApproveFrom: o.AutoApproveFrom,
=======
		// A live session runs with its own model and effort: the record says so
		// and names no value (ynh #1074).
		ModelFrom: FromLiveSession, EffortFrom: FromLiveSession,
>>>>>>> 4558eb70 (fix(delegation): a task queued at a live session reports live_session as the source of model and effort (#1074))
		Log:   strings.TrimSuffix(path, ".json") + ".log",
		Route: RouteLive, Session: o.Session.SessionID, SessionCwd: o.Session.Cwd,
		QueuedAt: &now, TakeBy: &by, Supervisor: o.Supervisor,
	}
	if err := create(path, rec); err != nil {
		return nil, "", err
	}
	rec, err = Update(path, func(r *Record) { r.Status = StatusQueued })
	if err != nil {
		return nil, "", err
	}
	dir := MailboxDir(o.Delegate)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", fmt.Errorf("creating the inbox folder: %w", err)
	}
	data, _ := json.Marshal(pointer{Record: path})
	if err := os.WriteFile(filepath.Join(dir, handle+".json"), data, 0o644); err != nil {
		return nil, "", fmt.Errorf("writing the inbox entry: %w", err)
	}
	return rec, path, nil
}

// InboxEntry is a task waiting in a mailbox.
type InboxEntry struct {
	Path string
	*Record
}

// Inbox lists the tasks waiting (queued) in the mailbox of the delegate harness
// id, oldest first. An entry whose record is gone or no longer queued is
// dropped from the folder as it is met.
func Inbox(delegate string) ([]InboxEntry, error) {
	dir := MailboxDir(delegate)
	files, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []InboxEntry
	for _, f := range files {
		if f.IsDir() || filepath.Ext(f.Name()) != ".json" {
			continue
		}
		entry := filepath.Join(dir, f.Name())
		data, err := os.ReadFile(entry)
		var p pointer
		if err != nil || json.Unmarshal(data, &p) != nil {
			continue
		}
		rec, err := Load(p.Record)
		if err != nil || rec.Status != StatusQueued {
			_ = os.Remove(entry)
			continue
		}
		out = append(out, InboxEntry{Path: p.Record, Record: rec})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// Take gives the queued task at path to session, atomically: the first caller
// moves it to running and owns it, any other gets ErrNotQueued.
func Take(path string, session presence.Entry) (*Record, error) {
	var lost bool
	rec, err := Update(path, func(r *Record) {
		if r.Status != StatusQueued {
			lost = true
			return
		}
		r.Status = StatusRunning
		r.Session, r.SessionCwd = session.SessionID, session.Cwd
	})
	if err != nil {
		return nil, err
	}
	if lost {
		return rec, ErrNotQueued
	}
	_ = os.Remove(filepath.Join(MailboxDir(rec.Delegate), rec.Handle+".json"))
	return rec, nil
}

// Done records the live session's result and completes the task.
func Done(path, sessionID, result string) (*Record, error) {
	return ownedUpdate(path, sessionID, func(r *Record) {
		r.Result = result
		code := 0
		r.ExitCode = &code
		r.Status = StatusSucceeded
	})
}

// Decline records that the live session will not do the task: rejected, which
// the sender may resend headless.
func Decline(path, sessionID, reason string) (*Record, error) {
	return ownedUpdate(path, sessionID, func(r *Record) {
		r.Error = reason
		r.Status = StatusRejected
	})
}

// ownedUpdate applies fn to a running task that sessionID took.
func ownedUpdate(path, sessionID string, fn func(*Record)) (*Record, error) {
	var why error
	rec, err := Update(path, func(r *Record) {
		switch {
		case r.Session != sessionID:
			why = errors.New("this task was taken by another session")
		case r.Status != StatusRunning:
			why = fmt.Errorf("the task is %s, not running", r.Status)
		default:
			fn(r)
		}
	})
	if err != nil {
		return nil, err
	}
	return rec, why
}

// settleLive moves a live-route record on when its session is gone, and
// returns the record as it then is. Queued: no live session of the harness
// remains, so the task falls back to a headless session, with the same task,
// nothing lost. Taken (running): the session died with the task half done, so
// it fails, with "resend", and is never re-run by itself.
func settleLive(path string, rec *Record) (*Record, error) {
	if rec.Route != RouteLive || rec.Status.Final() {
		return rec, nil
	}
	switch rec.Status {
	case StatusQueued:
		if len(LiveSessionsOf(rec.Delegate)) > 0 {
			return rec, nil
		}
		var started bool
		r, err := Update(path, func(r *Record) {
			if r.Status != StatusQueued {
				return
			}
			started = true
			r.Route, r.Status = RouteHeadless, StatusStarted
			// A headless session opens at the level the task resolved to.
			r.AutoApprove, r.AutoApproveFrom = r.FallbackAutoApprove, r.FallbackAutoApproveFrom
			r.FallbackAutoApprove, r.FallbackAutoApproveFrom = "", ""
			r.Error = "the live session ended before it took the task: started headless with the same task"
		})
		if err != nil || !started {
			return r, err
		}
		_ = os.Remove(filepath.Join(MailboxDir(rec.Delegate), rec.Handle+".json"))
		if len(r.Supervisor) == 0 {
			return r, errors.New("the queued task has no supervisor command to fall back to")
		}
		if _, _, err := launchSession(path, r.Folder, r.Supervisor, r.Chain); err != nil {
			return Load(path)
		}
		return Load(path)
	case StatusRunning:
		for _, e := range LiveSessionsOf(rec.Delegate) {
			if e.SessionID == rec.Session {
				return rec, nil
			}
		}
		return Update(path, func(r *Record) {
			if r.Status != StatusRunning {
				return
			}
			r.Error = "the live session ended; resend"
			r.Status = StatusFailed
		})
	}
	return rec, nil
}
