package main

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eyelock/ynh/internal/vendor"
)

// recordingAdapter records which launch method ran and with what, so
// launchSession can be tested without starting a vendor CLI.
type recordingAdapter struct {
	vendor.Adapter
	sessionID   string
	lookupErr   error
	resumeError error

	calls []string
}

func (r *recordingAdapter) Name() string         { return "fake" }
func (r *recordingAdapter) SupportsResume() bool { return true }
func (r *recordingAdapter) ResolveLastSession(string, time.Time) (string, error) {
	return r.sessionID, r.lookupErr
}
func (r *recordingAdapter) LaunchResume(_, sessionID, prompt string, interactive bool, _ []string) error {
	r.calls = append(r.calls, fmt.Sprintf("resume id=%q prompt=%q interactive=%v", sessionID, prompt, interactive))
	return r.resumeError
}
func (r *recordingAdapter) LaunchWithInitialPrompt(_, prompt string, _ []string) error {
	r.calls = append(r.calls, fmt.Sprintf("initial prompt=%q", prompt))
	return nil
}
func (r *recordingAdapter) LaunchNonInteractive(_, prompt string, _ []string) error {
	r.calls = append(r.calls, fmt.Sprintf("oneshot prompt=%q", prompt))
	return nil
}
func (r *recordingAdapter) LaunchInteractive(string, []string) error {
	r.calls = append(r.calls, "interactive")
	return nil
}

func TestLaunchSession(t *testing.T) {
	tests := []struct {
		name           string
		adapter        recordingAdapter
		ra             runArgs
		prompt         string // what a cold launch sends (explicit or focus)
		explicitPrompt string // only the trailing prompt
		want           []string
		wantErr        string
	}{
		{
			name:           "resume by id with a prompt, one-shot",
			adapter:        recordingAdapter{},
			ra:             runArgs{Resume: true, ResumeID: "abc"},
			prompt:         "fix it",
			explicitPrompt: "fix it",
			want:           []string{`resume id="abc" prompt="fix it" interactive=false`},
		},
		{
			name:           "resume by id with a prompt, interactive",
			ra:             runArgs{Resume: true, ResumeID: "abc", Interactive: true},
			prompt:         "fix it",
			explicitPrompt: "fix it",
			want:           []string{`resume id="abc" prompt="fix it" interactive=true`},
		},
		{
			name:           "resume continue-last with a prompt",
			adapter:        recordingAdapter{lookupErr: vendor.ErrSessionLookupUnavailable},
			ra:             runArgs{Resume: true},
			prompt:         "fix it",
			explicitPrompt: "fix it",
			want:           []string{`resume id="" prompt="fix it" interactive=false`},
		},
		{
			name:    "resume without a prompt stays interactive",
			adapter: recordingAdapter{sessionID: "found"},
			ra:      runArgs{Resume: true},
			want:    []string{`resume id="found" prompt="" interactive=false`},
		},
		{
			// A focus prompt starts a task; a resume continues one.
			name:    "resume with --focus does not resend the focus prompt",
			adapter: recordingAdapter{sessionID: "found"},
			ra:      runArgs{Resume: true, FocusFlag: "review"},
			prompt:  "review the diff",
			want:    []string{`resume id="found" prompt="" interactive=false`},
		},
		{
			name:    "resume found nothing: cold launch sends the focus prompt",
			adapter: recordingAdapter{lookupErr: vendor.ErrNoResumableSession},
			ra:      runArgs{Resume: true, FocusFlag: "review"},
			prompt:  "review the diff",
			want:    []string{`oneshot prompt="review the diff"`},
		},
		{
			name:           "resume found nothing: cold launch sends the explicit prompt",
			adapter:        recordingAdapter{lookupErr: vendor.ErrNoResumableSession},
			ra:             runArgs{Resume: true, Interactive: true},
			prompt:         "fix it",
			explicitPrompt: "fix it",
			want:           []string{`initial prompt="fix it"`},
		},
		{
			name:           "vendor that refuses names itself and does not launch",
			adapter:        recordingAdapter{resumeError: vendor.ErrResumePromptNotSupported},
			ra:             runArgs{Resume: true, ResumeID: "abc"},
			prompt:         "fix it",
			explicitPrompt: "fix it",
			want:           []string{`resume id="abc" prompt="fix it" interactive=false`},
			wantErr:        "fake cannot resume a session with a prompt; drop --resume or the prompt",
		},
		{
			name:           "refusal names the interactive mode",
			adapter:        recordingAdapter{resumeError: vendor.ErrResumePromptNotSupported},
			ra:             runArgs{Resume: true, ResumeID: "abc", Interactive: true},
			prompt:         "fix it",
			explicitPrompt: "fix it",
			want:           []string{`resume id="abc" prompt="fix it" interactive=true`},
			wantErr:        "fake cannot resume a session interactively with a prompt",
		},
		{
			name:   "cold launch unchanged: one-shot",
			ra:     runArgs{},
			prompt: "hi", explicitPrompt: "hi",
			want: []string{`oneshot prompt="hi"`},
		},
		{
			name: "cold launch unchanged: no prompt",
			ra:   runArgs{},
			want: []string{"interactive"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := tt.adapter
			err := launchSession(&a, t.TempDir(), "h", tt.ra, tt.prompt, tt.explicitPrompt, nil, io.Discard)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
			if !reflect.DeepEqual(a.calls, tt.want) {
				t.Errorf("calls = %q, want %q", a.calls, tt.want)
			}
		})
	}
}
