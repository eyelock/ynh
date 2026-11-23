package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/eyelock/ynh/internal/delegation"
	"github.com/eyelock/ynh/internal/harness"
)

// cmdDelegateRun is the runner of delegates_to: it starts a resolved delegate
// as its own detached ynh session and keeps its delegation record. It is
// internal, not listed in `ynh help`: the delegate server calls it, and
// `supervise` is what a started session runs as.
//
//	ynh delegate-run start --parent <id> --entry <n> --task <text> [--model <m>] [--folder <dir>]
//	ynh delegate-run status --handle <handle>
//	ynh delegate-run cancel --handle <handle>
//	ynh delegate-run supervise <record-path>
func cmdDelegateRun(args []string) error {
	return cmdDelegateRunTo(args, os.Stdout)
}

func cmdDelegateRunTo(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ynh delegate-run <start|status|cancel|supervise>")
	}
	switch args[0] {
	case "start":
		return delegateRunStart(args[1:], stdout)
	case "status":
		return delegateRunRecord(args[1:], stdout, false)
	case "cancel":
		return delegateRunRecord(args[1:], stdout, true)
	case "supervise":
		if len(args) != 2 {
			return fmt.Errorf("usage: ynh delegate-run supervise <record-path>")
		}
		return delegation.Supervise(args[1], delegateLauncher)
	default:
		return fmt.Errorf("unknown delegate-run subcommand: %s", args[0])
	}
}

// delegateRunStart resolves entry n of the parent's delegates_to and starts it.
// It prints the handle and record path as JSON and returns once the session is
// launched.
func delegateRunStart(args []string, stdout io.Writer) error {
	var parentID, task, model, folder string
	entry := -1
	for i := 0; i < len(args); i++ {
		if i+1 >= len(args) {
			return fmt.Errorf("%s requires a value", args[i])
		}
		v := args[i+1]
		switch args[i] {
		case "--parent":
			parentID = v
		case "--entry":
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return fmt.Errorf("--entry must be a position in delegates_to, from 0")
			}
			entry = n
		case "--task":
			task = v
		case "--model":
			model = v
		case "--folder":
			folder = v
		default:
			return fmt.Errorf("unknown flag: %s", args[i])
		}
		i++
	}
	if parentID == "" || entry < 0 || task == "" {
		return fmt.Errorf("usage: ynh delegate-run start --parent <id> --entry <n> --task <text> [--model <m>] [--folder <dir>]")
	}
	if folder == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		folder = wd
	}

	parent, err := harness.LoadByID(parentID)
	if err != nil {
		return fmt.Errorf("parent harness %s: %w", parentID, err)
	}
	if entry >= len(parent.DelegatesTo) {
		return fmt.Errorf("%s has no delegates_to entry %d (it has %d)", parentID, entry, len(parent.DelegatesTo))
	}
	store, err := loadTrustStore()
	if err != nil {
		return err
	}
	resolved, err := resolveDelegate(parent, parent.DelegatesTo[entry], store)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding the ynh binary: %w", err)
	}
	rec, path, err := delegation.Start(delegation.StartOptions{
		Parent: parentID, Delegate: resolved.ID, Task: task, Model: model,
		Profile: resolved.Profile, Focus: resolved.Focus, Folder: folder,
		Supervisor: []string{exe, "delegate-run", "supervise"},
	})
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string]string{"handle": rec.Handle, "record": path})
}

// delegateRunRecord prints the record, after cancelling it when cancel is set.
// A delegation is named by its handle, which is looked up under the delegations
// folder; --record, a path, is for tests and the supervisor, and is not what a
// model-facing caller uses. A record whose session process is gone is settled
// to failed before it is shown or cancelled.
func delegateRunRecord(args []string, stdout io.Writer, cancel bool) error {
	if len(args) != 2 || (args[0] != "--handle" && args[0] != "--record") {
		return fmt.Errorf("usage: ynh delegate-run status|cancel --handle <handle>")
	}
	path := args[1]
	if args[0] == "--handle" {
		p, err := delegation.FindHandle(args[1])
		if err != nil {
			return err
		}
		path = p
	}
	rec, err := delegation.Reconcile(path)
	if err != nil {
		return err
	}
	if cancel {
		rec, err = delegation.Cancel(path)
		if err != nil {
			return err
		}
	}
	return writeJSON(stdout, rec)
}

// delegateLauncher builds the delegate's ynh run: `ynh run <id>` of the
// installed harness, with the entry's focus or profile, the task as the prompt
// (so the run is non-interactive) and the model passed through to the vendor.
// The environment keeps the parent's, minus the launch selectors the parent's
// own session may carry, which would otherwise pick the delegate's focus.
func delegateLauncher(rec *delegation.Record) *exec.Cmd {
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	cmd := exec.Command(exe, delegateRunArgs(rec)...)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "YNH_FOCUS=") || strings.HasPrefix(kv, "YNH_HARNESS_FILE=") {
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	cmd.Env = append(cmd.Env, delegationHandleEnv+"="+rec.Handle,
		"YNH_DELEGATION_RECORD="+delegation.RecordPath(rec.Parent, rec.Folder, rec.Handle))
	return cmd
}

// delegateRunArgs is the argument list of the delegate's `ynh run`.
func delegateRunArgs(rec *delegation.Record) []string {
	args := []string{"run", rec.Delegate}
	switch {
	case rec.Focus != "":
		args = append(args, "--focus", rec.Focus)
	case rec.Profile != "":
		args = append(args, "--profile", rec.Profile)
	}
	task := rec.Task
	// A first word after -- that starts with "-" is a vendor flag, never the
	// prompt; a leading space keeps a task that starts with one a prompt.
	if strings.HasPrefix(task, "-") {
		task = " " + task
	}
	args = append(args, "--", task)
	if rec.Model != "" {
		args = append(args, "--model="+rec.Model)
	}
	return args
}
