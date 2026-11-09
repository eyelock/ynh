package main

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/vendor"
)

func TestParseRunArgs_Resume(t *testing.T) {
	t.Setenv("YNH_FOCUS", "")
	t.Setenv("YNH_HARNESS_FILE", "")

	tests := []struct {
		name           string
		args           []string
		wantResume     bool
		wantResumeID   string
		wantVendorArgs []string
		wantPrompt     string
	}{
		{
			name:       "bare --resume",
			args:       []string{"my-harness", "--resume"},
			wantResume: true,
		},
		{
			name:         "--resume with id",
			args:         []string{"my-harness", "--resume=6187c041-ee79-4646-b101-82f89c3e50ca"},
			wantResume:   true,
			wantResumeID: "6187c041-ee79-4646-b101-82f89c3e50ca",
		},
		{
			name:       "no --resume flag",
			args:       []string{"my-harness"},
			wantResume: false,
		},
		{
			name:         "--resume alongside a prompt",
			args:         []string{"my-harness", "--resume", "--", "carry on"},
			wantResume:   true,
			wantPrompt:   "carry on",
			wantResumeID: "",
		},
		{
			// The id may itself contain "="; only the first one separates.
			name:         "id containing an equals sign",
			args:         []string{"my-harness", "--resume=a=b"},
			wantResume:   true,
			wantResumeID: "a=b",
		},
		{
			// Must not be swallowed into VendorArgs by the generic "-" branch.
			name:         "--resume is not passed through to the vendor",
			args:         []string{"my-harness", "--resume=abc", "--verbose"},
			wantResume:   true,
			wantResumeID: "abc",
			wantVendorArgs: []string{
				"--verbose",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ra := parseRunArgs(tt.args)
			if ra.Resume != tt.wantResume {
				t.Errorf("Resume = %v, want %v", ra.Resume, tt.wantResume)
			}
			if ra.ResumeID != tt.wantResumeID {
				t.Errorf("ResumeID = %q, want %q", ra.ResumeID, tt.wantResumeID)
			}
			if ra.Prompt != tt.wantPrompt {
				t.Errorf("Prompt = %q, want %q", ra.Prompt, tt.wantPrompt)
			}
			if tt.wantVendorArgs != nil {
				if strings.Join(ra.VendorArgs, " ") != strings.Join(tt.wantVendorArgs, " ") {
					t.Errorf("VendorArgs = %v, want %v", ra.VendorArgs, tt.wantVendorArgs)
				}
			}
			for _, arg := range ra.VendorArgs {
				if strings.HasPrefix(arg, "--resume") {
					t.Errorf("--resume leaked into VendorArgs: %v", ra.VendorArgs)
				}
			}
		})
	}
}

// stubAdapter lets resolveResumeSession be tested against each ResolveLastSession
// outcome without touching a real vendor store.
type stubAdapter struct {
	vendor.Adapter
	sessionID string
	err       error
}

func (s stubAdapter) ResolveLastSession(string, time.Time) (string, error) {
	return s.sessionID, s.err
}

func TestResolveResumeSession(t *testing.T) {
	tests := []struct {
		name        string
		explicitID  string
		adapter     stubAdapter
		wantID      string
		wantResume  bool
		wantWarning string
	}{
		{
			name:       "explicit id wins without consulting the store",
			explicitID: "explicit-id",
			adapter:    stubAdapter{err: vendor.ErrNoResumableSession},
			wantID:     "explicit-id",
			wantResume: true,
		},
		{
			name:       "resolved id is used",
			adapter:    stubAdapter{sessionID: "resolved-id"},
			wantID:     "resolved-id",
			wantResume: true,
		},
		{
			// Codex and Cursor: no readable store, but continue-last still works.
			name:       "lookup unavailable still resumes with an empty id",
			adapter:    stubAdapter{err: vendor.ErrSessionLookupUnavailable},
			wantID:     "",
			wantResume: true,
		},
		{
			// Claude and Copilot: store was readable and genuinely empty.
			name:        "no session found launches cold with a warning",
			adapter:     stubAdapter{err: vendor.ErrNoResumableSession},
			wantResume:  false,
			wantWarning: "no previous session found",
		},
		{
			name:        "unreadable store launches cold rather than failing",
			adapter:     stubAdapter{err: errUnreadableStore},
			wantResume:  false,
			wantWarning: "could not read session history",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			id, resume := resolveResumeSession(tt.adapter, tt.explicitID, &stderr)

			if id != tt.wantID {
				t.Errorf("session id = %q, want %q", id, tt.wantID)
			}
			if resume != tt.wantResume {
				t.Errorf("resume = %v, want %v", resume, tt.wantResume)
			}
			if tt.wantWarning == "" {
				if stderr.Len() != 0 {
					t.Errorf("unexpected warning: %q", stderr.String())
				}
			} else if !strings.Contains(stderr.String(), tt.wantWarning) {
				t.Errorf("warning = %q, want it to contain %q", stderr.String(), tt.wantWarning)
			}
		})
	}
}

var errUnreadableStore = &storeError{}

type storeError struct{}

func (e *storeError) Error() string { return "permission denied" }

// snapshotAdapter is a vendor whose sessions are keyed by snapshot, recording
// the folder it was asked about.
type snapshotAdapter struct {
	vendor.Adapter
	id   string
	err  error
	root *string
}

func (s snapshotAdapter) Name() string { return "codex" }
func (s snapshotAdapter) ResolveLastSession(root string, _ time.Time) (string, error) {
	*s.root = root
	return s.id, s.err
}

// --resume on Codex and Cursor looks in every snapshot of the harness, finds
// the newest session by id, and refuses (no fall back to the vendor's own
// "last session") when there is none or the store cannot be read.
func TestResolveSnapshotSession(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	var root string
	id, err := resolveSnapshotSession(snapshotAdapter{id: "sess-1", root: &root}, "local--seat", io.Discard)
	if err != nil || id != "sess-1" {
		t.Fatalf("got %q, %v", id, err)
	}
	if want := filepath.Join(config.RunDir(), ".snapshots", "local--seat"); root != want {
		t.Errorf("looked in %s, want %s", root, want)
	}

	_, err = resolveSnapshotSession(snapshotAdapter{err: vendor.ErrNoResumableSession, root: &root}, "local--seat", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "local--seat") || !strings.Contains(err.Error(), "--resume=<session-id>") {
		t.Errorf("none found should refuse naming the harness and --resume=<id>: %v", err)
	}
	_, err = resolveSnapshotSession(snapshotAdapter{err: errUnreadableStore, root: &root}, "local--seat", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--resume=<session-id>") {
		t.Errorf("unreadable store should refuse: %v", err)
	}
}

func (s snapshotAdapter) SupportsResume() bool          { return true }
func (s snapshotAdapter) SessionsKeyedBySnapshot() bool { return true }
func (s snapshotAdapter) LaunchInteractive(string, []string) error {
	panic("a refused resume must not launch cold")
}
func (s snapshotAdapter) LaunchResume(_, id, _ string, _ bool, _ []string) error {
	*s.root = "resumed " + id
	return nil
}

func TestLaunchSession_SnapshotResumeNeverFallsBackToACold(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	var got string
	err := launchSession(snapshotAdapter{err: vendor.ErrNoResumableSession, root: &got}, "/run", "local--seat", runArgs{Resume: true}, "", "", nil, io.Discard)
	if err == nil {
		t.Fatal("--resume with no session must refuse, not launch")
	}
	if err := launchSession(snapshotAdapter{id: "s9", root: &got}, "/run", "local--seat", runArgs{Resume: true}, "", "", nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got != "resumed s9" {
		t.Errorf("got %q, want the session resumed by id", got)
	}
}
