package main

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/eyelock/ynh/internal/presence"
)

// sleeper starts a process that stands in for a session's wrapper, and records
// it in presence the way ynh run does.
func sleeper(t *testing.T, id, harness, mode, owner string) {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Skipf("sleep unavailable: %v", err)
	}
	go func() { _ = cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	start, err := presence.ProcessStart(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := presence.Save(presence.Record{
		SessionID: id, Harness: harness, Vendor: "claude", WrapperPID: cmd.Process.Pid, StartTime: start,
		StartedAt: time.Now(), Cwd: "/x", Mode: mode, OwnerSession: owner, Address: presence.Address(id),
		State: presence.StateAlive, LastBeat: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
}

func liveIDs(t *testing.T) map[string]bool {
	t.Helper()
	es, err := presence.List(false)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]bool{}
	for _, e := range es {
		m[e.SessionID] = true
	}
	return m
}

func TestStopOneSessionByIDOrAddress(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	stopWait = 5 * time.Second
	sleeper(t, "one", "local--a", presence.ModeInteractive, "")
	sleeper(t, "two", "local--b", presence.ModeInteractive, "")
	var out, errb bytes.Buffer
	if err := cmdStopTo([]string{"ynh:one"}, &out, &errb); err != nil {
		t.Fatalf("%v (%s)", err, errb.String())
	}
	if !strings.Contains(out.String(), "Stopped ynh:one") {
		t.Errorf("output: %q", out.String())
	}
	if got := liveIDs(t); got["one"] || !got["two"] {
		t.Errorf("live after stopping one: %v", got)
	}
	// A session that is gone is refused with a Fix line, and an unknown one is named.
	if err := cmdStopTo([]string{"one"}, &out, &errb); err == nil {
		t.Error("stopping a dead session was accepted")
	}
	if err := cmdStopTo([]string{"nope"}, &out, &errb); err == nil {
		t.Error("an unknown session was accepted")
	}
	if err := cmdStopTo(nil, &out, &errb); err == nil {
		t.Error("no session was accepted")
	}
}

func TestStopOwnerStopsEveryDelegateItStarted(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	sleeper(t, "desk", "local--desk", presence.ModeInteractive, "")
	sleeper(t, "d1", "local--x", presence.ModeDelegate, "desk")
	sleeper(t, "d2", "local--y", presence.ModeDelegate, "desk")
	sleeper(t, "other", "local--z", presence.ModeDelegate, "elsewhere")
	var out, errb bytes.Buffer
	if err := cmdStopTo([]string{"--owner", "desk"}, &out, &errb); err != nil {
		t.Fatalf("%v (%s)", err, errb.String())
	}
	got := liveIDs(t)
	if got["d1"] || got["d2"] || !got["desk"] || !got["other"] {
		t.Errorf("live after --owner desk: %v", got)
	}
}

// After the owner is gone, ynh ps lists the delegate as orphaned and ynh reap
// stops it; --history also clears the dead and ended records.
func TestReapStopsOrphansAndClearsHistory(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	sleeper(t, "orphan", "local--x", presence.ModeDelegate, "gone-owner")
	sleeper(t, "desk", "local--desk", presence.ModeInteractive, "")
	sleeper(t, "kept", "local--k", presence.ModeDelegate, "desk")
	if err := presence.Save(presence.Record{SessionID: "old", Harness: "local--o", WrapperPID: 2147483000, StartTime: 1, Cwd: "/x", Mode: presence.ModeInteractive}); err != nil {
		t.Fatal(err)
	}

	es, _ := presence.List(false)
	var orphaned bool
	for _, e := range es {
		if e.SessionID == "orphan" && e.Status == presence.StatusOrphaned {
			orphaned = true
		}
	}
	if !orphaned {
		t.Fatalf("the delegate of a gone owner is not listed as orphaned: %+v", es)
	}

	var out, errb bytes.Buffer
	if err := cmdReapTo([]string{"--dry-run"}, &out, &errb); err != nil || !strings.Contains(out.String(), "would stop ynh:orphan") || !liveIDs(t)["orphan"] {
		t.Fatalf("dry run: err %v, out %q, live %v", err, out.String(), liveIDs(t))
	}
	out.Reset()
	if err := cmdReapTo(nil, &out, &errb); err != nil {
		t.Fatalf("%v (%s)", err, errb.String())
	}
	got := liveIDs(t)
	if got["orphan"] || !got["desk"] || !got["kept"] {
		t.Errorf("live after reap: %v", got)
	}
	all, _ := presence.List(true)
	if len(all) != 4 {
		t.Errorf("reap without --history removed records: %d left", len(all))
	}
	out.Reset()
	if err := cmdReapTo([]string{"--history"}, &out, &errb); err != nil {
		t.Fatal(err)
	}
	all, _ = presence.List(true)
	for _, e := range all {
		if e.Status == presence.StatusDead || e.Status == presence.StatusEnded {
			t.Errorf("history record %s survived --history", e.SessionID)
		}
	}
	if !liveIDs(t)["desk"] {
		t.Error("--history touched a live session")
	}
}

// A recorded vendor pid that now belongs to another process is never
// signalled: the stop goes through the wrapper, whose identity is checked.
func TestStopNeverSignalsAReusedVendorPID(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	stopWait = 5 * time.Second
	// The stand-in for a process that is not the vendor but whose pid the
	// record names (the vendor's pid was reused).
	bystander := exec.Command("sleep", "60")
	if err := bystander.Start(); err != nil {
		t.Skipf("sleep unavailable: %v", err)
	}
	exited := make(chan struct{})
	go func() { _ = bystander.Wait(); close(exited) }()
	t.Cleanup(func() { _ = bystander.Process.Kill() })

	sleeper(t, "s1", "local--a", presence.ModeInteractive, "")
	e, err := presence.Find("s1")
	if err != nil {
		t.Fatal(err)
	}
	// Name the bystander as the vendor, with a start time that is not its own.
	start, _ := presence.ProcessStart(bystander.Process.Pid)
	e.VendorPID, e.VendorStart = bystander.Process.Pid, start+12345
	if err := presence.Save(e.Record); err != nil {
		t.Fatal(err)
	}
	e, _ = presence.Find("s1")

	var out, errb bytes.Buffer
	if err := cmdStopTo([]string{"s1"}, &out, &errb); err != nil {
		t.Fatalf("%v (%s)", err, errb.String())
	}
	if liveIDs(t)["s1"] {
		t.Error("the session was not stopped through its wrapper")
	}
	if now, err := presence.ProcessStart(bystander.Process.Pid); err != nil || now != start {
		t.Fatalf("the bystander was disturbed: start %v err %v", now, err)
	}
	// Read the Wait goroutine's result only through the channel it closes:
	// bystander.ProcessState is written by that goroutine.
	select {
	case <-exited:
		t.Error("the bystander process ended")
	default:
	}
	// And a vendor whose start does match is the one signalled.
	sleeper(t, "s2", "local--b", presence.ModeInteractive, "")
	v := exec.Command("sleep", "60")
	if err := v.Start(); err != nil {
		t.Skip(err)
	}
	go func() { _ = v.Wait() }()
	t.Cleanup(func() { _ = v.Process.Kill() })
	vs, _ := presence.ProcessStart(v.Process.Pid)
	e2, _ := presence.Find("s2")
	e2.VendorPID, e2.VendorStart = v.Process.Pid, vs
	if err := presence.Save(e2.Record); err != nil {
		t.Fatal(err)
	}
	e2, _ = presence.Find("s2")
	if got := presence.StopTarget(e2.Record); got != v.Process.Pid {
		t.Errorf("a matching vendor is not the target: %d, want %d", got, v.Process.Pid)
	}
}
