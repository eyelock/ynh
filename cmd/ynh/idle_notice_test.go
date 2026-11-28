package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/delegation"
	"github.com/eyelock/ynh/internal/presence"
)

func queueTask(t *testing.T, delegate, parent, task string) {
	t.Helper()
	// The session the task is queued for is live, as it is when ynh queues one.
	if _, ok := liveQueueSessions[t.Name()]; !ok {
		s, err := presence.Start(presence.Record{SessionID: "live-1", Harness: "local--eno", Vendor: "claude", Mode: presence.ModeInteractive, AcceptsTasks: true})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.End)
		liveQueueSessions[t.Name()] = true
	}
	_, _, err := delegation.Queue(delegation.QueueOptions{
		Parent: parent, Delegate: delegate, Task: task, Folder: t.TempDir(),
		Supervisor: []string{"true"}, Session: presence.Entry{Record: presence.Record{SessionID: "live-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

var liveQueueSessions = map[string]bool{}

type notified struct{ title, body string }

// An idle session with a task waiting is told by bell, window title and OS
// notification, once; a busy one is not, until its turn ends.
func TestIdleNoticeStep(t *testing.T) {
	isolateRun(t)
	const run = "local--eno"
	queueTask(t, "local/eno", "local/ynw", "review it")

	var tty bytes.Buffer
	var got []notified
	notify := func(title, body string) { got = append(got, notified{title, body}) }

	// Busy: nothing.
	writeActivity("s1", activityBusy)
	announced := idleNoticeStep(run, "s1", map[string]bool{}, &tty, notify)
	if tty.Len() != 0 || len(got) != 0 || len(announced) != 0 {
		t.Fatalf("a busy session was told: tty %q, notifications %v", tty.String(), got)
	}

	// The turn ends: bell, title and a notification.
	writeActivity("s1", activityIdle)
	announced = idleNoticeStep(run, "s1", announced, &tty, notify)
	if !strings.Contains(tty.String(), "\033]0;local--eno: 1 task from local/ynw\a") || !strings.HasSuffix(tty.String(), "\a\a") {
		t.Errorf("title and bell = %q", tty.String())
	}
	if len(got) != 1 || got[0].body != "local--eno: 1 task from local/ynw" {
		t.Errorf("notifications = %v", got)
	}

	// Told once: the next look says nothing.
	tty.Reset()
	announced = idleNoticeStep(run, "s1", announced, &tty, notify)
	if tty.Len() != 0 || len(got) != 1 {
		t.Errorf("the same task was announced again: %q %v", tty.String(), got)
	}

	// A second task is news.
	queueTask(t, "local/eno", "local/other", "and this")
	announced = idleNoticeStep(run, "s1", announced, &tty, notify)
	if len(got) != 2 || !strings.Contains(got[1].body, "2 tasks from local/ynw, local/other") {
		t.Errorf("notifications = %v", got)
	}
	_ = announced
}

// A session no hook has reported on yet, at its first prompt, counts as idle;
// an emptied inbox gives the title back.
func TestIdleNoticeStep_NoActivityIsIdleAndAnEmptyInboxClearsTheTitle(t *testing.T) {
	isolateRun(t)
	const run = "local--eno"
	queueTask(t, "local/eno", "local/ynw", "review it")
	var tty bytes.Buffer
	var n int
	notify := func(string, string) { n++ }
	announced := idleNoticeStep(run, "never-reported", map[string]bool{}, &tty, notify)
	if n != 1 || tty.Len() == 0 {
		t.Fatalf("no activity file: notifications %d, tty %q", n, tty.String())
	}
	es, _ := delegation.Inbox(run)
	for _, e := range es {
		if _, err := delegation.Take(e.Path, presence.Entry{Record: presence.Record{SessionID: "live-1"}}); err != nil {
			t.Fatal(err)
		}
	}
	tty.Reset()
	announced = idleNoticeStep(run, "never-reported", announced, &tty, notify)
	if tty.String() != "\033]0;\a" || len(announced) != 0 {
		t.Errorf("title not handed back: %q", tty.String())
	}
}

func TestInboxHookRecordsTheTurn(t *testing.T) {
	isolateRun(t)
	t.Setenv(inboxSessionEnv, "s-act")
	for event, want := range map[string]string{"before_prompt": "busy", "on_stop": "idle"} {
		var out strings.Builder
		if err := cmdInboxHook([]string{"--harness", "local--eno", "--vendor", "claude", "--event", event}, strings.NewReader("{}"), &out); err != nil {
			t.Fatal(err)
		}
		if got := readActivity("s-act"); got != want {
			t.Errorf("%s: activity = %q, want %q", event, got, want)
		}
	}
}

func TestAppleScriptString(t *testing.T) {
	if got := appleScriptString(`a "b" \c`); got != `"a \"b\" \\c"` {
		t.Errorf("%s", got)
	}
}
