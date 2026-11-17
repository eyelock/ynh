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
