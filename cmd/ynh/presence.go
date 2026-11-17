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
	"github.com/eyelock/ynh/internal/sessions"
	"github.com/eyelock/ynh/internal/vendor"
)

// startPresence records the session this run is about to launch and returns
// the function that ends it. The record is best effort: a failure to write it
// is reported on stderr and never stops the launch.
func startPresence(vendorName, harness string, oneShot, isDelegate bool) func() {
	id, err := sessions.NewID()
	if err != nil {
		return func() {}
	}
	cwd, _ := os.Getwd()
	rec := presence.Record{SessionID: id, Harness: harness, Vendor: vendorName, Cwd: cwd, Mode: presence.ModeInteractive}
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

// psResult is the JSON shape of `ynh ps --format json`.
type psResult struct {
	Sessions []presence.Entry `json:"sessions"`
}

// PsResult is what the same call returns on the MCP server.
func listPresence(all bool) (psResult, error) {
	es, err := presence.List(all)
	return psResult{Sessions: es}, err
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
	return tw.Flush()
}
