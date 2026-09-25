package agentplugin

import (
	"errors"
	"fmt"
)

// Issue is one finding from Validate. Fatal marks the findings that a
// conformant client would reject the plugin or a whole component type over;
// the rest are what it would report and carry on from. An author fixes
// both, so Validate returns them together.
type Issue struct {
	Path    string
	Message string
	Fatal   bool
}

func (i Issue) String() string {
	if i.Path == "" {
		return i.Message
	}
	return i.Path + ": " + i.Message
}

// Validate checks a directory as a complete Agent Plugin package: manifest,
// skills and MCP configuration, each against the specification's own rules
// and failure boundaries. It is the conformance check for a plugin someone
// is about to publish, and the same check ynh runs over its own export.
//
// A fatal manifest error ends validation there, because §5.2 says nothing
// else in the package is to be looked at once the manifest is rejected.
func Validate(dir string) []Issue {
	var issues []Issue

	m, diags, err := ReadManifest(dir)
	for _, d := range diags {
		issues = append(issues, Issue{d.Path, d.Message, false})
	}
	if err != nil {
		if errors.Is(err, ErrNotPlugin) {
			return []Issue{{ManifestFile, "missing (a plugin needs a root plugin.json declaring " + PluginSchemaID + ")", true}}
		}
		return append(issues, Issue{"", err.Error(), true})
	}
	_ = m

	_, sdiags := DiscoverSkills(dir)
	for _, d := range sdiags {
		issues = append(issues, Issue{d.Path, d.Message, false})
	}

	_, mdiags, err := ReadMCP(dir)
	if err != nil {
		issues = append(issues, Issue{"", fmt.Sprintf("%v (MCP disabled for this plugin)", err), true})
	}
	for _, d := range mdiags {
		issues = append(issues, Issue{d.Path, d.Message, false})
	}
	return issues
}
