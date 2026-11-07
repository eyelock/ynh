package assembler

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/eyelock/ynh/internal/harness"
)

// frontmatterOf parses the YAML block that opens an agent file.
func frontmatterOf(t *testing.T, content string) map[string]any {
	t.Helper()
	rest, ok := strings.CutPrefix(content, "---\n")
	if !ok {
		t.Fatalf("no frontmatter in %q", content)
	}
	block, _, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		t.Fatalf("unclosed frontmatter in %q", content)
	}
	var fm map[string]any
	if err := yaml.Unmarshal([]byte(block), &fm); err != nil {
		t.Fatalf("frontmatter does not parse: %v\n%s", err, block)
	}
	return fm
}

// TestBuildDelegateAgent_FrontmatterRoundTrips guards #756: a name or
// description with YAML syntax in it must come back exactly as written.
func TestBuildDelegateAgent_FrontmatterRoundTrips(t *testing.T) {
	for _, desc := range []string{
		"Reviews pull requests: one at a time",
		"Fast # not a comment",
		`"Quoted" at the start`,
		`'single' at the start`,
		"- looks like a list",
		"line one\nline two",
		`back\slash and "quotes"`,
		"plain description",
	} {
		t.Run(desc, func(t *testing.T) {
			p := &harness.Harness{Name: "team: dev #1", Description: desc}
			fm := frontmatterOf(t, BuildDelegateAgent(p, t.TempDir(), nil, nil, nil, ""))
			if fm["description"] != desc {
				t.Errorf("description = %#v, want %q", fm["description"], desc)
			}
			if fm["name"] != p.Name {
				t.Errorf("name = %#v, want %q", fm["name"], p.Name)
			}
		})
	}
}

func TestBuildDelegateAgent_DefaultDescriptionRoundTrips(t *testing.T) {
	p := &harness.Harness{Name: `odd: "name"`}
	fm := frontmatterOf(t, BuildDelegateAgent(p, t.TempDir(), nil, nil, nil, ""))
	want := `Delegate harness "odd: \"name\"".`
	if fm["description"] != want {
		t.Errorf("description = %#v, want %q", fm["description"], want)
	}
}
