package assembler

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/vendor"
)

// Codex reads a project's custom agents only as TOML files in .codex/agents/,
// so a harness's own agents/<name>.md is staged, and linked into the project,
// as <name>.toml, whether the harness takes all its agents or picks one.
func TestAssemble_CodexWritesHarnessAgentsAsTOML(t *testing.T) {
	repo := t.TempDir()
	agents := filepath.Join(repo, "agents")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agents, "reviewer.md"),
		[]byte("---\nname: reviewer\ndescription: Reviews code.\ntools: Read\n---\nReview it.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	adapter, err := vendor.Get("codex")
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name    string
		content resolver.ResolvedContent
	}{
		{"all", resolver.ResolvedContent{BasePath: repo}},
		{"picked", resolver.ResolvedContent{BasePath: repo, Paths: []string{"agents/reviewer.md"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var warned bytes.Buffer
			warnOut = &warned
			defer func() { warnOut = os.Stderr }()
			staged, err := Assemble(adapter, []resolver.ResolvedContent{tc.content})
			if err != nil {
				t.Fatal(err)
			}
			wantWarn := "  warning: agent \"reviewer\": Codex custom agents cannot restrict tools, so its tools list is dropped; it leaves out Bash, so the agent's shell tool is turned off (features.shell_tool = false)\n"
			if warned.String() != wantWarn {
				t.Errorf("warnings =\n%q\nwant\n%q", warned.String(), wantWarn)
			}
			defer Cleanup(staged)

			project := t.TempDir()
			if _, err := adapter.Install(staged, project); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(project, ".codex", "agents")
			data, err := os.ReadFile(filepath.Join(dir, "reviewer.toml"))
			if err != nil {
				t.Fatalf("agent file: %v", err)
			}
			want := "name = \"reviewer\"\ndescription = \"Reviews code.\"\nfeatures.shell_tool = false\ndeveloper_instructions = \"\"\"\nReview it.\n\"\"\"\n"
			if string(data) != want {
				t.Errorf("agent =\n%s\nwant\n%s", data, want)
			}
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".md") {
					t.Errorf("%s: Codex does not read a Markdown agent; none should be linked", e.Name())
				}
			}
		})
	}
}
