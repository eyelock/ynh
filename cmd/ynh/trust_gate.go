package main

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/namespace"
	"github.com/eyelock/ynh/internal/resolver"
)

// errCodeUntrusted is the structured error code of a trust-gate refusal
// (design decision 12, ynh #950). It is also the "error_code" of an
// `ynh agent run` result that was refused.
const errCodeUntrusted = "untrusted"

// untrustedError is a refusal by the trust gate: a harness, or a remote
// include of a folder run, whose executables the operator has not accepted.
type untrustedError struct{ msg string }

func (e *untrustedError) Error() string { return e.msg }

func untrustedf(format string, a ...any) error {
	return &untrustedError{msg: fmt.Sprintf(format, a...)}
}

// isUntrusted reports whether err is a trust-gate refusal.
func isUntrusted(err error) bool {
	var u *untrustedError
	return errors.As(err, &u)
}

// includeRecordPrefix keys a trust record for a remote include of a folder
// run, so it can never collide with an installed harness id.
const includeRecordPrefix = "include:"

// checkTrustGate is the gate `ynh check` applies. A variable so the tests of
// check's own refusals, which use harnesses a trust record cannot cover (an
// invalid profile makes a harness unreviewable), can stand it aside.
var checkTrustGate = trustGate

// maxListed caps how many executables a refusal lists before pointing at
// `ynh trust show`, which lists them all.
const maxListed = 8

// trustGate decides whether a harness may run, before anything launches.
// id is the harness's install id, "" for a run from a folder (design decision
// 18: `ynh run ./dir`, --harness-file, a manifest in the current folder,
// `ynh agent run --harness <path>`). p is the harness as loaded, before a
// focus or profile reshapes it, which is the form a trust record covers.
//
// An installed harness, a local install or a remote one alike, must match an
// accepted record (decision 1), and one whose includes cannot be read is
// refused (decision 15). A folder run's own commands run unchecked, with one
// line saying so; the commands its remote includes bring are refused unless
// accepted (decision 3b). A refusal is an *untrustedError.
func trustGate(id string, p *harness.Harness, stderr io.Writer) error {
	if p == nil {
		return nil
	}
	store, err := loadTrustStore()
	if err != nil {
		return untrustedf("refusing to run %s: the trust record could not be read: %v", gateName(id, p), err)
	}
	if id != "" {
		return installedGate(id, p, store)
	}
	return folderGate(p, store, stderr)
}

func gateName(id string, p *harness.Harness) string {
	if id != "" {
		return id
	}
	return p.Name
}

// installedGate refuses an installed harness whose current executables do
// not match an accepted record.
func installedGate(id string, p *harness.Harness, store trustStore) error {
	st := trustStatusFor(id, p, store, nil)
	if st.State == trustUnavailable {
		return untrustedf("refusing to run %s: %s", id, unavailableMessage(id, st.Problems))
	}
	// A script the digest could not fully read blocks the run, accepted or
	// not: accepting cannot make it reviewable.
	if blockers := scriptBlockers(st.Executables, st.Hooks, st.MCPServers); len(blockers) > 0 {
		return unreadableScriptsError(id, blockers)
	}
	if st.State == trustAccepted || st.State == trustNoCommands {
		return nil
	}
	var b strings.Builder
	if st.State == trustChanged {
		fmt.Fprintf(&b, "refusing to run %s: its commands differ from what you accepted on %s.\n", id, st.AcceptedAt)
		if st.LegacyDigest {
			b.WriteString(legacyNote(st.acceptedScheme) + "\n")
		}
	} else {
		fmt.Fprintf(&b, "refusing to run %s: you have not accepted what it executes.\n", id)
	}
	b.WriteString(listExecutables(st.Executables, st.Hooks, st.MCPServers))
	fmt.Fprintf(&b, "Review them: ynh trust show %s\nThen accept:  ynh trust accept %s", id, id)
	return &untrustedError{msg: b.String()}
}

// listExecutables names what a refusal asks the operator to review, as `ynh
// trust show` does, at most maxListed lines of it.
func listExecutables(sensors []executable, hooks []hookExec, servers []mcpExec) string {
	var lines []string
	for _, h := range hooks {
		lines = append(lines, fmt.Sprintf("  hook %s: %s", h.Event, h.Command)+fromSuffix(h.Source)+scopeSuffix(h.Profile))
	}
	for _, m := range servers {
		what := m.Command
		if what == "" {
			what = m.URL
		}
		lines = append(lines, fmt.Sprintf("  MCP server %s: %s", m.Name, what)+fromSuffix(m.Source)+delegateSuffix(m.Delegate)+scopeSuffix(m.Profile))
	}
	for _, e := range sensors {
		lines = append(lines, fmt.Sprintf("  sensor %s (%s): %s", e.Sensor, e.Field, e.Value)+fromSuffix(e.Source)+scopeSuffix(e.Profile))
	}
	if len(lines) == 0 {
		return ""
	}
	more := len(lines) - maxListed
	if more > 0 {
		lines = append(lines[:maxListed], fmt.Sprintf("  ... and %d more", more))
	}
	return "It executes:\n" + strings.Join(lines, "\n") + "\n"
}

// folderGate says a folder run is not trust-checked, then refuses it when a
// remote include brings commands that are not accepted. A remote include is a
// git include of another repository, at any depth; a local include and a git
// include of the harness's own repository are the harness's own files.
func folderGate(p *harness.Harness, store trustStore, stderr io.Writer) error {
	if stderr != nil {
		_, _ = fmt.Fprintf(stderr, "ynh: %s runs from a folder, so its own commands are not trust-checked; the commands its remote includes bring are.\n", p.Name)
	}
	brought, remote, err := remoteIncludeSet(p)
	if err != nil {
		return err
	}
	if brought.count() == 0 || len(remote) == 0 {
		return nil
	}
	if blockers := scriptBlockers(brought.Sensors, brought.Hooks, brought.MCPServers); len(blockers) > 0 {
		return unreadableScriptsError(p.Name, blockers)
	}
	digest := executablesDigest(brought)
	var unaccepted []string
	for _, key := range remote {
		if rec, ok := store.Harnesses[includeRecordPrefix+key]; !ok || rec.Digest != digest || rec.Scheme != trustScheme {
			unaccepted = append(unaccepted, key)
		}
	}
	if len(unaccepted) == 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "refusing to run %s: its remote includes bring commands you have not accepted:\n", p.Name)
	for _, k := range unaccepted {
		fmt.Fprintf(&b, "  %s\n", k)
	}
	b.WriteString(listExecutables(brought.Sensors, brought.Hooks, brought.MCPServers))
	fmt.Fprintf(&b, "Accept them for this folder: ynh trust accept %s", p.Dir)
	return &untrustedError{msg: b.String()}
}

// remoteIncludeSet returns the commands the includes of p bring, and the
// record keys of its remote includes (repository, subpath and ref). The
// commands are every one an include supplied, local ones too, because the
// record for a remote include covers what its run carries from includes: a
// change to any of them asks again, never the other way round. An include
// that cannot be read is refused, naming `ynh update` (decision 15).
func remoteIncludeSet(p *harness.Harness) (trustSet, []string, error) {
	set := harnessExecutables(p)
	if len(set.Problems) > 0 {
		return trustSet{}, nil, untrustedf("refusing to run %s: %s", p.Name, includesUnreadable(p, set.Problems))
	}
	resolved, err := resolver.ResolveIncludesLocal(p, harness.Selection{})
	if err != nil {
		return trustSet{}, nil, untrustedf("refusing to run %s: %s", p.Name, includesUnreadable(p, []string{err.Error()}))
	}
	var remote []string
	for _, r := range resolved {
		if r.Remote && !slices.Contains(remote, r.RemoteKey) {
			remote = append(remote, r.RemoteKey)
		}
	}
	slices.Sort(remote)

	var brought trustSet
	for _, e := range set.Sensors {
		if e.Source != "" {
			brought.Sensors = append(brought.Sensors, e)
		}
	}
	for _, h := range set.Hooks {
		if h.Source != "" {
			brought.Hooks = append(brought.Hooks, h)
		}
	}
	for _, m := range set.MCPServers {
		if m.Source != "" && m.Delegate == "" {
			brought.MCPServers = append(brought.MCPServers, m)
		}
	}
	return brought, remote, nil
}

func includesUnreadable(p *harness.Harness, problems []string) string {
	return fmt.Sprintf("what its includes execute could not be read in full.\n  %s\nFetch them with 'ynh update', then run it again.",
		strings.Join(problems, "\n  "))
}

// gateRef is the install id of a harness argument, "" when it is a path.
func gateRef(ref string) string {
	if namespace.Classify(ref) == namespace.RefID {
		return ref
	}
	return ""
}

// acceptFolderIncludes records the commands the remote includes of the folder
// harness at path bring, one record per remote include (its repository,
// subpath and ref), so a run of that folder passes the gate until an include
// brings something else.
func acceptFolderIncludes(path, format string, structured bool, stdout, stderr io.Writer) error {
	p, err := harness.LoadIDOrPath(path)
	if err != nil {
		return cliError(stderr, structured, errCodeNotFound, err.Error())
	}
	store, err := loadTrustStore()
	if err != nil {
		return cliError(stderr, structured, errCodeIOError, err.Error())
	}
	brought, remote, err := remoteIncludeSet(p)
	if err != nil {
		return cliError(stderr, structured, errCodeConfigError, err.Error())
	}
	if brought.count() == 0 || len(remote) == 0 {
		if format == "json" {
			return writeJSON(stdout, map[string]any{"harness": p.Dir, "accepted": []string{}})
		}
		_, err = fmt.Fprintf(stdout, "Nothing to accept: %s has no remote include that brings commands.\n", p.Dir)
		return err
	}
	rec := trustRecord{
		Digest:     executablesDigest(brought),
		Commands:   brought.count(),
		Scheme:     trustScheme,
		AcceptedAt: time.Now().UTC().Format(time.RFC3339),
	}
	for _, key := range remote {
		store.Harnesses[includeRecordPrefix+key] = rec
	}
	if err := saveTrustStore(store); err != nil {
		return cliError(stderr, structured, errCodeIOError, err.Error())
	}
	if format == "json" {
		return writeJSON(stdout, map[string]any{"harness": p.Dir, "accepted": remote, "digest": rec.Digest, "commands": rec.Commands})
	}
	_, err = fmt.Fprintf(stdout, "Recorded %d remote include(s) of %s: %d command(s), %s\n", len(remote), p.Dir, rec.Commands, rec.Digest)
	return err
}

// scriptBlockers lists, one line each, what the digest could not fully read of
// the scripts the commands run: a file it skipped or refused (a link out of
// the harness, a device, a dangling or looping link, a link to a folder), and
// a covered script whose folder is missing or unreadable. Such a command
// cannot be reviewed whole, so it blocks a run (human's decision on
// ynw-pz9kv, 2026-11-26). A script outside what the digest covers (project
// code) is not listed: it was never reviewable.
func scriptBlockers(sensors []executable, hooks []hookExec, servers []mcpExec) []string {
	var out []string
	add := func(what string, s *scriptRef) {
		if s == nil {
			return
		}
		for _, k := range s.Skipped {
			out = append(out, fmt.Sprintf("%s: script %s: %s", what, s.Path, k))
		}
		if s.Covered && (s.Digest == "missing" || s.Digest == "unreadable") {
			out = append(out, fmt.Sprintf("%s: script %s: its folder %s is %s", what, s.Path, s.Folder, s.Digest))
		}
	}
	for _, h := range hooks {
		add("hook "+h.Event, h.Script)
	}
	for _, m := range servers {
		add("MCP server "+m.Name, m.Script)
	}
	for _, e := range sensors {
		add(fmt.Sprintf("sensor %s (%s)", e.Sensor, e.Field), e.Script)
	}
	return out
}

// unreadableScriptsError is the refusal for a harness with such scripts. It
// has the gate's exit 1 and error code untrusted: the design names no other.
func unreadableScriptsError(name string, blockers []string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "refusing to run %s: a script its commands run cannot be read in full, so what runs cannot be reviewed:\n", name)
	for _, l := range blockers {
		fmt.Fprintf(&b, "  %s\n", l)
	}
	b.WriteString("Fix: make each listed file a regular file inside the harness (no link out of it, to a device, or in a loop), " +
		"or add the missing script, then run 'ynh trust show' to see it covered.")
	return &untrustedError{msg: b.String()}
}
