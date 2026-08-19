package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

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
