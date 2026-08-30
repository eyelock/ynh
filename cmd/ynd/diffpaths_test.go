package main

import (
	"testing"

	"github.com/eyelock/ynh/internal/vendor"
)

func adapterFor(t *testing.T, name string) vendor.Adapter {
	t.Helper()
	a, err := vendor.Get(name)
	if err != nil {
		t.Fatalf("vendor.Get(%q): %v", name, err)
	}
	return a
}

// Every vendor writes its own prefixes, so the raw file sets never intersected
// and `ynd diff` reported every file as "only in" one side — its
// content-comparison branch was unreachable dead code.
//
// Three prefixes differ, not one: the config dir, the manifest dir and the
// instructions filename.
func TestCanonicalPath(t *testing.T) {
	cases := []struct {
		vendor, in, want string
	}{
		{"claude", ".claude/agents/x.md", "<config>/agents/x.md"},
		{"cursor", ".cursor/agents/x.md", "<config>/agents/x.md"},
		{"claude", ".claude-plugin/plugin.json", "<manifest>/plugin.json"},
		{"cursor", ".cursor-plugin/plugin.json", "<manifest>/plugin.json"},
		{"codex", ".agents/plugins/plugin.json", "<manifest>/plugin.json"},
		{"copilot", ".github/plugin/plugin.json", "<manifest>/plugin.json"},
		{"claude", "CLAUDE.md", "<instructions>"},
		{"cursor", ".cursorrules", "<instructions>"},
		{"copilot", "AGENTS.md", "<instructions>"},
		// Untouched: not one of the three mapped prefixes.
		{"claude", "README.md", "README.md"},
	}
	for _, c := range cases {
		t.Run(c.vendor+"/"+c.in, func(t *testing.T) {
			if got := canonicalPath(adapterFor(t, c.vendor), c.in); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// The manifest dir must be tested before the config dir, or ".claude-plugin"
// is swallowed by a ".claude" prefix match and mapped to the wrong thing.
func TestCanonicalPath_ManifestDirIsNotSwallowedByConfigDir(t *testing.T) {
	got := canonicalPath(adapterFor(t, "claude"), ".claude-plugin/plugin.json")
	if got != "<manifest>/plugin.json" {
		t.Errorf("got %q — .claude-plugin was matched as .claude", got)
	}
}

// Prefix matching is on whole segments: ".cursorrules" is the instructions
// file, not something inside a ".cursor" directory.
func TestCanonicalPath_MatchesWholeSegments(t *testing.T) {
	if got := canonicalPath(adapterFor(t, "cursor"), ".cursorrules"); got != "<instructions>" {
		t.Errorf("got %q, want <instructions>", got)
	}
}

// Cursor renders rules as .mdc where the others use .md. That is the same
// content in the form each vendor requires — neither "identical" nor "only in
// one" — so it gets its own category rather than burying the real differences.
func TestRenderedDifferently(t *testing.T) {
	if !renderedDifferently("<config>/rules/a.md", "<config>/rules/a.mdc") {
		t.Error(".md vs .mdc is a rendering difference")
	}
	if renderedDifferently("<config>/rules/a.md", "<config>/rules/a.md") {
		t.Error("identical paths are not a rendering difference")
	}
	if renderedDifferently("<config>/rules/a.md", "<config>/rules/b.md") {
		t.Error("different names are not a rendering difference")
	}
}

// The key must collapse .mdc onto .md, or the two never meet and the
// rendering category is unreachable for the same reason the whole comparison
// was.
func TestCanonicalKey(t *testing.T) {
	if canonicalKey("<config>/rules/a.mdc") != canonicalKey("<config>/rules/a.md") {
		t.Error(".mdc and .md must share a key to be comparable at all")
	}
	if got := canonicalKey("plugin.json"); got != "plugin.json" {
		t.Errorf("unrelated paths must be untouched, got %q", got)
	}
}
