package presence

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func home(t *testing.T) {
	t.Helper()
	t.Setenv("YNH_HOME", t.TempDir())
}

func find(es []Entry, id string) (Entry, bool) {
	for _, e := range es {
		if e.SessionID == id {
			return e, true
		}
	}
	return Entry{}, false
}

func TestOwnProcessIsAlive(t *testing.T) {
	home(t)
	s, err := Start(Record{SessionID: "a", Harness: "h", Vendor: "claude", Mode: ModeInteractive})
	if err != nil {
		t.Fatal(err)
	}
	s.SetVendorPID(4242)
	es, _ := List(false)
	e, ok := find(es, "a")
	if !ok || e.Status != StatusLive || e.VendorPID != 4242 || e.Address != "ynh:a" || e.State != StateAlive {
		t.Fatalf("got %+v ok=%v", e, ok)
	}
	s.End()
	if es, _ = List(false); len(es) != 0 {
		t.Fatalf("ended session listed by default: %+v", es)
	}
	es, _ = List(true)
	if e, ok = find(es, "a"); !ok || e.Status != StatusEnded {
		t.Fatalf("want ended in --all, got %+v", e)
	}
}

func TestKilledSessionReadsDead(t *testing.T) {
	home(t)
	cmd := exec.Command(os.Args[0], "-test.run=NONE", "-test.timeout=1m")
	cmd.Env = append(os.Environ(), "PRESENCE_SLEEP=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	start, err := ProcessStart(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	r := Record{SessionID: "k", WrapperPID: cmd.Process.Pid, StartTime: start, Mode: ModeInteractive, LastBeat: time.Now()}
	if err := Save(r); err != nil {
		t.Fatal(err)
	}
	if !Alive(r) {
		t.Fatal("running process should be alive")
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	es, _ := List(true)
	if e, _ := find(es, "k"); e.Status != StatusDead {
		t.Fatalf("killed session should be dead, got %q", e.Status)
	}
	if es, _ = List(false); len(es) != 0 {
		t.Fatalf("dead session listed by default: %+v", es)
	}
}

func TestReusedPIDReadsDead(t *testing.T) {
	home(t)
	start, err := ProcessStart(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	// Our pid, but a different start time: a reused pid.
	r := Record{SessionID: "r", WrapperPID: os.Getpid(), StartTime: start - 1000, Mode: ModeInteractive}
	_ = Save(r)
	if Alive(r) {
		t.Fatal("a pid with a different start time must not read as alive")
	}
}

func TestOrphanedDelegate(t *testing.T) {
	home(t)
	start, _ := ProcessStart(os.Getpid())
	_ = Save(Record{SessionID: "d", WrapperPID: os.Getpid(), StartTime: start, Mode: ModeDelegate, OwnerSession: "gone"})
	_ = Save(Record{SessionID: "d2", WrapperPID: os.Getpid(), StartTime: start, Mode: ModeDelegate, OwnerSession: "o"})
	_ = Save(Record{SessionID: "o", WrapperPID: os.Getpid(), StartTime: start, Mode: ModeInteractive})
	es, _ := List(false)
	if e, _ := find(es, "d"); e.Status != StatusOrphaned {
		t.Fatalf("d: %q", e.Status)
	}
	if e, _ := find(es, "d2"); e.Status != StatusLive {
		t.Fatalf("d2: %q", e.Status)
	}
}

func TestInvalidIDAndEmptyDir(t *testing.T) {
	home(t)
	if err := Save(Record{SessionID: "../x"}); err == nil {
		t.Fatal("want error")
	}
	if es, err := List(false); err != nil || es == nil || len(es) != 0 {
		t.Fatalf("empty list should be [] not nil: %v %v", es, err)
	}
}

// The heartbeat and SetVendorPID share one record: under -race they must not
// collide, and once SetVendorPID has returned no later beat may drop the pid.
func TestSetVendorPIDDuringHeartbeat(t *testing.T) {
	home(t)
	s, err := Start(Record{SessionID: "race", Harness: "h", Vendor: "claude", Mode: ModeInteractive})
	if err != nil {
		t.Fatal(err)
	}
	// Replace the 15s heartbeat with a 1ms one.
	close(s.stop)
	<-s.done
	s.stop = make(chan struct{})
	s.done = make(chan struct{})
	go s.beat(time.Millisecond)

	for i := 1; i <= 200; i++ {
		s.SetVendorPID(1000 + i)
		time.Sleep(200 * time.Microsecond)
	}
	time.Sleep(20 * time.Millisecond) // let several more beats land
	s.End()

	es, _ := List(true)
	e, ok := find(es, "race")
	if !ok || e.VendorPID != 1200 {
		t.Fatalf("vendor_pid lost or stale: %+v ok=%v", e, ok)
	}
}

// The files kept beside a session's record go when the session ends, or when its
// dead record is removed: none is kept for ever.
func TestSideFilesGoWithTheSession(t *testing.T) {
	home(t)
	s, err := Start(Record{SessionID: "side", Harness: "h", Vendor: "claude", Mode: ModeInteractive})
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range SideFileSuffixes {
		if err := os.WriteFile(Dir()+"/side"+suffix, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s.End()
	for _, suffix := range SideFileSuffixes {
		if _, err := os.Stat(Dir() + "/side" + suffix); err == nil {
			t.Errorf("side%s outlived its session", suffix)
		}
	}
	// A record removed from history takes any left-behind side file with it (a
	// session that was killed before it could End).
	if err := Save(Record{SessionID: "dead", Harness: "h", Vendor: "claude", Mode: ModeInteractive, Ended: true}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Dir()+"/dead.announced", []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Remove(Entry{Record: Record{SessionID: "dead", Ended: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Dir() + "/dead.announced"); err == nil {
		t.Error("dead.announced outlived its removed record")
	}
}
