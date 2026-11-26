package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

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
