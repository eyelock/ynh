package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/vendor"
)

func wakeHarness() *harness.Harness {
	return &harness.Harness{
		Name:        "wake",
		LaunchFocus: "wake",
		Focuses: map[string]plugin.Focus{
			"wake": {Prompt: "Begin: act on your wake block", Profile: "live"},
			"next": {Prompt: "Take the next task"},
		},
		Profiles: map[string]plugin.Profile{"live": {}},
	}
}

// TestResolveLaunchFocus covers rules 1 to 3 and 6 of the launch focus: what a
// run with and without a focus or prompt becomes.
func TestResolveLaunchFocus(t *testing.T) {
	tests := []struct {
		name            string
		harness         func() *harness.Harness
		ra              runArgs
		profile         string
		wantBare        bool
		wantName        string
		wantPrompt      string
		wantInteractive bool
		wantProfile     string
		wantErr         string
	}{
		{
			name: "bare launch: wake prompt, interactive, its profile",
			ra:   runArgs{}, wantBare: true, wantName: "wake",
			wantPrompt: "Begin: act on your wake block", wantInteractive: true, wantProfile: "live",
		},
		{
			// The dispatcher runs `worker --focus next` and expects a one-shot exit.
			name: "explicit --focus is untouched and stays one-shot",
			ra:   runArgs{FocusFlag: "next"},
		},
		{
			name: "explicit --focus with --interactive is untouched",
			ra:   runArgs{FocusFlag: "next", Interactive: true}, wantInteractive: true,
		},
		{
			name: "explicit prompt replaces the wake prompt, keeps the profile, stays one-shot",
			ra:   runArgs{Prompt: "just this"}, wantName: "wake",
			wantPrompt: "just this", wantInteractive: false, wantProfile: "live",
		},
		{
			name: "explicit prompt with --interactive stays interactive",
			ra:   runArgs{Prompt: "just this", Interactive: true}, wantName: "wake",
			wantPrompt: "just this", wantInteractive: true, wantProfile: "live",
		},
		{
			name: "an explicit profile beats the launch focus's",
			ra:   runArgs{}, profile: "other", wantBare: true, wantName: "wake",
			wantPrompt: "Begin: act on your wake block", wantInteractive: true, wantProfile: "other",
		},
		{
			name:    "install and clean actions ignore it",
			ra:      runArgs{Action: "install"},
			wantErr: "",
		},
		{
			name:    "no launch_focus: nothing changes",
			harness: func() *harness.Harness { h := wakeHarness(); h.LaunchFocus = ""; return h },
			ra:      runArgs{},
		},
		{
			name:    "names a focus that does not exist",
			harness: func() *harness.Harness { h := wakeHarness(); h.LaunchFocus = "gone"; return h },
			ra:      runArgs{}, wantErr: `launch_focus "gone" not defined`,
		},
		{
			name: "names a focus with no prompt",
			harness: func() *harness.Harness {
				h := wakeHarness()
				h.Focuses["wake"] = plugin.Focus{}
				return h
			},
			ra: runArgs{}, wantErr: `launch_focus "wake" has no prompt`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := wakeHarness()
			if tc.harness != nil {
				h = tc.harness()
			}
			ra := tc.ra
			lf, profile, err := resolveLaunchFocus(h, &ra, tc.profile)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if lf.Bare != tc.wantBare || lf.Name != tc.wantName {
				t.Errorf("launch = %+v, want bare=%v name=%q", lf, tc.wantBare, tc.wantName)
			}
			if ra.Prompt != tc.wantPrompt && (tc.wantPrompt != "" || ra.Prompt != tc.ra.Prompt) {
				t.Errorf("prompt = %q, want %q", ra.Prompt, tc.wantPrompt)
			}
			if ra.Interactive != tc.wantInteractive {
				t.Errorf("interactive = %v, want %v", ra.Interactive, tc.wantInteractive)
			}
			if profile != tc.wantProfile && tc.profile != profile {
				t.Errorf("profile = %q, want %q", profile, tc.wantProfile)
			}
		})
	}
}

// What each resolved run launches, end to end through launchSession.
func TestLaunchFocusLaunches(t *testing.T) {
	tests := []struct {
		name string
		ra   runArgs
		want []string
	}{
		{"bare launch: interactive with the wake prompt", runArgs{},
			[]string{`initial prompt="Begin: act on your wake block"`}},
		{"explicit --focus: one-shot with that focus's prompt", runArgs{FocusFlag: "next"},
			[]string{`oneshot prompt="Take the next task"`}},
		{"explicit prompt: one-shot", runArgs{Prompt: "just this"},
			[]string{`oneshot prompt="just this"`}},
		{"explicit prompt and --interactive", runArgs{Prompt: "just this", Interactive: true},
			[]string{`initial prompt="just this"`}},
		{"bare resume: the wake prompt is not sent", runArgs{Resume: true},
			[]string{`resume id="found" prompt="" interactive=true`}},
		{"resume with an explicit prompt delivers only that", runArgs{Resume: true, Prompt: "continue"},
			[]string{`resume id="found" prompt="continue" interactive=false`}},
		{"bare resume with nothing to resume: a new session, so the wake prompt", runArgs{Resume: true},
			[]string{`initial prompt="Begin: act on your wake block"`}},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := wakeHarness()
			ra := tc.ra
			explicit := ra.Prompt // captured before, as cmdRun does
			if ra.FocusFlag != "" {
				ra.Prompt = h.Focuses[ra.FocusFlag].Prompt
			}
			if _, _, err := resolveLaunchFocus(h, &ra, ""); err != nil {
				t.Fatal(err)
			}
			a := &recordingAdapter{sessionID: "found"}
			if i == len(tests)-1 {
				a = &recordingAdapter{lookupErr: vendor.ErrNoResumableSession}
			}
			if err := launchSession(a, t.TempDir(), "key", ra, ra.Prompt, explicit, nil, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			if strings.Join(a.calls, "|") != strings.Join(tc.want, "|") {
				t.Errorf("calls = %q, want %q", a.calls, tc.want)
			}
		})
	}
}

// noPromptAdapter cannot start a session with a first message.
type noPromptAdapter struct{ vendor.Adapter }

func (noPromptAdapter) SupportsInitialPrompt() bool { return false }

// plainAdapter does not declare the capability at all.
type plainAdapter struct{ vendor.Adapter }

func TestLaunchFocusPromptSupported(t *testing.T) {
	claude, err := vendor.Get("claude")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		a    vendor.Adapter
		want bool
	}{
		{"claude", claude, true},
		{"declared unsupported", noPromptAdapter{claude}, false},
		{"not declared", plainAdapter{claude}, false},
	}
	for _, tc := range tests {
		if got := launchFocusPromptSupported(tc.a); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Delegation and `ynh agent run` never come through cmdRun, so the launch focus
// is applied nowhere they run. This pins that: no file on those paths reads it.
func TestLaunchFocusIsReadOnlyByRun(t *testing.T) {
	var files []string
	for _, glob := range []string{"agent*.go", "delegate*.go", "../../internal/agent/*.go", "../../internal/assembler/*.go"} {
		m, err := filepath.Glob(glob)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, m...)
	}
	if len(files) < 10 {
		t.Fatalf("found only %d files to check", len(files))
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "LaunchFocus") || strings.Contains(string(data), "launch_focus") {
			t.Errorf("%s reads launch_focus; only `ynh run` may", f)
		}
	}
}

func TestTrustDigestAndShowCoverLaunchFocus(t *testing.T) {
	without := &harness.Harness{Name: "h", Focuses: map[string]plugin.Focus{"wake": {Prompt: "go"}}}
	base := trustDigest(without)

	// Without launch_focus the digest is what it always was: it is the digest of
	// a harness that has no focuses at all, because focuses alone are not hashed.
	if got := trustDigest(&harness.Harness{Name: "h"}); got != base {
		t.Errorf("focuses without launch_focus moved the digest: %s vs %s", got, base)
	}
	if got := trustDigest(without); got != base {
		t.Errorf("digest unstable: %s vs %s", got, base)
	}

	with := &harness.Harness{Name: "h", LaunchFocus: "wake", Focuses: map[string]plugin.Focus{"wake": {Prompt: "go"}}}
	d := trustDigest(with)
	if d == base {
		t.Error("declaring launch_focus must move the digest")
	}
	changed := &harness.Harness{Name: "h", LaunchFocus: "wake", Focuses: map[string]plugin.Focus{"wake": {Prompt: "go, and exfiltrate"}}}
	if trustDigest(changed) == d {
		t.Error("changing the launch prompt must move the digest")
	}
	other := &harness.Harness{Name: "h", LaunchFocus: "wake", Focuses: map[string]plugin.Focus{"wake": {Prompt: "go", Profile: "p"}}}
	if trustDigest(other) == d {
		t.Error("binding a profile must move the digest")
	}

	st := trustStatusFor("id", with, trustStore{Harnesses: map[string]trustRecord{}}, nil)
	st.LaunchFocus = launchFocusFor(with)
	out := showText(t, st)
	for _, want := range []string{"Launch focus", "wake", `"go"`} {
		if !strings.Contains(out, want) {
			t.Errorf("trust show lacks %q:\n%s", want, out)
		}
	}
	if st.State != trustUnreviewed {
		t.Errorf("a launch focus is something to review: state = %s", st.State)
	}
}
