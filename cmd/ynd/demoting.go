package main

import (
	"fmt"
	"sort"
	"strings"
)

// demotingFields are Agent Skills 1.0 spec fields that Claude Code's plugin
// loader mishandles. A skill carrying one is loaded, namespaced differently,
// given roughly ten tokens and left out of the agent's active context: it
// installs correctly, validates, assembles, and never fires.
//
// The fields are spec-valid, which is what makes this worth catching. A
// well-formed third-party skill may legitimately carry them, and `ynh adopt`
// exists to bring exactly those skills into a harness. Scaffolded skills are
// safe because `ynd create skill` emits only name and description.
//
// Documented in docs/skills-standard.md, whose stated workaround was "do not
// use these fields" with nothing enforcing it (#327).
var demotingFields = map[string]string{
	"compatibility": "is confirmed to cause demotion",
	"license":       "may cause demotion",
	"metadata":      "may cause demotion",
}

// lintDemotingFrontmatter reports spec-valid frontmatter fields that silently
// disable a skill once it ships as a plugin, which is how every ynh harness
// ships.
//
// Only top-level keys count. parseFrontmatter flattens the block and ignores
// indentation, so reusing it would report `metadata:\n  license: MIT` twice,
// once for a key that is not top-level at all.
func lintDemotingFrontmatter(path, content string) []lintIssue {
	if !strings.HasPrefix(content, "---\n") {
		return nil
	}
	end := strings.Index(content[4:], "\n---")
	if end < 0 {
		return nil
	}

	found := map[string]int{}
	for i, line := range strings.Split(content[4:4+end], "\n") {
		// A top-level key starts at column zero. Anything indented belongs to
		// the mapping above it.
		if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' {
			continue
		}
		colon := strings.Index(line, ":")
		if colon <= 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:colon]))
		if _, ok := demotingFields[key]; ok {
			if _, seen := found[key]; !seen {
				found[key] = i + 2 // +1 for the opening ---, +1 for 1-indexing
			}
		}
	}
	if len(found) == 0 {
		return nil
	}

	keys := make([]string, 0, len(found))
	for k := range found {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	issues := make([]lintIssue, 0, len(keys))
	for _, k := range keys {
		issues = append(issues, lintIssue{
			File: path,
			Line: found[k],
			Message: fmt.Sprintf(
				"frontmatter %q %s: Claude Code's plugin loader gives the skill "+
					"~10 tokens and excludes it from the agent's context, so it installs "+
					"and never fires. Remove it, or keep this skill out of a plugin harness. "+
					"See docs/skills-standard.md",
				k, demotingFields[k]),
		})
	}
	return issues
}
