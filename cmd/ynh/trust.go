package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
)

// A sensor's `command` and `version_command` are handed to `/bin/sh -c` with
// the operator's whole environment. That is the feature: a sensor exists to run
// the project's own tools. But a harness is installed from a Git URL into
// ~/.ynh/harnesses/, and its sensors then run against a tree somewhere else, so
// the code that executes lives nowhere near the directory the operator is
// looking at. Running `make check` on a repository at least puts the Makefile
// in front of you.
//
// `ynh trust` closes that gap by showing what a harness will execute, where it
// came from, and at which commit, and by recording that an operator saw it.
//
// It does not block. A recorded decision nobody acts on would be the same
// hollow control this repository spent 2026-09-01 removing elsewhere, so the
// record has to pay for itself immediately: it pins the exact set of commands
// that were reviewed, and `ynh check` reports when a harness's commands have
// changed since. Detecting the change is the value now; refusing to run on it
// is a later decision, and deliberately not this one.

// trustFile is the operator's record, under YNH_HOME.
//
// Operator state, not project state, and pointedly not inside the harness
// directory: a harness carrying its own trust record would be authorising
// itself. That is the same trap SkillSpector's --use-shipped-baseline exists to
// avoid, where a scanned artifact supplies the baseline that clears it.
const trustFile = "trust.json"

// trustRecord is one accepted harness.
type trustRecord struct {
	// Digest covers every string this harness can get executed. It is the
	// whole point of the record: comparing it is how a changed command is
	// noticed.
	Digest string `json:"digest"`
	// Commands is how many executable strings the digest covered, so the
	// report can say "12 commands" without re-reading the manifest.
	Commands int `json:"commands"`
	// Source, Ref and SHA are copied from provenance at acceptance time.
	// Copied rather than referenced: the point is what the operator was
	// looking at when they accepted, which an update would otherwise rewrite.
	Source     string `json:"source,omitempty"`
	Ref        string `json:"ref,omitempty"`
	SHA        string `json:"sha,omitempty"`
	AcceptedAt string `json:"accepted_at"`
}

type trustStore struct {
	Version   int                    `json:"version"`
	Harnesses map[string]trustRecord `json:"harnesses"`
}

func trustStorePath() string { return filepath.Join(config.HomeDir(), trustFile) }

// loadTrustStore reads the record, returning an empty store when absent.
//
// A missing file is "nothing has been accepted", not an error: every install
// predating this command is in exactly that state, and it must not be an error
// condition for them.
func loadTrustStore() (trustStore, error) {
	s := trustStore{Version: 1, Harnesses: map[string]trustRecord{}}
	data, err := os.ReadFile(trustStorePath())
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("reading trust record: %w", err)
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("parsing %s: %w", trustStorePath(), err)
	}
	if s.Harnesses == nil {
		s.Harnesses = map[string]trustRecord{}
	}
	return s, nil
}

func saveTrustStore(s trustStore) error {
	s.Version = 1
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding trust record: %w", err)
	}
	if err := os.MkdirAll(config.HomeDir(), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", config.HomeDir(), err)
	}
	// 0600: it records what this operator reviewed. Nothing secret, but it is
	// theirs, and a world-writable file inviting edits would defeat it.
	if err := os.WriteFile(trustStorePath(), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing trust record: %w", err)
	}
	return nil
}

// executable is one string a harness can get run through a shell.
type executable struct {
	Sensor string `json:"sensor"`
	Field  string `json:"field"` // "command" or "version_command"
	Value  string `json:"value"`
}

// harnessExecutables lists every string the harness can get executed, sorted.
//
// Sorted so the digest does not depend on Go's map iteration order, which is
// randomised: an unsorted digest would differ between two runs over an
// unchanged harness and report drift that is not there.
func harnessExecutables(p *harness.Harness) []executable {
	// Never nil. A nil slice marshals to null, and a consumer iterating the
	// JSON then has to special-case it. `ynh doctor` shipped exactly this bug
	// with its findings array (#328); the published schema caught it here.
	out := make([]executable, 0, len(p.Sensors))
	for name, s := range p.Sensors {
		if s.Source.Kind() == "command" && strings.TrimSpace(s.Source.Command) != "" {
			out = append(out, executable{Sensor: name, Field: "command", Value: s.Source.Command})
		}
		if strings.TrimSpace(s.VersionCommand) != "" {
			out = append(out, executable{Sensor: name, Field: "version_command", Value: s.VersionCommand})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Sensor != out[j].Sensor {
			return out[i].Sensor < out[j].Sensor
		}
		return out[i].Field < out[j].Field
	})
	return out
}

// executablesDigest fingerprints the command set.
//
// The sensor name and field are included, not just the command text: moving the
// same command onto a different sensor changes when it runs and what its
// failure gates, which the operator reviewed and should be asked about again.
func executablesDigest(exes []executable) string {
	h := sha256.New()
	for _, e := range exes {
		// hash.Hash's Write never returns an error, by its own contract.
		_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00", e.Sensor, e.Field, e.Value)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))[:16]
}

// undeclaredEnvNames lists variables a sensor inherits that the harness never
// declared, sorted. Names only, never values, matching the trajectory rule that
// a record meant to prevent a leak must not become one.
//
// Sensors are handed os.Environ() whole. The harness manifest already has
// env_passthrough, whose contract says an inheriting process "holds every
// credential the operator holds, which is not a default anyone chose" — but
// that contract names MCP expansion and agent workers, and sensors were never
// wired to it. This reports the gap rather than closing it: scrubbing would
// break any sensor relying on inherited configuration, `gh` reading GH_TOKEN
// among them, and that is a change to make deliberately with the breakage
// measured first.
func undeclaredEnvNames(p *harness.Harness, environ []string) []string {
	declared := map[string]bool{}
	for _, n := range processEnvBaseline {
		declared[n] = true
	}
	for _, n := range p.EnvPassthrough {
		declared[n] = true
	}
	var out []string
	for _, kv := range environ {
		name, _, ok := strings.Cut(kv, "=")
		if !ok || declared[name] || strings.HasPrefix(name, "LC_") {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// processEnvBaseline mirrors internal/agent's processEnvNames: the variables a
// subprocess needs to run at all. Duplicated rather than exported because the
// agent list is the agent's contract, and coupling a report to it would make
// changing one silently change the other.
var processEnvBaseline = []string{
	"PATH", "HOME", "USER", "LOGNAME", "SHELL", "TMPDIR", "TZ", "LANG", "TERM",
}

// trustStatus is the report for one harness.
type trustStatus struct {
	Harness     string       `json:"harness"`
	Source      string       `json:"source,omitempty"`
	SourceType  string       `json:"source_type,omitempty"`
	Ref         string       `json:"ref,omitempty"`
	SHA         string       `json:"sha,omitempty"`
	Digest      string       `json:"digest"`
	Executables []executable `json:"executables"`
	// State is "unreviewed", "accepted", or "changed".
	State string `json:"state"`
	// AcceptedDigest and AcceptedAt are populated when a record exists.
	AcceptedDigest string `json:"accepted_digest,omitempty"`
	AcceptedAt     string `json:"accepted_at,omitempty"`
	// UndeclaredEnv names variables the sensors inherit but the harness never
	// declared. Names only.
	UndeclaredEnv []string `json:"undeclared_env,omitempty"`
}

const (
	trustUnreviewed = "unreviewed"
	trustAccepted   = "accepted"
	trustChanged    = "changed"
	// trustNoCommands is not a review outcome. A harness that ships only
	// skills executes nothing, so calling it "accepted" would claim an
	// operator approved something they were never shown, and calling it
	// "unreviewed" would put a warning on it forever. Most installed
	// harnesses are in this state.
	trustNoCommands = "no-commands"
)

// trustStatusFor builds the report without deciding anything about it.
func trustStatusFor(id string, p *harness.Harness, store trustStore, environ []string) trustStatus {
	exes := harnessExecutables(p)
	st := trustStatus{
		Harness:       id,
		Digest:        executablesDigest(exes),
		Executables:   exes,
		State:         trustUnreviewed,
		UndeclaredEnv: undeclaredEnvNames(p, environ),
	}
	if pr := p.InstalledFrom; pr != nil {
		st.Source, st.SourceType, st.Ref, st.SHA = pr.Source, pr.SourceType, pr.Ref, pr.SHA
	}
	if rec, ok := store.Harnesses[id]; ok {
		st.AcceptedDigest, st.AcceptedAt = rec.Digest, rec.AcceptedAt
		if rec.Digest == st.Digest {
			st.State = trustAccepted
		} else {
			st.State = trustChanged
		}
	}
	// Nothing to review outranks "never reviewed": a harness that executes
	// nothing cannot be a risk, and flagging it forever would train people to
	// ignore the state column that matters.
	if len(exes) == 0 && st.State == trustUnreviewed {
		st.State = trustNoCommands
	}
	return st
}

// cmdTrust dispatches `ynh trust <ls|show|accept>`.
func cmdTrust(args []string) error { return cmdTrustTo(args, os.Stdout, os.Stderr) }

func cmdTrustTo(args []string, stdout, stderr io.Writer) error {
	// A leading flag is not a subcommand. `ynh trust --format json` is the
	// listing, and rejecting it as an unknown subcommand is the kind of paper
	// cut that makes a command feel broken on first contact.
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return cmdTrustLs(args, stdout, stderr)
	}
	switch args[0] {
	case "ls", "list":
		return cmdTrustLs(args[1:], stdout, stderr)
	case "show":
		return cmdTrustShow(args[1:], stdout, stderr)
	case "accept":
		return cmdTrustAccept(args[1:], stdout, stderr)
	default:
		return cliError(stderr, false, errCodeInvalidInput,
			fmt.Sprintf("unknown trust subcommand: %s", args[0]))
	}
}

// parseTrustArgs pulls --format and at most one positional harness name.
func parseTrustArgs(args []string, stderr io.Writer) (name, format string, structured bool, err error) {
	structured = detectJSONFormat(args)
	format = "text"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return "", "", structured, cliError(stderr, structured, errCodeInvalidInput, "--format requires a value")
			}
			i++
			format = args[i]
		default:
			if strings.HasPrefix(args[i], "-") {
				return "", "", structured, cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unknown flag: %s", args[i]))
			}
			if name != "" {
				return "", "", structured, cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unexpected argument: %s", args[i]))
			}
			name = args[i]
		}
	}
	if format != "text" && format != "json" {
		return "", "", structured, cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("invalid --format value %q (want text or json)", format))
	}
	return name, format, structured, nil
}

func cmdTrustLs(args []string, stdout, stderr io.Writer) error {
	name, format, structured, err := parseTrustArgs(args, stderr)
	if err != nil {
		return err
	}
	if name != "" {
		return cmdTrustShow(args, stdout, stderr)
	}

	store, err := loadTrustStore()
	if err != nil {
		return cliError(stderr, structured, errCodeIOError, err.Error())
	}
	entries, err := harness.ListAll()
	if err != nil {
		return cliError(stderr, structured, errCodeIOError, err.Error())
	}

	environ := os.Environ()
	statuses := make([]trustStatus, 0, len(entries))
	for _, e := range entries {
		id := canonicalIDForEntry(e)
		p, loadErr := harness.LoadQualified(id)
		if loadErr != nil {
			// A harness that will not load cannot be reported on, and this
			// command is not the place to fail the operator's whole listing
			// for it. `ynh doctor` is.
			continue
		}
		statuses = append(statuses, trustStatusFor(id, p, store, environ))
	}

	if format == "json" {
		return writeJSON(stdout, statuses)
	}
	return printTrustList(stdout, statuses)
}

func cmdTrustShow(args []string, stdout, stderr io.Writer) error {
	name, format, structured, err := parseTrustArgs(args, stderr)
	if err != nil {
		return err
	}
	if name == "" {
		return cliError(stderr, structured, errCodeInvalidInput,
			"usage: ynh trust show <harness> [--format text|json]")
	}
	p, err := harness.LoadQualified(name)
	if err != nil {
		return cliError(stderr, structured, errCodeNotFound, err.Error())
	}
	store, err := loadTrustStore()
	if err != nil {
		return cliError(stderr, structured, errCodeIOError, err.Error())
	}
	st := trustStatusFor(name, p, store, os.Environ())

	if format == "json" {
		return writeJSON(stdout, st)
	}
	return printTrustDetail(stdout, st)
}

func cmdTrustAccept(args []string, stdout, stderr io.Writer) error {
	name, format, structured, err := parseTrustArgs(args, stderr)
	if err != nil {
		return err
	}
	if name == "" {
		return cliError(stderr, structured, errCodeInvalidInput,
			"usage: ynh trust accept <harness>")
	}
	p, err := harness.LoadQualified(name)
	if err != nil {
		return cliError(stderr, structured, errCodeNotFound, err.Error())
	}
	store, err := loadTrustStore()
	if err != nil {
		return cliError(stderr, structured, errCodeIOError, err.Error())
	}

	exes := harnessExecutables(p)
	rec := trustRecord{
		Digest:     executablesDigest(exes),
		Commands:   len(exes),
		AcceptedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if pr := p.InstalledFrom; pr != nil {
		rec.Source, rec.Ref, rec.SHA = pr.Source, pr.Ref, pr.SHA
	}
	store.Harnesses[name] = rec
	if err := saveTrustStore(store); err != nil {
		return cliError(stderr, structured, errCodeIOError, err.Error())
	}

	if format == "json" {
		return writeJSON(stdout, trustStatusFor(name, p, store, os.Environ()))
	}
	_, err = fmt.Fprintf(stdout, "Recorded %s: %d command(s), %s\n", name, rec.Commands, rec.Digest)
	return err
}

func printTrustList(w io.Writer, statuses []trustStatus) error {
	if len(statuses) == 0 {
		_, err := fmt.Fprintln(w, "No harnesses installed.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "HARNESS\tSTATE\tCOMMANDS\tSOURCE"); err != nil {
		return err
	}
	changed, unreviewed := 0, 0
	for _, s := range statuses {
		switch s.State {
		case trustChanged:
			changed++
		case trustUnreviewed:
			unreviewed++
		}
		src := s.Source
		if src == "" {
			src = "(local)"
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", s.Harness, s.State, len(s.Executables), src); err != nil {
			return err
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if changed+unreviewed == 0 {
		return nil
	}
	_, err := fmt.Fprintf(w, "\nRun 'ynh trust show <harness>' to see what a harness will execute.\n")
	return err
}

func printTrustDetail(w io.Writer, s trustStatus) error {
	_, _ = fmt.Fprintf(w, "%s\n", s.Harness)
	if s.Source != "" {
		_, _ = fmt.Fprintf(w, "  source:   %s", s.Source)
		if s.Ref != "" {
			_, _ = fmt.Fprintf(w, " (%s)", s.Ref)
		}
		_, _ = fmt.Fprintln(w)
		if s.SHA != "" {
			_, _ = fmt.Fprintf(w, "  commit:   %s\n", s.SHA)
		}
	} else {
		_, _ = fmt.Fprintf(w, "  source:   (local)\n")
	}
	_, _ = fmt.Fprintf(w, "  state:    %s\n", s.State)
	_, _ = fmt.Fprintf(w, "  digest:   %s\n", s.Digest)
	if s.State == trustChanged {
		_, _ = fmt.Fprintf(w, "  accepted: %s on %s\n", s.AcceptedDigest, s.AcceptedAt)
	}

	if len(s.Executables) == 0 {
		_, _ = fmt.Fprintln(w, "\nThis harness declares no commands.")
		return nil
	}

	_, _ = fmt.Fprintf(w, "\nWill execute via /bin/sh -c, in the tree being measured:\n\n")
	for _, e := range s.Executables {
		_, _ = fmt.Fprintf(w, "  %s (%s)\n    %s\n", e.Sensor, e.Field, e.Value)
	}

	if len(s.UndeclaredEnv) > 0 {
		_, _ = fmt.Fprintf(w, "\nThese commands inherit your full environment. %d variable(s) reach them\n"+
			"that this harness never declared in env_passthrough:\n\n", len(s.UndeclaredEnv))
		_, _ = fmt.Fprintf(w, "  %s\n", strings.Join(s.UndeclaredEnv, ", "))
	}

	switch s.State {
	case trustUnreviewed:
		_, _ = fmt.Fprintf(w, "\nNot yet reviewed. 'ynh trust accept %s' records that you have read the above.\n", s.Harness)
	case trustChanged:
		_, _ = fmt.Fprintf(w, "\nThese commands have changed since you accepted them.\n"+
			"'ynh trust accept %s' records the new set.\n", s.Harness)
	}
	return nil
}

// warnIfTrustChanged reports on stderr when a harness's commands differ from
// what the operator accepted.
//
// Only "changed" warns. An unreviewed harness is the state of every install
// predating this command, so warning on it would put a line on almost every
// check, and a warning that is always there is one nobody reads. That is the
// same reasoning the stale-claim workflow records for notifying rather than
// gating: the false-positive rate decides whether anyone keeps looking.
//
// It never fails the check. A harness whose commands changed may well be fine,
// and this release does not claim to know which. Blocking is a later decision.
func warnIfTrustChanged(stderr io.Writer, id string, p *harness.Harness) {
	if stderr == nil || p == nil {
		return
	}
	store, err := loadTrustStore()
	if err != nil {
		// A trust record that cannot be read must not stop a gate from running.
		return
	}
	rec, ok := store.Harnesses[id]
	if !ok {
		return
	}
	digest := executablesDigest(harnessExecutables(p))
	if digest == rec.Digest {
		return
	}
	_, _ = fmt.Fprintf(stderr,
		"warning: %s declares different commands than you accepted on %s.\n"+
			"  accepted %s, now %s. Review with 'ynh trust show %s'.\n",
		id, rec.AcceptedAt, rec.Digest, digest, id)
}
