package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/namespace"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/workspace"
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

// checkTrustGate is the decision `ynh check` asks for. A variable so the tests
// of check's own refusals, which use harnesses a trust record cannot cover (an
// invalid profile makes a harness unreviewable), can stand it aside.
var checkTrustGate = func(id string, p *harness.Harness, stderr io.Writer) error {
	return decideTrust(id, p, purposeCheck, stderr).err()
}

// maxListed caps how many executables a refusal lists before pointing at
// `ynh trust show`, which lists them all.
const maxListed = 8

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
	if st.State == trustChanged && st.Moved != "" {
		fmt.Fprintf(&b, "refusing to run %s: your acceptance on %s does not cover it: %s.\n", id, st.AcceptedAt, st.Moved)
	} else if st.State == trustChanged {
		fmt.Fprintf(&b, "refusing to run %s: its commands differ from what you accepted on %s.\n", id, st.AcceptedAt)
		if st.LegacyDigest {
			b.WriteString(legacyNote(st.acceptedScheme, kindsOf(len(st.Executables), len(st.Hooks), len(st.MCPServers))) + "\n")
		}
	} else if len(st.Executables)+len(st.Hooks)+len(st.MCPServers) == 0 && len(p.Workspace) > 0 {
		// Only workspace folders: nothing executes, but granting folders is
		// a permission the operator has not given (ynh #978).
		fmt.Fprintf(&b, "refusing to run %s: you have not accepted what it asks for.\n", id)
	} else {
		fmt.Fprintf(&b, "refusing to run %s: you have not accepted what it executes.\n", id)
	}
	b.WriteString(listExecutables(st.Executables, st.Hooks, st.MCPServers))
	b.WriteString(listWorkspace(p))
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

// listWorkspace names the workspace folders p requests, as written and as
// they resolve, for a refusal to show beside what the harness executes. They
// are covered by the digest, so accepting grants them.
func listWorkspace(p *harness.Harness) string {
	if len(p.Workspace) == 0 {
		return ""
	}
	home, _ := os.UserHomeDir()
	var b strings.Builder
	b.WriteString("It requests workspace folders:\n")
	for _, e := range p.Workspace {
		resolved := workspace.Resolve(e, p.Dir, home)
		if resolved == e {
			fmt.Fprintf(&b, "  %s\n", e)
		} else {
			fmt.Fprintf(&b, "  %s (resolves to %s)\n", e, resolved)
		}
	}
	return b.String()
}

// folderGate says a folder run is not trust-checked, then refuses it when a
// remote include brings commands that are not accepted. A remote include is a
// git include of another repository, at any depth; a local include and a git
// include of the harness's own repository are the harness's own files.
func folderGate(p *harness.Harness, store trustStore, stderr io.Writer) error {
	if stderr != nil {
		_, _ = fmt.Fprintf(stderr, "ynh: %s runs from a folder, so its own commands are not trust-checked; the commands its remote includes bring are.\n", p.Name)
	}
	remote, err := remoteIncludesFor(p, "run", folderFetchFix)
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
// read is refused, naming `ynh update` (decision 15); verb is the act refused,
// "run" for the gate and "accept" for an acceptance, so a refused acceptance
// does not say it refuses to run. Hooks are counted
// whether or not the include is consented to, so the record never covers less
// than the include could run.
func remoteIncludes(p *harness.Harness, verb string) ([]remoteInclude, error) {
	return remoteIncludesFor(p, verb, updateFetchFix)
}

// What a refusal tells the operator to do about an include that is not in the
// cache. `ynh update` takes installed ids only, so a folder run is pointed at
// --trust, which fetches an include pinned to a full SHA (ynh #993).
const (
	updateFetchFix = "Fetch them with 'ynh update', then run it again."
	folderFetchFix = "Fetch them by running it with --trust, once each remote include is pinned to a full 40-character commit SHA " +
		"(ynh run <folder> --trust, or ynh agent run --harness <folder> --trust). ynh update takes installed harnesses only."
)

// remoteIncludesFor is remoteIncludes with the Fix line of its refusals named
// by the caller.
func remoteIncludesFor(p *harness.Harness, verb, fix string) ([]remoteInclude, error) {
	if set := harnessExecutables(p); len(set.Problems) > 0 {
		return nil, untrustedf("refusing to %s %s: %s", verb, p.Name, includesUnreadable(p, set.Problems, fix))
	}
	resolved, err := resolver.ResolveIncludesLocal(p, harness.Selection{})
	if err != nil {
		return nil, untrustedf("refusing to %s %s: %s", verb, p.Name, includesUnreadable(p, []string{err.Error()}, fix))
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
			return nil, untrustedf("refusing to %s %s: %s", verb, p.Name, includesUnreadable(p, own.Problems, fix))
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

func includesUnreadable(p *harness.Harness, problems []string, fix string) string {
	// The resolver's own hint names ynh update; the Fix line below names the
	// right route for this run, and two routes would contradict.
	problems = slices.Clone(problems)
	for i, pr := range problems {
		problems[i] = strings.ReplaceAll(pr, "\nrun `ynh update` (checking a gate must not reach the network)", "")
	}
	return fmt.Sprintf("what its includes execute could not be read in full.\n  %s\n%s",
		strings.Join(problems, "\n  "), fix)
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
	remote, err := remoteIncludesFor(p, "accept", folderFetchFix)
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
			Via:        viaAccept,
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
	_, err = fmt.Fprintf(stdout, "Accepted %d remote include(s) of %s: %d command(s)\n", len(keys), p.Dir, commands)
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
