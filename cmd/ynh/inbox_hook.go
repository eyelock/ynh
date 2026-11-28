package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/eyelock/ynh/internal/delegation"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/presence"
)

// The inbox hooks tell a live interactive session that a task is waiting in
// its harness's mailbox (docs/delegate-sessions.md, "Delegating to a live
// session"). ynh adds a before_prompt and an on_stop hook to such a session;
// each runs `ynh inbox-hook`, which prints the waiting-task line when the
// mailbox is not empty and nothing otherwise.
//
// How each hook's output reaches the model differs by vendor (spike ynh #907,
// Claude Code 2.1.326, Codex 0.166.0, Copilot CLI 1.0.113):
//
//	before_prompt   Claude Code and Codex: plain stdout.
//	                Copilot CLI: {"additionalContext": "..."} (plain stdout is lost).
//	on_stop         all three: only the block JSON {"decision":"block","reason":...}
//	                reaches the model, and it then continues into inbox_take.

// The hook events the inbox uses, as ynh's canonical names.
const (
	inboxEventBeforePrompt = "before_prompt"
	inboxEventOnStop       = "on_stop"
)

// inboxHookCommand is the command line of one inbox hook. The session id is
// not in it (the snapshot is shared by every run of the harness): the hook
// reads it from YNH_SESSION_ID.
func inboxHookCommand(exe, runName, vendorName, event string) string {
	return shellQuote(exe) + " inbox-hook --harness " + shellQuote(runName) + " --vendor " + vendorName + " --event " + event
}

// withInboxHooks returns a copy of p whose hooks include the inbox's
// before_prompt and on_stop hooks, after the harness's own. p is not changed.
func withInboxHooks(p *harness.Harness, exe, runName, vendorName string) *harness.Harness {
	cp := *p
	cp.Hooks = make(map[string][]plugin.HookEntry, len(p.Hooks)+2)
	for k, v := range p.Hooks {
		cp.Hooks[k] = slices.Clone(v)
	}
	for _, ev := range []string{inboxEventBeforePrompt, inboxEventOnStop} {
		cp.Hooks[ev] = append(cp.Hooks[ev], plugin.HookEntry{Command: inboxHookCommand(exe, runName, vendorName, ev), Timeout: 10})
	}
	return &cp
}

// waitingLine is the line the model is given: finish what the person asked
// first (D7), say a task is waiting and from whom, then take it.
func waitingLine(es []delegation.InboxEntry) string {
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
	return fmt.Sprintf("ynh inbox: %d %s waiting in your inbox from %s. Finish what the person asked first, tell them a task is waiting and from whom, then call inbox_take.",
		len(es), noun, strings.Join(from, ", "))
}

// inboxHookOutput is what the hook prints for vendorName and event, "" when
// nothing is to be said. announced is the set of task handles an on_stop hook
// has already woken the model for in this session: a task is announced once,
// so a model that declines it is not woken again in a loop. stopActive is the
// vendor's own sign that this stop is the continuation a block caused.
func inboxHookOutput(vendorName, event string, es []delegation.InboxEntry, announced map[string]bool, stopActive bool) (string, []string) {
	if len(es) == 0 {
		return "", nil
	}
	line := waitingLine(es)
	switch event {
	case inboxEventBeforePrompt:
		if vendorName == "copilot" {
			b, _ := json.Marshal(map[string]string{"additionalContext": line})
			return string(b), nil
		}
		return line, nil
	case inboxEventOnStop:
		if stopActive {
			return "", nil
		}
		var fresh []string
		for _, e := range es {
			if !announced[e.Handle] {
				fresh = append(fresh, e.Handle)
			}
		}
		if len(fresh) == 0 {
			return "", nil
		}
		b, _ := json.Marshal(map[string]string{"decision": "block", "reason": line})
		return string(b), fresh
	}
	return "", nil
}

// cmdInboxHook is the hook the inbox adds to an interactive session.
//
//	ynh inbox-hook --harness <run name> --vendor <vendor> --event <before_prompt|on_stop>
//
// It never fails the session: a mailbox it cannot read prints nothing.
func cmdInboxHook(args []string, stdin io.Reader, stdout io.Writer) error {
	var run, vendorName, event string
	for i := 0; i < len(args); i += 2 {
		if i+1 >= len(args) {
			return fmt.Errorf("%s requires a value\nusage: ynh inbox-hook --harness <run name> --vendor <vendor> --event <before_prompt|on_stop>", args[i])
		}
		switch args[i] {
		case "--harness":
			run = args[i+1]
		case "--vendor":
			vendorName = args[i+1]
		case "--event":
			event = args[i+1]
		default:
			return fmt.Errorf("unknown flag %s\nusage: ynh inbox-hook --harness <run name> --vendor <vendor> --event <before_prompt|on_stop>", args[i])
		}
	}
	if run == "" || vendorName == "" || (event != inboxEventBeforePrompt && event != inboxEventOnStop) {
		return fmt.Errorf("usage: ynh inbox-hook --harness <run name> --vendor <vendor> --event <before_prompt|on_stop>")
	}
	session := os.Getenv(inboxSessionEnv)
	// A turn starting or ending is what the idle notice waits on: it does not
	// ring a session that is in the middle of a turn.
	if event == inboxEventBeforePrompt {
		writeActivity(session, activityBusy)
	} else {
		writeActivity(session, activityIdle)
	}
	es, err := delegation.Inbox(run)
	if err != nil || len(es) == 0 {
		return nil
	}
	var in struct {
		StopHookActive bool `json:"stop_hook_active"`
	}
	_ = json.NewDecoder(stdin).Decode(&in)
	announced := readAnnounced(session)
	out, fresh := inboxHookOutput(vendorName, event, es, announced, in.StopHookActive)
	if out == "" {
		return nil
	}
	if len(fresh) > 0 {
		writeAnnounced(session, announced, fresh)
	}
	_, err = fmt.Fprintln(stdout, out)
	return err
}

func announcedPath(session string) string {
	if session == "" || strings.ContainsAny(session, `/\`) {
		return ""
	}
	return filepath.Join(presence.Dir(), session+".announced")
}

func readAnnounced(session string) map[string]bool {
	out := map[string]bool{}
	p := announcedPath(session)
	if p == "" {
		return out
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return out
	}
	for _, h := range strings.Fields(string(data)) {
		out[h] = true
	}
	return out
}

func writeAnnounced(session string, announced map[string]bool, fresh []string) {
	p := announcedPath(session)
	if p == "" {
		return
	}
	for _, h := range fresh {
		announced[h] = true
	}
	var all []string
	for h := range announced {
		all = append(all, h)
	}
	slices.Sort(all)
	_ = os.WriteFile(p, []byte(strings.Join(all, "\n")+"\n"), 0o644)
}
