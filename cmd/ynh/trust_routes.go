package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/namespace"
	"github.com/eyelock/ynh/internal/resolver"
)

// How an acceptance was recorded (design decision 16), in a trust record's
// "via" and shown by `ynh trust show` and `ynh trust ls`.
const (
	viaAccept      = "accept" // ynh trust accept
	viaInstallTrus = "install --trust"
	viaRunTrust    = "run --trust"
	viaUpdateTrust = "update --trust"
	viaIncludeTrus = "include --trust"
)

// isFullSHA reports whether s is a full commit SHA, 40 hexadecimal digits. A
// tag, a branch and a short SHA are not: they can name another commit
// tomorrow, so they do not pin what runs (decision 4a).
func isFullSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// notPinned says why ref is not a full-SHA pin, "" when it is one.
func notPinned(ref string) string {
	switch {
	case ref == "":
		return "no ref, so it floats with the default branch"
	case isFullSHA(ref):
		return ""
	case len(ref) >= 7 && len(ref) < 40 && isHex(ref):
		return fmt.Sprintf("%q is a short SHA, which does not pin the commit", ref)
	default:
		return fmt.Sprintf("%q is a tag or branch, which can move", ref)
	}
}

func isHex(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return s != ""
}

// includeRef is the ref a remote include's record key carries: what follows
// the last "@" of "<repository>//<path>@<ref>".
func includeRef(key string) string {
	if i := strings.LastIndex(key, "@"); i >= 0 {
		return key[i+1:]
	}
	return ""
}

// unpinnedIncludes lists, one line each, the remote includes of p that bring
// commands and are not pinned to a full SHA.
func unpinnedIncludes(remote []remoteInclude) []string {
	var out []string
	for _, r := range remote {
		if why := notPinned(includeRef(r.Key)); why != "" {
			out = append(out, fmt.Sprintf("%s: %s", r.Key, why))
		}
	}
	return out
}

// refuseUnpinned is the refusal of --trust when something that runs is not
// pinned: it names each and the way out.
func refuseUnpinned(what, flag string, reasons []string) error {
	return refuseUnpinnedWith(what, flag, reasons,
		"Pin it to a 40-character commit SHA, or review it and accept it by hand with 'ynh trust accept'")
}

// refuseUnpinnedWith is refuseUnpinned with the closing line named by the
// caller.
func refuseUnpinnedWith(what, flag string, reasons []string, closing string) error {
	return fmt.Errorf("%s refuses to accept %s: it is not pinned to a full commit SHA, so what runs could change under the acceptance:\n  %s\n%s",
		flag, what, strings.Join(reasons, "\n  "), closing)
}

// uncachedUnpinnedClosing ends the refusal of an uncached include that is not
// pinned. `ynh trust accept <folder>` reads includes from the cache and this
// one is not in it, so it would only point back at --trust (ynh #993).
const uncachedUnpinnedClosing = "Pin it to a 40-character commit SHA and run it again with --trust. " +
	"'ynh trust accept' cannot help: it reads an include from the cache, and this one is not in it"

// checkInstallTrust refuses `ynh install --trust` before anything is written
// when the install is not pinned: a git or registry install needs --ref at a
// full SHA (a local folder install is the operator's own files, decision 4c),
// and every remote include that brings commands needs one too (decision 4b).
// Includes are fetched first, as install does anyway, so they can be judged.
func checkInstallTrust(p *harness.Harness, cfg *config.Config, isLocal bool, ref string) error {
	if !isLocal {
		if why := notPinned(ref); why != "" {
			return refuseUnpinned("this install", "--trust", []string{"the harness's own ref: " + why})
		}
	}
	if _, err := resolver.Resolve(p, cfg); err != nil {
		return fmt.Errorf("--trust could not read the includes of %s: %w", p.Name, err)
	}
	remote, err := remoteIncludes(p, "accept")
	if err != nil {
		return err
	}
	if bad := unpinnedIncludes(remote); len(bad) > 0 {
		return refuseUnpinned("this install", "--trust", bad)
	}
	return nil
}

// recordAcceptance records that the operator accepted p, as `ynh trust
// accept` does, with how it was accepted, and prints what was accepted.
func recordAcceptance(id string, p *harness.Harness, via string, stdout io.Writer) error {
	store, err := loadTrustStore()
	if err != nil {
		return err
	}
	set := harnessExecutables(p)
	if len(set.Problems) > 0 {
		return fmt.Errorf("%s", unavailableMessage(id, set.Problems))
	}
	rec := trustRecord{
		Digest:     executablesDigest(set),
		Commands:   set.count(),
		Scheme:     trustScheme,
		Via:        via,
		AcceptedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if pr := p.InstalledFrom; pr != nil {
		rec.Source, rec.Ref, rec.SHA = pr.Source, pr.Ref, pr.SHA
	}
	store.Harnesses[id] = rec
	if err := saveTrustStore(store); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Accepted %s (%s): %d command(s), %s\n", id, via, rec.Commands, rec.Digest)
	_, _ = fmt.Fprint(stdout, listExecutables(set.Sensors, set.Hooks, set.MCPServers))
	return nil
}

// acceptInstalled records the acceptance of the harness just installed under
// id, by `ynh install --trust`.
func acceptInstalled(id string, stdout io.Writer) error {
	p, err := harness.LoadByID(id)
	if err != nil {
		return err
	}
	return recordAcceptance(id, p, viaInstallTrus, stdout)
}

// acceptFolderRun is `ynh run <dir> --trust` and `ynh agent run --harness
// <dir> --trust` (decision 10a): it accepts the folder's remote includes, only
// when every one that brings commands is pinned to a full SHA, and prints what
// it accepted. An installed harness never takes --trust at run time.
func acceptFolderRun(ref string, p *harness.Harness, stdout io.Writer) error {
	if namespace.Classify(ref) != namespace.RefPath {
		return fmt.Errorf("--trust is for a run from a folder; an installed harness is accepted with 'ynh trust accept %s', or at install with 'ynh install --trust'", ref)
	}
	return acceptFolderRunOf(p, stdout)
}

// acceptFolderRunOf accepts the remote includes of p, a harness already known
// to be a folder run: one found by path, by --harness-file (whose directory is
// not always a path ref, "hs" for example) or by a manifest in the current
// folder.
func acceptFolderRunOf(p *harness.Harness, stdout io.Writer) error {
	if err := warmFolderIncludes(p); err != nil {
		return err
	}
	remote, err := remoteIncludesFor(p, "accept", folderFetchFix)
	if err != nil {
		return err
	}
	if bad := unpinnedIncludes(remote); len(bad) > 0 {
		return refuseUnpinned(p.Name, "--trust", bad)
	}
	if len(remote) == 0 {
		return nil
	}
	store, err := loadTrustStore()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, r := range remote {
		store.Harnesses[includeRecordPrefix+r.Key] = trustRecord{
			Digest:     executablesDigest(r.Set),
			Commands:   r.Set.count(),
			Scheme:     trustScheme,
			Via:        viaRunTrust,
			AcceptedAt: now,
		}
		_, _ = fmt.Fprintf(stdout, "Accepted %s (%s): %d command(s)\n", r.Key, viaRunTrust, r.Set.count())
		_, _ = fmt.Fprint(stdout, listExecutables(r.Set.Sensors, r.Set.Hooks, r.Set.MCPServers))
	}
	return saveTrustStore(store)
}

// warmFolderIncludes fetches the remote includes of a folder run that are
// missing from the cache, so the gate can read what they execute (ynh #993).
// Only an include pinned to a full 40-character SHA is fetched: that makes the
// fetch deterministic, and --trust is the operator's explicit act. An uncached
// include that is not pinned refuses the run, and the ones named by the folder
// itself are judged before anything is fetched. A cached include is not
// fetched again, so a second run reaches no network.
func warmFolderIncludes(p *harness.Harness) error {
	var bad []string
	for _, inc := range p.Includes {
		if inc.IsLocal() || inc.Git == "" || resolver.IsOwnRepoSource(inc.GitSource, p.Dir) {
			continue
		}
		if _, ok := resolver.LookupCache(resolver.GitSourceURL(inc.Git, p.Dir), inc.Ref); ok {
			continue
		}
		if why := notPinned(inc.Ref); why != "" {
			bad = append(bad, fmt.Sprintf("%s: %s", inc.Git, why))
		}
	}
	if len(bad) > 0 {
		return refuseUnpinnedWith(p.Name, "--trust", bad, uncachedUnpinnedClosing)
	}
	err := resolver.WarmPinnedIncludes(p)
	var unpinned *resolver.UnpinnedUncachedError
	if errors.As(err, &unpinned) {
		return refuseUnpinnedWith(p.Name, "--trust", []string{fmt.Sprintf("%s: %s", unpinned.Git, notPinned(unpinned.Ref))}, uncachedUnpinnedClosing)
	}
	if err != nil {
		return fmt.Errorf("--trust could not fetch the includes of %s: %w", p.Name, err)
	}
	return nil
}

// acceptFolderRunFor loads the folder harness named by ref and accepts it, for
// the commands that take --trust.
func acceptFolderRunFor(ref string, stdout io.Writer) error {
	if namespace.Classify(ref) != namespace.RefPath {
		return fmt.Errorf("--trust is for a run from a folder; an installed harness is accepted with 'ynh trust accept %s', or at install with 'ynh install --trust'", ref)
	}
	p, err := harness.LoadIDOrPath(ref)
	if err != nil {
		return err
	}
	return acceptFolderRun(ref, p, stdout)
}

// noteUntrustedAfterChange says, after `ynh update` or an include change,
// that the harness is now refused because what it executes moved since it was
// accepted (decision 14). before is its state before the change.
func noteUntrustedAfterChange(id string, before string, w io.Writer) {
	p, err := harness.LoadByID(id)
	if err != nil {
		return
	}
	store, err := loadTrustStore()
	if err != nil {
		return
	}
	st := trustStatusFor(id, p, store, nil)
	if before != trustAccepted || st.State != trustChanged {
		return
	}
	_, _ = fmt.Fprintf(w, "\n%s is no longer trusted: what it executes changed since you accepted it on %s, so ynh run, ynh agent run and ynh check refuse it.\n"+
		"  Review it: ynh trust show %s\n  Then accept: ynh trust accept %s\n", id, st.AcceptedAt, id, id)
}

// trustStateOf is the trust state of the installed harness id, "" when it
// cannot be told.
func trustStateOf(id string) string {
	p, err := harness.LoadByID(id)
	if err != nil {
		return ""
	}
	store, err := loadTrustStore()
	if err != nil {
		return ""
	}
	return trustStatusFor(id, p, store, nil).State
}

// includeHarnessRef is the harness argument of ynh include add or update: the
// first word that is neither a flag nor a flag's value, wherever the flags sit.
func includeHarnessRef(args []string) string {
	// Flags that take a value, for include add and include update.
	valued := map[string]bool{"--path": true, "--pick": true, "--ref": true, "--as": true, "--from-path": true}
	for i := 0; i < len(args); i++ {
		switch {
		case valued[args[i]]:
			i++
		case strings.HasPrefix(args[i], "-"):
		default:
			return args[i]
		}
	}
	return ""
}

// takeTrustFlag removes every --trust from args and reports whether there was
// one.
func takeTrustFlag(args []string) ([]string, bool) {
	out := make([]string, 0, len(args))
	found := false
	for _, a := range args {
		switch a {
		case "--trust":
			found = true
		default:
			out = append(out, a)
		}
	}
	return out, found
}

// pinProblems lists what keeps the installed harness p from being accepted at
// a full SHA: its own ref when it floats from a git or registry source, and
// each remote include that brings commands (decisions 4b and 14).
func pinProblems(p *harness.Harness) ([]string, error) {
	return pinProblemsFix(p, updateFetchFix)
}

// pinProblemsFix is pinProblems with the Fix line of an include that cannot be
// read named by the caller.
func pinProblemsFix(p *harness.Harness, fix string) ([]string, error) {
	var bad []string
	if harnessHasRemoteSource(p) {
		if why := notPinned(p.InstalledFrom.Ref); why != "" {
			bad = append(bad, "the harness's own ref: "+why)
		}
	}
	remote, err := remoteIncludesFor(p, "accept", fix)
	if err != nil {
		return nil, err
	}
	return append(bad, unpinnedIncludes(remote)...), nil
}

// checkIncludeChange is the check before the write of --trust on ynh include
// add, update and remove: it builds the harness id as the change would leave
// it, in memory, and refuses the change when something that runs would not be
// pinned to a full SHA, or when the includes cannot be read in full. Nothing
// has been written when it returns, so a refusal leaves the manifest and the
// trust record as they were (ynh #1041).
func checkIncludeChange(id string, edit harness.ManifestEdit) error {
	p, err := harness.LoadByID(id)
	if err != nil {
		return err
	}
	candidate, err := p.WithManifestEdit(edit)
	if err != nil {
		return err
	}
	bad, err := pinProblemsFix(candidate, changeNotMadeFetchFix(id))
	if err != nil {
		return err
	}
	if len(bad) > 0 {
		return refuseUnpinnedWith(id+" after this change", "--trust", bad,
			"The include change was not made. Pin each of them to a 40-character commit SHA and run it again with --trust, "+
				"or make the change without --trust: "+id+" then reads 'changed' and is refused until each remote include is pinned and it is accepted")
	}
	return nil
}

// changeNotMadeFetchFix is the Fix line of an include that cannot be read when
// --trust refused an include change before writing it. The harness was left
// unchanged, so 'ynh update' cannot fetch the include yet: the change is made
// without --trust first, and 'ynh trust accept' then takes it only once each
// remote include is pinned to a full SHA.
func changeNotMadeFetchFix(id string) string {
	return "The include change was not made, so 'ynh update' has nothing to fetch yet. " +
		"Make the change without --trust, run 'ynh update " + id + "' to fetch them, " +
		"then 'ynh trust accept " + id + "' once each remote include is pinned to a full 40-character commit SHA."
}

// acceptIncludeChange records the acceptance of id once the include change
// that checkIncludeChange passed has been written.
func acceptIncludeChange(id string, stdout io.Writer) error {
	p, err := harness.LoadByID(id)
	if err != nil {
		return err
	}
	return recordAcceptance(id, p, viaIncludeTrus, stdout)
}

// acceptAfterChange is the --trust of `ynh update` and `ynh include`
// (decision 14): once the change is made it records the acceptance of the
// installed harness id in one step, with via, and only when everything that
// runs is pinned to a full SHA.
func acceptAfterChange(id, via string, stdout io.Writer) error {
	p, err := harness.LoadByID(id)
	if err != nil {
		return err
	}
	bad, err := pinProblems(p)
	if err != nil {
		return err
	}
	if len(bad) > 0 {
		return refuseUnpinned(id+" after this change", "--trust", bad)
	}
	return recordAcceptance(id, p, via, stdout)
}
