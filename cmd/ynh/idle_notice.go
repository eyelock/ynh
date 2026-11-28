package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/eyelock/ynh/internal/delegation"
	"github.com/eyelock/ynh/internal/presence"
)

// The idle notice: a session waiting at its prompt runs no hook, and nothing can
// type into it, so when a task is queued for its harness `ynh run`, which stays
// alive as the vendor's parent, tells the person at the terminal: it rings the
// bell, sets the window title ("<harness>: N task(s) from <sender>") and posts
// an OS notification where the OS has one. The person presses Enter on any
// prompt and the inbox hooks do the rest (docs/delegate-sessions.md, ynh #912).
//
// A busy session is left alone: the inbox hooks record when a turn starts and
// ends (activity), and the notice waits for the turn to end. Before any hook has
// run, a session sitting at its first prompt counts as idle.

// idleNoticePoll is how often the mailbox is looked at.
const idleNoticePoll = 2 * time.Second

// Activity values the inbox hooks record per session.
const (
	activityBusy = "busy"
	activityIdle = "idle"
)

func activityPath(session string) string {
	if session == "" || strings.ContainsAny(session, `/\`) {
		return ""
	}
	return filepath.Join(presence.Dir(), session+".activity")
}

// writeActivity records that the session's turn started (busy) or ended
// (idle). Best effort: the hook never fails the session.
func writeActivity(session, state string) {
	p := activityPath(session)
	if p == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(p, []byte(state+"\n"), 0o644)
}

// readActivity is "busy", "idle" or "" when no hook has reported.
func readActivity(session string) string {
	p := activityPath(session)
	if p == "" {
		return ""
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// idleNoticeTTY opens the terminal the notice's bell and title go to, or nil
// when there is none. It is /dev/tty, not stdout: the vendor owns the
// session's output, and an escape sequence of ours must not land in a pipe.
// Replaceable so tests never write to the developer's terminal.
var idleNoticeTTY = func() io.WriteCloser {
	f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return nil
	}
	return f
}

// idleNoticeNotify posts an OS notification, where the OS has one: macOS
// through osascript, Linux through notify-send when installed. It never waits
// for the notifier and never fails. Replaceable for tests.
var idleNoticeNotify = func(title, body string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf("display notification %s with title %s", appleScriptString(body), appleScriptString(title))
		cmd = exec.Command("osascript", "-e", script)
	case "linux":
		bin, err := exec.LookPath("notify-send")
		if err != nil {
			return
		}
		cmd = exec.Command(bin, title, body)
	default:
		return
	}
	if err := cmd.Start(); err != nil {
		return
	}
	go func() { _ = cmd.Wait() }()
}

func appleScriptString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// idleNoticeLine is the title and notification text.
func idleNoticeLine(run string, es []delegation.InboxEntry) string {
	var from []string
	for _, e := range es {
		if !slices.Contains(from, e.Parent) {
			from = append(from, e.Parent)
		}
	}
	noun := "task"
	if len(es) != 1 {
		noun = "tasks"
	}
	return fmt.Sprintf("%s: %d %s from %s", run, len(es), noun, strings.Join(from, ", "))
}

// idleNoticeStep is one look at the mailbox. announced is the set of tasks the
// person has been told of; it returns the updated set. A session that is busy,
// or an inbox with nothing new, says nothing; an emptied inbox clears the title
// the notice set.
func idleNoticeStep(run, session string, announced map[string]bool, tty io.Writer, notify func(title, body string)) map[string]bool {
	es, err := delegation.Inbox(run)
	if err != nil {
		return announced
	}
	if len(es) == 0 {
		if len(announced) > 0 && tty != nil {
			_, _ = io.WriteString(tty, "\033]0;\a") // hand the title back to the terminal
		}
		return map[string]bool{}
	}
	if readActivity(session) == activityBusy {
		return announced
	}
	fresh := false
	next := map[string]bool{}
	for _, e := range es {
		next[e.Handle] = true
		if !announced[e.Handle] {
			fresh = true
		}
	}
	if !fresh {
		return next
	}
	line := idleNoticeLine(run, es)
	if tty != nil {
		_, _ = io.WriteString(tty, "\033]0;"+line+"\a\a") // the title, then the bell
	}
	if notify != nil {
		notify("ynh: a task is waiting", line)
	}
	return next
}

// startIdleNotice watches the harness's mailbox for the session and returns the
// function that stops it.
func startIdleNotice(run, session string) (stop func()) {
	tty := idleNoticeTTY()
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		announced := map[string]bool{}
		t := time.NewTicker(idleNoticePoll)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				var w io.Writer
				if tty != nil {
					w = tty
				}
				announced = idleNoticeStep(run, session, announced, w, idleNoticeNotify)
			}
		}
	}()
	return func() {
		close(done)
		<-finished
		if tty != nil {
			_ = tty.Close()
		}
	}
}
