package exporter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/eyelock/ynh/internal/agentplugin"
	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/vendor"
)

// AgentPluginVendor is the ExportResult.Vendor value for a portable package.
const AgentPluginVendor = "agent-plugin"

// exportAgentPlugin writes one Agent Plugins package (agent-plugins.org,
// specification 1.0.0) to opts.OutputDir.
//
// The portable core is the same whatever vendors are selected: root
// plugin.json, skills/ and mcp.json. The selected vendors decide what else
// the package carries, each through its AgentPluginLayout: a client that
// loads the format gets its namespace directory and extension data; one
// that does not (Claude Code) gets its own manifest and legacy layout at the
// root, which the specification calls a compatibility package. The two
// never collide, because the portable files and every vendor's files have
// distinct names.
//
// The output is validated against the specification before this returns.
// A fatal finding is an error: ynh must not publish a package it would
// itself refuse to load.
func exportAgentPlugin(opts ExportOptions, hj *plugin.HarnessJSON, p *harness.Harness, content []resolver.ResolvedContent, instructionsPath string, vendors []string) ([]ExportResult, error) {
	out := opts.OutputDir
	result := ExportResult{Vendor: AgentPluginVendor, OutputDir: out}

	if err := os.RemoveAll(out); err != nil {
		return nil, fmt.Errorf("cleaning output: %w", err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, fmt.Errorf("creating output: %w", err)
	}

	// Portable core: skills. Only immediate children of skills/ are
	// discovered by a client (§7.1), which is also all ynh ever ships there.
	skillsOnly := map[string]string{"skills": "skills"}
	if err := copyContent(out, restrictContent(content, skillsOnly), skillsOnly, nil); err != nil {
		return nil, err
	}
	result.Skills = countDir(filepath.Join(out, "skills"))

	// AGENTS.md is not a portable component, but it is how ynh carries
	// instructions and what ynh reads back on import.
	var instructions []byte
	if instructionsPath != "" {
		var err error
		if instructions, err = os.ReadFile(instructionsPath); err != nil {
			return nil, fmt.Errorf("reading instructions: %w", err)
		}
		if err := os.WriteFile(filepath.Join(out, "AGENTS.md"), instructions, 0o644); err != nil {
			return nil, fmt.Errorf("writing AGENTS.md: %w", err)
		}
	}

	extensions := map[string]json.RawMessage{}
	carried := map[string]bool{} // artifact types some selected vendor receives
	hooksCarried := false
	rootHooks := false
	var adapters []vendor.Adapter
	for _, v := range vendors {
		a, err := vendor.Get(v)
		if err != nil {
			return nil, err
		}
		adapters = append(adapters, a)
	}

	for _, a := range adapters {
		layout := a.AgentPluginLayout()
		name := a.Name()

		// Non-portable artifacts and delegates, where the client can take them.
		if layout.ArtifactDir != "" {
			dirs := a.ExportArtifactDirs()
			if dirs == nil {
				dirs = a.ArtifactDirs()
			}
			delete(dirs, "skills")
			var transform assembler.ArtifactTransform
			if t, ok := a.(assembler.ArtifactTransformer); ok {
				transform = t.TransformArtifact
			}
			target := filepath.Join(out, filepath.FromSlash(layout.ArtifactDir))
			if len(dirs) > 0 {
				if err := copyContent(target, restrictContent(content, dirs), dirs, transform); err != nil {
					return nil, fmt.Errorf("%s artifacts: %w", name, err)
				}
			}
			for t := range dirs {
				carried[t] = true
			}
			if len(p.DelegatesTo) > 0 && a.SupportsExportDelegates() {
				if err := ExportDelegates(target, p.DelegatesTo); err != nil {
					return nil, fmt.Errorf("%s delegates: %w", name, err)
				}
				carried["delegates"] = true
			}
			result.Agents += countDir(filepath.Join(target, "agents"))
		}

		// Hooks, in the vendor's own shape, at the place it reads them.
		if len(p.Hooks) > 0 && layout.Hooks != "" {
			files, err := a.GenerateHookConfig(p.Hooks)
			if err != nil {
				return nil, fmt.Errorf("%s hooks: %w", name, err)
			}
			if data := singleFile(files); data != nil {
				if err := writeGeneratedFiles(out, map[string][]byte{filepath.FromSlash(layout.Hooks): data}); err != nil {
					return nil, err
				}
				hooksCarried = true
				if !strings.Contains(layout.Hooks, "/") || strings.HasPrefix(layout.Hooks, "hooks/") {
					rootHooks = true
				}
				if layout.HooksExtension && layout.Namespace != "" {
					ext, err := json.Marshal(map[string]string{"hooks": "./" + layout.Hooks})
					if err != nil {
						return nil, err
					}
					extensions[layout.Namespace] = ext
				}
			}
		}

		// A client outside the format gets its own manifest, MCP file and
		// instruction files at the root.
		if !layout.LoadsFormat {
			manifest, err := a.GeneratePluginManifest(hj, out)
			if err != nil {
				return nil, fmt.Errorf("%s manifest: %w", name, err)
			}
			if err := writeGeneratedFiles(out, manifest); err != nil {
				return nil, err
			}
			if len(instructions) > 0 {
				if err := writeGeneratedFiles(out, a.GenerateSystemPrompt(instructions)); err != nil {
					return nil, err
				}
			}
		}
		if len(p.MCPServers) > 0 && layout.MCP != "" {
			files, err := a.GenerateMCPConfig(p.MCPServers)
			if err != nil {
				return nil, fmt.Errorf("%s MCP config: %w", name, err)
			}
			if data := singleFile(files); data != nil {
				if err := writeGeneratedFiles(out, map[string][]byte{filepath.FromSlash(layout.MCP): data}); err != nil {
					return nil, err
				}
			}
		}
	}

	// Portable MCP configuration, pruned to the servers the format can
	// express, then the manifest.
	if len(p.MCPServers) > 0 {
		warnings, err := writePortableMCP(out, p.MCPServers, adapters)
		if err != nil {
			return nil, err
		}
		result.Warnings = append(result.Warnings, warnings...)
	}

	// A harness name starts with an alphanumeric (harness.LoadDir enforces
	// it), so normalisation always leaves something.
	name, ok := agentplugin.NormalizeName(hj.Name)
	if !ok {
		result.Warnings = append(result.Warnings, fmt.Sprintf("name %q is not a valid Agent Plugins name; exported as %q", hj.Name, name))
	}
	manifest := agentplugin.Manifest{
		Schema:      agentplugin.PluginSchemaID,
		Name:        name,
		Version:     hj.Version,
		Description: hj.Description,
		Keywords:    hj.Keywords,
	}
	if hj.Author != nil {
		manifest.Author = &agentplugin.Author{Name: hj.Author.Name, Email: hj.Author.Email, URL: hj.Author.URL}
	}
	if len(extensions) > 0 {
		manifest.Extensions = extensions
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling plugin.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(out, agentplugin.ManifestFile), append(data, '\n'), 0o644); err != nil {
		return nil, fmt.Errorf("writing plugin.json: %w", err)
	}

	result.Warnings = append(result.Warnings, uncarriedWarnings(p, content, carried, hooksCarried, vendors)...)
	if rootHooks && !hooksCarriedByNamespace(adapters) {
		result.Warnings = append(result.Warnings, "hooks/hooks.json is a compatibility file for Claude Code; Codex discovers the same path by default and its commands are anchored for Claude, so include codex in -v to give Codex its own hooks")
	}

	// The conformance check: our own package must load under the rules we
	// hold everyone else to.
	for _, issue := range agentplugin.Validate(out) {
		if issue.Fatal {
			return nil, fmt.Errorf("exported package does not conform: %s", issue)
		}
		result.Warnings = append(result.Warnings, "conformance: "+issue.String())
	}

	return []ExportResult{result}, nil
}

// restrictContent narrows resolved content to the artifact types in dirs.
// A picked path names its type in its first segment, and CopyPicked refuses
// a type it has no directory for; here the portable core and each vendor's
// share are copied in separate passes with different type sets, so a pick
// outside the current set is simply not this pass's to copy. An entry whose
// picks all fall outside is dropped rather than left empty, because an
// entry with no picks means "copy everything".
func restrictContent(content []resolver.ResolvedContent, dirs map[string]string) []resolver.ResolvedContent {
	var out []resolver.ResolvedContent
	for _, rc := range content {
		if len(rc.Paths) == 0 {
			out = append(out, rc)
			continue
		}
		kept := rc
		kept.Paths = nil
		for _, p := range rc.Paths {
			if _, ok := dirs[strings.SplitN(p, "/", 2)[0]]; ok {
				kept.Paths = append(kept.Paths, p)
			}
		}
		if len(kept.Paths) > 0 {
			out = append(out, kept)
		}
	}
	return out
}

// singleFile returns the one file an adapter generated, whatever relative
// path it chose for its own layout; the caller decides where it goes in the
// package. Adapters that write the same content to two paths (Cursor) still
// count as one file.
func singleFile(files map[string][]byte) []byte {
	var data []byte
	for _, d := range files {
		if data != nil && string(d) != string(data) {
			return data // distinct files: keep the first in map order; adapters do not do this today
		}
		data = d
	}
	return data
}

func hooksCarriedByNamespace(adapters []vendor.Adapter) bool {
	for _, a := range adapters {
		l := a.AgentPluginLayout()
		if l.Hooks != "" && l.Namespace != "" && strings.HasPrefix(l.Hooks, l.Namespace+"/") {
			return true
		}
	}
	return false
}

// envRef matches a ${VAR} reference. Clients expand only ${PLUGIN_ROOT} and
// ${PLUGIN_DATA} (§9.2); anything else reaches the server as literal text.
var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// writePortableMCP converts the harness servers to the portable mcp.json,
// keeping only the entries the format can express. It validates its own
// output through the same reader a client would use, so a server that would
// be skipped on load is left out here, with the reason as a warning.
func writePortableMCP(out string, servers map[string]plugin.MCPServer, adapters []vendor.Adapter) ([]string, error) {
	cfg := agentplugin.MCPConfig{Schema: agentplugin.MCPSchemaID, Servers: map[string]agentplugin.Server{}}
	var warnings []string
	names := make([]string, 0, len(servers))
	for n := range servers {
		names = append(names, n)
	}
	sort.Strings(names)

	cursorSelected := false
	for _, a := range adapters {
		if a.Name() == "cursor" {
			cursorSelected = true
		}
	}

	for _, n := range names {
		s := servers[n]
		ps := agentplugin.Server{
			Type:    s.Transport(),
			Command: s.Command,
			Args:    s.Args,
			Env:     s.Env,
			Cwd:     s.Cwd,
			URL:     s.URL,
			Headers: s.Headers,
		}
		// A bare relative working directory is the plugin-relative form
		// spelt without its ./, which is the one normalisation that is
		// unambiguous. Anything else is left for validation to judge.
		if ps.Cwd != "" && !strings.HasPrefix(ps.Cwd, "./") && !strings.HasPrefix(ps.Cwd, "${") && !filepath.IsAbs(ps.Cwd) && !strings.HasPrefix(ps.Cwd, "..") {
			ps.Cwd = "./" + ps.Cwd
		}
		cfg.Servers[n] = ps

		for _, field := range []struct {
			label string
			m     map[string]string
		}{{"env", s.Env}, {"headers", s.Headers}} {
			keys := make([]string, 0, len(field.m))
			for k := range field.m {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				for _, m := range envRef.FindAllStringSubmatch(field.m[k], -1) {
					if v := m[1]; v != agentplugin.EnvRoot && v != agentplugin.EnvData {
						warnings = append(warnings, fmt.Sprintf("mcp server %q: %s.%s references ${%s}; Agent Plugins clients expand only ${PLUGIN_ROOT} and ${PLUGIN_DATA}, so the server will receive it literally", n, field.label, k, v))
					}
				}
			}
		}
		if cursorSelected && usesPlaceholders(ps) {
			warnings = append(warnings, fmt.Sprintf("mcp server %q uses ${PLUGIN_ROOT} or ${PLUGIN_DATA}, which Cursor does not expand in an Agent Plugin's mcp.json", n))
		}
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling mcp.json: %w", err)
	}
	parsed, diags, err := agentplugin.ParseMCP(out, data)
	if err != nil {
		return nil, fmt.Errorf("portable mcp.json: %w", err)
	}
	for _, d := range diags {
		warnings = append(warnings, "mcp server not portable, left out of mcp.json: "+strings.TrimPrefix(d.String(), agentplugin.MCPFile+" server "))
	}
	if len(parsed.Servers) == 0 {
		return append(warnings, "no MCP server could be expressed portably; mcp.json not written"), nil
	}
	if len(parsed.Servers) != len(cfg.Servers) {
		if data, err = json.MarshalIndent(parsed, "", "  "); err != nil {
			return nil, fmt.Errorf("marshalling mcp.json: %w", err)
		}
	}
	if err := os.WriteFile(filepath.Join(out, agentplugin.MCPFile), append(data, '\n'), 0o644); err != nil {
		return nil, fmt.Errorf("writing mcp.json: %w", err)
	}
	return warnings, nil
}

func usesPlaceholders(s agentplugin.Server) bool {
	has := func(v string) bool {
		return strings.Contains(v, agentplugin.PlaceholderRoot) || strings.Contains(v, agentplugin.PlaceholderData)
	}
	if has(s.Cwd) {
		return true
	}
	for _, a := range s.Args {
		if has(a) {
			return true
		}
	}
	for _, v := range s.Env {
		if has(v) {
			return true
		}
	}
	return false
}

// uncarriedWarnings names what the harness has that no selected vendor can
// receive from this package: agents, rules, commands, delegates and hooks are
// client-specific in Agent Plugins, so they travel only inside a namespace a
// selected client has published, or in a compatibility layer.
func uncarriedWarnings(p *harness.Harness, content []resolver.ResolvedContent, carried map[string]bool, hooksCarried bool, vendors []string) []string {
	var warnings []string
	for _, t := range []string{"agents", "rules", "commands"} {
		if carried[t] {
			continue
		}
		n := 0
		for _, rc := range content {
			n += countDir(filepath.Join(rc.BasePath, t))
		}
		if n > 0 {
			warnings = append(warnings, fmt.Sprintf("%d %s not portable: no selected vendor (%s) carries them in an Agent Plugin", n, t, strings.Join(vendors, ", ")))
		}
	}
	if len(p.DelegatesTo) > 0 && !carried["delegates"] {
		warnings = append(warnings, fmt.Sprintf("%d delegates not portable: no selected vendor (%s) carries them in an Agent Plugin", len(p.DelegatesTo), strings.Join(vendors, ", ")))
	}
	if len(p.Hooks) > 0 && !hooksCarried {
		warnings = append(warnings, fmt.Sprintf("hooks not portable: no selected vendor (%s) loads them from an Agent Plugin", strings.Join(vendors, ", ")))
	}
	return warnings
}
