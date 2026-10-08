package exporter

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/vendor"
)

// ExportDelegate is a delegate resolved for export: its harness, where it
// was loaded from, its resolved includes, and the MCP servers it carries,
// unexpanded.
type ExportDelegate struct {
	harness  *harness.Harness
	basePath string
	included []resolver.ResolveResult
	servers  map[string]plugin.MCPServer
}

// ResolveExportDelegates resolves each delegate as a harness: its includes
// are resolved against its own directory (checked against cfg), and its MCP
// servers are composed the way an export carries them, with ${VAR} left
// literal. A server that points into the delegate's or an include's
// directory cannot be exported and is an error. harnessDir is the exporting
// harness's directory, which a relative delegate source resolves against.
func ResolveExportDelegates(delegates []harness.Delegate, harnessDir string, cfg *config.Config) ([]ExportDelegate, error) {
	var out []ExportDelegate
	for _, del := range delegates {
		basePath, _, err := resolver.ResolveGitSource(del.GitSource, harnessDir)
		if err != nil {
			return nil, fmt.Errorf("delegate: %w", err)
		}

		delHarness, err := harness.LoadDir(basePath)
		if err != nil {
			return nil, fmt.Errorf("loading delegate harness %s: %w", del.Git, err)
		}

		included, _, err := resolver.ResolveSelected(delHarness, cfg, harness.Selection{})
		if err != nil {
			return nil, fmt.Errorf("resolving includes of delegate %s: %w", delHarness.Name, err)
		}
		servers, err := harness.ComposeDelegateMCPServersForExport(delHarness, resolver.IncludedHarnesses(included))
		if err != nil {
			return nil, err
		}
		out = append(out, ExportDelegate{harness: delHarness, basePath: basePath, included: included, servers: servers})
	}
	return out, nil
}

// WriteDelegates writes the delegates' agent files directly into
// outputDir/agents/. Unlike assembler.AssembleDelegates, this writes to the
// plugin root, not inside ConfigDir. A carrier gives each agent its MCP
// servers; with none, they are left out (see DelegateMCPWarnings).
func WriteDelegates(outputDir string, delegates []ExportDelegate, carrier assembler.DelegateMCPCarrier) error {
	if len(delegates) == 0 {
		return nil
	}

	agentsDir := filepath.Join(outputDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return err
	}

	for _, del := range delegates {
		mcpLine := ""
		if len(del.servers) > 0 && carrier != nil {
			var err error
			if mcpLine, err = carrier.DelegateMCPFrontmatter(del.servers); err != nil {
				return fmt.Errorf("delegate %s: %w", del.harness.Name, err)
			}
		}

		agentContent := assembler.BuildDelegateAgent(del.harness, del.basePath, del.included, mcpLine)
		agentFile := filepath.Join(agentsDir, del.harness.Name+".md")
		if err := os.WriteFile(agentFile, []byte(agentContent), 0o644); err != nil {
			return fmt.Errorf("writing delegate agent %s: %w", del.harness.Name, err)
		}
	}

	return nil
}

// delegateCarrier returns the adapter's way of carrying a delegate's MCP
// servers, nil when its subagents cannot.
func delegateCarrier(a vendor.Adapter) assembler.DelegateMCPCarrier {
	c, _ := a.(assembler.DelegateMCPCarrier)
	return c
}

// DelegateMCPWarnings returns one warning per delegate that declares MCP
// servers the vendor's subagents cannot carry, or, for Claude, will not load
// from an installed plugin. A vendor without delegate
// support in exports has no subagent to warn about.
func DelegateMCPWarnings(delegates []ExportDelegate, a vendor.Adapter) []string {
	if !a.SupportsExportDelegates() {
		return nil
	}
	var out []string
	for _, del := range delegates {
		if len(del.servers) == 0 {
			continue
		}
		names := slices.Sorted(maps.Keys(del.servers))
		if delegateCarrier(a) != nil {
			// The field is written, but an installed plugin's agents do not
			// get it (code.claude.com/docs/en/sub-agents).
			out = append(out, fmt.Sprintf("delegate %s declares MCP servers (%s); Claude Code ignores mcpServers in plugin agents, so they will not load when this plugin is installed",
				del.harness.Name, strings.Join(names, ", ")))
			continue
		}
		out = append(out, assembler.DelegateMCPWarning(del.harness.Name, names, a.DisplayName()))
	}
	return out
}
