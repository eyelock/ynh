// Package delegation runs a delegate as its own detached ynh session and keeps
// the delegation record that says what became of it.
//
// A record lives at
// $YNH_HOME/delegations/<parent-harness-fs>/<folder-key>/<handle>.json, keyed
// by the parent harness and the working folder the delegate runs in. The
// session writes it as it goes, so a parent that went away and came back, or a
// server that holds no state of its own, learns everything from the file
// (docs/delegate-sessions.md, "Outliving the parent").
package delegation

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/namespace"
)

// Status is where a delegation is. Started, running and any question waiting
// for an answer are live; succeeded, failed and cancelled are final.
type Status string

const (
	// StatusStarted: the record exists and the session process was launched,
	// but the delegate's ynh run has not begun.
	StatusStarted Status = "started"
	// StatusRunning: the delegate's ynh run is going.
	StatusRunning Status = "running"
	// StatusSucceeded: the run ended with exit code 0.
	StatusSucceeded Status = "succeeded"
	// StatusFailed: the run ended with any other exit code, or could not start.
	StatusFailed Status = "failed"
	// StatusCancelled: the delegation was cancelled. Final, whatever the
	// run did next.
	StatusCancelled Status = "cancelled"
)

// Final reports whether s is a state a delegation never leaves.
func (s Status) Final() bool {
	return s == StatusSucceeded || s == StatusFailed || s == StatusCancelled
}

// Change is one move of the status, with when it happened.
type Change struct {
	Status Status    `json:"status"`
	At     time.Time `json:"at"`
}

// Question is something the delegate asked its parent, and the answer once
// there is one. A question asked while the parent is away waits here.
type Question struct {
	ID         string     `json:"id"`
	Text       string     `json:"text"`
	AskedAt    time.Time  `json:"asked_at"`
	Answer     *string    `json:"answer,omitempty"`
	AnsweredAt *time.Time `json:"answered_at,omitempty"`
	// LapsedAt is set when the delegate withdrew the question, having gone on
	// without an answer. A lapsed question is no longer open: the delegation
	// is back to working.
	LapsedAt *time.Time `json:"lapsed_at,omitempty"`
}

// DeclinedAnswer is the answer recorded when the parent (or the human behind
// it) declined to answer a question. The delegate's tool reports it as a
// decline, so the delegate goes on with its own default or fails plainly.
const DeclinedAnswer = "(the parent declined to answer)"

// Open reports whether the question still waits for an answer.
func (q Question) Open() bool { return q.Answer == nil && q.LapsedAt == nil }

// Record is the delegation record. Every field is written by this package
// except Questions, which the delegate's ask tool fills (package parentmcp) and the parent's side answers.
type Record struct {
	Handle string `json:"handle"`
	// Parent is the canonical id of the harness whose delegates_to named the
	// delegate; Delegate is the id of the installed harness it ran as.
	Parent   string `json:"parent"`
	Delegate string `json:"delegate"`
	// Folder is the parent's working folder, the delegate's too.
	Folder  string `json:"folder"`
	Task    string `json:"task"`
	Model   string `json:"model,omitempty"`
	Profile string `json:"profile,omitempty"`
	Focus   string `json:"focus,omitempty"`

	Status  Status   `json:"status"`
	History []Change `json:"history"`

	Questions []Question `json:"questions,omitempty"`

	// ExitCode is the exit code of the delegate's ynh run, which is the vendor
	// CLI's own, passed through exactly. It is absent until the run ends, and
	// when the run was ended by a signal, which Error then says.
	ExitCode *int `json:"exit_code,omitempty"`
	// Result is the delegate's output (its standard output, the last part of
	// it when long) once the run has ended.
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
	// Log is the file holding the session's standard output and error.
	Log string `json:"log"`

	// Pid is the session process (the supervisor); Pgid is the process group
	// the delegate's ynh run and its vendor CLI run in, the group cancel stops.
	Pid  int `json:"pid,omitempty"`
	Pgid int `json:"pgid,omitempty"`
	// PidStart and PgidStart are when those two processes started. A pid alone
	// may be reused once its process is gone, so nothing is signalled or
	// reported alive unless the process at the pid started when this says.
	PidStart  string `json:"pid_start,omitempty"`
	PgidStart string `json:"pgid_start,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Dir is the root of all delegation records.
func Dir() string { return filepath.Join(config.HomeDir(), "delegations") }

// FolderKey is the folder's component of a record's path: a short hash of the
// absolute folder, so any path is a safe directory name.
func FolderKey(folder string) string {
	sum := sha256.Sum256([]byte(folder))
	return hex.EncodeToString(sum[:8])
}

// RecordPath is where the record for handle lives.
func RecordPath(parent, folder, handle string) string {
	return filepath.Join(Dir(), namespace.IDToFSName(parent), FolderKey(folder), handle+".json")
}

// NewHandle returns a fresh random handle.
func NewHandle() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("creating a delegation handle: %w", err)
	}
	return "dlg-" + hex.EncodeToString(b[:]), nil
}

// Load reads the record at path.
func Load(path string) (*Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading delegation record: %w", err)
	}
	var r Record
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parsing delegation record %s: %w", path, err)
	}
	return &r, nil
}

// write saves r at path atomically: a temp file in the same folder, then a
// rename, so a reader never sees half a record.
func write(path string, r *Record) error {
	r.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding delegation record: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("writing delegation record: %w", err)
	}
	_, werr := tmp.Write(append(data, '\n'))
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("writing delegation record: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("writing delegation record: %w", err)
	}
	return nil
}

// Update reads the record at path, applies fn and writes it back atomically,
// all under an exclusive lock, so the session, a cancel and the delegate server
// never overwrite one another's changes. A status fn leaves different from the
// one it found is recorded in History with the time.
func Update(path string, fn func(*Record)) (*Record, error) {
	unlock, err := lockFile(path + ".lock")
	if err != nil {
		return nil, err
	}
	defer unlock()
	r, err := Load(path)
	if err != nil {
		return nil, err
	}
	before := r.Status
	fn(r)
	if r.Status != before {
		r.History = append(r.History, Change{Status: r.Status, At: time.Now().UTC()})
	}
	if err := write(path, r); err != nil {
		return nil, err
	}
	return r, nil
}

// create writes a new record in the started state.
func create(path string, r *Record) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating delegation folder: %w", err)
	}
	now := time.Now().UTC()
	r.Status = StatusStarted
	r.History = []Change{{Status: StatusStarted, At: now}}
	r.CreatedAt = now
	return write(path, r)
}
