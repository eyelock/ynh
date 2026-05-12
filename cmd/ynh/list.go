package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/migration"
	"github.com/eyelock/ynh/internal/namespace"
	"github.com/eyelock/ynh/internal/resolver"
)

// listEnvelope wraps the array of harnesses with the wire-contract version
// (capabilities) and the ynh release version. Every structured response from
// ls and info follows this envelope shape so consumers can gate behaviour
// without re-parsing the per-command body.
type listEnvelope struct {
	Capabilities string `json:"capabilities"`
	// SchemaVersion is the on-disk format version of ~/.ynh that this
	// binary expects. Distinct from Capabilities (the wire-contract
	// version): consumers gate format-level decisions (e.g. "is migration
	// required before I can trust id?") on schema_version, and gate
	// shape-level decisions (e.g. "does this binary emit field X?") on
	// capabilities. See internal/config.SchemaVersion.
	SchemaVersion int         `json:"schema_version"`
	YnhVersion    string      `json:"ynh_version"`
	Harnesses     []listEntry `json:"harnesses"`
}

// listEntry is the JSON shape for a single harness in the `ynh ls` output.
// Field order drives JSON key order via MarshalIndent.
type listEntry struct {
	// ID is the canonical, host-prefixed harness id —
	// "<host>/<org>/<repo>/<name>" for remote-sourced installs, or
	// "local/<name>" for local/forked/aliased installs. This is the single
	// identity form consumers should pass back to ynh at the CLI boundary
	// for any harness-targeting command (info, remove, update, uninstall).
	//
	// Derived on-read from installed_from.source + name in schema 1; will
	// be stored explicitly in installed.json under schema 2.
	ID   string `json:"id"`
	Name string `json:"name"`
	// Namespace is the URL-derived "<org>/<repo>" for namespaced installs
	// (registry installs, or any install that landed under
	// ~/.ynh/harnesses/<ns>/<name>/). Empty for flat/local installs.
	// Retained for back-compat with schema-1 consumers; new consumers
	// should prefer ID, which is host-prefixed and self-describing.
	Namespace string `json:"namespace,omitempty"`
	// Kind classifies the install: "local-fork" (pointer-shaped, forked
	// from an upstream), "local" (local path, no upstream), "registry",
	// "git", or "-" for pre-migration entries with no recorded source.
	Kind             string `json:"kind"`
	VersionInstalled string `json:"version_installed"`
	VersionAvailable string `json:"version_available,omitempty"`
	Description      string `json:"description,omitempty"`
	DefaultVendor    string `json:"default_vendor"`
	Path             string `json:"path"`
	RefInstalled     string `json:"ref_installed,omitempty"`
	RefAvailable     string `json:"ref_available,omitempty"`
	// SHAAvailable is the live upstream SHA for the harness source itself
	// (probed via ls-remote against the recorded install ref). Distinct from
	// RefAvailable, which for registry harnesses comes from the registry
	// entry's recorded SHA. Both can be present — answers different
	// questions: "has the source moved?" vs "has the registry entry moved?".
	SHAAvailable  string             `json:"sha_available,omitempty"`
	IsPinned      bool               `json:"is_pinned"`
	InstalledFrom *listInstalledFrom `json:"installed_from,omitempty"`
	Artifacts     listArtifacts      `json:"artifacts"`
	// BrokenReason is non-empty when the harness registration exists but the
	// source could not be loaded (e.g. plugin.json missing, source path gone).
	// Kind is "local-fork-broken" in this case. Absent for healthy entries.
	BrokenReason string         `json:"broken_reason,omitempty"`
	Includes     []listInclude  `json:"includes"`
	DelegatesTo  []listDelegate `json:"delegates_to"`
}

type listInstalledFrom struct {
	SourceType string `json:"source_type"`
	Source     string `json:"source"`
	// Ref is the branch/tag/SHA recorded at install time — the ref this
	// harness actually tracks. Used by --check-updates to probe the same ref
	// ynh update tracks. Empty for pre-migration installs.
	Ref          string          `json:"ref,omitempty"`
	SHA          string          `json:"sha,omitempty"`
	Path         string          `json:"path,omitempty"`
	RegistryName string          `json:"registry_name,omitempty"`
	InstalledAt  string          `json:"installed_at"`
	ForkedFrom   *listForkedFrom `json:"forked_from,omitempty"`
}

// listForkedFrom is the JSON shape of installed_from.forked_from — the
// upstream a local harness was forked from. Populated by `ynh fork`; absent
// otherwise.
type listForkedFrom struct {
	SourceType   string `json:"source_type"`
	Source       string `json:"source"`
	Ref          string `json:"ref,omitempty"`
	SHA          string `json:"sha,omitempty"`
	Path         string `json:"path,omitempty"`
	RegistryName string `json:"registry_name,omitempty"`
	Version      string `json:"version,omitempty"`
}

type listArtifacts struct {
	Skills   int `json:"skills"`
	Agents   int `json:"agents"`
	Rules    int `json:"rules"`
	Commands int `json:"commands"`
}

type listInclude struct {
	Git string `json:"git"`
	// Ref is the ref this include actually tracks — equal to the manifest
	// pin if non-empty, otherwise the resolved branch name recorded in
	// installed.json (e.g. "main" for an empty manifest ref where the
	// cache resolved to main at clone time). Internal field, not emitted
	// in JSON output. Used by --check-updates to probe the same ref that
	// ynh update tracks, so the two stay consistent across upstream
	// default-branch changes.
	Ref          string   `json:"-"`
	RefInstalled string   `json:"ref_installed,omitempty"`
	RefAvailable string   `json:"ref_available,omitempty"`
	IsPinned     bool     `json:"is_pinned"`
	Path         string   `json:"path,omitempty"`
	Pick         []string `json:"pick,omitempty"`
}

type listDelegate struct {
	Git string `json:"git"`
	// Ref — see listInclude.Ref.
	Ref          string `json:"-"`
	RefInstalled string `json:"ref_installed,omitempty"`
	RefAvailable string `json:"ref_available,omitempty"`
	IsPinned     bool   `json:"is_pinned"`
	Path         string `json:"path,omitempty"`
}

func cmdList(args []string) error {
	return cmdListTo(args, os.Stdout, os.Stderr)
}

func cmdListTo(args []string, stdout, stderr io.Writer) error {
	structured := detectJSONFormat(args)

	format := "text"
	checkUpdates := false
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--format requires a value")
			}
			i++
			format = args[i]
		case "--check-updates":
			checkUpdates = true
		default:
			if strings.HasPrefix(args[i], "-") {
				return cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unknown flag: %s", args[i]))
			}
			return cliError(stderr, structured, errCodeInvalidInput,
				fmt.Sprintf("unexpected argument: %s", args[i]))
		}
		i++
	}

	switch format {
	case "text":
		if checkUpdates {
			return cliError(stderr, structured, errCodeInvalidInput,
				"--check-updates requires --format json")
		}
		return printListText(stdout)
	case "json":
		return printListJSON(stdout, checkUpdates)
	default:
		return cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("invalid --format value %q (want text or json)", format))
	}
}

func printListText(w io.Writer) error {
	entries, err := harness.ListAll()
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		_, _ = fmt.Fprintln(w, "No harnesses installed.")
		_, _ = fmt.Fprintln(w, "Install one with: ynh install <git-url|path>")
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tKIND\tVENDOR\tSOURCE\tARTIFACTS\tINCLUDES\tDELEGATES TO")

	for _, e := range entries {
		// For pointer-form installs (local harnesses), load via the pointer first
		// so provenance is included. Fall back to LoadDir for tree-form installs.
		p, err := harness.LoadByID(canonicalIDForEntry(e))
		if err != nil {
			// Not a pointer-form install; try loading as a tree-form install
			p, err = harness.LoadDir(e.Dir)
		}
		if err != nil {
			_, _ = fmt.Fprintf(tw, "%s\t(error: %v)\t\t\t\t\t\n", e.Name, err)
			continue
		}

		vendorName := p.DefaultVendor
		if vendorName == "" {
			vendorName = "-"
		}

		kind := classifyKind(p.InstalledFrom)
		source := formatProvenance(p.InstalledFrom)
		artifacts := formatArtifactSummaryDir(e.Dir)
		includes := formatIncludes(p.Includes)
		delegates := formatDelegates(p.DelegatesTo)

		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", p.Name, kind, vendorName, source, artifacts, includes, delegates)
	}

	return tw.Flush()
}

func printListJSON(w io.Writer, checkUpdates bool) error {
	all, err := harness.ListAll()
	if err != nil {
		return err
	}

	entries := make([]listEntry, 0, len(all))
	for _, e := range all {
		// For pointer-form installs (local harnesses), load via the pointer first
		// so provenance is included. Fall back to LoadDir for tree-form installs.
		p, loadErr := harness.LoadByID(canonicalIDForEntry(e))
		if loadErr != nil {
			// Not a pointer-form install; try loading as a tree-form install
			p, loadErr = harness.LoadDir(e.Dir)
		}

		if loadErr != nil {
			// Load failed; check if there's a broken pointer for error details.
			// Surface broken pointer entries with kind "local-fork-broken" and
			// a broken_reason so JSON consumers (e.g. TermQ) can route them to
			// a QUARANTINED group rather than displaying them as empty-field
			// healthy harnesses.
			//
			// Pointer lookup: try schema-1 (name-keyed) then schema-2 (id-keyed)
			// so entries written by both fork generations are covered.
			ptr, _ := harness.LoadPointer(e.Name)
			if ptr == nil {
				ptr, _ = harness.LoadPointerByID(canonicalIDForEntry(e))
			}
			if ptr != nil {
				id := namespace.CanonicalID(ptr.Source, ptr.Name)
				ns, _ := namespace.SplitID(id)
				entries = append(entries, listEntry{
					ID:           id,
					Name:         ptr.Name,
					Namespace:    ns,
					Kind:         "local-fork-broken",
					Path:         ptr.Source,
					BrokenReason: loadErr.Error(),
					InstalledFrom: &listInstalledFrom{
						SourceType:  ptr.SourceType,
						Source:      ptr.Source,
						InstalledAt: ptr.InstalledAt,
					},
					Includes:    []listInclude{},
					DelegatesTo: []listDelegate{},
				})
			}
			continue
		}
		entries = append(entries, buildListEntry(p))
	}

	if checkUpdates {
		fillUpdates(entries, defaultProbe())
	}

	envelope := listEnvelope{
		Capabilities:  config.CapabilitiesVersion,
		SchemaVersion: migration.ReadSchemaVersion(config.HomeDir()),
		YnhVersion:    config.Version,
		Harnesses:     entries,
	}

	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding list: %w", err)
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

func backfillProvenanceFromCache(prov *harness.Provenance) {
	if prov == nil {
		return
	}
	if prov.SHA != "" && prov.Ref != "" {
		return
	}
	switch prov.SourceType {
	case "git", "registry":
		// continue
	default:
		return
	}
	if prov.Source == "" {
		return
	}
	res, ok := resolver.LookupCache(prov.Source, prov.Ref)
	if !ok {
		return
	}
	if prov.SHA == "" {
		prov.SHA = res.SHA
	}
	if prov.Ref == "" {
		prov.Ref = res.ResolvedRef
	}
}

// canonicalIDForHarness returns the canonical id for a loaded harness.
// Derived on-read from the recorded source URL. Under schema 2 the id is
// also stamped into installed.json by the migration routine; this helper
// remains the source of truth for what gets emitted in JSON envelopes
// because the derive logic is uniform across pre- and post-migration
// installs.
func canonicalIDForHarness(p *harness.Harness) string {
	var sourceURL string
	if p.InstalledFrom != nil {
		sourceURL = p.InstalledFrom.Source
	}
	return namespace.CanonicalID(sourceURL, p.Name)
}

// canonicalNamespaceForHarness returns the host-prefixed namespace portion
// of a harness's canonical id — everything before the last "/". Replaces
// the legacy host-stripped namespace ("eyelock/assistants") with the full
// canonical prefix ("github.com/eyelock/assistants" or "local"), so the
// invariant id == namespace + "/" + name holds in schema 2.
func canonicalNamespaceForHarness(p *harness.Harness) string {
	id := canonicalIDForHarness(p)
	ns, _ := namespace.SplitID(id)
	// "local" is an internal sentinel; consumers expect empty for flat
	// (non-namespaced) installs. Keep the canonical id self-describing
	// while preserving the schema-1 promise that `namespace` is empty
	// for local harnesses.
	if ns == "local" {
		return ""
	}
	return ns
}

// canonicalIDForEntry reconstructs the canonical id for a ListAll result.
// Pointer-form entries arrive with empty Namespace and are conventionally
// addressed as "local/<name>" (see topology.go). Tree-form entries carry
// their full namespace (e.g. "github.com/eyelock/assistants").
func canonicalIDForEntry(e harness.ListEntry) string {
	if e.Namespace == "" {
		return "local/" + e.Name
	}
	return e.Namespace + "/" + e.Name
}

// buildListEntry assembles the structured-output entry for a loaded harness.
// Shared by cmdListTo and cmdInfoTo so the per-harness shape stays uniform
// between the two commands.
func buildListEntry(p *harness.Harness) listEntry {
	// Best-effort backfill of pre-migration provenance from the cache so
	// downstream consumers see a populated installed_from.{sha,ref} without
	// requiring a manual `ynh update`.
	backfillProvenanceFromCache(p.InstalledFrom)

	entry := listEntry{
		ID:               canonicalIDForHarness(p),
		Name:             p.Name,
		Namespace:        canonicalNamespaceForHarness(p),
		Kind:             classifyKind(p.InstalledFrom),
		VersionInstalled: p.Version,
		Description:      p.Description,
		DefaultVendor:    p.DefaultVendor,
		Path:             p.Dir,
		Artifacts:        scanArtifactCounts(p.Dir),
		Includes:         buildIncludes(p.Includes),
		DelegatesTo:      buildDelegates(p.DelegatesTo),
	}

	if p.InstalledFrom != nil {
		entry.RefInstalled = p.InstalledFrom.SHA
		if entry.RefInstalled == "" {
			entry.RefInstalled = p.InstalledFrom.Ref
		}
		entry.IsPinned = harness.IsPinnedRef(entry.RefInstalled)
		entry.InstalledFrom = &listInstalledFrom{
			SourceType:   p.InstalledFrom.SourceType,
			Source:       p.InstalledFrom.Source,
			Ref:          p.InstalledFrom.Ref,
			SHA:          p.InstalledFrom.SHA,
			Path:         p.InstalledFrom.Path,
			RegistryName: p.InstalledFrom.RegistryName,
			InstalledAt:  p.InstalledFrom.InstalledAt,
		}
		if p.InstalledFrom.ForkedFrom != nil {
			ff := p.InstalledFrom.ForkedFrom
			entry.InstalledFrom.ForkedFrom = &listForkedFrom{
				SourceType:   ff.SourceType,
				Source:       ff.Source,
				Ref:          ff.Ref,
				SHA:          ff.SHA,
				Path:         ff.Path,
				RegistryName: ff.RegistryName,
				Version:      ff.Version,
			}
		}
	}

	return entry
}

func scanArtifactCounts(dir string) listArtifacts {
	arts, _ := harness.ScanArtifactsDir(dir)
	return listArtifacts{
		Skills:   len(arts.Skills),
		Agents:   len(arts.Agents),
		Rules:    len(arts.Rules),
		Commands: len(arts.Commands),
	}
}

func buildIncludes(includes []harness.Include) []listInclude {
	result := make([]listInclude, 0, len(includes))
	for _, inc := range includes {
		// Prefer the resolved SHA recorded at install/update time. Fall back
		// to the manifest ref so pre-migration installs still report something.
		refInstalled := inc.SHA
		if refInstalled == "" {
			refInstalled = inc.Ref
		}
		// Probe target: prefer the ref recorded in installed.json (the actual
		// tracked branch) over the manifest ref, so default-branch changes
		// upstream don't produce phantom drift against ynh update.
		probeRef := inc.ResolvedRef
		if probeRef == "" {
			probeRef = inc.Ref
		}
		li := listInclude{
			Git:          inc.Git,
			Ref:          probeRef,
			RefInstalled: refInstalled,
			IsPinned:     harness.IsPinnedRef(inc.Ref),
			Path:         inc.Path,
		}
		if len(inc.Pick) > 0 {
			li.Pick = inc.Pick
		}
		result = append(result, li)
	}
	return result
}

func buildDelegates(delegates []harness.Delegate) []listDelegate {
	result := make([]listDelegate, 0, len(delegates))
	for _, del := range delegates {
		refInstalled := del.SHA
		if refInstalled == "" {
			refInstalled = del.Ref
		}
		probeRef := del.ResolvedRef
		if probeRef == "" {
			probeRef = del.Ref
		}
		result = append(result, listDelegate{
			Git:          del.Git,
			Ref:          probeRef,
			RefInstalled: refInstalled,
			IsPinned:     harness.IsPinnedRef(del.Ref),
			Path:         del.Path,
		})
	}
	return result
}

// formatArtifactSummaryDir formats the ARTIFACTS column for ynh ls from an explicit directory.
func formatArtifactSummaryDir(dir string) string {
	arts, _ := harness.ScanArtifactsDir(dir)
	if arts.Total() == 0 {
		return "0"
	}
	var parts []string
	if len(arts.Skills) > 0 {
		parts = append(parts, fmt.Sprintf("%ds", len(arts.Skills)))
	}
	if len(arts.Agents) > 0 {
		parts = append(parts, fmt.Sprintf("%da", len(arts.Agents)))
	}
	if len(arts.Rules) > 0 {
		parts = append(parts, fmt.Sprintf("%dr", len(arts.Rules)))
	}
	if len(arts.Commands) > 0 {
		parts = append(parts, fmt.Sprintf("%dc", len(arts.Commands)))
	}
	return strings.Join(parts, " ")
}

// classifyKind returns the KIND label for ynh ls.
//   - local-fork: a fork registered via pointer (forked_from is set)
//   - local: installed from a local path, no upstream
//   - registry: installed via a registry reference
//   - git: installed from a remote git URL
//   - "-": unknown / pre-migration
func classifyKind(prov *harness.Provenance) string {
	if prov == nil {
		return "-"
	}
	if prov.ForkedFrom != nil {
		return "local-fork"
	}
	if prov.SourceType == "" {
		return "-"
	}
	return prov.SourceType
}

// formatProvenance formats the SOURCE column for ynh ls.
func formatProvenance(prov *harness.Provenance) string {
	if prov == nil {
		return "-"
	}
	short := shortGitURL(prov.Source)
	if prov.Path != "" {
		short += "/" + prov.Path
	}
	if prov.RegistryName != "" {
		short += " (" + prov.RegistryName + ")"
	}
	return short
}

// formatIncludes formats the INCLUDES column for ynh ls.
func formatIncludes(includes []harness.Include) string {
	if len(includes) == 0 {
		return "0"
	}
	parts := make([]string, 0, len(includes))
	for _, inc := range includes {
		s := shortGitURL(inc.Git)
		if inc.Path != "" {
			s += "/" + inc.Path
		}
		if inc.Ref != "" && inc.Ref != "main" && inc.Ref != "HEAD" {
			s += "@" + inc.Ref
		}
		if len(inc.Pick) > 0 {
			s += fmt.Sprintf(" [%d]", len(inc.Pick))
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}

// formatDelegates formats the DELEGATES TO column for ynh ls.
func formatDelegates(delegates []harness.Delegate) string {
	if len(delegates) == 0 {
		return "0"
	}
	parts := make([]string, 0, len(delegates))
	for _, del := range delegates {
		s := shortGitURL(del.Git)
		if del.Path != "" {
			s += "/" + del.Path
		}
		if del.Ref != "" && del.Ref != "main" && del.Ref != "HEAD" {
			s += "@" + del.Ref
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}

// shortGitURL abbreviates a git URL for display.
// "github.com/eyelock/ynh" -> "eyelock/ynh"
// "/tmp/ynh-walkthrough/foo" -> "/tmp/ynh-walkthrough/foo"
func shortGitURL(url string) string {
	// Local paths: keep as-is
	if strings.HasPrefix(url, "/") || strings.HasPrefix(url, ".") {
		return url
	}
	// Strip host prefix: "github.com/user/repo" -> "user/repo"
	parts := strings.SplitN(url, "/", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return url
}
