package assembler

import (
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/eyelock/ynh/internal/agentplugin"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/mcpexec"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/vendor"
)

// DelegateMCPCarrier is implemented by a vendor adapter whose subagent
// definitions can carry their own MCP servers. Claude Code's subagent
// frontmatter takes an inline "mcpServers" map, connected when the subagent
// starts and disconnected when it finishes
// (code.claude.com/docs/en/sub-agents).
//
// Claude Code ignores that field for a plugin's subagents, though, and a run
// loads the assembled directory as a plugin. So a run hands the subagents
// that declare servers to the CLI directly (DelegateLaunchFile), where the
// field is honoured. The agent file keeps the field too: it is what an
// exported agent carries, and what works once copied into .claude/agents/.
type DelegateMCPCarrier interface {
	// DelegateMCPFrontmatter returns the frontmatter line (without a
	// trailing newline) that gives a subagent the servers.
	DelegateMCPFrontmatter(servers map[string]plugin.MCPServer) (string, error)

	// DelegateLaunchFile returns the file, relative to the run directory,
	// that the launch reads to give the CLI the subagents directly, and its
	// content.
	DelegateLaunchFile(agents []vendor.DelegateLaunchAgent) (string, []byte, error)
}

// DelegateMCP records one MCP server a delegate declares: the delegate, the
// server's name, and where the delegate got it (harness.MCPSourceRoot for its
// own, or the include that supplied it).
type DelegateMCP struct {
	Delegate string
	Server   string
	Source   string
}

// DelegateOptions are what AssembleDelegates needs besides the delegates.
type DelegateOptions struct {
	// Config is the allow-list the delegate's own includes are checked
	// against. Nil checks nothing.
	Config *config.Config
	// Lookup reads environment variables for a delegate's ${VAR} references.
	// Nil means os.LookupEnv.
	Lookup func(string) (string, bool)
	// Warn receives the warnings. Nil means os.Stderr.
	Warn io.Writer
	// Launch is set by a run, which hands the vendor CLI the delegates that
	// declare servers and writes the env file of each. Preview and image
	// assembly leave it off: they write neither, so no secret reaches them.
	Launch bool
}

// DelegateMCPWarning is the warning for a delegate whose MCP servers a
// vendor's subagents cannot carry.
func DelegateMCPWarning(delegate string, servers []string, vendorName string) string {
	return fmt.Sprintf("delegate %s declares MCP servers (%s) that %s subagents cannot carry; they are not available to it",
		delegate, strings.Join(servers, ", "), vendorName)
}

// DelegateServers is the MCP servers a delegate carries.
//
// The servers keep every ${VAR} literal: a secret is never part of the
// definition. Secrets holds the values those references name, for the env file
// of a launch, and Wrapped names the servers that reference any.
type DelegateServers struct {
	Servers map[string]plugin.MCPServer
	Sources []harness.MCPProvenance
	Secrets map[string]string
	Wrapped map[string]bool
}

// ComposeDelegateMCP returns the MCP servers a delegate carries: those of its
// resolved includes, then its own, each in its own harness's context.
// included is the delegate's resolved includes.
//
// ${PLUGIN_ROOT}, ${PLUGIN_DATA} and ./ paths are expanded; ${VAR} is not,
// but is checked as an expansion would: it must be in the declaring
// harness's env_passthrough and set, or this fails with the error an
// expansion gives.
func ComposeDelegateMCP(del *harness.Harness, included []resolver.ResolveResult, lookup func(string) (string, bool)) (*DelegateServers, error) {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	// ExpandMCPEnv validates every reference in env and headers and asks for
	// its value; handing back the reference itself leaves it in place.
	values := make(map[string]string)
	keep := func(name string) (string, bool) {
		v, ok := lookup(name)
		if !ok {
			return "", false
		}
		values[name] = v
		return "${" + name + "}", true
	}
	incs := resolver.IncludedHarnesses(included)
	servers, sources, err := harness.ComposeMCPServers(del, incs, harness.PluginDataDir(del), keep)
	if err != nil {
		return nil, fmt.Errorf("delegate %s: %w", del.Name, err)
	}

	declaring := map[string]*harness.Harness{harness.MCPSourceRoot: del}
	for _, inc := range incs {
		declaring[inc.Source] = inc.Harness
	}
	out := &DelegateServers{Servers: servers, Sources: sources, Secrets: map[string]string{}, Wrapped: map[string]bool{}}
	for _, src := range sources {
		s := servers[src.Server]
		h := declaring[src.Source]
		refs := mcpexec.Names(s)
		// An Agent Plugin has no allowlist, and a client expands only the
		// two placeholders: its references reach the server as written.
		if h == nil || h.Format == agentplugin.Format {
			continue
		}
		if len(plugin.EnvRefNames(s.URL)) > 0 {
			return nil, fmt.Errorf("delegate %s: mcp server %q: the url references a variable; a URL is visible to every process, so move the secret to a header", del.Name, src.Server)
		}
		if len(refs) == 0 {
			continue
		}
		for _, name := range refs {
			if name == plugin.MCPEnvRoot || name == plugin.MCPEnvData {
				continue
			}
			v, seen := values[name]
			if !seen {
				// Referenced in the command or args, which an expansion
				// leaves alone: held to the same two rules.
				if !slices.Contains(h.EnvPassthrough, name) {
					return nil, fmt.Errorf("delegate %s: mcp server %q: args references ${%s}, which is not in %s", del.Name, src.Server, name, plugin.EnvPassthroughField)
				}
				var ok bool
				if v, ok = lookup(name); !ok {
					return nil, fmt.Errorf("delegate %s: mcp server %q: args references ${%s}, which is not set", del.Name, src.Server, name)
				}
			}
			out.Secrets[name] = v
		}
		out.Wrapped[src.Server] = true
	}
	return out, nil
}

// writeEnvFile writes the secrets of one delegate to
// <workDir>/delegates/<delegate>/.env.ynh, mode 0600 in directories of mode
// 0700, and returns its absolute path. It is the only place the file is
// written, and workDir is a run directory.
func writeEnvFile(workDir, delegate string, secrets map[string]string) (string, error) {
	if delegate == "" || delegate == "." || delegate == ".." || strings.ContainsAny(delegate, `/\`) {
		return "", fmt.Errorf("delegate name %q cannot name a directory", delegate)
	}
	abs, err := filepath.Abs(workDir)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(abs, "delegates", delegate)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	data, err := mcpexec.Encode(secrets)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, mcpexec.EnvFileName)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	// WriteFile keeps the mode of a file that already exists.
	if err := os.Chmod(path, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// AssembleDelegates generates agent files for each delegate harness
// in the assembled config directory. Each delegate becomes a vendor-native
// agent that the parent harness can invoke. harnessDir is the parent
// harness's directory, which a relative delegate source resolves against.
//
// A delegate is resolved as a harness: its includes are resolved against its
// own directory, their skills are listed, and their MCP servers and its own
// are composed. A vendor whose subagents can carry MCP servers gets them in
// the agent file; for any other vendor one warning per delegate names the
// servers it cannot have. Delegates of delegates are not followed.
//
// The returned list names every server each delegate declares, for preview.
func AssembleDelegates(workDir string, adapter LayoutProvider, delegates []harness.Delegate, harnessDir string, opts DelegateOptions) ([]DelegateMCP, error) {
	if len(delegates) == 0 {
		return nil, nil
	}

	agentsDir, ok := adapter.ArtifactDirs()["agents"]
	if !ok {
		return nil, nil
	}

	configDir := filepath.Join(workDir, adapter.ConfigDir())
	agentsPath := filepath.Join(configDir, agentsDir)
	if err := os.MkdirAll(agentsPath, 0o755); err != nil {
		return nil, err
	}

	warn := opts.Warn
	if warn == nil {
		warn = os.Stderr
	}
	carrier, _ := adapter.(DelegateMCPCarrier)
	vendorName := configDir
	if n, ok := adapter.(interface{ DisplayName() string }); ok {
		vendorName = n.DisplayName()
	}

	var declared []DelegateMCP
	var launch []vendor.DelegateLaunchAgent
	for _, del := range delegates {
		basePath, _, err := resolver.ResolveGitSourceFromCache(del.GitSource, harnessDir)
		if err != nil {
			return nil, fmt.Errorf("delegate: %w", err)
		}

		delHarness, err := harness.LoadDir(basePath)
		if err != nil {
			return nil, fmt.Errorf("loading delegate harness %s: %w", del.Git, err)
		}

		included, _, err := resolver.ResolveSelectedFromCache(delHarness, opts.Config, harness.Selection{})
		if err != nil {
			return nil, fmt.Errorf("resolving includes of delegate %s: %w", delHarness.Name, err)
		}
		composed, err := ComposeDelegateMCP(delHarness, included, opts.Lookup)
		if err != nil {
			return nil, err
		}
		servers := composed.Servers

		mcpLine := ""
		if len(servers) > 0 {
			names := slices.Sorted(maps.Keys(servers))
			for _, s := range composed.Sources {
				declared = append(declared, DelegateMCP{Delegate: delHarness.Name, Server: s.Server, Source: s.Source})
			}
			if carrier != nil {
				if mcpLine, err = carrier.DelegateMCPFrontmatter(servers); err != nil {
					return nil, fmt.Errorf("delegate %s: %w", delHarness.Name, err)
				}
				if opts.Launch {
					agent := vendor.DelegateLaunchAgent{
						Name:        delHarness.Name,
						Description: delegateDescription(delHarness),
						Prompt:      delegateBody(delHarness, basePath, included),
						Servers:     servers,
						Wrapped:     composed.Wrapped,
					}
					if len(composed.Secrets) > 0 {
						if agent.EnvFile, err = writeEnvFile(workDir, delHarness.Name, composed.Secrets); err != nil {
							return nil, fmt.Errorf("delegate %s: writing env file: %w", delHarness.Name, err)
						}
					}
					launch = append(launch, agent)
				}
			} else {
				_, _ = fmt.Fprintf(warn, "  warning: %s\n", DelegateMCPWarning(delHarness.Name, names, vendorName))
			}
		}

		agentContent := BuildDelegateAgent(delHarness, basePath, included, mcpLine)
		agentFile := filepath.Join(agentsPath, delHarness.Name+".md")
		if err := os.WriteFile(agentFile, []byte(agentContent), 0o644); err != nil {
			return nil, fmt.Errorf("writing delegate agent %s: %w", delHarness.Name, err)
		}
	}

	if carrier != nil && len(launch) > 0 {
		rel, data, err := carrier.DelegateLaunchFile(launch)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(workDir, rel), data, 0o600); err != nil {
			return nil, fmt.Errorf("writing delegate launch file: %w", err)
		}
	}

	return declared, nil
}

func delegateDescription(p *harness.Harness) string {
	if p.Description != "" {
		return p.Description
	}
	return fmt.Sprintf("Delegate harness %q.", p.Name)
}

// BuildDelegateAgent generates a markdown agent file for a delegate harness.
// It includes the delegate's instructions, rules, and available skills,
// those of its resolved includes among them. mcpLine, when not empty, is the
// frontmatter line that carries the delegate's MCP servers.
func BuildDelegateAgent(p *harness.Harness, basePath string, included []resolver.ResolveResult, mcpLine string) string {
	var b strings.Builder

	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", p.Name)
	if p.Description != "" {
		fmt.Fprintf(&b, "description: %s\n", p.Description)
	} else {
		fmt.Fprintf(&b, "description: Delegate harness %q.\n", p.Name)
	}
	if mcpLine != "" {
		b.WriteString(mcpLine)
		b.WriteString("\n")
	}
	b.WriteString("---\n\n")
	b.WriteString(delegateBody(p, basePath, included))
	return b.String()
}

// delegateBody is the agent's prompt: who it is, its instructions and rules,
// and the skills it has, those of its resolved includes among them.
func delegateBody(p *harness.Harness, basePath string, included []resolver.ResolveResult) string {
	var b strings.Builder

	fmt.Fprintf(&b, "You are the **%s** harness, invoked as a delegate.\n\n", p.Name)

	// Include harness instructions (instructions.md / CLAUDE.md)
	instructions := readInstructionsFrom(basePath)
	if instructions != "" {
		b.WriteString("## Instructions\n\n")
		b.WriteString(instructions)
		b.WriteString("\n\n")
	}

	// Inline rules as context
	rules := readRulesFrom(basePath)
	if len(rules) > 0 {
		b.WriteString("## Rules\n\n")
		for name, content := range rules {
			fmt.Fprintf(&b, "### %s\n\n%s\n\n", name, content)
		}
	}

	// List available skills: the includes', then the delegate's own.
	var skills []string
	for _, r := range included {
		for _, s := range listSkillsFrom(r.Content.BasePath) {
			if pickedSkill(r.Content.Paths, s) && !slices.Contains(skills, s) {
				skills = append(skills, s)
			}
		}
	}
	for _, s := range listSkillsFrom(basePath) {
		if !slices.Contains(skills, s) {
			skills = append(skills, s)
		}
	}
	if len(skills) > 0 {
		b.WriteString("## Available Skills\n\n")
		for _, skill := range skills {
			fmt.Fprintf(&b, "- %s\n", skill)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// pickedSkill reports whether an include's pick list leaves the named skill
// in: an empty list takes the whole include.
func pickedSkill(picks []string, skill string) bool {
	if len(picks) == 0 {
		return true
	}
	return slices.ContainsFunc(picks, func(p string) bool {
		return filepath.ToSlash(filepath.Clean(p)) == "skills/"+skill
	})
}

// readInstructionsFrom reads the harness's instructions file.
// Checks instructions.md first, then AGENTS.md, then CLAUDE.md as fallback.
func readInstructionsFrom(basePath string) string {
	for _, name := range []string{"instructions.md", "AGENTS.md", "CLAUDE.md"} {
		data, err := os.ReadFile(filepath.Join(basePath, name))
		if err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return ""
}

// readRulesFrom reads all rule markdown files from basePath/rules/.
func readRulesFrom(basePath string) map[string]string {
	rulesDir := filepath.Join(basePath, "rules")
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		return nil
	}

	rules := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(rulesDir, entry.Name()))
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".md")
		rules[name] = strings.TrimSpace(string(data))
	}
	return rules
}

// listSkillsFrom discovers skill directories under basePath/skills/.
func listSkillsFrom(basePath string) []string {
	skillsDir := filepath.Join(basePath, "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil
	}

	var skills []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillMD := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		if _, err := os.Stat(skillMD); err == nil {
			skills = append(skills, entry.Name())
		}
	}
	return skills
}
