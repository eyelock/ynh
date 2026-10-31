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

// When the root and an include both carry agents/reviewer.md, the root's copy
// (later in content) is the one staged, and the warnings describe that copy
// alone, once: none when it declares no tools, even though the include's does.
func TestAssemble_CodexAgentWarningsFollowTheStagedCopy(t *testing.T) {
	adapter, err := vendor.Get("codex")
	if err != nil {
		t.Fatal(err)
	}
	writeAgent := func(t *testing.T, body string) string {
		t.Helper()
		repo := t.TempDir()
		if err := os.MkdirAll(filepath.Join(repo, "agents"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, "agents", "reviewer.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return repo
	}
	const toolsWarning = "  warning: agent \"reviewer\": Codex custom agents cannot restrict tools, so its tools list is dropped; it leaves out Bash, so the agent's shell tool is turned off (features.shell_tool = false)\n"
	for _, tc := range []struct {
		name, root, wantWarn string
		shellOff             bool
	}{
		{"root declares tools", "---\nname: reviewer\ntools: Read, Grep\n---\nRoot.\n", toolsWarning, true},
		{"root declares none", "---\nname: reviewer\n---\nRoot.\n", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			include := writeAgent(t, "---\nname: reviewer\ntools: Read\n---\nInclude.\n")
			root := writeAgent(t, tc.root)

			var warned bytes.Buffer
			warnOut = &warned
			defer func() { warnOut = os.Stderr }()
			staged, err := Assemble(adapter, []resolver.ResolvedContent{{BasePath: include}, {BasePath: root}})
			if err != nil {
				t.Fatal(err)
			}
			defer Cleanup(staged)

			if warned.String() != tc.wantWarn {
				t.Errorf("warnings =\n%q\nwant\n%q", warned.String(), tc.wantWarn)
			}
			data, err := os.ReadFile(filepath.Join(staged, adapter.ConfigDir(), "agents", "reviewer.toml"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "Root.") {
				t.Errorf("staged agent is not the root's:\n%s", data)
			}
			if got := strings.Contains(string(data), "features.shell_tool = false"); got != tc.shellOff {
				t.Errorf("shell_tool off = %v, want %v:\n%s", got, tc.shellOff, data)
			}
		})
	}
}
