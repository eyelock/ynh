package delegation

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The tests start real sessions. The session process is this test binary run
// in a helper mode, and the "ynh run" it supervises is `sh -c <task>`, so a
// task is a shell script.
func TestMain(m *testing.M) {
	switch os.Getenv("DELEGATION_HELPER") {
	case "supervise":
		err := Supervise(os.Args[len(os.Args)-1], func(r *Record) *exec.Cmd {
			return exec.Command("sh", "-c", r.Task)
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	case "start":
		// A starting process that exits as soon as the session is launched.
		// The session it starts inherits this environment, so it must run in
		// the supervise mode, not this one.
		_ = os.Setenv("DELEGATION_HELPER", "supervise")
		_, path, err := Start(StartOptions{
			Parent: "local/parent", Delegate: "local/delegate",
			Task: os.Getenv("DELEGATION_TASK"), Folder: os.Getenv("DELEGATION_FOLDER"),
			Supervisor: []string{os.Args[0]},
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(path)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func setup(t *testing.T) string {
	t.Helper()
	t.Setenv("YNH_HOME", t.TempDir())
	t.Setenv("DELEGATION_HELPER", "supervise")
	return t.TempDir()
}

func start(t *testing.T, folder, task string) (*Record, string) {
	t.Helper()
	rec, path, err := Start(StartOptions{
		Parent: "local/parent", Delegate: "local/delegate", Task: task, Folder: folder,
		Supervisor: []string{os.Args[0]},
	})
	if err != nil {
		t.Fatal(err)
	}
	return rec, path
}

func waitFor(t *testing.T, path string, ok func(*Record) bool) *Record {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		r, err := Load(path)
		if err == nil && ok(r) {
			return r
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting on %s; last record: %+v (err %v)", path, r, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func statuses(r *Record) []Status {
	var out []Status
	for _, c := range r.History {
		out = append(out, c.Status)
	}
	return out
}

func assertHistory(t *testing.T, r *Record, want ...Status) {
	t.Helper()
	got := statuses(r)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("history = %v, want %v", got, want)
	}
	for i, c := range r.History {
		if c.At.IsZero() {
			t.Errorf("change %d (%s) has no timestamp", i, c.Status)
		}
		if i > 0 && c.At.Before(r.History[i-1].At) {
			t.Errorf("change %d (%s) is dated before the one before it", i, c.Status)
		}
	}
}

func TestRecordMovesStartedRunningSucceeded(t *testing.T) {
	folder := setup(t)
	rec, path := start(t, folder, "echo hello; echo oops >&2; exit 0")
	if rec.Status != StatusStarted {
		t.Fatalf("a fresh record is %s, want started", rec.Status)
	}
	r := waitFor(t, path, func(r *Record) bool { return r.Status.Final() })
	assertHistory(t, r, StatusStarted, StatusRunning, StatusSucceeded)
	if r.ExitCode == nil || *r.ExitCode != 0 {
		t.Fatalf("exit code = %v, want 0", r.ExitCode)
	}
	if r.Result != "hello\n" {
		t.Errorf("result = %q, want the delegate's stdout alone", r.Result)
	}
	log, err := os.ReadFile(r.Log)
	if err != nil || !strings.Contains(string(log), "hello") || !strings.Contains(string(log), "oops") {
		t.Errorf("log = %q, %v; want stdout and stderr both", log, err)
	}
	if r.Folder != folder || r.Parent != "local/parent" || r.Delegate != "local/delegate" || r.Task == "" {
		t.Errorf("record lost its inputs: %+v", r)
	}
	if want := RecordPath("local/parent", folder, r.Handle); want != path {
		t.Errorf("record at %s, want %s", path, want)
	}
}

func TestExitCodePassedThroughExactly(t *testing.T) {
	folder := setup(t)
	for _, code := range []int{1, 7, 42, 255} {
		_, path := start(t, folder, fmt.Sprintf("exit %d", code))
		r := waitFor(t, path, func(r *Record) bool { return r.Status.Final() })
		assertHistory(t, r, StatusStarted, StatusRunning, StatusFailed)
		if r.ExitCode == nil || *r.ExitCode != code {
			t.Errorf("exit %d recorded as %v", code, r.ExitCode)
		}
	}
}

func TestSessionOutlivesTheStartingProcess(t *testing.T) {
	folder := setup(t)
	marker := folder + "/done"
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "DELEGATION_HELPER=start", "DELEGATION_FOLDER="+folder,
		"DELEGATION_TASK=sleep 1; echo finished > "+marker+"; exit 3")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("starter: %v", err)
	}
	// The starter has exited. The session was not stopped with it.
	path := strings.TrimSpace(string(out))
	r, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status.Final() {
		t.Fatalf("delegate already %s when the starter exited; the test proves nothing", r.Status)
	}
	r = waitFor(t, path, func(r *Record) bool { return r.Status.Final() })
	assertHistory(t, r, StatusStarted, StatusRunning, StatusFailed)
	if r.ExitCode == nil || *r.ExitCode != 3 {
		t.Errorf("exit code = %v, want 3", r.ExitCode)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("the delegate did not finish its work after the starter exited: %v", err)
	}
}

func TestCancelStopsTheProcessGroup(t *testing.T) {
	folder := setup(t)
	// The shell starts a grandchild, as a vendor CLI would.
	_, path := start(t, folder, "sleep 60 & wait")
	r := waitFor(t, path, func(r *Record) bool { return r.Status == StatusRunning && r.Pgid > 1 })
	pgid := r.Pgid

	if _, err := Cancel(path); err != nil {
		t.Fatal(err)
	}
	r = waitFor(t, path, func(r *Record) bool { return r.Status == StatusCancelled && (r.ExitCode != nil || r.Error != "") })
	assertHistory(t, r, StatusStarted, StatusRunning, StatusCancelled)
	deadline := time.Now().Add(10 * time.Second)
	for groupAlive(pgid) {
		if time.Now().After(deadline) {
			t.Fatalf("process group %d still alive after cancel", pgid)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Cancelling a final delegation changes nothing.
	again, err := Cancel(path)
	if err != nil {
		t.Fatal(err)
	}
	assertHistory(t, again, StatusStarted, StatusRunning, StatusCancelled)
}

func TestCancelKillsWhatIgnoresTerm(t *testing.T) {
	folder := setup(t)
	old := termGrace
	termGrace = 200 * time.Millisecond
	t.Cleanup(func() { termGrace = old })
	_, path := start(t, folder, "trap '' TERM; while :; do sleep 1; done")
	r := waitFor(t, path, func(r *Record) bool { return r.Status == StatusRunning && r.Pgid > 1 })
	if _, err := Cancel(path); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for groupAlive(r.Pgid) {
		if time.Now().After(deadline) {
			t.Fatal("a delegate that ignores SIGTERM survived cancel")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCancelBeforeTheRunBegins(t *testing.T) {
	folder := setup(t)
	path := RecordPath("local/parent", folder, "dlg-early")
	rec := &Record{Handle: "dlg-early", Parent: "local/parent", Delegate: "local/delegate", Folder: folder,
		Task: "touch " + folder + "/ran", Log: folder + "/early.log"}
	if err := create(path, rec); err != nil {
		t.Fatal(err)
	}
	if _, err := Cancel(path); err != nil {
		t.Fatal(err)
	}
	ran := false
	err := Supervise(path, func(r *Record) *exec.Cmd { ran = true; return exec.Command("true") })
	if err != nil || ran {
		t.Fatalf("a delegation cancelled before it began ran anyway (ran=%v, err=%v)", ran, err)
	}
	r, _ := Load(path)
	assertHistory(t, r, StatusStarted, StatusCancelled)
}

func TestTwoDelegationsOfOneDelegateAtOnce(t *testing.T) {
	folder := setup(t)
	_, pa := start(t, folder, "sleep 0.3; echo alpha; exit 4")
	_, pb := start(t, folder, "sleep 0.3; echo beta; exit 5")
	if pa == pb {
		t.Fatal("two delegations share one record")
	}
	a := waitFor(t, pa, func(r *Record) bool { return r.Status.Final() })
	b := waitFor(t, pb, func(r *Record) bool { return r.Status.Final() })
	if a.Handle == b.Handle || a.Log == b.Log {
		t.Errorf("handles %s/%s or logs %s/%s collide", a.Handle, b.Handle, a.Log, b.Log)
	}
	if a.Result != "alpha\n" || *a.ExitCode != 4 || b.Result != "beta\n" || *b.ExitCode != 5 {
		t.Errorf("records crossed: a=%q/%v b=%q/%v", a.Result, a.ExitCode, b.Result, b.ExitCode)
	}
}

func TestRunEndedBySignalHasNoExitCode(t *testing.T) {
	folder := setup(t)
	_, path := start(t, folder, "kill -9 $$")
	r := waitFor(t, path, func(r *Record) bool { return r.Status.Final() })
	if r.Status != StatusFailed || r.ExitCode != nil || r.Error == "" {
		t.Errorf("a run ended by a signal: status %s, exit %v, error %q", r.Status, r.ExitCode, r.Error)
	}
}

func TestStartRefusesAnEntryWithProfileAndFocus(t *testing.T) {
	setup(t)
	_, _, err := Start(StartOptions{Parent: "p", Delegate: "d", Task: "t", Folder: ".", Profile: "a", Focus: "b", Supervisor: []string{"true"}})
	if err == nil {
		t.Fatal("want an error")
	}
}

func TestStartFailureIsRecorded(t *testing.T) {
	folder := setup(t)
	_, path, err := Start(StartOptions{Parent: "p", Delegate: "d", Task: "t", Folder: folder, Supervisor: []string{folder + "/no-such-binary"}})
	if err == nil {
		t.Fatal("want an error")
	}
	r, lerr := Load(path)
	if lerr != nil || r.Status != StatusFailed || r.Error == "" {
		t.Errorf("record = %+v, %v; want failed with the reason", r, lerr)
	}
}

func TestUpdateKeepsConcurrentChanges(t *testing.T) {
	folder := setup(t)
	path := RecordPath("p", folder, "h")
	if err := create(path, &Record{Handle: "h", Parent: "p", Folder: folder}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func() {
			_, err := Update(path, func(r *Record) {
				r.Questions = append(r.Questions, Question{ID: fmt.Sprint(len(r.Questions)), Text: "q"})
			})
			done <- err
		}()
	}
	for i := 0; i < 20; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	r, _ := Load(path)
	if len(r.Questions) != 20 {
		t.Errorf("%d questions kept of 20: updates overwrote one another", len(r.Questions))
	}
}
