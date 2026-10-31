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
	"text/tabwriter"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
)

// composeOutput is the top-level JSON shape for `ynd compose`.
type composeOutput struct {
	Name          string                   `json:"name"`
	Version       string                   `json:"version"`
	Description   string                   `json:"description,omitempty"`
	DefaultVendor string                   `json:"default_vendor"`
	Artifacts     composeArtifacts         `json:"artifacts"`
	Includes      []composeInclude         `json:"includes"`
	DelegatesTo   []composeDelegate        `json:"delegates_to"`
	Hooks         map[string][]composeHook `json:"hooks,omitempty"`
	// HooksNotActive lists, per event, the included harnesses that declare
	// hooks a run does not carry, because the include does not say
	// "hooks": true.
	HooksNotActive []composeHookOrigin `json:"hooks_not_active,omitempty"`
	// HooksError says why the hooks of includes could not be composed (a
	// script outside its harness); Hooks then holds only the root's own.
	HooksError string                `json:"hooks_error,omitempty"`
	MCPServers map[string]composeMCP `json:"mcp_servers,omitempty"`
	// MCPServersError says why the servers of includes could not be composed
	// (two includes define one name differently); MCPServers then holds only
	// the root's own.
	MCPServersError string                    `json:"mcp_servers_error,omitempty"`
	Profiles        map[string]composeProfile `json:"profiles"`
	Focuses         map[string]composeFocus   `json:"focuses,omitempty"`
	LaunchFocus     string                    `json:"launch_focus,omitempty"`
	Sensors         map[string]composeSensor  `json:"sensors,omitempty"`
	// SensorsError says why the sensors of includes could not be merged (a
	// name declared twice, a stale override); Sensors then holds only the
	// root's own.
	SensorsError string        `json:"sensors_error,omitempty"`
	Counts       composeCounts `json:"counts"`
}

type composeSensor struct {
	// Origin names the include that declared the sensor; empty for the
	// root's own. (Source is the sensor's own source, what it observes.)
	Origin   string              `json:"origin,omitempty"`
	Category string              `json:"category,omitempty"`
	Role     string              `json:"role,omitempty"`
	Source   composeSensorSource `json:"source"`
	Output   composeSensorOutput `json:"output"`
}

type composeSensorSource struct {
	Files   []string                  `json:"files,omitempty"`
	Command string                    `json:"command,omitempty"`
	Focus   *composeSensorSourceFocus `json:"focus,omitempty"`
}

type composeSensorSourceFocus struct {
	// Either a name reference to a top-level focus, OR an inline focus.
	Name    string `json:"name,omitempty"`
	Profile string `json:"profile,omitempty"`
	Prompt  string `json:"prompt,omitempty"`
	Inline  bool   `json:"inline,omitempty"`
}

type composeSensorOutput struct {
	Format  string `json:"format"`
	Channel string `json:"channel,omitempty"`
	Path    string `json:"path,omitempty"`
}

type composeArtifacts struct {
	Skills   []composeArtifact `json:"skills"`
	Agents   []composeArtifact `json:"agents"`
	Rules    []composeArtifact `json:"rules"`
	Commands []composeArtifact `json:"commands"`
}

type composeArtifact struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

type composeInclude struct {
	Git string `json:"git"`
	// Local is the path form of an include. It was absent from the composed
	// view entirely, so a locally-included harness listed as `{"git": ""}` with
	// no indication of where it came from — in the one command whose job is
	// answering "where did this come from".
	Local    string   `json:"local,omitempty"`
	Ref      string   `json:"ref,omitempty"`
	Path     string   `json:"path,omitempty"`
	Pick     []string `json:"pick,omitempty"`
	Resolved bool     `json:"resolved"`
}

type composeDelegate struct {
	Git  string `json:"git"`
	Ref  string `json:"ref,omitempty"`
	Path string `json:"path,omitempty"`
	// MCPServers are the servers the delegate carries for its own subagent,
	// not the session's: those of the harnesses its includes reach, then its
	// own ("root" is the delegate itself), composed as a run composes them.
	MCPServers map[string]composeMCP `json:"mcp_servers,omitempty"`
	// MCPServersError says why the delegate's servers could not be read (it
	// is not cached, or two of its includes conflict); MCPServers then holds
	// none.
	MCPServersError string `json:"mcp_servers_error,omitempty"`
}

type composeHook struct {
	// Source is the include chain that supplied the hook; empty for the
	// root's own.
	Source  string `json:"source,omitempty"`
	Command string `json:"command"`
	Matcher string `json:"matcher,omitempty"`
	Timeout int    `json:"timeout,omitempty"`
}

// composeHookOrigin is an included harness's hooks for one event.
type composeHookOrigin struct {
	Event  string `json:"event"`
	Source string `json:"source"`
}

type composeMCP struct {
	// Source is where a composed server was declared: "root", or the include
	// chain that supplied it ("eyelock/a > eyelock/b"), as `ynh info` and
	// `ynd preview` name it. Empty inside a profile, which is root's own.
	Source  string            `json:"source,omitempty"`
	Type    string            `json:"type"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

type composeProfile struct {
	Hooks      map[string][]composeHook `json:"hooks,omitempty"`
	MCPServers map[string]*composeMCP   `json:"mcp_servers,omitempty"`
	Includes   []composeInclude         `json:"includes,omitempty"`
}

type composeFocus struct {
	Profile string `json:"profile,omitempty"`
	Prompt  string `json:"prompt"`
}

type composeCounts struct {
	Skills   int `json:"skills"`
	Agents   int `json:"agents"`
	Rules    int `json:"rules"`
	Commands int `json:"commands"`
}

func cmdCompose(args []string) error {
	return cmdComposeTo(args, os.Stdout, os.Stderr)
}

func cmdComposeTo(args []string, stdout, stderr io.Writer) error {
	structured := composeIsJSON(args)
	var (
		source   string
		profiles []string
		format   = "json"
	)

	i := 0
	for i < len(args) {
		switch args[i] {
		case "--harness":
			if i+1 >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--harness requires a value")
			}
			i++
			source = args[i]
		case "--profile":
			if i+1 >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--profile requires a value")
			}
			i++
			profiles = append(profiles, args[i])
		case "--format":
			if i+1 >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--format requires a value")
			}
			i++
			format = args[i]
		case "-h", "--help":
			return errHelp
		default:
			if strings.HasPrefix(args[i], "-") {
				return cliError(stderr, structured, errCodeInvalidInput, fmt.Sprintf("unknown flag: %s", args[i]))
			}
			if source != "" {
				return cliError(stderr, structured, errCodeInvalidInput, fmt.Sprintf("unexpected argument: %s", args[i]))
			}
			source = args[i]
		}
		i++
	}

	// Resolve source: --harness flag > YNH_HARNESS > positional > error
	if source == "" {
		source = resolveHarnessEnv()
	}
	if source == "" {
		return cliError(stderr, structured, errCodeInvalidInput,
			"usage: ynd compose <harness-dir> [--profile name] [--format text|json]")
	}

	switch format {
	case "json", "text":
		// valid
	default:
		return cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("invalid --format value %q (want text or json)", format))
	}

	// Resolve source to local path
	srcDir, err := resolveSource(source)
	if err != nil {
		return cliError(stderr, structured, errCodeNotFound, err.Error())
	}

	// Load harness
	h, workDir, err := loadHarnessForPreview(srcDir)
	if err != nil {
		return cliError(stderr, structured, errCodeConfigError, fmt.Sprintf("loading harness: %v", err))
	}
	if workDir != "" {
		defer func() { _ = os.RemoveAll(workDir) }()
		srcDir = workDir
	}

	// Apply profile if specified. A profile of an included harness is applied
	// as the includes resolve.
	sel, err := resolveSelection(srcDir, profiles, "")
	if err != nil {
		return cliError(stderr, structured, errCodeInvalidInput, err.Error())
	}
	if sel.Profile != "" {
		h, err = harness.ResolveProfile(h, sel.Profile)
		if err != nil {
			return cliError(stderr, structured, errCodeInvalidInput, err.Error())
		}
	}

	// Load config for remote source checking
	cfg, err := config.Load()
	if err != nil {
		cfg = &config.Config{}
	}

	// Resolve includes
	resolved, _, err := resolver.ResolveSelected(h, cfg, sel)
	if err != nil {
		return cliError(stderr, structured, errCodeIOError, fmt.Sprintf("resolving includes: %v", err))
	}

	// Build the composed output
	out := buildComposeOutput(h, srcDir, resolved)

	switch format {
	case "json":
		if err := printComposeJSON(stdout, out); err != nil {
			return cliError(stderr, structured, errCodeIOError, err.Error())
		}
		return nil
	case "text":
		return printComposeText(stdout, out)
	}
	return nil
}

// composeIsJSON reports whether compose's output, and so its errors, will be
// JSON. Unlike every other command, compose defaults to JSON, so only an
// explicit --format other than json turns the envelope off. The last
// --format wins, as it does when the flags are parsed.
func composeIsJSON(args []string) bool {
	structured := true
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--format" {
			structured = args[i+1] == "json"
		}
	}
	return structured
}

func buildComposeOutput(h *harness.Harness, srcDir string, resolved []resolver.ResolveResult) composeOutput {
	out := composeOutput{
		Name:          h.Name,
		Version:       h.Version,
		Description:   h.Description,
		DefaultVendor: h.DefaultVendor,
	}

	// Collect artifacts from main harness dir
	mainArts, _ := harness.ScanArtifactsDir(srcDir)
	mainSource := h.Name

	var allSkills, allAgents, allRules, allCommands []composeArtifact

	for _, s := range mainArts.Skills {
		allSkills = append(allSkills, composeArtifact{Name: s, Source: mainSource})
	}
	for _, a := range mainArts.Agents {
		allAgents = append(allAgents, composeArtifact{Name: a, Source: mainSource})
	}
	for _, r := range mainArts.Rules {
		allRules = append(allRules, composeArtifact{Name: r, Source: mainSource})
	}
	for _, c := range mainArts.Commands {
		allCommands = append(allCommands, composeArtifact{Name: c, Source: mainSource})
	}

	// Collect artifacts from resolved includes
	for _, r := range resolved {
		// r.Source names a local include by its path, and a git one by its
		// short URL; a local include has no Git URL, so ShortGitURL would
		// leave every artifact from one unattributed. `local` is the
		// low-friction adoption path the shipped ynh-adopt skill teaches,
		// which makes this the attribution a new adopter hits first. Results
		// also include the includes of included harnesses, so they cannot be
		// matched to h.Includes by position.
		incSource := r.Source
		if r.Path != "" {
			incSource += "/" + r.Path
		}

		// Scan the resolved base path for artifacts
		incArts, _ := harness.ScanArtifactsDir(r.Content.BasePath)

		// If pick is specified, filter to only those names
		pickSet := make(map[string]bool)
		for _, p := range r.Content.Paths {
			pickSet[p] = true
		}

		for _, s := range incArts.Skills {
			if len(pickSet) > 0 && !pickSet["skills/"+s] {
				continue
			}
			allSkills = append(allSkills, composeArtifact{Name: s, Source: incSource})
		}
		// A top-level skill folder picked by its name, with no skills/
		// directory (harness.TopLevelSkill).
		for _, p := range r.Content.Paths {
			if harness.TopLevelSkill(r.Content.BasePath, p) {
				name, _ := harness.PickedSkillName(r.Content.BasePath, p)
				allSkills = append(allSkills, composeArtifact{Name: name, Source: incSource})
			}
		}
		for _, a := range incArts.Agents {
			if len(pickSet) > 0 && !pickSet["agents/"+a] {
				continue
			}
			allAgents = append(allAgents, composeArtifact{Name: a, Source: incSource})
		}
		for _, r := range incArts.Rules {
			if len(pickSet) > 0 && !pickSet["rules/"+r] {
				continue
			}
			allRules = append(allRules, composeArtifact{Name: r, Source: incSource})
		}
		for _, c := range incArts.Commands {
			if len(pickSet) > 0 && !pickSet["commands/"+c] {
				continue
			}
			allCommands = append(allCommands, composeArtifact{Name: c, Source: incSource})
		}
	}

	// Ensure empty arrays are [] not null
	if allSkills == nil {
		allSkills = []composeArtifact{}
	}
	if allAgents == nil {
		allAgents = []composeArtifact{}
	}
	if allRules == nil {
		allRules = []composeArtifact{}
	}
	if allCommands == nil {
		allCommands = []composeArtifact{}
	}

	out.Artifacts = composeArtifacts{
		Skills:   allSkills,
		Agents:   allAgents,
		Rules:    allRules,
		Commands: allCommands,
	}

	// Includes with resolved status
	includes := make([]composeInclude, 0, len(h.Includes))
	for idx, inc := range h.Includes {
		ci := composeInclude{
			Git:      inc.Git,
			Local:    inc.Local,
			Ref:      inc.Ref,
			Path:     inc.Path,
			Resolved: idx < len(resolved),
		}
		if len(inc.Pick) > 0 {
			ci.Pick = inc.Pick
		}
		includes = append(includes, ci)
	}
	out.Includes = includes

	// Delegates
	delegates := make([]composeDelegate, 0, len(h.DelegatesTo))
	for _, del := range h.DelegatesTo {
		cd := composeDelegate{
			Git:  del.Git,
			Ref:  del.Ref,
			Path: del.Path,
		}
		if servers, provenance, err := delegateMCPServers(del, h.Dir); err != nil {
			cd.MCPServersError = err.Error()
		} else {
			cd.MCPServers = composeMCPServers(servers, provenance)
		}
		delegates = append(delegates, cd)
	}
	out.DelegatesTo = delegates

	// Hooks — what a run carries: those of included harnesses whose include
	// consented ("hooks": true), then the root's own (assembler.ComposeHooks,
	// as a run and `ynh trust` compose them). An included "./" script is
	// shown at the path the session runs it from.
	hookSet, err := assembler.ComposeHooks(h.Hooks, resolved)
	if err != nil {
		hookSet = assembler.HookSet{Hooks: h.Hooks}
		out.HooksError = err.Error()
	}
	if len(hookSet.Hooks) > 0 {
		hooks := make(map[string][]composeHook)
		for event, entries := range hookSet.Hooks {
			var ch []composeHook
			for i, e := range entries {
				hook := composeHook{
					Command: e.Command,
					Matcher: e.Matcher,
					Timeout: e.Timeout,
				}
				if src := hookSet.Sources[event]; i < len(src) {
					hook.Source = src[i]
				}
				ch = append(ch, hook)
			}
			hooks[event] = ch
		}
		out.Hooks = hooks
	}
	for _, o := range hookSet.NotActive {
		out.HooksNotActive = append(out.HooksNotActive, composeHookOrigin{Event: o.Event, Source: o.Source})
	}

	// MCP Servers — what a run carries: the servers of every harness the
	// includes reach, then the root's own, with the root's removals applied.
	// Composed as written (nothing expanded), as `ynh info` and `ynh trust`
	// read them.
	servers, provenance, err := harness.ComposeMCPServersRaw(h, resolver.IncludedHarnesses(resolved))
	if err != nil {
		servers, provenance, _ = harness.ComposeMCPServersRaw(h, nil)
		out.MCPServersError = err.Error()
	}
	out.MCPServers = composeMCPServers(servers, provenance)

	// Profiles — full content so consumers can render/diff without re-parsing
	// the underlying manifest. Always emit a non-nil map so JSON consumers see
	// an object (possibly empty) rather than null.
	profiles := make(map[string]composeProfile, len(h.Profiles))
	for name, p := range h.Profiles {
		cp := composeProfile{}
		if len(p.Hooks) > 0 {
			cp.Hooks = make(map[string][]composeHook, len(p.Hooks))
			for event, entries := range p.Hooks {
				ch := make([]composeHook, len(entries))
				for i, e := range entries {
					ch[i] = composeHook{Command: e.Command, Matcher: e.Matcher, Timeout: e.Timeout}
				}
				cp.Hooks[event] = ch
			}
		}
		if len(p.MCPServers) > 0 {
			cp.MCPServers = make(map[string]*composeMCP, len(p.MCPServers))
			for sName, srv := range p.MCPServers {
				if srv == nil {
					cp.MCPServers[sName] = nil
					continue
				}
				cp.MCPServers[sName] = &composeMCP{
					Type:    srv.Transport(),
					Command: srv.Command,
					Args:    srv.Args,
					Env:     srv.Env,
					Cwd:     srv.Cwd,
					URL:     srv.URL,
					Headers: srv.Headers,
				}
			}
		}
		for _, inc := range p.Includes {
			cp.Includes = append(cp.Includes, composeInclude{
				Git:   inc.Git,
				Local: inc.Local,
				Ref:   inc.Ref,
				Path:  inc.Path,
				Pick:  inc.Pick,
			})
		}
		profiles[name] = cp
	}
	out.Profiles = profiles

	// Focuses
	if len(h.Focuses) > 0 {
		focuses := make(map[string]composeFocus)
		for name, f := range h.Focuses {
			focuses[name] = composeFocus{
				Profile: f.Profile,
				Prompt:  f.Prompt,
			}
		}
		out.Focuses = focuses
		out.LaunchFocus = h.LaunchFocus
	}

	// Sensors — the root's own and those its direct includes declare, with
	// the include-level and profile overrides applied, as `ynh check` sees
	// them (resolver.WithIncludedSensors). A copy, so h is not changed.
	sensorSet := h.Sensors
	withIncluded := *h
	if merged, err := resolver.WithIncludedSensors(&withIncluded); err != nil {
		out.SensorsError = err.Error()
	} else {
		sensorSet = merged.Sensors
	}
	if len(sensorSet) > 0 {
		sensors := make(map[string]composeSensor)
		for name, s := range sensorSet {
			cs := composeSensor{
				Origin:   s.Origin,
				Category: s.Category,
				Role:     s.Role,
				Output: composeSensorOutput{
					Format:  s.Output.Format,
					Channel: s.Output.Channel,
					Path:    s.Output.Path,
				},
			}
			cs.Source.Files = s.Source.Files
			cs.Source.Command = s.Source.Command
			if s.Source.Focus != nil {
				if s.Source.Focus.Inline != nil {
					cs.Source.Focus = &composeSensorSourceFocus{
						Profile: s.Source.Focus.Inline.Profile,
						Prompt:  s.Source.Focus.Inline.Prompt,
						Inline:  true,
					}
				} else {
					cs.Source.Focus = &composeSensorSourceFocus{Name: s.Source.Focus.Name}
				}
			}
			sensors[name] = cs
		}
		out.Sensors = sensors
	}

	// Counts
	out.Counts = composeCounts{
		Skills:   len(allSkills),
		Agents:   len(allAgents),
		Rules:    len(allRules),
		Commands: len(allCommands),
	}

	return out
}

func printComposeJSON(w io.Writer, out composeOutput) error {
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding compose output: %w", err)
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

// composeMCPServers renders composed MCP servers with where each came from,
// or nil when there are none.
func composeMCPServers(servers map[string]plugin.MCPServer, provenance []harness.MCPProvenance) map[string]composeMCP {
	if len(provenance) == 0 {
		return nil
	}
	out := make(map[string]composeMCP, len(provenance))
	for _, pr := range provenance {
		srv := servers[pr.Server]
		out[pr.Server] = composeMCP{
			Source:  pr.Source,
			Type:    srv.Transport(),
			Command: srv.Command,
			Args:    srv.Args,
			Env:     srv.Env,
			Cwd:     srv.Cwd,
			URL:     srv.URL,
			Headers: srv.Headers,
		}
	}
	return out
}

// delegateMCPServers composes the MCP servers a delegate carries, as written:
// its includes (resolved against its own directory) and then its own, the way
// a run composes them. It reads the disk and the cache only, never the
// network, as `ynh trust` does; a delegate not yet cached is an error.
func delegateMCPServers(del harness.Delegate, harnessDir string) (map[string]plugin.MCPServer, []harness.MCPProvenance, error) {
	base, err := resolver.ResolveDelegateLocal(del.GitSource, harnessDir)
	if err != nil {
		return nil, nil, err
	}
	dh, err := harness.LoadDir(base)
	if err != nil {
		return nil, nil, err
	}
	var included []harness.IncludedHarness
	if len(dh.Includes) > 0 {
		resolved, err := resolver.ResolveIncludesLocal(dh, harness.Selection{})
		if err != nil {
			return nil, nil, fmt.Errorf("includes: %w", err)
		}
		included = resolver.IncludedHarnesses(resolved)
	}
	return harness.ComposeMCPServersRaw(dh, included)
}

func printComposeText(w io.Writer, out composeOutput) error {
	_, _ = fmt.Fprintf(w, "Name:         %s\n", out.Name)
	if out.Version != "" {
		_, _ = fmt.Fprintf(w, "Version:      %s\n", out.Version)
	}
	if out.Description != "" {
		_, _ = fmt.Fprintf(w, "Description:  %s\n", out.Description)
	}
	_, _ = fmt.Fprintf(w, "Vendor:       %s\n", out.DefaultVendor)

	_, _ = fmt.Fprintf(w, "\nArtifacts (%d total):\n",
		out.Counts.Skills+out.Counts.Agents+out.Counts.Rules+out.Counts.Commands)

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "  TYPE\tNAME\tSOURCE")
	for _, s := range out.Artifacts.Skills {
		_, _ = fmt.Fprintf(tw, "  skill\t%s\t%s\n", s.Name, s.Source)
	}
	for _, a := range out.Artifacts.Agents {
		_, _ = fmt.Fprintf(tw, "  agent\t%s\t%s\n", a.Name, a.Source)
	}
	for _, r := range out.Artifacts.Rules {
		_, _ = fmt.Fprintf(tw, "  rule\t%s\t%s\n", r.Name, r.Source)
	}
	for _, c := range out.Artifacts.Commands {
		_, _ = fmt.Fprintf(tw, "  command\t%s\t%s\n", c.Name, c.Source)
	}
	_ = tw.Flush()

	if len(out.Includes) > 0 {
		_, _ = fmt.Fprintf(w, "\nIncludes (%d):\n", len(out.Includes))
		for _, inc := range out.Includes {
			// A local include has no Git URL; without this the line was two
			// spaces and a status.
			origin := inc.Git
			if origin == "" {
				origin = inc.Local
			}
			line := "  " + origin
			if inc.Path != "" {
				line += "  path=" + inc.Path
			}
			if inc.Ref != "" {
				line += "  ref=" + inc.Ref
			}
			if len(inc.Pick) > 0 {
				line += fmt.Sprintf("  pick=%v", inc.Pick)
			}
			if inc.Resolved {
				line += "  [resolved]"
			}
			_, _ = fmt.Fprintln(w, line)
		}
	}

	if len(out.DelegatesTo) > 0 {
		_, _ = fmt.Fprintf(w, "\nDelegates (%d):\n", len(out.DelegatesTo))
		for _, del := range out.DelegatesTo {
			line := "  " + del.Git
			if del.Path != "" {
				line += "  path=" + del.Path
			}
			_, _ = fmt.Fprintln(w, line)
			// The delegate's own subagent's servers, not the session's.
			for _, name := range slices.Sorted(maps.Keys(del.MCPServers)) {
				_, _ = fmt.Fprintln(w, "    MCP server "+composeMCPLine(name, del.MCPServers[name], "its own"))
			}
			if del.MCPServersError != "" {
				_, _ = fmt.Fprintf(w, "    MCP servers not shown: %s\n", del.MCPServersError)
			}
		}
	}

	if len(out.Hooks) > 0 || len(out.HooksNotActive) > 0 || out.HooksError != "" {
		_, _ = fmt.Fprintln(w, "\nHooks:")
		for _, event := range slices.Sorted(maps.Keys(out.Hooks)) {
			for _, e := range out.Hooks[event] {
				line := "  " + event + ": " + e.Command
				if e.Matcher != "" {
					line += "  (matcher=" + e.Matcher + ")"
				}
				if e.Source != "" {
					line += "  (from " + e.Source + ")"
				}
				_, _ = fmt.Fprintln(w, line)
			}
		}
		for _, o := range out.HooksNotActive {
			_, _ = fmt.Fprintf(w, "  %s: not active, from %s (add \"hooks\": true to the include)\n", o.Event, o.Source)
		}
		if out.HooksError != "" {
			_, _ = fmt.Fprintf(w, "  hooks from includes not shown: %s\n", out.HooksError)
		}
	}

	if len(out.MCPServers) > 0 || out.MCPServersError != "" {
		_, _ = fmt.Fprintln(w, "\nMCP Servers:")
		for _, name := range slices.Sorted(maps.Keys(out.MCPServers)) {
			_, _ = fmt.Fprintln(w, "  "+composeMCPLine(name, out.MCPServers[name], "own"))
		}
		if out.MCPServersError != "" {
			_, _ = fmt.Fprintf(w, "  servers from includes not shown: %s\n", out.MCPServersError)
		}
	}

	if len(out.Sensors) > 0 || out.SensorsError != "" {
		_, _ = fmt.Fprintln(w, "\nSensors:")
		for _, name := range slices.Sorted(maps.Keys(out.Sensors)) {
			line := "  " + name
			if origin := out.Sensors[name].Origin; origin != "" {
				line += "  (from " + origin + ")"
			} else {
				line += "  (own)"
			}
			_, _ = fmt.Fprintln(w, line)
		}
		if out.SensorsError != "" {
			_, _ = fmt.Fprintf(w, "  sensors from includes not shown: %s\n", out.SensorsError)
		}
	}

	if len(out.Profiles) > 0 {
		names := make([]string, 0, len(out.Profiles))
		for name := range out.Profiles {
			names = append(names, name)
		}
		sort.Strings(names)
		_, _ = fmt.Fprintf(w, "\nProfiles: %s\n", strings.Join(names, ", "))
	}

	if len(out.Focuses) > 0 {
		_, _ = fmt.Fprintln(w, "\nFocuses:")
		for name, f := range out.Focuses {
			profileLabel := "(default)"
			if f.Profile != "" {
				profileLabel = "profile=" + f.Profile
			}
			_, _ = fmt.Fprintf(w, "  %s    %s    %q\n", name, profileLabel, f.Prompt)
		}
		if out.LaunchFocus != "" {
			_, _ = fmt.Fprintf(w, "  launch focus: %s\n", out.LaunchFocus)
		}
	}

	return nil
}

// composeMCPLine is one server for the text format: its name, its command or
// URL, and where it came from, with own labelling a server the harness
// declares itself.
func composeMCPLine(name string, srv composeMCP, own string) string {
	line := name
	if srv.Command != "" {
		line += ": " + srv.Command
	} else if srv.URL != "" {
		line += ": " + srv.URL
	}
	if srv.Source == harness.MCPSourceRoot {
		line += "  (" + own + ")"
	} else {
		line += "  (from " + srv.Source + ")"
	}
	return line
}
