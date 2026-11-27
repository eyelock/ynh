// Package presence records every session `ynh run` launches and says which
// are live (docs/presence.md). One JSON record per session lives under
// $YNH_HOME/presence; the wrapper writes it at launch, refreshes last_beat
// while the session runs and marks it ended on a clean exit. A record whose
// wrapper process is gone, or whose pid now belongs to a process that started
// at a different time, reads as dead, so a crash or a reused pid never reads
// as alive. Callers read presence through `ynh ps`, never the files.
package presence

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/eyelock/ynh/internal/config"
)

// Launch modes.
const (
	ModeInteractive = "interactive"
	ModeOneShot     = "one-shot"
	ModeDelegate    = "delegate"
)

// States a live session reports. Busy and idle are filled only where a vendor
// can say; everywhere else the state is unknown.
const (
	StateAlive   = "alive"
	StateBusy    = "busy"
	StateIdle    = "idle"
	StateUnknown = "unknown"
)

// Status is what `ynh ps` derives from a record and the live process table.
const (
	StatusLive     = "live"     // wrapper process is alive
	StatusOrphaned = "orphaned" // a delegate whose owner session is no longer live
	StatusDead     = "dead"     // wrapper gone without a clean exit (crash, kill)
	StatusEnded    = "ended"    // clean exit
)

// BeatEvery is how often a running session refreshes last_beat.
const BeatEvery = 15 * time.Second

// Record is one session's presence.
type Record struct {
	SessionID   string    `json:"session_id"`
	Harness     string    `json:"harness"`
	Vendor      string    `json:"vendor"`
	VendorPID   int       `json:"vendor_pid,omitempty"`
	VendorStart int64     `json:"vendor_start,omitempty"` // vendor process start (ProcessStart), so a reused pid is not mistaken for it
	WrapperPID  int       `json:"wrapper_pid"`
	StartTime   int64     `json:"start_time"` // wrapper process start, an opaque per-OS number (ProcessStart)
	StartedAt   time.Time `json:"started_at"` // wall clock, for people; liveness uses StartTime
	Cwd         string    `json:"cwd"`
	Mode        string    `json:"mode"`
	// AcceptsTasks is true for an interactive session that can take a task a
	// parent delegated to its harness: it has the ynh-inbox MCP server and an
	// allow rule for it, on a vendor that supports it (docs/delegate-sessions.md).
	AcceptsTasks bool      `json:"accepts_tasks"`
	OwnerSession string    `json:"owner_session,omitempty"`
	Address      string    `json:"address"`
	State        string    `json:"state"`
	LastBeat     time.Time `json:"last_beat"`
	Ended        bool      `json:"ended"`
}

// Entry is a Record with its derived status, the shape `ynh ps` lists.
type Entry struct {
	Record
	Status string `json:"status"`
}

// Dir is where the records live.
func Dir() string { return filepath.Join(config.HomeDir(), "presence") }

func path(id string) string { return filepath.Join(Dir(), id+".json") }

// Address is the name a session answers to in `ynh ps`.
func Address(sessionID string) string { return "ynh:" + sessionID }

// Save writes r atomically (temp file, then rename).
func Save(r Record) error {
	if r.SessionID == "" || strings.ContainsAny(r.SessionID, `/\`) {
		return fmt.Errorf("presence: invalid session id %q", r.SessionID)
	}
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return fmt.Errorf("creating presence dir: %w", err)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(Dir(), r.SessionID+".*.tmp")
	if err != nil {
		return fmt.Errorf("writing presence record: %w", err)
	}
	_, werr := tmp.Write(append(data, '\n'))
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("writing presence record: %w", err)
	}
	if err := os.Rename(tmp.Name(), path(r.SessionID)); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("writing presence record: %w", err)
	}
	return nil
}

// Session is a running session's handle on its own record.
//
// mu guards rec and is held across each mutate-and-Save, so the heartbeat
// goroutine and the run's goroutine never race on rec and their renames land
// in the order their changes were made: a later Save always carries every
// earlier change (vendor_pid is never dropped by a stale beat).
type Session struct {
	mu   sync.Mutex
	rec  Record
	stop chan struct{}
	done chan struct{}
}

// Start records the session, with the wrapper's own pid and start time, and
// begins its heartbeat. Callers treat an error as non-fatal: presence must
// never stop a launch.
func Start(r Record) (*Session, error) {
	r.WrapperPID = os.Getpid()
	start, err := ProcessStart(r.WrapperPID)
	if err != nil {
		return nil, fmt.Errorf("reading own start time: %w", err)
	}
	r.StartTime = start
	r.StartedAt = time.Now().UTC()
	r.Address = Address(r.SessionID)
	if r.State == "" {
		r.State = StateAlive
	}
	r.LastBeat = time.Now().UTC()
	if err := Save(r); err != nil {
		return nil, err
	}
	s := &Session{rec: r, stop: make(chan struct{}), done: make(chan struct{})}
	go s.beat(BeatEvery)
	return s, nil
}

func (s *Session) beat(every time.Duration) {
	defer close(s.done)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			s.mu.Lock()
			s.rec.LastBeat = time.Now().UTC()
			_ = Save(s.rec)
			s.mu.Unlock()
		}
	}
}

// SetVendorPID records the vendor process once it has started.
func (s *Session) SetVendorPID(pid int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rec.VendorPID = pid
	// The vendor's start time, read as soon as it has a pid: Stop signals the
	// vendor only while the process at the pid still started then. Absent when
	// it cannot be read, and Stop then goes through the wrapper.
	s.rec.VendorStart = 0
	if start, err := ProcessStart(pid); err == nil {
		s.rec.VendorStart = start
	}
	s.rec.LastBeat = time.Now().UTC()
	_ = Save(s.rec)
}

// End stops the heartbeat and marks the record ended: the clean-exit marker.
func (s *Session) End() {
	close(s.stop)
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rec.Ended = true
	s.rec.LastBeat = time.Now().UTC()
	_ = Save(s.rec)
}

// List reads every record and derives each one's status. Without all it
// returns live and orphaned sessions only. Unreadable files are skipped.
func List(all bool) ([]Entry, error) {
	files, err := os.ReadDir(Dir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Entry{}, nil
		}
		return nil, fmt.Errorf("reading presence dir: %w", err)
	}
	var recs []Record
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(Dir(), f.Name()))
		if err != nil {
			continue
		}
		var r Record
		if json.Unmarshal(data, &r) != nil || r.SessionID == "" {
			continue
		}
		recs = append(recs, r)
	}
	liveIDs := map[string]bool{}
	for _, r := range recs {
		if !r.Ended && Alive(r) {
			liveIDs[r.SessionID] = true
		}
	}
	out := []Entry{}
	for _, r := range recs {
		e := Entry{Record: r}
		switch {
		case r.Ended:
			e.Status = StatusEnded
		case !Alive(r):
			e.Status = StatusDead
		case r.Mode == ModeDelegate && r.OwnerSession != "" && !liveIDs[r.OwnerSession]:
			e.Status = StatusOrphaned
		default:
			e.Status = StatusLive
		}
		if !all && e.Status != StatusLive && e.Status != StatusOrphaned {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartTime != out[j].StartTime {
			return out[i].StartTime < out[j].StartTime
		}
		return out[i].SessionID < out[j].SessionID
	})
	return out, nil
}

// Alive reports whether the record's wrapper process is still the one that
// wrote it: the pid exists and started at the recorded time. A reused pid
// started later, so it fails the second test.
func Alive(r Record) bool {
	if r.WrapperPID <= 0 {
		return false
	}
	start, err := ProcessStart(r.WrapperPID)
	if err != nil {
		return false
	}
	return start == r.StartTime
}
