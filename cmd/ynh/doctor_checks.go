package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
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
