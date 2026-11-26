package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/presence"
)

func startLive(t *testing.T, harness string) *presence.Session {
	t.Helper()
	s, err := presence.Start(presence.Record{SessionID: "s-" + harness + "-" + t.Name(), Harness: harness, Vendor: "claude", Cwd: t.TempDir(), Mode: presence.ModeInteractive})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOnLiveRefuseWarnAllow(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	var buf bytes.Buffer

	// Nothing live: every mode starts, silently.
	for _, m := range []string{"", "refuse", "allow"} {
		if err := checkOnLive("local--seat", "local/seat", m, "", &buf); err != nil || buf.Len() != 0 {
			t.Fatalf("mode %q with nothing live: err %v, stderr %q", m, err, buf.String())
		}
	}

	defer startLive(t, "local--seat").End()

	// Unset: warns, names the session's address, and starts.
	if err := checkOnLive("local--seat", "local/seat", "", "", &buf); err != nil {
		t.Fatalf("unset harness refused: %v", err)
	}
	if out := buf.String(); !strings.Contains(out, "warning:") || !strings.Contains(out, "ynh:s-local--seat") {
		t.Errorf("warning does not name the live session: %q", out)
	}

	// The harness's own setting refuses, with the address and a Fix line.
	err := checkOnLive("local--seat", "local/seat", "refuse", "", &buf)
	if err == nil || !strings.Contains(err.Error(), "ynh:s-local--seat") || !strings.Contains(err.Error(), "Fix:") {
		t.Fatalf("refuse: %v", err)
	}

	// --on-live=allow overrides the harness, and starts without a word.
	buf.Reset()
	if err := checkOnLive("local--seat", "local/seat", "refuse", "allow", &buf); err != nil || buf.Len() != 0 {
		t.Errorf("allow: err %v, stderr %q", err, buf.String())
	}
	// --on-live=refuse overrides an unset harness.
	if err := checkOnLive("local--seat", "local/seat", "", "refuse", &buf); err == nil {
		t.Error("--on-live=refuse did not refuse")
	}

	// Another harness is not blocked, and a bad value is refused.
	if err := checkOnLive("local--other", "local/other", "refuse", "", &buf); err != nil {
		t.Errorf("another harness refused: %v", err)
	}
	if err := checkOnLive("local--seat", "local/seat", "", "maybe", &buf); err == nil {
		t.Error("an invalid --on-live was accepted")
	}
}

// A crashed session (a record whose wrapper is gone) and an ended one do not
// block a new run.
func TestOnLiveIgnoresDeadAndEndedSessions(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	if err := presence.Save(presence.Record{SessionID: "dead", Harness: "local--seat", WrapperPID: 2147483000, StartTime: 1, Cwd: "/x", Mode: presence.ModeInteractive}); err != nil {
		t.Fatal(err)
	}
	s := startLive(t, "local--seat")
	s.End()
	if err := checkOnLive("local--seat", "local/seat", "refuse", "", &bytes.Buffer{}); err != nil {
		t.Errorf("a dead or ended session blocked the run: %v", err)
	}
}

func TestParseRunArgsOnLive(t *testing.T) {
	for _, args := range [][]string{{"seat", "--on-live=allow", "--", "hi"}, {"seat", "--on-live", "allow", "--", "hi"}} {
		ra := parseRunArgs(args)
		if ra.OnLive != "allow" || ra.HarnessName != "seat" || ra.Prompt != "hi" || len(ra.VendorArgs) != 0 {
			t.Errorf("%v: %+v", args, ra)
		}
	}
}
