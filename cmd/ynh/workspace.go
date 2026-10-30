package main

import (
	"fmt"
	"io"
	"os"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/vendor"
	"github.com/eyelock/ynh/internal/workspace"
)

// A harness requests workspace folders; the operator grants them
// (docs/workspace.md). This file is the glue between the manifest, the
// operator's allow-list and trust record, and the launch. The decision itself
// is internal/workspace; the vendor flag is internal/vendor.WorkspaceArgs.

// isRemoteInstall reports whether a harness came from a remote source. A
// harness installed from git or a registry is somebody else's text, so it is
// never granted a folder. Local installs, forks, configured sources and
// harnesses run from a path carry source_type local or source (or no
// provenance at all) and are the operator's own. Any other source type,
// including an unrecognised or empty one on an installed harness, is treated
// as remote: the safe reading of a value this code does not know.
func isRemoteInstall(p *harness.Harness) bool {
	if p == nil || p.InstalledFrom == nil {
		return false
	}
	switch p.InstalledFrom.SourceType {
	case "local", "source":
		return false
	}
	return true
}

// workspaceTrusted reports whether id is trust-accepted with a digest that
// covers p's current workspace entries. The digest covers the whole reviewed
// set, so any change to it, workspace or not, reads as not accepted.
func workspaceTrusted(id string, p *harness.Harness) bool {
	if id == "" || p == nil {
		return false
	}
	store, err := loadTrustStore()
	if err != nil {
		return false
	}
	rec, ok := store.Harnesses[id]
	if !ok {
		return false
	}
	set := harnessExecutables(p)
	if len(set.Problems) > 0 {
		return false
	}
	return rec.Digest == executablesDigest(set)
}

// workspaceAllowlist loads the operator's allow-list from the config file and
// YNH_WORKSPACE_ROOTS, which replaces the configured list when set.
func workspaceAllowlist(home string) workspace.Allowlist {
	var roots []string
	if cfg, err := config.Load(); err == nil {
		roots = cfg.WorkspaceRoots
	}
	env, set := os.LookupEnv(workspace.EnvRoots)
	return workspace.LoadAllowlist(roots, env, set, home)
}

// workspaceGrant decides p's requested workspace folders for a launch under
// vendorName, prints one line per folder on stderr, and returns the folders to
// grant. id is the installed id the harness is trusted under, or "" for a
// harness run from a path, which has no trust record. loaded must be the
// harness as installed, before a profile or focus reshaped it: the trust
// digest covers that form.
//
// A vendor with no way to grant a folder gets every granted folder reported
// as not granted, naming the vendor. Nothing is dropped silently.
func workspaceGrant(loaded *harness.Harness, id, vendorName, projectDir string, noWorkspace bool, stderr io.Writer) []string {
	if loaded == nil || len(loaded.Workspace) == 0 {
		return nil
	}
	if noWorkspace {
		_, _ = fmt.Fprintf(stderr, "  workspace: --no-workspace, %d requested folder(s) not granted\n", len(loaded.Workspace))
		return nil
	}
	home, _ := os.UserHomeDir()
	allow := workspaceAllowlist(home)
	for _, n := range allow.Notes {
		_, _ = fmt.Fprintf(stderr, "  warning: workspace: %s\n", n)
	}
	hint := fmt.Sprintf("no workspace allow-list is set and the harness is not trusted: run 'ynh trust accept %s', or set %s", id, workspace.EnvRoots)
	if id == "" {
		hint = fmt.Sprintf("no workspace allow-list is set, and a harness run from a path has no trust record: install it and run 'ynh trust accept', or set %s", workspace.EnvRoots)
	}
	entries := workspace.Decide(workspace.Request{
		Entries:    loaded.Workspace,
		HarnessDir: loaded.Dir,
		ProjectDir: projectDir,
		Home:       home,
		Remote:     isRemoteInstall(loaded),
		Allow:      allow,
		Trusted:    !allow.Set && workspaceTrusted(id, loaded),
		TrustHint:  hint,
	})
	var granted []string
	for _, e := range entries {
		if !e.Granted {
			_, _ = fmt.Fprintf(stderr, "  warning: workspace: %q not granted: %s\n", e.Requested, e.Reason)
			continue
		}
		granted = append(granted, e.Resolved)
	}
	if len(granted) == 0 {
		return nil
	}
	if _, unsupported, err := vendor.WorkspaceArgs(vendorName, granted); err != nil || unsupported != "" {
		reason := unsupported
		if err != nil {
			reason = err.Error()
		}
		for _, g := range granted {
			_, _ = fmt.Fprintf(stderr, "  warning: workspace: %s not granted: %s: %s\n", g, vendorName, reason)
		}
		return nil
	}
	for _, e := range entries {
		if e.Granted {
			_, _ = fmt.Fprintf(stderr, "  workspace: granted %s (%s)\n", e.Resolved, e.Reason)
		}
	}
	return granted
}

// workspaceLaunchArgs is workspaceGrant rendered as the vendor's flags for
// `ynh run`.
func workspaceLaunchArgs(loaded *harness.Harness, id, vendorName string, noWorkspace bool, stderr io.Writer) ([]string, error) {
	projectDir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	granted := workspaceGrant(loaded, id, vendorName, projectDir, noWorkspace, stderr)
	args, _, err := vendor.WorkspaceArgs(vendorName, granted)
	return args, err
}

// workspaceReport is the `trust show` view of a harness's workspace request.
type workspaceReport struct {
	// AllowList is "env" or "config" when an operator allow-list is in force,
	// and absent when none is, in which case trust decides.
	AllowList string `json:"allow_list,omitempty"`
	// RemoteInstall is true when the harness came from a remote source, so
	// nothing below is ever granted.
	RemoteInstall bool                   `json:"remote_install,omitempty"`
	Entries       []workspaceReportEntry `json:"entries"`
}

type workspaceReportEntry struct {
	Requested string `json:"requested"`
	Resolved  string `json:"resolved"`
	// Refused is why this folder can never be granted, when it cannot.
	Refused string `json:"refused,omitempty"`
	// AllowListCovers is whether the allow-list would grant it. Absent with no
	// allow-list.
	AllowListCovers *bool `json:"allow_list_covers,omitempty"`
}

// workspaceReportFor builds the report, or nil when the harness requests no
// folder. It decides nothing: it shows what a run from the current directory
// would meet.
func workspaceReportFor(p *harness.Harness) *workspaceReport {
	if len(p.Workspace) == 0 {
		return nil
	}
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	allow := workspaceAllowlist(home)
	r := &workspaceReport{RemoteInstall: isRemoteInstall(p), Entries: []workspaceReportEntry{}}
	if allow.Set {
		r.AllowList = allow.Source
	}
	for _, e := range p.Workspace {
		abs := workspace.Expand(e, p.Dir, home)
		ent := workspaceReportEntry{Requested: e, Resolved: workspace.Resolve(e, p.Dir, home)}
		ent.Refused = workspace.Refusal(abs, p.Dir, cwd, home)
		if allow.Set && ent.Refused == "" {
			c := allow.Covers(ent.Resolved)
			ent.AllowListCovers = &c
		}
		r.Entries = append(r.Entries, ent)
	}
	return r
}

func printWorkspaceSection(w io.Writer, r *workspaceReport) {
	if r == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "\nWorkspace folders, requested for the vendor CLI to reach (a request, not a grant):\n\n")
	for _, e := range r.Entries {
		_, _ = fmt.Fprintf(w, "  %s\n    resolves to: %s\n", e.Requested, e.Resolved)
		switch {
		case e.Refused != "":
			_, _ = fmt.Fprintf(w, "    never granted: %s\n", e.Refused)
		case e.AllowListCovers != nil && *e.AllowListCovers:
			_, _ = fmt.Fprintf(w, "    the %s allow-list covers it\n", allowListLabel(r.AllowList))
		case e.AllowListCovers != nil:
			_, _ = fmt.Fprintf(w, "    outside the %s allow-list: not granted\n", allowListLabel(r.AllowList))
		}
	}
	switch {
	case r.RemoteInstall:
		_, _ = fmt.Fprintf(w, "\nThis harness was installed from a remote source, so none of these is ever granted.\n")
	case r.AllowList != "":
		_, _ = fmt.Fprintf(w, "\nThe allow-list grants a covered folder without trust. Trust cannot grant one it does not cover.\n")
	default:
		_, _ = fmt.Fprintf(w, "\nNo allow-list is set, so these are granted only while this harness is accepted.\n")
	}
}

func allowListLabel(source string) string {
	if source == "env" {
		return workspace.EnvRoots
	}
	return "workspace_roots"
}
