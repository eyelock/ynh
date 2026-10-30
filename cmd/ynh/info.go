package main

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/eyelock/ynh/internal/agentplugin"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/migration"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
)

// infoEnvelope wraps a single harness with the wire-contract version
// (capabilities) and the ynh release version. Mirrors listEnvelope so
// consumers can decode either response with the same outer shape.
type infoEnvelope struct {
	Capabilities  string    `json:"capabilities"`
	SchemaVersion int       `json:"schema_version"`
	YnhVersion    string    `json:"ynh_version"`
	Harness       infoEntry `json:"harness"`
}

// infoEntry is the JSON shape for `ynh info` structured output.
// Carries the same per-harness fields as listEntry, plus the raw manifest.
type infoEntry struct {
	// ID is the canonical, host-prefixed harness id — see listEntry.ID.
	ID   string `json:"id"`
	Name string `json:"name"`
	// Namespace is the URL-derived "<org>/<repo>" — see listEntry.Namespace.
	// Retained for back-compat; new consumers should prefer ID.
	Namespace string `json:"namespace,omitempty"`
	// Kind classifies the install — see listEntry.Kind.
	Kind             string             `json:"kind"`
	VersionInstalled string             `json:"version_installed"`
	VersionAvailable string             `json:"version_available,omitempty"`
	Description      string             `json:"description,omitempty"`
	DefaultVendor    string             `json:"default_vendor"`
	Path             string             `json:"path"`
	RefInstalled     string             `json:"ref_installed,omitempty"`
	RefAvailable     string             `json:"ref_available,omitempty"`
	SHAAvailable     string             `json:"sha_available,omitempty"`
	IsPinned         bool               `json:"is_pinned"`
	InstalledFrom    *listInstalledFrom `json:"installed_from,omitempty"`
	Includes         []listInclude      `json:"includes"`
	DelegatesTo      []listDelegate     `json:"delegates_to"`
	// MCPServers is what a run of the harness carries: the servers of every
	// harness its includes reach, then its own, each with where it came from.
	MCPServers []infoMCPServer `json:"mcp_servers"`
	// MCPServersError says why the servers of includes could not be read
	// (an include not in the cache, say); MCPServers then holds only the
	// harness's own.
	MCPServersError string `json:"mcp_servers_error,omitempty"`
	// Selection is the profile or focus --profile or --focus selected, when
	// one was. MCPServers is then what a run with that selection carries.
	Selection *infoSelection  `json:"selection,omitempty"`
	Manifest  json.RawMessage `json:"manifest"`
}

// infoSelection is what `ynh info --profile/--focus` selected. Focus is the
// --focus value as given; Profiles are the profiles applied, each as
// --profile takes it ("name" for the harness's own, "namespace:name" for an
// included harness's), including the one a focus names.
type infoSelection struct {
	Focus    string   `json:"focus,omitempty"`
	Profiles []string `json:"profiles,omitempty"`
}

// infoMCPServer is one composed MCP server in `ynh info`. Source is "root"
// for a server the harness declares itself, or the include chain that
// supplied it ("eyelock/a > eyelock/b"), as `ynd preview` names it.
type infoMCPServer struct {
	Name    string `json:"name"`
	Source  string `json:"source"`
	Command string `json:"command,omitempty"`
	URL     string `json:"url,omitempty"`
}

// infoView is the harness ynh info shows: as loaded, or reshaped by the
// profile or focus selected, with the MCP servers a run of it carries.
type infoView struct {
	harness   *harness.Harness
	servers   []infoMCPServer
	mcpErr    error
	selection *infoSelection
}

// resolveInfoView applies the --profile values and --focus value to p as
// `ynh run` does, and lists the MCP servers that run carries, sorted by name.
// Includes are resolved as a run resolves them but only from disk and the
// cache: info never reaches the network. When they cannot be resolved the
// view holds p's own servers and the error, so the caller can say what is
// missing rather than show a partial list as complete.
//
// A selection info cannot honour is an error: --focus with --profile, a
// profile or focus the harness does not define, and a namespaced one when
// the includes it names cannot be read, since info could not show what it
// applies.
func resolveInfoView(p *harness.Harness, profiles []string, focus string) (*infoView, error) {
	if focus != "" && len(profiles) > 0 {
		return nil, fmt.Errorf("cannot use --focus and --profile together (focus includes a profile)")
	}
	sel, err := harness.ParseSelection(profiles, focus)
	if err != nil {
		return nil, err
	}
	view := &infoView{harness: p}
	if focus != "" || len(profiles) > 0 {
		view.selection = &infoSelection{Focus: focus, Profiles: slices.Clone(profiles)}
	}

	rootProfile := sel.Profile
	if focus != "" && sel.FocusNS == "" {
		f, ok := p.Focuses[focus]
		if !ok {
			if len(p.Focuses) == 0 {
				return nil, fmt.Errorf("focus %q not defined in harness (the harness declares no focuses)", focus)
			}
			return nil, fmt.Errorf("focus %q not defined in harness (available: %v)", focus, slices.Sorted(maps.Keys(p.Focuses)))
		}
		if f.Profile != "" {
			rootProfile = f.Profile
			view.selection.Profiles = []string{f.Profile}
		}
	}
	if rootProfile != "" {
		if view.harness, err = harness.ResolveProfile(p, rootProfile); err != nil {
			return nil, err
		}
	}

	var includes []harness.IncludedHarness
	if len(view.harness.Includes) > 0 {
		resolved, nsFocus, err := resolver.ResolveSelectedLocal(view.harness, sel)
		switch {
		case err != nil && (len(sel.Included) > 0 || sel.FocusNS != ""):
			return nil, err
		case err != nil:
			view.mcpErr = err
		default:
			includes = resolver.IncludedHarnesses(resolved)
			if nsFocus != nil && nsFocus.Profile != "" {
				view.selection.Profiles = []string{sel.FocusNS + ":" + nsFocus.Profile}
			}
		}
	} else if len(sel.Included) > 0 || sel.FocusNS != "" {
		return nil, fmt.Errorf("no included harness has namespace %q (the harness includes no other harnesses)", firstNamespace(sel))
	}

	servers, provenance, err := harness.ComposeMCPServersRaw(view.harness, includes)
	if err != nil {
		// A conflict between includes: fall back to the harness's own.
		servers, provenance, _ = harness.ComposeMCPServersRaw(view.harness, nil)
		view.mcpErr = err
	}
	view.servers = make([]infoMCPServer, 0, len(provenance))
	for _, pr := range provenance {
		s := servers[pr.Server]
		view.servers = append(view.servers, infoMCPServer{Name: pr.Server, Source: pr.Source, Command: s.Command, URL: s.URL})
	}
	return view, nil
}

// firstNamespace names a namespace sel selects, for an error: the focus's,
// else the first of the profiles' in sorted order.
func firstNamespace(sel harness.Selection) string {
	if sel.FocusNS != "" {
		return sel.FocusNS
	}
	return slices.Sorted(maps.Keys(sel.Included))[0]
}

func cmdInfo(args []string) error {
	return cmdInfoTo(args, os.Stdout, os.Stderr)
}

func cmdInfoTo(args []string, stdout, stderr io.Writer) error {
	structured := detectJSONFormat(args)

	format := "text"
	checkUpdates := false
	var name, focus string
	var profiles []string
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
		case "--profile", "--focus":
			if i+1 >= len(args) || args[i+1] == "" {
				return cliError(stderr, structured, errCodeInvalidInput, args[i]+" requires a value")
			}
			if args[i] == "--profile" {
				profiles = append(profiles, args[i+1])
			} else {
				if focus != "" {
					return cliError(stderr, structured, errCodeInvalidInput, "--focus given twice")
				}
				focus = args[i+1]
			}
			i++
		default:
			if strings.HasPrefix(args[i], "-") {
				return cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unknown flag: %s", args[i]))
			}
			if name != "" {
				return cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unexpected argument: %s", args[i]))
			}
			name = args[i]
		}
		i++
	}

	if name == "" {
		return cliError(stderr, structured, errCodeInvalidInput, "usage: ynh info <harness-name>")
	}

	switch format {
	case "text":
		if checkUpdates {
			return cliError(stderr, structured, errCodeInvalidInput,
				"--check-updates requires --format json")
		}
		return printInfoText(stdout, name, profiles, focus)
	case "json":
		return printInfoJSON(stdout, stderr, name, profiles, focus, checkUpdates)
	default:
		return cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("invalid --format value %q (want text or json)", format))
	}
}

func printInfoText(w io.Writer, name string, profiles []string, focus string) error {
	loaded, err := harness.LoadQualified(name)
	if err != nil {
		return err
	}
	view, err := resolveInfoView(loaded, profiles, focus)
	if err != nil {
		return err
	}
	// p is the harness with the selected profile applied: its hooks and MCP
	// servers are what that run carries. Profiles and focuses are listed as
	// declared, from the harness as loaded.
	p := view.harness

	vendorName := p.DefaultVendor
	if vendorName == "" {
		vendorName = "-"
	}

	// ID is the canonical, host-prefixed id — what every other ynh command
	// accepts. Surface it first so users copying from info land on the
	// form that works against run/installed/uninstall/update. Name stays
	// for human readability but is intentionally secondary.
	_, _ = fmt.Fprintf(w, "ID:           %s\n", name)
	_, _ = fmt.Fprintf(w, "Name:         %s\n", p.Name)
	_, _ = fmt.Fprintf(w, "Vendor:       %s\n", vendorName)
	if sel := view.selection; sel != nil {
		var parts []string
		if sel.Focus != "" {
			parts = append(parts, "focus "+sel.Focus)
		}
		if len(sel.Profiles) > 0 {
			parts = append(parts, "profile "+strings.Join(sel.Profiles, ", "))
		}
		_, _ = fmt.Fprintf(w, "Selected:     %s\n", strings.Join(parts, ", "))
	}

	if p.InstalledFrom != nil {
		_, _ = fmt.Fprintf(w, "Installed:    %s\n", p.InstalledFrom.InstalledAt)
		_, _ = fmt.Fprintf(w, "Source:       %s (%s)\n", p.InstalledFrom.Source, p.InstalledFrom.SourceType)
		if p.InstalledFrom.Path != "" {
			_, _ = fmt.Fprintf(w, "Path:         %s\n", p.InstalledFrom.Path)
		}
		if p.InstalledFrom.RegistryName != "" {
			_, _ = fmt.Fprintf(w, "Registry:     %s\n", p.InstalledFrom.RegistryName)
		}
	} else {
		_, _ = fmt.Fprintf(w, "Installed:    -\n")
		_, _ = fmt.Fprintf(w, "Source:       -\n")
	}

	// Local artifacts
	arts, _ := harness.ScanArtifactsDir(p.Dir)
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Artifacts:")
	if arts.Total() == 0 {
		_, _ = fmt.Fprintln(w, "  (none)")
	} else {
		if len(arts.Skills) > 0 {
			_, _ = fmt.Fprintf(w, "  skills:    %s\n", strings.Join(arts.Skills, ", "))
		}
		if len(arts.Agents) > 0 {
			_, _ = fmt.Fprintf(w, "  agents:    %s\n", strings.Join(arts.Agents, ", "))
		}
		if len(arts.Rules) > 0 {
			_, _ = fmt.Fprintf(w, "  rules:     %s\n", strings.Join(arts.Rules, ", "))
		}
		if len(arts.Commands) > 0 {
			_, _ = fmt.Fprintf(w, "  commands:  %s\n", strings.Join(arts.Commands, ", "))
		}
	}

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Includes:")
	if len(p.Includes) == 0 {
		_, _ = fmt.Fprintln(w, "  (none)")
	} else {
		for _, inc := range p.Includes {
			source := inc.Git
			if inc.IsLocal() {
				source = inc.Local
			}
			line := "  " + source
			if inc.Path != "" {
				line += "  path=" + inc.Path
			}
			if inc.Ref != "" {
				line += "  ref=" + inc.Ref
			}
			if len(inc.Pick) > 0 {
				line += "  pick=[" + strings.Join(inc.Pick, ", ") + "]"
			}
			_, _ = fmt.Fprintln(w, line)
		}
	}

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Delegates to:")
	if len(p.DelegatesTo) == 0 {
		_, _ = fmt.Fprintln(w, "  (none)")
	} else {
		for _, del := range p.DelegatesTo {
			line := "  " + del.Git
			if del.Path != "" {
				line += "  path=" + del.Path
			}
			if del.Ref != "" {
				line += "  ref=" + del.Ref
			}
			_, _ = fmt.Fprintln(w, line)
		}
	}

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Hooks:")
	if len(p.Hooks) == 0 {
		_, _ = fmt.Fprintln(w, "  (none)")
	} else {
		for event, entries := range p.Hooks {
			for _, entry := range entries {
				line := "  " + event + ": " + entry.Command
				if entry.Matcher != "" {
					line += "  (matcher=" + entry.Matcher + ")"
				}
				_, _ = fmt.Fprintln(w, line)
			}
		}
	}

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "MCP Servers:")
	servers, mcpErr := view.servers, view.mcpErr
	if len(servers) == 0 && mcpErr == nil {
		_, _ = fmt.Fprintln(w, "  (none)")
	}
	for _, s := range servers {
		line := "  " + s.Name
		if target := s.Command; target != "" {
			line += ": " + target
		} else if s.URL != "" {
			line += ": " + s.URL
		}
		if s.Source == harness.MCPSourceRoot {
			line += "  (own)"
		} else {
			line += "  (from " + s.Source + ")"
		}
		_, _ = fmt.Fprintln(w, line)
	}
	if mcpErr != nil {
		_, _ = fmt.Fprintf(w, "  servers from includes not shown: %s\n", mcpErr)
	}

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Profiles:")
	if len(loaded.Profiles) == 0 {
		_, _ = fmt.Fprintln(w, "  (none)")
	} else {
		// Profiles, hook events and servers are all map keys: sorted, so the
		// listing does not change order between runs (#520).
		for _, pname := range slices.Sorted(maps.Keys(loaded.Profiles)) {
			profile := loaded.Profiles[pname]
			var parts []string
			if len(profile.Hooks) > 0 {
				parts = append(parts, "hooks: "+strings.Join(slices.Sorted(maps.Keys(profile.Hooks)), ", "))
			}
			if len(profile.MCPServers) > 0 {
				parts = append(parts, "mcp_servers: "+strings.Join(slices.Sorted(maps.Keys(profile.MCPServers)), ", "))
			}
			if len(parts) == 0 {
				_, _ = fmt.Fprintf(w, "  %s\n", pname)
			} else {
				_, _ = fmt.Fprintf(w, "  %s    %s\n", pname, strings.Join(parts, "    "))
			}
		}
	}

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Focus:")
	if len(p.Focuses) == 0 {
		_, _ = fmt.Fprintln(w, "  (none)")
	} else {
		focusNames := make([]string, 0, len(p.Focuses))
		for fname := range p.Focuses {
			focusNames = append(focusNames, fname)
		}
		sort.Strings(focusNames)
		for _, fname := range focusNames {
			focus := p.Focuses[fname]
			profileLabel := "(default)"
			if focus.Profile != "" {
				profileLabel = "profile=" + focus.Profile
			}
			_, _ = fmt.Fprintf(w, "  %s    %s    %q\n", fname, profileLabel, focus.Prompt)
		}
		if p.LaunchFocus != "" {
			_, _ = fmt.Fprintf(w, "  launch focus: %s\n", p.LaunchFocus)
		}
	}

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Sensors:")
	if len(p.Sensors) == 0 {
		_, _ = fmt.Fprintln(w, "  (none)")
	} else {
		names := make([]string, 0, len(p.Sensors))
		for n := range p.Sensors {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			s := p.Sensors[n]
			cat := s.Category
			if cat == "" {
				cat = "-"
			}
			kind := s.Source.Kind()
			if s.Source.Focus != nil && s.Source.Focus.Inline != nil {
				kind = "focus*" // * = inline focus
			}
			_, _ = fmt.Fprintf(w, "  %s    %s    %s    %s\n", n, cat, kind, s.Output.Format)
		}
	}

	return nil
}

func printInfoJSON(stdout, stderr io.Writer, name string, profiles []string, focus string, checkUpdates bool) error {
	p, err := harness.LoadQualified(name)
	if err != nil {
		code := errCodeNotFound
		if !strings.Contains(err.Error(), "not found") {
			code = errCodeIOError
		}
		return cliError(stderr, true, code, err.Error())
	}

	// Migration chain has run (harness.LoadQualified was called above), so the
	// manifest is a plugin.json in one of the manifest directories. A harness
	// derived from an Agent Plugins package has no file to read; its manifest
	// is the one the loader built.
	var raw []byte
	if p.Format == agentplugin.Format {
		raw, err = json.Marshal(p.Manifest)
	} else {
		raw, err = os.ReadFile(plugin.PluginPath(p.Dir))
	}
	if err != nil {
		return cliError(stderr, true, errCodeIOError,
			fmt.Sprintf("reading manifest: %v", err))
	}

	// Compact the raw JSON to normalise whitespace
	var compacted json.RawMessage
	if err := json.Unmarshal(raw, &compacted); err != nil {
		return cliError(stderr, true, errCodeIOError,
			fmt.Sprintf("parsing manifest: %v", err))
	}

	base := buildListEntry(p)
	if checkUpdates {
		single := []listEntry{base}
		fillUpdates(single, defaultProbe())
		base = single[0]
	}
	entry := infoEntry{
		ID:               base.ID,
		Name:             base.Name,
		Namespace:        base.Namespace,
		Kind:             base.Kind,
		VersionInstalled: base.VersionInstalled,
		VersionAvailable: base.VersionAvailable,
		Description:      base.Description,
		DefaultVendor:    base.DefaultVendor,
		Path:             base.Path,
		RefInstalled:     base.RefInstalled,
		RefAvailable:     base.RefAvailable,
		SHAAvailable:     base.SHAAvailable,
		IsPinned:         base.IsPinned,
		InstalledFrom:    base.InstalledFrom,
		Includes:         base.Includes,
		DelegatesTo:      base.DelegatesTo,
		Manifest:         compacted,
	}
	view, err := resolveInfoView(p, profiles, focus)
	if err != nil {
		return cliError(stderr, true, errCodeInvalidInput, err.Error())
	}
	entry.MCPServers = view.servers
	if view.mcpErr != nil {
		entry.MCPServersError = view.mcpErr.Error()
	}
	entry.Selection = view.selection

	envelope := infoEnvelope{
		Capabilities:  config.CapabilitiesVersion,
		SchemaVersion: migration.ReadSchemaVersion(config.HomeDir()),
		YnhVersion:    config.Version,
		Harness:       entry,
	}

	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding info: %w", err)
	}
	_, err = fmt.Fprintln(stdout, string(data))
	return err
}
