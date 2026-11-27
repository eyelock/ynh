package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eyelock/ynh/internal/delegation"
	"github.com/eyelock/ynh/internal/delegation/delegationtest"
	"github.com/eyelock/ynh/internal/presence"
)

func TestPsListsLiveSessionsAsJSON(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	s, err := presence.Start(presence.Record{SessionID: "s1", Harness: "local/seat", Vendor: "codex", Mode: presence.ModeOneShot, Cwd: "/w"})
	if err != nil {
		t.Fatal(err)
	}
	s.SetVendorPID(77)
	var out, errb bytes.Buffer
	if err := cmdPsTo([]string{"--json"}, &out, &errb); err != nil {
		t.Fatal(err)
	}
	var res psResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("%v: %s", err, out.String())
	}
	if len(res.Sessions) != 1 {
		t.Fatalf("got %+v", res)
	}
	e := res.Sessions[0]
	if e.Status != "live" || e.Mode != "one-shot" || e.Vendor != "codex" || e.VendorPID != 77 || e.WrapperPID != os.Getpid() || e.StartedAt.IsZero() {
		t.Errorf("entry = %+v", e)
	}
	s.End()
	out.Reset()
	_ = cmdPsTo([]string{"--format", "json"}, &out, &errb)
	if !strings.Contains(out.String(), `"sessions": []`) {
		t.Errorf("ended session should be hidden: %s", out.String())
	}
	out.Reset()
	_ = cmdPsTo([]string{"--all", "--json"}, &out, &errb)
	if !strings.Contains(out.String(), `"ended"`) {
		t.Errorf("--all should show history: %s", out.String())
	}
}

func TestPsTextAndBadFlag(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	var out, errb bytes.Buffer
	if err := cmdPsTo(nil, &out, &errb); err != nil || !strings.Contains(out.String(), "No live sessions") {
		t.Fatalf("%v %q", err, out.String())
	}
	if err := cmdPsTo([]string{"--bogus"}, &out, &errb); err == nil {
		t.Error("unknown flag should fail")
	}
}

func TestPsListsOpenQuestionsWithAgeAgainstTheOwningSession(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	t.Setenv("YNH_TEST_SUPERVISE", "1")
	cwd, _ := os.Getwd()
	s, err := presence.Start(presence.Record{SessionID: "seat1", Harness: "seat", Vendor: "claude", Mode: presence.ModeInteractive, Cwd: cwd})
	if err != nil {
		t.Fatal(err)
	}
	defer s.End()
	rec, _, err := delegation.Start(delegation.StartOptions{Parent: "local/seat", Delegate: "local/reviewer", Task: "sleep 30", Folder: cwd, Supervisor: []string{os.Args[0]}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { delegationtest.StopAll(t) })
	path := delegation.RecordPath("local/seat", cwd, rec.Handle)
	asked := time.Now().UTC().Add(-3 * time.Minute)
	if _, err := delegation.Update(path, func(r *delegation.Record) {
		r.Questions = []delegation.Question{{ID: "q1", Text: "which branch?", AskedAt: asked}}
	}); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if err := cmdPsTo([]string{"--json"}, &out, &errb); err != nil {
		t.Fatal(err)
	}
	var res psResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	var seat *psSession
	for i := range res.Sessions {
		if res.Sessions[i].SessionID == "seat1" {
			seat = &res.Sessions[i]
		}
	}
	if seat == nil || len(seat.OpenQuestions) != 1 {
		t.Fatalf("sessions = %+v", res.Sessions)
	}
	q := seat.OpenQuestions[0]
	if q.Delegate != "reviewer" || q.Handle != rec.Handle || q.Question != "which branch?" || q.WaitingSeconds < 179 {
		t.Errorf("question = %+v", q)
	}
	out.Reset()
	if err := cmdPsTo(nil, &out, &errb); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "reviewer asks (waiting 3m") || !strings.Contains(out.String(), "which branch?") {
		t.Errorf("text output lacks the question:\n%s", out.String())
	}
	// A question answered is no longer listed.
	if _, err := delegation.Update(path, func(r *delegation.Record) {
		a := "main"
		r.Questions[0].Answer = &a
	}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	_ = cmdPsTo([]string{"--json"}, &out, &errb)
	if strings.Contains(out.String(), "open_questions") {
		t.Errorf("an answered question is still listed: %s", out.String())
	}
}
