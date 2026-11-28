package a2aserve

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"

	"github.com/eyelock/ynh/internal/delegation"
	"github.com/eyelock/ynh/internal/permission"
	"github.com/eyelock/ynh/internal/presence"
)

// liveSession registers an interactive session of local/<name> that accepts
// tasks, as `ynh run` does, and ends it when the test does.
func liveSession(t *testing.T, id, name string) (func(), presence.Entry) {
	t.Helper()
	s, err := presence.Start(presence.Record{SessionID: id, Harness: "local--" + name, Vendor: "claude", Cwd: "/work/" + id, Mode: presence.ModeInteractive, AcceptsTasks: true})
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	end := func() { once.Do(s.End) }
	t.Cleanup(end)
	live := delegation.LiveSessionsOf("local/" + name)
	for _, e := range live {
		if e.SessionID == id {
			return end, e
		}
	}
	t.Fatalf("session %s not found live: %v", id, live)
	return end, presence.Entry{}
}

func openFor(t *testing.T, folder, name, parent string, mutate func(*Options)) *Endpoint {
	t.Helper()
	o := Options{Name: name, Label: "github.com/x/" + name, Parent: parent, Delegate: "local/" + name, Folder: folder, Supervisor: []string{os.Args[0]}}
	if mutate != nil {
		mutate(&o)
	}
	e, err := Open(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	return e
}

func recordOf(t *testing.T, task *a2a.Task) (*delegation.Record, string) {
	t.Helper()
	handle, _ := task.Metadata[MetaHandle].(string)
	path, err := delegation.FindHandle(handle)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := delegation.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return rec, path
}

// A live interactive session of the delegate gets the task, and no second
// process is started: the task waits in its inbox, is taken there and its
// result goes back to the sender.
func TestLiveSessionTakesTheTaskAndNoSecondProcessStarts(t *testing.T) {
	folder := setup(t)
	_, me := liveSession(t, "eno-1", "eno")
	e := openFor(t, folder, "eno", "local/parent", nil)
	c, ctx := client(t, e, e.Token)

	task := send(t, c, ctx, "", "echo should-not-run", true)
	if task.Metadata[MetaRoute] != "live" || task.Metadata[MetaSession] != "eno-1" || task.Metadata[MetaCwd] != "/work/eno-1" {
		t.Fatalf("metadata = %v, want route live, session eno-1 and its cwd", task.Metadata)
	}
	rec, path := recordOf(t, task)
	if rec.Status != delegation.StatusQueued || rec.Pid != 0 || rec.Pgid != 0 {
		t.Fatalf("record = %s pid %d: want queued and no process", rec.Status, rec.Pid)
	}
	inbox, err := delegation.Inbox("local/eno")
	if err != nil || len(inbox) != 1 || inbox[0].Handle != rec.Handle {
		t.Fatalf("inbox = %v, %v", inbox, err)
	}
	if got := waitState(t, c, ctx, task.ID, a2a.TaskStateSubmitted); got == nil {
		t.Fatal("task not submitted")
	}

	if _, err := delegation.Take(path, me); err != nil {
		t.Fatal(err)
	}
	if _, err := delegation.Done(path, "eno-1", "reviewed: fine"); err != nil {
		t.Fatal(err)
	}
	done := waitState(t, c, ctx, task.ID, a2a.TaskStateCompleted)
	// The task is completed by the record, which the executor followed; a status
	// read after the executor has gone sees the record too.
	if rec, _ := delegation.Load(path); rec.Result != "reviewed: fine" || rec.Session != "eno-1" {
		t.Fatalf("record = %+v", rec)
	}
	_ = done
	if n := len(records(t)); n != 1 {
		t.Fatalf("%d delegation records, want 1", n)
	}
}

// With no live session of the harness, the task goes headless, as it did.
func TestNoLiveSessionGoesHeadless(t *testing.T) {
	folder := setup(t)
	e := openFor(t, folder, "eno", "local/parent", nil)
	c, ctx := client(t, e, e.Token)

	task := send(t, c, ctx, "", "echo hello-headless", false)
	if task.Status.State != a2a.TaskStateCompleted || artifactText(task) == "" {
		t.Fatalf("state %s, artifact %q", task.Status.State, artifactText(task))
	}
	rec, _ := recordOf(t, task)
	if rec.Route != delegation.RouteHeadless || rec.Pid == 0 {
		t.Fatalf("route %q pid %d, want a headless session", rec.Route, rec.Pid)
	}
}

// A live session that is not accepting tasks (a delegate's own run, a one-shot
// run) is not a taker.
func TestASessionThatDoesNotAcceptTasksIsNotLive(t *testing.T) {
	folder := setup(t)
	s, err := presence.Start(presence.Record{SessionID: "x", Harness: "local--eno", Vendor: "opencode", Cwd: "/w", Mode: presence.ModeInteractive,
		NoTasksReason: "opencode's generated plugin drops hook output, so a live session cannot be told a task is waiting"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.End)
	e := openFor(t, folder, "eno", "local/parent", nil)
	c, ctx := client(t, e, e.Token)
	task := send(t, c, ctx, "", "echo hi", false)
	rec, _ := recordOf(t, task)
	if rec.Route != delegation.RouteHeadless {
		t.Fatalf("route %q, want headless", rec.Route)
	}
	// The task went headless although a session of the harness was open, and
	// the record, the task metadata and the result say why.
	if !strings.Contains(rec.RouteReason, "drops hook output") || !strings.Contains(fmt.Sprint(RouteMeta(rec)[MetaRouteReason]), "drops hook output") {
		t.Errorf("route reason = %q", rec.RouteReason)
	}
}

// on_live headless sends the task to a session of its own even with a live one.
func TestOnLiveHeadlessSkipsTheLiveSession(t *testing.T) {
	folder := setup(t)
	liveSession(t, "eno-1", "eno")
	e := openFor(t, folder, "eno", "local/parent", func(o *Options) { o.OnLive = "headless" })
	c, ctx := client(t, e, e.Token)
	task := send(t, c, ctx, "", "echo hi", false)
	if rec, _ := recordOf(t, task); rec.Route != delegation.RouteHeadless {
		t.Fatalf("route %q, want headless", rec.Route)
	}
	// And per call.
	e2 := openFor(t, folder, "eno", "local/parent2", nil)
	c2, ctx2 := client(t, e2, e2.Token)
	m := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("echo hi"))
	m.Metadata = map[string]any{MetaOnLive: "headless"}
	r, err := c2.SendMessage(ctx2, &a2a.SendMessageRequest{Message: m})
	if err != nil {
		t.Fatal(err)
	}
	if rec, _ := recordOf(t, r.(*a2a.Task)); rec.Route != delegation.RouteHeadless {
		t.Fatalf("per-call route %q, want headless", rec.Route)
	}
}

// Queued, and the live session exits before taking it: the next status read
// starts the headless session with the same task. Nothing is lost.
func TestQueueThenExitFallsBackToHeadless(t *testing.T) {
	folder := setup(t)
	end, _ := liveSession(t, "eno-1", "eno")
	e := openFor(t, folder, "eno", "local/parent", nil)
	c, ctx := client(t, e, e.Token)
	task := send(t, c, ctx, "", "echo picked-up-headless", true)
	rec, path := recordOf(t, task)
	if rec.Status != delegation.StatusQueued {
		t.Fatalf("status %s, want queued", rec.Status)
	}
	end()

	deadline := time.Now().Add(15 * time.Second)
	for {
		rec, err := delegation.Reconcile(path)
		if err != nil {
			t.Fatal(err)
		}
		if rec.Status == delegation.StatusSucceeded {
			if rec.Route != delegation.RouteHeadless || rec.Task != "echo picked-up-headless" || rec.Result == "" {
				t.Fatalf("record = %+v", rec)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("status stayed %s", rec.Status)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got, _ := delegation.Inbox("local/eno"); len(got) != 0 {
		t.Fatalf("inbox still holds %d task(s)", len(got))
	}
}

// Taken, and the session dies: failed, to be resent, never re-run.
func TestTakenThenSessionDiesFails(t *testing.T) {
	folder := setup(t)
	end, me := liveSession(t, "eno-1", "eno")
	e := openFor(t, folder, "eno", "local/parent", nil)
	c, ctx := client(t, e, e.Token)
	task := send(t, c, ctx, "", "echo x", true)
	_, path := recordOf(t, task)
	if _, err := delegation.Take(path, me); err != nil {
		t.Fatal(err)
	}
	end()
	rec, err := delegation.Reconcile(path)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != delegation.StatusFailed || rec.Error != "the live session ended; resend" {
		t.Fatalf("status %s error %q", rec.Status, rec.Error)
	}
}

// Past take-by the stream ends with the task still submitted and says where it
// waits; nothing switches to headless.
func TestNotTakenByTheDeadlineSaysSoAndStaysQueued(t *testing.T) {
	folder := setup(t)
	liveSession(t, "eno-1", "eno")
	e := openFor(t, folder, "eno", "local/parent", func(o *Options) { o.TakeBy = 200 * time.Millisecond })
	c, ctx := client(t, e, e.Token)
	first := send(t, c, ctx, "", "echo x", true)
	// The sender asked to return at once (as ynh's own MCP server does); once the
	// take-by has passed, the task says where it waits.
	var task *a2a.Task
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := c.GetTask(ctx, &a2a.GetTaskRequest{ID: first.ID})
		if err != nil {
			t.Fatal(err)
		}
		if got.Status.Message != nil {
			task = got
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the task never said it was still waiting")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if task.Status.State != a2a.TaskStateSubmitted || !strings.Contains(task.Status.Message.Parts[0].Text(), "queued at live session eno-1") {
		t.Fatalf("state %s, message %v: want submitted, saying where it waits", task.Status.State, task.Status.Message)
	}
	rec, _ := recordOf(t, task)
	if rec.Status != delegation.StatusQueued || rec.Route != delegation.RouteLive {
		t.Fatalf("record %s %s: want still queued at the live session", rec.Status, rec.Route)
	}
}

// A decline rejects the task, which the sender may resend headless.
func TestDeclineRejectsTheTask(t *testing.T) {
	folder := setup(t)
	_, me := liveSession(t, "eno-1", "eno")
	e := openFor(t, folder, "eno", "local/parent", nil)
	c, ctx := client(t, e, e.Token)
	task := send(t, c, ctx, "", "echo x", true)
	_, path := recordOf(t, task)
	if _, err := delegation.Take(path, me); err != nil {
		t.Fatal(err)
	}
	if _, err := delegation.Decline(path, "eno-1", "busy with the release"); err != nil {
		t.Fatal(err)
	}
	waitState(t, c, ctx, task.ID, a2a.TaskStateRejected)
}

// Two parents delegate to one live seat: each task is taken once, by one
// session, and a task taken by one is refused to the other.
func TestTwoParentsOneSeat(t *testing.T) {
	folder := setup(t)
	_, s1 := liveSession(t, "eno-1", "eno")
	_, s2 := liveSession(t, "eno-2", "eno")
	pa := openFor(t, folder, "eno", "local/alpha", nil)
	pb := openFor(t, folder, "eno", "local/beta", nil)
	ca, ctxa := client(t, pa, pa.Token)
	cb, ctxb := client(t, pb, pb.Token)
	ta := send(t, ca, ctxa, "", "task from alpha", true)
	// Oldest first: make the creation times differ.
	time.Sleep(20 * time.Millisecond)
	tb := send(t, cb, ctxb, "", "task from beta", true)
	_, patha := recordOf(t, ta)
	_, pathb := recordOf(t, tb)

	inbox, err := delegation.Inbox("local/eno")
	if err != nil || len(inbox) != 2 {
		t.Fatalf("inbox = %d, %v", len(inbox), err)
	}

	// Both sessions race for the same task: exactly one wins.
	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	for _, s := range []presence.Entry{s1, s2} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := delegation.Take(patha, s); err == nil {
				mu.Lock()
				won++
				mu.Unlock()
			} else if err != delegation.ErrNotQueued {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if won != 1 {
		t.Fatalf("%d sessions took the task, want exactly 1", won)
	}
	owner, _ := delegation.Load(patha)
	// The other task is still waiting, and the other session takes it.
	rest, _ := delegation.Inbox("local/eno")
	if len(rest) != 1 || rest[0].Handle == owner.Handle {
		t.Fatalf("inbox after the take = %v", rest)
	}
	other := s1
	if owner.Session == "eno-1" {
		other = s2
	}
	if _, err := delegation.Take(pathb, other); err != nil {
		t.Fatal(err)
	}
	if rb, _ := delegation.Load(pathb); rb.Session != other.SessionID || rb.Parent != "local/beta" {
		t.Fatalf("beta's task = %+v", rb)
	}
}

// A task queued to a live interactive session records and shows "interactive":
// a person approves there, so no level is claimed (ynh #1058). If the session
// ends and the task falls back to headless, the record shows the level the
// task resolved to.
func TestQueuedTaskShowsInteractiveNotALevelAndFallbackRestoresIt(t *testing.T) {
	folder := setup(t)
	end, _ := liveSession(t, "eno-1", "eno")
	e := openFor(t, folder, "eno", "local/parent", func(o *Options) {
		o.AutoApprove, o.AutoApproveFrom = permission.Edits, permission.SourceEntry
	})
	c, ctx := client(t, e, e.Token)
	task := send(t, c, ctx, "", "echo picked-up-headless", true)
	rec, path := recordOf(t, task)
	if rec.AutoApprove != "" || rec.AutoApproveFrom != delegation.AutoApproveInteractive {
		t.Fatalf("queued record shows %q (%s), want no level and interactive", rec.AutoApprove, rec.AutoApproveFrom)
	}
	if task.Metadata[MetaAutoApproveFrom] != "interactive" || task.Metadata[MetaAutoApprove] != nil {
		t.Errorf("task metadata = %v", task.Metadata)
	}
	end()
	deadline := time.Now().Add(15 * time.Second)
	for {
		rec, err := delegation.Reconcile(path)
		if err != nil {
			t.Fatal(err)
		}
		if rec.Route == delegation.RouteHeadless {
			if rec.AutoApprove != "edits" || rec.AutoApproveFrom != "entry" {
				t.Fatalf("after the fallback the record shows %q (%s), want edits (entry)", rec.AutoApprove, rec.AutoApproveFrom)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("route stayed %s", rec.Route)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
