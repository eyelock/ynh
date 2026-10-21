package harness

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/eyelock/ynh/internal/plugin"
)

// MCPSourceRoot is the provenance of a server the root harness declares.
const MCPSourceRoot = "root"

// IncludedHarness is a harness reached through an include, with the display
// name of where it came from ("eyelock/assistants//ynh/github", or
// "eyelock/a > eyelock/b" when it was reached through another harness). The
// resolver builds these; the harness package only reads them, which keeps it
// free of an import on the resolver.
type IncludedHarness struct {
	Harness *Harness
	Source  string
}

// MCPProvenance records where a composed server was declared: MCPSourceRoot,
// or the Source of the include that supplied it.
type MCPProvenance struct {
	Server string
	Source string
}

// copyMCPServer returns s with its slices and maps copied, so a caller that
// edits the copy cannot reach the harness the original came from.
func copyMCPServer(s plugin.MCPServer) plugin.MCPServer {
	s.Args = slices.Clone(s.Args)
	s.Env = maps.Clone(s.Env)
	s.Headers = maps.Clone(s.Headers)
	return s
}

// ComposeMCPServers returns the MCP servers a run of root should carry: the
// servers of every included harness, then root's own, expanded the way
// AssembleMCPServers does it.
//
// Each included harness's servers are expanded in that harness's own
// context: ${PLUGIN_ROOT} and ./ paths against its directory, and ${VAR}
// references against its own env_passthrough. root's env_passthrough is
// never widened by an include, so an include cannot read a variable the root
// did not declare, and a variable only root declared is not visible to an
// include's servers. dataDir is the root's, for ${PLUGIN_DATA}.
//
// Merge rules: included servers go in first, in includes order. Two includes
// declaring the same name must expand to identical definitions, or it is an
// error naming both. root's own server of the same name replaces an included
// one entirely. root.MCPRemovals is applied last, so a null removes a server
// whichever harness declared it. Removing a name that exists nowhere is not
// an error.
//
// The provenance lists each surviving server's source, sorted by name.
func ComposeMCPServers(root *Harness, includes []IncludedHarness, dataDir string, lookup func(string) (string, bool)) (map[string]plugin.MCPServer, []MCPProvenance, error) {
	servers, sources, err := composeMCP(root, includes, func(h *Harness) (map[string]plugin.MCPServer, error) {
		return AssembleMCPServers(h, dataDir, lookup)
	})
	if err != nil {
		return nil, nil, err
	}
	return servers, provenanceList(sources), nil
}

// ComposeMCPServersForExport is ComposeMCPServers for `ynd export`: nothing
// is expanded, since an exported plugin is distributed and leaves ${VAR}
// literal for the consumer. A server taken from an include that points into
// the include's own directory cannot be exported, because the plugin does not
// contain that directory, and is an error.
func ComposeMCPServersForExport(root *Harness, includes []IncludedHarness) (map[string]plugin.MCPServer, error) {
	servers, sources, err := composeMCP(root, includes, func(h *Harness) (map[string]plugin.MCPServer, error) {
		return h.MCPServers, nil
	})
	if err != nil {
		return nil, err
	}
	for _, name := range slices.Sorted(maps.Keys(servers)) {
		if src := sources[name]; src != MCPSourceRoot && mcpUsesOwnDir(servers[name]) {
			return nil, fmt.Errorf("included MCP server %q from %s uses a path inside the include and cannot be exported; declare it in the root harness", name, src)
		}
	}
	return servers, nil
}

// ComposeDelegateMCPServersForExport is ComposeMCPServersForExport for a
// delegate: the delegate's own servers are held to the same portability rule
// as its includes', because the exported plugin carries the delegate's agent
// file and not its directory. A server that points into the delegate's or an
// include's directory is an error naming it.
func ComposeDelegateMCPServersForExport(del *Harness, includes []IncludedHarness) (map[string]plugin.MCPServer, error) {
	servers, err := ComposeMCPServersForExport(del, includes)
	if err != nil {
		return nil, fmt.Errorf("delegate %s: %w", del.Name, err)
	}
	for _, name := range slices.Sorted(maps.Keys(servers)) {
		if _, own := del.MCPServers[name]; own && mcpUsesOwnDir(servers[name]) {
			return nil, fmt.Errorf("delegate %s: MCP server %q uses a path inside the delegate and cannot be exported; declare it with an absolute command or a URL", del.Name, name)
		}
	}
	return servers, nil
}

// mcpUsesOwnDir reports whether a stdio server names a path inside its
// harness's directory: a ${PLUGIN_ROOT} reference or a plugin-relative ./
// command or cwd.
func mcpUsesOwnDir(s plugin.MCPServer) bool {
	if s.Transport() != plugin.MCPTypeStdio {
		return false
	}
	if strings.HasPrefix(s.Command, "./") || strings.HasPrefix(s.Cwd, "./") {
		return true
	}
	fields := append([]string{s.Command, s.Cwd}, s.Args...)
	for _, v := range s.Env {
		fields = append(fields, v)
	}
	return slices.ContainsFunc(fields, func(v string) bool {
		return strings.Contains(v, plugin.MCPPlaceholderRoot)
	})
}

// composeMCP merges the servers of includes and root, each obtained through
// assemble, and applies root's removals. sources maps each surviving name to
// its provenance.
func composeMCP(root *Harness, includes []IncludedHarness, assemble func(*Harness) (map[string]plugin.MCPServer, error)) (map[string]plugin.MCPServer, map[string]string, error) {
	merged := make(map[string]plugin.MCPServer)
	sources := make(map[string]string)

	// Names the root declares or removes never reach an include's servers:
	// they are neither expanded (an unset variable in a server the root
	// replaced or nulled must not fail the run) nor compared for conflicts.
	skip := make(map[string]bool, len(root.MCPServers)+len(root.MCPRemovals))
	for name := range root.MCPServers {
		skip[name] = true
	}
	for _, name := range root.MCPRemovals {
		skip[name] = true
	}

	for _, inc := range includes {
		if inc.Harness == nil {
			continue
		}
		// A shallow copy with a filtered map: the loaded harness is shared
		// and must not change.
		view := *inc.Harness
		view.MCPServers = make(map[string]plugin.MCPServer, len(inc.Harness.MCPServers))
		for name, s := range inc.Harness.MCPServers {
			if !skip[name] {
				view.MCPServers[name] = s
			}
		}
		servers, err := assemble(&view)
		if err != nil {
			return nil, nil, fmt.Errorf("MCP servers of included harness %s: %w", inc.Source, err)
		}
		for _, name := range slices.Sorted(maps.Keys(servers)) {
			s := servers[name]
			if prev, ok := merged[name]; ok {
				if !reflect.DeepEqual(prev, s) {
					return nil, nil, fmt.Errorf("MCP server %q is defined differently by %s and %s; declare it in the root harness to choose one", name, sources[name], inc.Source)
				}
				continue
			}
			merged[name] = s
			sources[name] = inc.Source
		}
	}

	rootServers, err := assemble(root)
	if err != nil {
		return nil, nil, err
	}
	for name, s := range rootServers {
		merged[name] = s
		sources[name] = MCPSourceRoot
	}

	for _, name := range root.MCPRemovals {
		delete(merged, name)
		delete(sources, name)
	}
	return merged, sources, nil
}

func provenanceList(sources map[string]string) []MCPProvenance {
	out := make([]MCPProvenance, 0, len(sources))
	for _, name := range slices.Sorted(maps.Keys(sources)) {
		out = append(out, MCPProvenance{Server: name, Source: sources[name]})
	}
	return out
}
