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
	MCPServers    map[string]composeMCP    `json:"mcp_servers,omitempty"`
	Profiles      []string                 `json:"profiles"`
	Focuses       map[string]composeFocus  `json:"focuses,omitempty"`
	Sensors       map[string]composeSensor `json:"sensors,omitempty"`
	Counts        composeCounts            `json:"counts"`
}

type composeSensor struct {
	Category string              `json:"category,omitempty"`
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
	Git      string   `json:"git"`
	Ref      string   `json:"ref,omitempty"`
	Path     string   `json:"path,omitempty"`
	Pick     []string `json:"pick,omitempty"`
	Resolved bool     `json:"resolved"`
}

type composeDelegate struct {
	Git  string `json:"git"`
	Ref  string `json:"ref,omitempty"`
	Path string `json:"path,omitempty"`
}

type composeHook struct {
	Command string `json:"command"`
	Matcher string `json:"matcher,omitempty"`
}

type composeMCP struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
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
	var (
		source      string
		profileName string
		format      = "json"
	)

	i := 0
	for i < len(args) {
		switch args[i] {
		case "--harness":
			if i+1 >= len(args) {
				return fmt.Errorf("--harness requires a value")
			}
			i++
			source = args[i]
		case "--profile":
			if i+1 >= len(args) {
				return fmt.Errorf("--profile requires a value")
			}
			i++
			profileName = args[i]
		case "--format":
			if i+1 >= len(args) {
				return fmt.Errorf("--format requires a value")
			}
			i++
			format = args[i]
		case "-h", "--help":
			return errHelp
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown flag: %s", args[i])
			}
			if source != "" {
				return fmt.Errorf("unexpected argument: %s", args[i])
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
		return fmt.Errorf("usage: ynd compose <harness-dir> [--profile name] [--format text|json]")
	}

	switch format {
	case "json", "text":
		// valid
	default:
		return fmt.Errorf("invalid --format value %q (want text or json)", format)
	}

	// Resolve source to local path
	srcDir, err := resolveSource(source)
	if err != nil {
		return err
	}

	// Load harness
	h, workDir, err := loadHarnessForPreview(srcDir)
	if err != nil {
		return fmt.Errorf("loading harness: %w", err)
	}
	if workDir != "" {
		defer func() { _ = os.RemoveAll(workDir) }()
		srcDir = workDir
	}

	// Apply profile if specified
	if profileName == "" {
		profileName = os.Getenv("YNH_PROFILE")
	}
	if profileName != "" {
		h, err = harness.ResolveProfile(h, profileName)
		if err != nil {
			return err
		}
	}

	// Load config for remote source checking
	cfg, err := config.Load()
	if err != nil {
		cfg = &config.Config{}
	}

	// Resolve includes
	resolved, err := resolver.Resolve(h, cfg)
	if err != nil {
		return fmt.Errorf("resolving includes: %w", err)
	}

	// Build the composed output
	out := buildComposeOutput(h, srcDir, resolved)

	switch format {
	case "json":
		return printComposeJSON(stdout, out)
	case "text":
		return printComposeText(stdout, out)
	}
	return nil
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
	for idx, r := range resolved {
		incSource := resolver.ShortGitURL(h.Includes[idx].Git)
		if h.Includes[idx].Path != "" {
			incSource += "/" + h.Includes[idx].Path
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
		delegates = append(delegates, composeDelegate{
			Git:  del.Git,
			Ref:  del.Ref,
			Path: del.Path,
		})
	}
	out.DelegatesTo = delegates

	// Hooks
	if len(h.Hooks) > 0 {
		hooks := make(map[string][]composeHook)
		for event, entries := range h.Hooks {
			var ch []composeHook
			for _, e := range entries {
				ch = append(ch, composeHook{
					Command: e.Command,
					Matcher: e.Matcher,
				})
			}
			hooks[event] = ch
		}
		out.Hooks = hooks
	}

	// MCP Servers
	if len(h.MCPServers) > 0 {
		servers := make(map[string]composeMCP)
		for name, srv := range h.MCPServers {
			servers[name] = composeMCP{
				Command: srv.Command,
				Args:    srv.Args,
				Env:     srv.Env,
				URL:     srv.URL,
				Headers: srv.Headers,
			}
		}
		out.MCPServers = servers
	}

	// Profiles — just names
	profiles := make([]string, 0, len(h.Profiles))
	for name := range h.Profiles {
		profiles = append(profiles, name)
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
	}

	// Sensors — root harness only (included harnesses' sensors are dropped by design)
	if len(h.Sensors) > 0 {
		sensors := make(map[string]composeSensor)
		for name, s := range h.Sensors {
			cs := composeSensor{
				Category: s.Category,
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
			line := "  " + inc.Git
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
		}
	}

	if len(out.Hooks) > 0 {
		_, _ = fmt.Fprintln(w, "\nHooks:")
		for event, entries := range out.Hooks {
			for _, e := range entries {
				line := "  " + event + ": " + e.Command
				if e.Matcher != "" {
					line += "  (matcher=" + e.Matcher + ")"
				}
				_, _ = fmt.Fprintln(w, line)
			}
		}
	}

	if len(out.MCPServers) > 0 {
		_, _ = fmt.Fprintln(w, "\nMCP Servers:")
		for name, srv := range out.MCPServers {
			if srv.Command != "" {
				_, _ = fmt.Fprintf(w, "  %s: %s\n", name, srv.Command)
			} else if srv.URL != "" {
				_, _ = fmt.Fprintf(w, "  %s: %s\n", name, srv.URL)
			}
		}
	}

	if len(out.Profiles) > 0 {
		_, _ = fmt.Fprintf(w, "\nProfiles: %s\n", strings.Join(out.Profiles, ", "))
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
	}

	return nil
}
