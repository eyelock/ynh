package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/symlink"
)

// Severity values. `error` means something is broken now; `warn` means it will
// bite under some condition; `info` is context with no action.
const (
	sevOK    = "ok"
	sevInfo  = "info"
	sevWarn  = "warn"
	sevError = "error"
)

// doctorFinding is one result. Remedy carries the command that fixes it:
// a diagnostic that names a problem without naming the fix makes the user
// go looking, which is the state they were already in.
type doctorFinding struct {
	Severity string `json:"severity"`
	Subject  string `json:"subject,omitempty"`
	Message  string `json:"message"`
	Remedy   string `json:"remedy,omitempty"`
}

// doctorCheck groups findings from one diagnostic area.
type doctorCheck struct {
	Name     string          `json:"name"`
	Title    string          `json:"title"`
	Status   string          `json:"status"`
	Findings []doctorFinding `json:"findings"`
}

// worst returns the check's overall status: the most severe finding it holds.
func (c *doctorCheck) worst() string {
	status := sevOK
	for _, f := range c.Findings {
		switch {
		case f.Severity == sevError:
			return sevError
		case f.Severity == sevWarn:
			status = sevWarn
		case f.Severity == sevInfo && status == sevOK:
			status = sevInfo
		}
	}
	return status
}

// ---- vendors -------------------------------------------------------------

// checkVendors reports vendor CLIs that installed harnesses expect but which
// are not on PATH. Only vendors something actually targets are reported: a
// missing `copilot` matters to someone whose harness defaults to it and to
// nobody else.
func checkVendors() doctorCheck {
	c := doctorCheck{Name: "vendors", Title: "vendor CLIs"}

	entries, err := vendorEntries()
	if err != nil {
		c.Findings = append(c.Findings, doctorFinding{Severity: sevError,
			Message: fmt.Sprintf("cannot inspect vendor adapters: %v", err)})
		c.Status = c.worst()
		return c
	}
	available := make(map[string]bool, len(entries))
	cli := make(map[string]string, len(entries))
	for _, e := range entries {
		available[e.Name] = e.Available
		cli[e.Name] = e.CLI
	}

	wanted := map[string][]string{} // vendor -> harness ids that want it
	list, err := harness.ListAll()
	if err == nil {
		for _, e := range list {
			id := canonicalIDForEntry(e)
			h, err := harness.LoadByID(id)
			if err != nil || h.DefaultVendor == "" {
				continue
			}
			wanted[h.DefaultVendor] = append(wanted[h.DefaultVendor], id)
		}
	}

	for _, v := range sortedVendorNames(wanted) {
		if available[v] {
			continue
		}
		c.Findings = append(c.Findings, doctorFinding{
			Severity: sevError,
			Subject:  v,
			Message: fmt.Sprintf("%s is the default vendor for %s, but %q is not on PATH",
				v, strings.Join(wanted[v], ", "), cli[v]),
			Remedy: fmt.Sprintf("install the %s CLI, or set another vendor with: ynh run <harness> -v <vendor>", v),
		})
	}
	c.Status = c.worst()
	return c
}

func sortedVendorNames(m map[string][]string) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// ---- installed harnesses -------------------------------------------------

// checkHarnesses loads every installed harness. A pointer whose target moved,
// or a manifest that stopped parsing after a migration, fails here rather than
// at run time with no warning.
func checkHarnesses() doctorCheck {
	c := doctorCheck{Name: "harnesses", Title: "installed harnesses"}

	list, err := harness.ListAll()
	if err != nil {
		c.Findings = append(c.Findings, doctorFinding{Severity: sevError,
			Message: fmt.Sprintf("cannot list installed harnesses: %v", err),
			Remedy:  "check " + config.HarnessesDir()})
		c.Status = c.worst()
		return c
	}
	if len(list) == 0 {
		c.Findings = append(c.Findings, doctorFinding{Severity: sevInfo,
			Message: "no harnesses installed",
			Remedy:  "ynh install <git-url|path>"})
		c.Status = c.worst()
		return c
	}
	for _, e := range list {
		id := canonicalIDForEntry(e)
		if _, err := harness.LoadByID(id); err != nil {
			c.Findings = append(c.Findings, doctorFinding{
				Severity: sevError,
				Subject:  id,
				Message:  fmt.Sprintf("listed as installed but will not load: %v", err),
				Remedy:   fmt.Sprintf("ynh uninstall %s, then reinstall it", id),
			})
		}
	}
	c.Status = c.worst()
	return c
}

// ---- symlinks ------------------------------------------------------------

// checkSymlinks reports recorded installations whose links no longer exist or
// no longer point where the log says. `ynh prune` cleans run directories, not
// these, so nothing else notices.
func checkSymlinks() doctorCheck {
	c := doctorCheck{Name: "symlinks", Title: "project symlinks"}

	log, err := symlink.LoadLog()
	if err != nil {
		c.Findings = append(c.Findings, doctorFinding{Severity: sevError,
			Message: fmt.Sprintf("cannot read the symlink log: %v", err),
			Remedy:  "check " + symlink.LogPath()})
		c.Status = c.worst()
		return c
	}
	for _, ins := range log.Installations {
		if _, err := os.Stat(ins.Project); err != nil {
			c.Findings = append(c.Findings, doctorFinding{
				Severity: sevWarn,
				Subject:  ins.Project,
				Message: fmt.Sprintf("%s/%s is recorded here, but the project directory is gone",
					ins.Harness, ins.Vendor),
				Remedy: "ynh prune",
			})
			continue
		}
		for _, e := range ins.Symlinks {
			if _, err := os.Lstat(e.Link); err != nil {
				c.Findings = append(c.Findings, doctorFinding{
					Severity: sevWarn,
					Subject:  e.Link,
					Message: fmt.Sprintf("recorded for %s/%s in %s, but the link is missing",
						ins.Harness, ins.Vendor, ins.Project),
					Remedy: fmt.Sprintf("ynh run %s -v %s --install", ins.Harness, ins.Vendor),
				})
			}
		}
	}
	c.Status = c.worst()
	return c
}

// ---- launcher / PATH -----------------------------------------------------

// checkLauncher reports a bin directory that is not on PATH. `ynh install`
// reports success and writes a launcher there, so without this the user is
// told everything worked and then cannot run the thing.
func checkLauncher() doctorCheck {
	c := doctorCheck{Name: "launcher", Title: "launcher directory on PATH"}

	bin := config.BinDir()
	if _, err := os.Stat(bin); err != nil {
		c.Findings = append(c.Findings, doctorFinding{Severity: sevInfo,
			Subject: bin, Message: "no launcher directory yet; it is created on first install"})
		c.Status = c.worst()
		return c
	}
	if onPath(bin) {
		c.Status = c.worst()
		return c
	}
	c.Findings = append(c.Findings, doctorFinding{
		Severity: sevWarn,
		Subject:  bin,
		Message:  "launchers are written here, but it is not on PATH, so installed harnesses are not runnable by name",
		Remedy:   fmt.Sprintf("export PATH=%q:$PATH", bin),
	})
	c.Status = c.worst()
	return c
}

// onPath reports whether dir is one of PATH's entries, comparing resolved
// paths so a symlinked or trailing-slash entry still matches.
func onPath(dir string) bool {
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		want = filepath.Clean(dir)
	}
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p == "" {
			continue
		}
		got, err := filepath.EvalSymlinks(p)
		if err != nil {
			got = filepath.Clean(p)
		}
		if got == want {
			return true
		}
	}
	return false
}

// ---- quarantine ----------------------------------------------------------

// checkQuarantine surfaces anything a failed migration set aside. Today you
// have to know `ynh quarantine list` exists to discover it.
func checkQuarantine() doctorCheck {
	c := doctorCheck{Name: "quarantine", Title: "quarantined harnesses"}

	entries, err := scanQuarantine()
	if err != nil {
		c.Findings = append(c.Findings, doctorFinding{Severity: sevError,
			Message: fmt.Sprintf("cannot read the quarantine directory: %v", err)})
		c.Status = c.worst()
		return c
	}
	for _, e := range entries {
		c.Findings = append(c.Findings, doctorFinding{
			Severity: sevWarn,
			Subject:  e.Name,
			Message:  "set aside by a failed migration and not in use",
			Remedy:   fmt.Sprintf("ynh quarantine restore %s, or ynh quarantine drop %s", e.Name, e.Name),
		})
	}
	c.Status = c.worst()
	return c
}

// ---- delegates -----------------------------------------------------------

// checkDelegates resolves each installed harness's delegates_to the way
// `ynh run` does, without fetching or writing anything: a delegate from the
// harness's own repository is read from the committed tree, any other from
// the cache. A harness can install and load and still fail every run because
// a delegate stopped resolving (#806); nothing else in doctor looks.
//
// Every depth is checked: a delegate's own delegates_to are walked too, as a
// run's flattening reaches them, each finding naming the chain that reached it
// ("desk > worker > reviewer"). A finding belongs to the delegates_to entry
// that names the broken delegate, and is reported once: a harness is walked
// once however many routes reach it, so its entries are checked once, and the
// root, reached again through its own source or by its name, is skipped as a
// run skips it. Two entries that name one missing harness are two edits, in
// two harnesses, and give two findings. A cycle is not itself a finding: a run
// skips the edge back to a harness already reached rather than failing on it.
//
// This follows what `ynh run` does today: assembler.AssembleDelegates, then
// resolver.FlattenDelegates, then ResolveSourceForRun. resolveDelegate
// (cmd/ynh/delegate_resolve.go) is called only by `ynh delegate-run start`,
// one entry at a time, as a delegate's own session begins. After ynw-golo,
// `transport: native` keeps this flattening in `ynh run`, and an a2a
// delegate's session is started lazily by `ynh delegate-run`, which resolves
// it with resolveDelegate (installed and trusted). So the check is not
// expected to move wholesale to resolveDelegate: once the transport setting
// lands (ynw-op8n.8, ynh #825) it has to follow each entry's transport,
// flattening for a native entry and resolveDelegate for an a2a one. Until
// then it walks every entry as the flattening does.
func checkDelegates() doctorCheck {
	c := doctorCheck{Name: "delegates", Title: "delegates"}

	list, err := harness.ListAll()
	if err != nil {
		// checkHarnesses reports the unlistable directory.
		c.Status = c.worst()
		return c
	}
	for _, e := range list {
		id := canonicalIDForEntry(e)
		h, err := harness.LoadByID(id)
		if err != nil {
			continue // checkHarnesses reports it
		}
		c.Findings = append(c.Findings, walkDelegates(id, h)...)
	}
	c.Status = c.worst()
	return c
}

// walkDelegates checks every delegate a run of the installed harness id
// reaches, breadth first, and returns what would fail it.
func walkDelegates(id string, root *harness.Harness) []doctorFinding {
	type step struct {
		entry harness.Delegate
		dir   string   // the declaring harness's folder
		owner string   // the declaring harness's name
		chain []string // names from the root down to the declaring harness
	}
	var out []doctorFinding
	seen := map[string]bool{realDir(root.Dir): true}
	// The root is recognised the way a run's flattening recognises it: by
	// its folder, or by its name, so reaching it through its source's cached
	// clone does not walk it again.
	var queue []step
	for _, d := range root.DelegatesTo {
		queue = append(queue, step{d, root.Dir, id, []string{root.Name}})
	}
	for len(queue) > 0 {
		st := queue[0]
		queue = queue[1:]
		f, bad, dir := checkDelegate(id, st.owner, st.dir, st.entry)
		if bad {
			if len(st.chain) > 1 {
				f.Message += " (reached by " + strings.Join(st.chain, " > ") + " > " + delegateLabel(st.entry.GitSource) + ")"
			}
			out = append(out, f)
			continue
		}
		if dir == "" || seen[realDir(dir)] {
			continue
		}
		seen[realDir(dir)] = true
		h, err := harness.LoadDir(dir)
		if err == nil && h.Name == root.Name {
			continue
		}
		if err != nil {
			out = append(out, doctorFinding{Severity: sevError, Subject: id,
				Message: fmt.Sprintf("delegate %q cannot be loaded (reached by %s): %v", delegateLabel(st.entry.GitSource), strings.Join(st.chain, " > "), err),
				Remedy:  fmt.Sprintf("fix its manifest, or remove the entry from delegates_to of %s", st.owner)})
			continue
		}
		chain := append(append([]string{}, st.chain...), h.Name)
		for _, d := range h.DelegatesTo {
			queue = append(queue, step{d, dir, h.Name, chain})
		}
	}
	return out
}

func delegateLabel(gs harness.GitSource) string {
	if gs.Path != "" {
		return gs.Path
	}
	if gs.IsLocal() {
		return gs.Local
	}
	return gs.Git
}

// checkDelegate reports whether one delegates_to entry, declared by the
// harness named owner living in harnessDir, would fail a run. When it would
// not, it returns the folder the delegate resolves to, read without fetching,
// so the walk can go on into the delegate's own entries; "" when it cannot say.
func checkDelegate(id, owner, harnessDir string, d harness.Delegate) (doctorFinding, bool, string) {
	gs := d.GitSource
	label := delegateLabel(gs)
	fail := func(sev, msg, remedy string) (doctorFinding, bool, string) {
		return doctorFinding{Severity: sev, Subject: id, Message: msg, Remedy: remedy}, true, ""
	}

	if gs.IsLocal() {
		base := gs.Local
		if !filepath.IsAbs(base) {
			base = filepath.Join(harnessDir, base)
		}
		dir := filepath.Join(base, gs.Path)
		if _, err := os.Stat(dir); err != nil {
			return fail(sevError, fmt.Sprintf("delegate %q: local path %q does not exist", label, gs.Local),
				fmt.Sprintf("restore it, or remove the entry from delegates_to of %s", owner))
		}
		return doctorFinding{}, false, dir
	}

	repo := resolver.GitSourceURL(gs.Git, harnessDir)

	if resolver.IsOwnRepoSource(gs, harnessDir) {
		// run clones this folder (EnsureRepo on GitSourceURL), and git can
		// clone only a repository's root: a harness in a subfolder of its
		// repository, with git ".", fails every run however its delegate is
		// committed.
		if !isRepoRoot(repo) {
			return fail(sevError,
				fmt.Sprintf("delegate %q: %s is a subfolder of its repository, and `ynh run` can clone only a repository's root", label, repo),
				fmt.Sprintf("give the delegate a git URL in delegates_to of %s, or keep the harness at the root of its repository", owner))
		}
		ref := gs.Ref
		if ref == "" {
			ref = "HEAD"
		}
		// "./" makes the path relative to repo (the harness's folder), as the
		// stat and the remedy below are, not to the repository's top.
		spec := ref + ":./" + filepath.ToSlash(filepath.Clean(gs.Path))
		if gs.Path == "" {
			spec = ref + "^{tree}"
		}
		if err := exec.Command("git", "-C", repo, "cat-file", "-e", spec).Run(); err == nil {
			// Committed. Read its own entries from the working tree: the
			// walk fetches nothing, and a run's clone holds the same files
			// unless they changed since the commit.
			return doctorFinding{}, false, filepath.Join(repo, gs.Path)
		}
		if gs.Path != "" {
			if _, err := os.Stat(filepath.Join(repo, gs.Path)); err == nil {
				return fail(sevError,
					fmt.Sprintf("delegate %q is not in the committed tree of %s (it exists on disk but is not committed)", label, gs.Git),
					fmt.Sprintf("git -C %s add %s && git -C %s commit", repo, gs.Path, repo))
			}
		}
		return fail(sevError,
			fmt.Sprintf("delegate %q is missing from the committed tree of %s (and not on disk)", label, gs.Git),
			fmt.Sprintf("restore %s in %s and commit it, or remove the entry from delegates_to of %s", label, repo, owner))
	}

	res, ok := resolver.LookupCache(repo, gs.Ref)
	if !ok {
		return fail(sevWarn,
			fmt.Sprintf("delegate %q: %s is not cached, so the next run fetches it", label, gs.Git),
			fmt.Sprintf("ynh update %s", id))
	}
	dir := filepath.Join(res.Path, gs.Path)
	if gs.Path != "" {
		if _, err := os.Stat(dir); err != nil {
			return fail(sevError,
				fmt.Sprintf("delegate %q is missing from the cached copy of %s", label, gs.Git),
				fmt.Sprintf("ynh update %s", id))
		}
	}
	return doctorFinding{}, false, dir
}

// isRepoRoot reports whether dir is the top of a git working tree.
func isRepoRoot(dir string) bool {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return false
	}
	top, err1 := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
	here, err2 := filepath.EvalSymlinks(dir)
	return err1 == nil && err2 == nil && top == here
}
