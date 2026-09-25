package agentplugin

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/eyelock/ynh/internal/plugin"
)

// Format is what an install record carries for a harness derived from an
// Agent Plugins package.
const Format = "agent-plugin"

// Derivation is an Agent Plugin expressed as a ynh harness, in memory.
// Nothing about the package on disk is changed to produce it.
type Derivation struct {
	// Manifest is what .ynh-plugin/plugin.json would say if the package
	// had one.
	Manifest *plugin.HarnessJSON
	// Extensions lists the client namespaces the package's manifest carries
	// data for. ynh assigns them no meaning (the specification says a client
	// must not), and re-export does not reproduce them; they are named so
	// that loss is visible rather than silent.
	Extensions []string
	// Diagnostics is everything the loader skipped or ignored, which the
	// specification asks a client to report.
	Diagnostics []Diagnostic
}

// DeriveHarness reads the Agent Plugin at dir and returns it as a harness.
//
// The portable core maps directly: manifest identity to the harness
// identity, skills/ to skills (ynh already discovers that directory), and
// mcp.json to mcp_servers with each server's transport kept. A version the
// package does not declare defaults to 0.0.0, the value ynh gives any other
// manifest-less source.
//
// artifactDirs names client namespace directories that hold artifacts in
// ynh's own layout (agents/, rules/, commands/), as each vendor adapter
// declares. Such a directory becomes a local include of the derived
// harness, so the ordinary resolver carries its artifacts with source
// attribution and no new plumbing. Hooks are not imported from anywhere:
// they are client-specific by definition, in each client's own shape, and
// there is no honest way to read one client's hook file as ynh's canonical
// events. Their presence is reported.
//
// A fatal manifest error rejects the package (§5.2). A failure isolated to
// MCP or to one skill is a diagnostic, and the rest loads (§11.3).
func DeriveHarness(dir string, artifactDirs []string) (*Derivation, error) {
	m, diags, err := ReadManifest(dir)
	if err != nil {
		return nil, err
	}
	d := &Derivation{Diagnostics: diags}
	hj := &plugin.HarnessJSON{
		Name:        m.Name,
		Version:     m.Version,
		Description: m.Description,
		Keywords:    m.Keywords,
	}
	if hj.Version == "" {
		hj.Version = "0.0.0"
	}
	if m.Author != nil {
		hj.Author = &plugin.AuthorInfo{Name: m.Author.Name, Email: m.Author.Email, URL: m.Author.URL}
	}
	for ns := range m.Extensions {
		d.Extensions = append(d.Extensions, ns)
	}
	sort.Strings(d.Extensions)

	_, skillDiags := DiscoverSkills(dir)
	d.Diagnostics = append(d.Diagnostics, skillDiags...)

	cfg, mcpDiags, err := ReadMCP(dir)
	d.Diagnostics = append(d.Diagnostics, mcpDiags...)
	if err != nil {
		d.Diagnostics = append(d.Diagnostics, Diagnostic{MCPFile, err.Error() + "; MCP disabled for this plugin"})
	} else if cfg != nil && len(cfg.Servers) > 0 {
		hj.MCPServers = make(map[string]plugin.MCPServer, len(cfg.Servers))
		for name, s := range cfg.Servers {
			hj.MCPServers[name] = plugin.MCPServer{
				Type:    s.Type,
				Command: s.Command,
				Args:    s.Args,
				Env:     s.Env,
				Cwd:     s.Cwd,
				URL:     s.URL,
				Headers: s.Headers,
			}
		}
	}

	for _, ns := range artifactDirs {
		if hasArtifacts(filepath.Join(dir, ns)) {
			hj.Includes = append(hj.Includes, plugin.IncludeMeta{Local: ns})
		}
	}

	for _, rel := range hookFiles(dir, artifactDirs) {
		d.Diagnostics = append(d.Diagnostics, Diagnostic{rel, "client-specific hooks are not imported"})
	}

	d.Manifest = hj
	return d, nil
}

func hasArtifacts(dir string) bool {
	for _, sub := range []string{"agents", "rules", "commands"} {
		if entries, err := os.ReadDir(filepath.Join(dir, sub)); err == nil && len(entries) > 0 {
			return true
		}
	}
	return false
}

// hookFiles lists hook configuration files at the places clients read them:
// hooks/hooks.json at the root (Claude Code, Codex by default) and under
// each artifact namespace.
func hookFiles(dir string, namespaces []string) []string {
	candidates := []string{"hooks/hooks.json"}
	for _, ns := range namespaces {
		candidates = append(candidates, ns+"/hooks/hooks.json")
	}
	var found []string
	for _, rel := range candidates {
		if info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err == nil && info.Mode().IsRegular() {
			found = append(found, rel)
		}
	}
	return found
}
