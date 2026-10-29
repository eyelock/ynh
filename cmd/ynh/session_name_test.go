package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eyelock/ynh/internal/sessions"
	"github.com/eyelock/ynh/internal/vendor"
)

// claudeStore wraps the real Claude adapter, so session lookup runs against a
// fake ~/.claude, and records launches instead of exec-ing the CLI.
type claudeStore struct {
	*vendor.Claude
	calls []string
}

func (c *claudeStore) LaunchResume(_, id, prompt string, interactive bool, _ []string) error {
	c.calls = append(c.calls, fmt.Sprintf("resume id=%s prompt=%q interactive=%v", id, prompt, interactive))
	return nil
}
func (c *claudeStore) LaunchWithSessionID(_, id, prompt string, interactive bool, _ []string) error {
	c.calls = append(c.calls, fmt.Sprintf("new id=%s prompt=%q interactive=%v", id, prompt, interactive))
	return nil
}
func (c *claudeStore) LaunchInteractive(string, []string) error {
	c.calls = append(c.calls, "interactive")
	return nil
}
func (c *claudeStore) LaunchNonInteractive(_, prompt string, _ []string) error {
	c.calls = append(c.calls, fmt.Sprintf("oneshot prompt=%q", prompt))
	return nil
}

// sessionEnv points YNH_HOME and HOME at temp dirs and chdirs into a project
// dir, returning a writer for fake Claude sessions in that project.
func sessionEnv(t *testing.T) (writeSession func(id string, age time.Duration)) {
	t.Helper()
	t.Setenv("YNH_HOME", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	proj, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(proj)
	t.Setenv("PWD", proj)
	dir := filepath.Join(home, ".claude", "projects", strings.NewReplacer("/", "-", ".", "-").Replace(proj))
	return func(id string, age time.Duration) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, id+".jsonl")
		if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := time.Now().Add(-age)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
}

func fixedIDs(t *testing.T, ids ...string) {
	t.Helper()
	old := newSessionID
	t.Cleanup(func() { newSessionID = old })
	n := 0
	newSessionID = func() (string, error) {
		id := ids[n%len(ids)]
		n++
		return id, nil
	}
}

func recorded(t *testing.T, harness, name string) string {
	t.Helper()
	cwd, _ := os.Getwd()
	b, ok, err := sessions.Load(sessions.Key(harness, cwd, name))
	if err != nil || !ok {
		return ""
	}
	return b.SessionID
}

func TestLaunchSessionBound(t *testing.T) {
	const (
		newID1 = `new id=id-1 prompt="" interactive=false`
		newID2 = `new id=id-2 prompt="" interactive=false`
	)
	tests := []struct {
		name  string
		setup func(write func(string, time.Duration))
		runs  []runArgs // launched in order; calls accumulate
		vargs []string
		want  []string
		note  string // stderr must contain
		rec   string // id recorded for ("h","a") afterwards
	}{
		{
			name: "fresh launch records and passes the session id",
			runs: []runArgs{{SessionName: "a"}},
			want: []string{newID1},
			rec:  "id-1",
		},
		{
			name: "resume finds the record, even with a newer unrelated session",
			setup: func(w func(string, time.Duration)) {
				w("id-1", time.Hour)
				w("someone-else", 0) // the hijack case
			},
			runs: []runArgs{{SessionName: "a"}, {SessionName: "a", Resume: true}},
			want: []string{newID1, `resume id=id-1 prompt="" interactive=false`},
			rec:  "id-1",
		},
		{
			name: "recorded session missing from the store starts a new one",
			runs: []runArgs{{SessionName: "a"}, {SessionName: "a", Resume: true}},
			want: []string{newID1, newID2},
			note: "no longer in the vendor's history",
			rec:  "id-2",
		},
		{
			name:  "resume with no record starts new, never the newest",
			setup: func(w func(string, time.Duration)) { w("someone-else", 0) },
			runs:  []runArgs{{SessionName: "a", Resume: true}},
			want:  []string{newID1},
			note:  "no session recorded",
			rec:   "id-1",
		},
		{
			name: "a fresh launch replaces the record",
			setup: func(w func(string, time.Duration)) {
				w("id-1", time.Hour)
				w("id-2", time.Minute)
			},
			runs: []runArgs{{SessionName: "a"}, {SessionName: "a"}, {SessionName: "a", Resume: true}},
			want: []string{newID1, newID2, `resume id=id-2 prompt="" interactive=false`},
			rec:  "id-2",
		},
		{
			name: "two names in one directory stay separate",
			setup: func(w func(string, time.Duration)) {
				w("id-1", time.Hour)
				w("id-2", time.Minute)
			},
			runs: []runArgs{{SessionName: "a"}, {SessionName: "b"}, {SessionName: "a", Resume: true}, {SessionName: "b", Resume: true}},
			want: []string{newID1, newID2, `resume id=id-1 prompt="" interactive=false`, `resume id=id-2 prompt="" interactive=false`},
			rec:  "id-1",
		},
		{
			name: "explicit id wins and the name follows it",
			setup: func(w func(string, time.Duration)) {
				w("picked", time.Hour)
				w("newer", 0)
			},
			runs: []runArgs{{SessionName: "a", Resume: true, ResumeID: "picked"}, {SessionName: "a", Resume: true}},
			want: []string{`resume id=picked prompt="" interactive=false`, `resume id=picked prompt="" interactive=false`},
			rec:  "picked",
		},
		{
			name: "explicit id the vendor lacks is passed through but not recorded",
			runs: []runArgs{{SessionName: "a", Resume: true, ResumeID: "ghost"}},
			want: []string{`resume id=ghost prompt="" interactive=false`},
		},
		{
			name: "no session name: newest in the directory, nothing recorded",
			setup: func(w func(string, time.Duration)) {
				w("old", time.Hour)
				w("newest", 0)
			},
			runs: []runArgs{{Resume: true}},
			want: []string{`resume id=newest prompt="" interactive=false`},
		},
		{
			name:  "passthrough --session-id: ynh adds none and binds nothing",
			setup: func(w func(string, time.Duration)) { w("newest", 0) },
			runs:  []runArgs{{SessionName: "a"}, {SessionName: "a", Resume: true}},
			vargs: []string{"--session-id=mine"},
			want:  []string{"interactive", `resume id=newest prompt="" interactive=false`},
			note:  "--session-id was passed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			write := sessionEnv(t)
			fixedIDs(t, "id-1", "id-2")
			if tt.setup != nil {
				tt.setup(write)
			}
			a := &claudeStore{Claude: &vendor.Claude{}}
			var stderr bytes.Buffer
			for _, ra := range tt.runs {
				if err := launchSession(a, t.TempDir(), "h", ra, "", "", tt.vargs, &stderr); err != nil {
					t.Fatal(err)
				}
			}
			if !reflect.DeepEqual(a.calls, tt.want) {
				t.Errorf("calls = %q\nwant    %q", a.calls, tt.want)
			}
			if tt.note != "" && !strings.Contains(stderr.String(), tt.note) {
				t.Errorf("stderr %q lacks %q", stderr.String(), tt.note)
			}
			if got := recorded(t, "h", "a"); got != tt.rec {
				t.Errorf("recorded = %q, want %q", got, tt.rec)
			}
		})
	}
}

func TestLaunchSessionBoundPrompts(t *testing.T) {
	tests := []struct {
		name   string
		ra     runArgs
		stored bool
		want   string
	}{
		{"cold one-shot", runArgs{SessionName: "a"}, false, `new id=id-1 prompt="fix it" interactive=false`},
		{"cold interactive", runArgs{SessionName: "a", Interactive: true}, false, `new id=id-1 prompt="fix it" interactive=true`},
		{"resume one-shot", runArgs{SessionName: "a", Resume: true}, true, `resume id=id-1 prompt="fix it" interactive=false`},
		{"resume interactive", runArgs{SessionName: "a", Resume: true, Interactive: true}, true, `resume id=id-1 prompt="fix it" interactive=true`},
		{"resume cold start still sends the prompt", runArgs{SessionName: "a", Resume: true}, false, `new id=id-1 prompt="fix it" interactive=false`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			write := sessionEnv(t)
			fixedIDs(t, "id-1")
			a := &claudeStore{Claude: &vendor.Claude{}}
			if tt.stored {
				write("id-1", time.Minute)
				if err := launchSession(a, t.TempDir(), "h", runArgs{SessionName: "a"}, "", "", nil, &bytes.Buffer{}); err != nil {
					t.Fatal(err)
				}
				a.calls = nil
			}
			if err := launchSession(a, t.TempDir(), "h", tt.ra, "fix it", "fix it", nil, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			if len(a.calls) != 1 || a.calls[0] != tt.want {
				t.Errorf("calls = %q, want %q", a.calls, tt.want)
			}
		})
	}
}

func TestLaunchSessionNameUnchangedForOtherVendors(t *testing.T) {
	sessionEnv(t)
	// recordingAdapter is not a SessionBinder, as Codex, Copilot and Cursor are not.
	a := &recordingAdapter{sessionID: "newest"}
	if err := launchSession(a, t.TempDir(), "h", runArgs{Resume: true, SessionName: "a"}, "", "", nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	want := []string{`resume id="newest" prompt="" interactive=false`}
	if !reflect.DeepEqual(a.calls, want) {
		t.Errorf("calls = %q, want %q", a.calls, want)
	}
	if entries, _ := os.ReadDir(sessions.Dir()); len(entries) != 0 {
		t.Errorf("recorded %d files for a vendor that cannot bind", len(entries))
	}
}

func TestConflictingSessionFlag(t *testing.T) {
	for args, want := range map[string]string{
		"":               "",
		"--model opus":   "",
		"--session-id=x": "--session-id",
		"--continue":     "--continue",
		"-r":             "-r",
		"--resume=abc":   "--resume",
		"--resume-thing": "",
	} {
		if got := conflictingSessionFlag(strings.Fields(args)); got != want {
			t.Errorf("%q: got %q want %q", args, got, want)
		}
	}
}
