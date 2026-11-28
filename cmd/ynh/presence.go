// Seat presence: the record each `ynh run` writes while its session lives, and
// `ynh ps`, which lists them (docs/presence.md).

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/eyelock/ynh/internal/delegation"
	"github.com/eyelock/ynh/internal/presence"
	"github.com/eyelock/ynh/internal/vendor"
	"github.com/eyelock/ynh/internal/warn"
)

// startPresence records the session this run is about to launch and returns
// the function that ends it. The record is best effort: a failure to write it
// is reported on stderr and never stops the launch.
func startPresence(id, vendorName, harness string, oneShot, isDelegate, acceptsTasks bool, noTasksWhy string) func() {
	cwd, _ := os.Getwd()
	rec := presence.Record{SessionID: id, Harness: harness, Vendor: vendorName, Cwd: cwd, Mode: presence.ModeInteractive, AcceptsTasks: acceptsTasks && !isDelegate && !oneShot}
	if !rec.AcceptsTasks && !isDelegate && !oneShot {
		rec.NoTasksReason = noTasksWhy
	}
	switch {
	case isDelegate:
		rec.Mode = presence.ModeDelegate
		rec.OwnerSession = ownerSession(os.Getenv(delegationRecordEnv))
	case oneShot:
		rec.Mode = presence.ModeOneShot
	}
	s, err := presence.Start(rec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "presence: not recorded: %v\n", err)
		return func() {}
	}
	removePID := vendor.OnChildPID(s.SetVendorPID)
	return func() {
		removePID()
		s.End()
	}
}

// ownerSession names the live session that started a delegate: the newest
// non-delegate session of the parent harness in the folder the delegation
// record names. Empty when the record or such a session cannot be found.
func ownerSession(recordPath string) string {
	if recordPath == "" {
		return ""
	}
	rec, err := delegation.Load(recordPath)
	if err != nil {
		return ""
	}
	live, err := presence.List(false)
	if err != nil {
		return ""
	}
	owner := ""
	for _, e := range live {
		if e.Mode != presence.ModeDelegate && e.Cwd == rec.Folder && presenceHarnessMatches(e.Harness, rec.Parent) {
			owner = e.SessionID // List is oldest first, so the newest wins
		}
	}
	return owner
}

func presenceHarnessMatches(recorded, parent string) bool {
	return recorded != "" && (recorded == parent || strings.HasSuffix(parent, "/"+recorded))
}

// psSession is one session of `ynh ps`: its presence entry and the questions
// its delegates have asked it that nobody has answered.
type psSession struct {
	presence.Entry
	OpenQuestions []psQuestion `json:"open_questions,omitempty"`
}

// psQuestion is an open question from a delegate, with how long it has waited.
type psQuestion struct {
	Delegate       string `json:"delegate"`
	Handle         string `json:"handle"`
	Question       string `json:"question"`
	AskedAt        string `json:"asked_at"`
	WaitingSeconds int    `json:"waiting_seconds"`
}

// psResult is the JSON shape of `ynh ps --format json`.
type psResult struct {
	Sessions []psSession `json:"sessions"`
}

// listPresence is what `ynh ps` and the ynh_ps MCP tool return. A delegate's
// open question is listed against the newest live non-delegate session of the
// parent harness in the folder the delegation ran in: the session whose chat
// it surfaces in.
func listPresence(all bool) (psResult, error) {
	es, err := presence.List(all)
	res := psResult{Sessions: make([]psSession, len(es))}
	for i, e := range es {
		res.Sessions[i].Entry = e
	}
	if err != nil {
		return res, err
	}
	recs, lerr := delegation.ListAll()
	if lerr != nil {
		return res, nil // the questions are extra; the listing stands without them
	}
	now := time.Now()
	for _, rec := range recs {
		if rec.Status.Final() {
			continue
		}
		var q *delegation.Question
		for i := range rec.Questions {
			if rec.Questions[i].Open() {
				q = &rec.Questions[i]
				break
			}
		}
		if q == nil {
			continue
		}
		owner := -1
		for i, e := range es {
			if e.Status == presence.StatusLive && e.Mode != presence.ModeDelegate && e.Cwd == rec.Folder && presenceHarnessMatches(e.Harness, rec.Parent) {
				owner = i // oldest first, so the newest wins
			}
		}
		if owner < 0 {
			continue
		}
		name := rec.Delegate
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		res.Sessions[owner].OpenQuestions = append(res.Sessions[owner].OpenQuestions, psQuestion{
			Delegate: name, Handle: rec.Handle, Question: q.Text,
			AskedAt: q.AskedAt.UTC().Format(time.RFC3339), WaitingSeconds: int(now.Sub(q.AskedAt).Seconds()),
		})
	}
	return res, nil
}

func cmdPs(args []string) error { return cmdPsTo(args, os.Stdout, os.Stderr) }

func cmdPsTo(args []string, stdout, stderr io.Writer) error {
	structured := detectJSONFormat(args)
	format, all := "text", false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			format = "json"
		case "--all":
			all = true
		case "--format":
			if i+1 >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--format requires a value")
			}
			i++
			format = args[i]
		default:
			if strings.HasPrefix(args[i], "-") {
				return cliError(stderr, structured, errCodeInvalidInput, fmt.Sprintf("unknown flag: %s", args[i]))
			}
			return cliError(stderr, structured, errCodeInvalidInput, fmt.Sprintf("unexpected argument: %s", args[i]))
		}
	}
	if format != "text" && format != "json" {
		return cliError(stderr, structured, errCodeInvalidInput, fmt.Sprintf("invalid --format value %q (want text or json)", format))
	}
	res, err := listPresence(all)
	if err != nil {
		return err
	}
	if format == "json" {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	if len(res.Sessions) == 0 {
		_, _ = fmt.Fprintln(stdout, "No live sessions.")
		return nil
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ADDRESS\tHARNESS\tVENDOR\tMODE\tSTATUS\tSTATE\tWRAPPER\tVENDOR PID\tSTARTED\tCWD")
	for _, e := range res.Sessions {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%d\t%s\t%s\n", e.Address, e.Harness, e.Vendor, e.Mode,
			e.Status, e.State, e.WrapperPID, e.VendorPID, e.StartedAt.Local().Format(time.DateTime), e.Cwd)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	for _, s := range res.Sessions {
		for _, q := range s.OpenQuestions {
			_, _ = fmt.Fprintf(stdout, "%s: %s asks (waiting %s): %s\n", s.Address, q.Delegate,
				(time.Duration(q.WaitingSeconds) * time.Second).String(), q.Question)
		}
	}
	return nil
}

// liveSessionsOf lists the live sessions recorded for a harness's run name, in
// this YNH_HOME, oldest first. A dead or ended record is not live, so a
// crashed session never blocks a new one.
func liveSessionsOf(runName string) []presence.Entry {
	live, err := presence.List(false)
	if err != nil {
		return nil
	}
	var out []presence.Entry
	for _, e := range live {
		if e.Harness == runName {
			out = append(out, e)
		}
	}
	return out
}

// checkOnLive decides what a run does when a session of its harness is already
// live (docs/presence.md). flag is --on-live and setting the harness's on_live;
// the flag wins. "allow" starts the run quietly, "refuse" returns an error
// naming the live session and the way out, and neither (the default) warns on
// stderr and lets the run start.
func checkOnLive(runName, id, setting, flag string, stderr io.Writer) error {
	mode := setting
	if flag != "" {
		mode = flag
	}
	switch mode {
	case "", "refuse", "allow":
	default:
		return fmt.Errorf("--on-live and on_live take refuse or allow, not %q", mode)
	}
	if mode == "allow" {
		return nil
	}
	live := liveSessionsOf(runName)
	if len(live) == 0 {
		return nil
	}
	first := live[0]
	what := fmt.Sprintf("%s already has a live session: %s (%s, started %s, in %s)",
		id, first.Address, first.Mode, first.StartedAt.Local().Format("15:04:05"), first.Cwd)
	if len(live) > 1 {
		what += fmt.Sprintf(" and %d more", len(live)-1)
	}
	if mode == "refuse" {
		return fmt.Errorf("%s\n  Fix: use that session, end it, or run `ynh run --on-live=allow %s` to start another", what, id)
	}
	warn.Print(stderr, warn.Warning{
		Message: what + ". Starting another.",
		Fix:     "pass --on-live=refuse, or set on_live to refuse in the harness, to stop this",
	})
	return nil
}
