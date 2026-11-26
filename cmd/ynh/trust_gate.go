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

// gateTrusted reports whether the harness passed the gate as trusted: an
// installed harness whose current commands match an accepted record (or that
// has none). A folder run is never trusted, since the gate does not check its
// own commands (design decisions 5, 5a, 5c, ynh #952). Every pre-approval
// (MCP tools, Copilot's --allow-all-tools, --auto-approve) asks this, so none
// can be given to a harness the gate did not vouch for.
func gateTrusted(id string, p *harness.Harness) bool {
	if id == "" || p == nil {
		return false
	}
	store, err := loadTrustStore()
	if err != nil {
		return false
	}
	return installedGate(id, p, store) == nil
}

// copilotOneShotRefusal refuses a one-shot Copilot run of a harness that
// is not trusted: Copilot's one-shot mode approves every tool, so there is no
// narrower grant to give (decision 5b).
func copilotOneShotRefusal(id string, p *harness.Harness) error {
	if id == "" {
		return untrustedf("refusing to run %s one-shot on copilot: a run from a folder is not trust-checked, and copilot's one-shot mode approves every tool.\n"+
			"Install it and accept what it executes (ynh install <folder>, then ynh trust accept local/%s), run it interactively, or use another vendor.", p.Name, p.Name)
	}
	return untrustedf("refusing to run %s one-shot on copilot: copilot's one-shot mode approves every tool, and you have not accepted what %s executes.\n"+
		"Accept it: ynh trust accept %s", id, id, id)
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
	remote, err := remoteIncludes(p)
	if err != nil {
		return err
	}
	var unaccepted []remoteInclude
	for _, r := range remote {
		if blockers := scriptBlockers(r.Set.Sensors, r.Set.Hooks, r.Set.MCPServers); len(blockers) > 0 {
			return unreadableScriptsError(p.Name, blockers)
		}
		if rec, ok := store.Harnesses[includeRecordPrefix+r.Key]; !ok || rec.Digest != executablesDigest(r.Set) || rec.Scheme != trustScheme {
			unaccepted = append(unaccepted, r)
		}
	}
	if len(unaccepted) == 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "refusing to run %s: its remote includes bring commands you have not accepted:\n", p.Name)
	for _, r := range unaccepted {
		fmt.Fprintf(&b, "  %s\n", r.Key)
		b.WriteString(listExecutables(r.Set.Sensors, r.Set.Hooks, r.Set.MCPServers))
	}
	fmt.Fprintf(&b, "Accept them for this folder: ynh trust accept %s", p.Dir)
	return &untrustedError{msg: b.String()}
}

// remoteInclude is one remote include a folder run reaches and the commands
// that include brings by itself: its own sensors, hooks and MCP servers, and
// those of its own local includes, never those of the folder or of another
// include, so the record for it means the same to every folder that includes it.
type remoteInclude struct {
	// Key is the include's repository, subpath and ref (RemoteKey).
	Key string
	Set trustSet
}

// remoteIncludes returns the remote includes of p that bring commands, each
// with its own commands, sorted by key. An include whose commands cannot be
// read is refused, naming `ynh update` (decision 15). Hooks are counted
// whether or not the include is consented to, so the record never covers less
// than the include could run.
func remoteIncludes(p *harness.Harness) ([]remoteInclude, error) {
	if set := harnessExecutables(p); len(set.Problems) > 0 {
		return nil, untrustedf("refusing to run %s: %s", p.Name, includesUnreadable(p, set.Problems))
	}
	resolved, err := resolver.ResolveIncludesLocal(p, harness.Selection{})
	if err != nil {
		return nil, untrustedf("refusing to run %s: %s", p.Name, includesUnreadable(p, []string{err.Error()}))
	}
	seen := map[string]bool{}
	var out []remoteInclude
	for _, r := range resolved {
		if !r.Remote || r.Harness == nil || seen[r.RemoteKey] {
			continue
		}
		seen[r.RemoteKey] = true
		own := harnessExecutables(r.Harness)
		if len(own.Problems) > 0 {
			return nil, untrustedf("refusing to run %s: %s", p.Name, includesUnreadable(p, own.Problems))
		}
		set := trustSet{Sensors: own.Sensors, Hooks: own.Hooks, MCPServers: own.MCPServers}
		if set.count() == 0 {
			continue
		}
		out = append(out, remoteInclude{Key: r.RemoteKey, Set: set})
	}
	slices.SortFunc(out, func(a, b remoteInclude) int { return strings.Compare(a.Key, b.Key) })
	return out, nil
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

// acceptFolderIncludes records the commands of each remote include of the
// folder harness at path, one record per include (its repository, subpath and
// ref) over that include's own commands. A record is shared by every folder
// that includes the same repository at the same ref, and accepting one folder
// never changes another's.
func acceptFolderIncludes(path, format string, structured bool, stdout, stderr io.Writer) error {
	p, err := harness.LoadIDOrPath(path)
	if err != nil {
		return cliError(stderr, structured, errCodeNotFound, err.Error())
	}
	store, err := loadTrustStore()
	if err != nil {
		return cliError(stderr, structured, errCodeIOError, err.Error())
	}
	remote, err := remoteIncludes(p)
	if err != nil {
		return cliError(stderr, structured, errCodeConfigError, err.Error())
	}
	if len(remote) == 0 {
		if format == "json" {
			return writeJSON(stdout, map[string]any{"harness": p.Dir, "accepted": []string{}})
		}
		_, err = fmt.Fprintf(stdout, "Nothing to accept: %s has no remote include that brings commands.\n", p.Dir)
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var keys []string
	commands := 0
	for _, r := range remote {
		store.Harnesses[includeRecordPrefix+r.Key] = trustRecord{
			Digest:     executablesDigest(r.Set),
			Commands:   r.Set.count(),
			Scheme:     trustScheme,
			AcceptedAt: now,
		}
		keys = append(keys, r.Key)
		commands += r.Set.count()
	}
	if err := saveTrustStore(store); err != nil {
		return cliError(stderr, structured, errCodeIOError, err.Error())
	}
	if format == "json" {
		return writeJSON(stdout, map[string]any{"harness": p.Dir, "accepted": keys, "commands": commands})
	}
	_, err = fmt.Fprintf(stdout, "Recorded %d remote include(s) of %s: %d command(s)\n", len(keys), p.Dir, commands)
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
