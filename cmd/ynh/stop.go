// `ynh stop` and `ynh reap`: end sessions on request (docs/presence.md).

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/eyelock/ynh/internal/presence"
)

// stopWait is how long a stop waits for a session to go before it kills it.
var stopWait = 5 * time.Second

func cmdStop(args []string) error { return cmdStopTo(args, os.Stdout, os.Stderr) }

// cmdStopTo is `ynh stop <session>` and `ynh stop --owner <session>`: it stops
// one live session, or every live session whose owner is the one named. Each
// is named by its id or its address.
func cmdStopTo(args []string, stdout, stderr io.Writer) error {
	structured := detectJSONFormat(args)
	var name, owner string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--owner" && i+1 < len(args):
			owner = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--owner="):
			owner = strings.TrimPrefix(args[i], "--owner=")
		case strings.HasPrefix(args[i], "-"):
			return cliError(stderr, structured, errCodeInvalidInput, fmt.Sprintf("unknown flag: %s", args[i]))
		case name == "":
			name = args[i]
		default:
			return cliError(stderr, structured, errCodeInvalidInput, fmt.Sprintf("unexpected argument: %s", args[i]))
		}
	}
	if (name == "") == (owner == "") {
		return cliError(stderr, structured, errCodeInvalidInput, "usage: ynh stop <session> | ynh stop --owner <session>")
	}
	if owner != "" {
		own, err := presence.Find(owner)
		if err != nil {
			return cliError(stderr, structured, errCodeInvalidInput, err.Error())
		}
		live, err := presence.List(false)
		if err != nil {
			return err
		}
		n := 0
		var errs []error
		for _, e := range live {
			if e.OwnerSession != own.SessionID {
				continue
			}
			if err := presence.Stop(e, stopWait); err != nil {
				errs = append(errs, err)
				continue
			}
			n++
			_, _ = fmt.Fprintf(stdout, "Stopped %s (%s, %s)\n", e.Address, e.Harness, e.Mode)
		}
		if n == 0 && len(errs) == 0 {
			_, _ = fmt.Fprintf(stdout, "%s owns no live session.\n", own.Address)
		}
		return errors.Join(errs...)
	}
	e, err := presence.Find(name)
	if err != nil {
		return cliError(stderr, structured, errCodeInvalidInput, err.Error())
	}
	if err := presence.Stop(e, stopWait); err != nil {
		if errors.Is(err, presence.ErrNotLive) {
			return cliError(stderr, structured, errCodeInvalidInput, err.Error()+"\n  Fix: `ynh ps` lists the live sessions")
		}
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Stopped %s (%s, %s)\n", e.Address, e.Harness, e.Mode)
	return nil
}

func cmdReap(args []string) error { return cmdReapTo(args, os.Stdout, os.Stderr) }

// cmdReapTo is `ynh reap`: it stops every orphaned session (a delegate whose
// owner has gone), and with --history also deletes the records of ended and
// dead sessions. --dry-run says what it would do and does nothing.
func cmdReapTo(args []string, stdout, stderr io.Writer) error {
	structured := detectJSONFormat(args)
	var history, dry bool
	for _, a := range args {
		switch a {
		case "--history":
			history = true
		case "--dry-run":
			dry = true
		default:
			return cliError(stderr, structured, errCodeInvalidInput, fmt.Sprintf("unknown argument: %s", a))
		}
	}
	all, err := presence.List(true)
	if err != nil {
		return err
	}
	stopped, removed := 0, 0
	var errs []error
	for _, e := range all {
		switch {
		case e.Status == presence.StatusOrphaned:
			if dry {
				_, _ = fmt.Fprintf(stdout, "would stop %s (%s, owner %s gone)\n", e.Address, e.Harness, e.OwnerSession)
				stopped++
				continue
			}
			if err := presence.Stop(e, stopWait); err != nil {
				errs = append(errs, err)
				continue
			}
			stopped++
			_, _ = fmt.Fprintf(stdout, "Stopped %s (%s, owner %s gone)\n", e.Address, e.Harness, e.OwnerSession)
		case history && (e.Status == presence.StatusEnded || e.Status == presence.StatusDead):
			if dry {
				removed++
				continue
			}
			if err := presence.Remove(e); err != nil {
				errs = append(errs, err)
				continue
			}
			removed++
		}
	}
	verb := ""
	if dry {
		verb = "would be "
	}
	_, _ = fmt.Fprintf(stdout, "%d orphaned session(s) %sstopped", stopped, verb)
	if history {
		_, _ = fmt.Fprintf(stdout, ", %d history record(s) %sremoved", removed, verb)
	}
	_, _ = fmt.Fprintln(stdout, ".")
	return errors.Join(errs...)
}
