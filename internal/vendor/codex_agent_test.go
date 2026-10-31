package vendor

import (
	"slices"
	"strings"
	"testing"
)

func TestCodexDelegateAgentFile(t *testing.T) {
	c := &Codex{}
	name, data, err := c.DelegateAgentFile("probe", "A \"quoted\" one\nover two lines", "Line one.\n\tTabbed \"\"\" C:\\x \x01 end")
	if err != nil {
		t.Fatal(err)
	}
	if name != "probe.toml" {
		t.Errorf("file = %q, want probe.toml", name)
	}
	want := "name = \"probe\"\n" +
		"description = \"A \\\"quoted\\\" one\\nover two lines\"\n" +
		"developer_instructions = \"\"\"\nLine one.\n\tTabbed \\\"\\\"\\\" C:\\\\x \\u0001 end\"\"\"\n"
	if string(data) != want {
		t.Errorf("got\n%s\nwant\n%s", data, want)
	}
}

func TestCodexDelegateAgentFile_RefusesPathNames(t *testing.T) {
	c := &Codex{}
	for _, n := range []string{"", ".", "..", "a/b", `a\b`} {
		if _, _, err := c.DelegateAgentFile(n, "d", "p"); err == nil {
			t.Errorf("name %q: want an error", n)
		}
	}
}

// A harness's own agent is written as a TOML custom agent: Codex reads no
// Markdown in .codex/agents/. name, description and the body map onto the
// three fields; fields with no Codex counterpart are dropped. Its tools list
// leaves out Bash, so the shell tool is turned off.
func TestCodexTransformArtifact_AgentToTOML(t *testing.T) {
	c := &Codex{}
	src := "---\nname: code-reviewer\ndescription: >\n  Reviews \"diffs\"\n  for bugs.\ntools: Read, Grep\nmodel: sonnet\n---\n\nYou review code.\nCheck C:\\tmp.\n"
	name, data := c.TransformArtifact("agents", "reviewer.md", []byte(src))
	if name != "reviewer.toml" {
		t.Errorf("file = %q, want reviewer.toml", name)
	}
	want := "name = \"code-reviewer\"\n" +
		"description = \"Reviews \\\"diffs\\\" for bugs.\"\n" +
		"features.shell_tool = false\n" +
		"developer_instructions = \"\"\"\nYou review code.\nCheck C:\\\\tmp.\n\"\"\"\n"
	if string(data) != want {
		t.Errorf("got\n%s\nwant\n%s", data, want)
	}
}

// With no frontmatter the whole file is the instructions, the file name the
// agent's name, and the required description names the agent.
func TestCodexTransformArtifact_AgentWithoutFrontmatter(t *testing.T) {
	c := &Codex{}
	name, data := c.TransformArtifact("agents", "helper.md", []byte("Just help.\n"))
	if name != "helper.toml" {
		t.Errorf("file = %q, want helper.toml", name)
	}
	want := "name = \"helper\"\n" +
		"description = \"Agent \\\"helper\\\".\"\n" +
		"developer_instructions = \"\"\"\nJust help.\n\"\"\"\n"
	if string(data) != want {
		t.Errorf("got\n%s\nwant\n%s", data, want)
	}
}

func TestCodexTransformArtifact_Passthrough(t *testing.T) {
	c := &Codex{}
	for _, tc := range []struct{ typ, name string }{
		{"rules", "style.md"},
		{"commands", "deploy.md"},
		{"skills", "SKILL.md"},
		{"agents", "notes.txt"},
	} {
		name, data := c.TransformArtifact(tc.typ, tc.name, []byte("x"))
		if name != tc.name || string(data) != "x" {
			t.Errorf("%s/%s: got %q %q, want it unchanged", tc.typ, tc.name, name, data)
		}
	}
}

// An agent's effort becomes model_reasoning_effort for ynh's three levels,
// mapped as ynh agent run maps them for Codex; Claude Code's other levels
// have no ynh mapping and are dropped rather than guessed at.
func TestCodexTransformArtifact_AgentEffort(t *testing.T) {
	c := &Codex{}
	for _, tc := range []struct{ effort, want string }{
		{"low", "model_reasoning_effort = \"low\"\n"},
		{"medium", "model_reasoning_effort = \"medium\"\n"},
		{" high ", "model_reasoning_effort = \"high\"\n"},
		{"xhigh", ""},
		{"max", ""},
		{"High", ""},
		{"", ""},
	} {
		src := "---\nname: probe\ndescription: d\neffort: " + tc.effort + "\n---\nBody.\n"
		_, data := c.TransformArtifact("agents", "probe.md", []byte(src))
		want := "name = \"probe\"\n" +
			"description = \"d\"\n" +
			tc.want +
			"developer_instructions = \"\"\"\nBody.\n\"\"\"\n"
		if string(data) != want {
			t.Errorf("effort %q: got\n%s\nwant\n%s", tc.effort, data, want)
		}
	}
}

// A tools list that leaves out Bash turns the agent's shell tool off; one
// that grants Bash, whole or for some commands, leaves it alone, as does an
// agent with no tools list or only disallowedTools.
func TestCodexTransformArtifact_ShellTool(t *testing.T) {
	c := &Codex{}
	for _, tc := range []struct {
		name, frontmatter string
		off               bool
	}{
		{"tools without Bash", "tools: Read, Grep", true},
		{"flow list without Bash", "tools: [Read, Edit]", true},
		{"block list without Bash", "tools:\n  - Read\n  - Glob", true},
		{"tools with Bash", "tools: Read, Bash", false},
		{"Bash for some commands", "tools: Read, Bash(git status:*, git diff:*)", false},
		{"block list with Bash", "tools:\n  - Read\n  - Bash", false},
		{"no tools", "model: sonnet", false},
		{"empty tools", "tools:", false},
		{"disallowedTools only", "disallowedTools: Bash", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "---\nname: probe\ndescription: d\n" + tc.frontmatter + "\n---\nBody.\n"
			_, data := c.TransformArtifact("agents", "probe.md", []byte(src))
			if got := strings.Contains(string(data), "features.shell_tool = false\n"); got != tc.off {
				t.Errorf("shell_tool off = %v, want %v; got\n%s", got, tc.off, data)
			}
		})
	}
}

// An agent that declares tools or disallowedTools gets one warning per list,
// naming the agent, because a Codex custom agent cannot restrict its tools.
// The tools warning says when the shell is turned off.
func TestCodexArtifactWarnings(t *testing.T) {
	c := &Codex{}
	dropTools := `agent "probe": Codex custom agents cannot restrict tools, so its tools list is dropped`
	shellOff := "; it leaves out Bash, so the agent's shell tool is turned off (features.shell_tool = false)"
	dropDisallowed := `agent "probe": Codex custom agents cannot restrict tools, so its disallowedTools list is dropped`
	for _, tc := range []struct {
		name, frontmatter string
		want              []string
	}{
		{"tools without Bash", "tools: Read, Grep", []string{dropTools + shellOff}},
		{"tools with Bash", "tools: [Read, Bash]", []string{dropTools}},
		{"disallowedTools", "disallowedTools: Write, Edit", []string{dropDisallowed}},
		{"both", "tools:\n  - Read\n  - Bash\ndisallowedTools:\n  - Write", []string{dropTools, dropDisallowed}},
		{"neither", "model: sonnet\neffort: high", nil},
		{"empty lists", "tools:\ndisallowedTools: []", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "---\nname: probe\ndescription: d\n" + tc.frontmatter + "\n---\nBody.\n"
			got := c.ArtifactWarnings("agents", "file.md", []byte(src))
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %q\nwant %q", got, tc.want)
			}
		})
	}
}

// The warning names the agent by the name Codex will know it by: the file
// name when the frontmatter has none. Other artifacts never warn.
func TestCodexArtifactWarnings_NameAndPassthrough(t *testing.T) {
	c := &Codex{}
	got := c.ArtifactWarnings("agents", "helper.md", []byte("---\ntools: Read\n---\nHelp.\n"))
	if len(got) != 1 || !strings.HasPrefix(got[0], `agent "helper": `) {
		t.Errorf("got %q, want one warning naming helper", got)
	}
	for _, tc := range []struct{ typ, name string }{{"rules", "style.md"}, {"skills", "SKILL.md"}, {"agents", "notes.txt"}} {
		if w := c.ArtifactWarnings(tc.typ, tc.name, []byte("---\ntools: Read\n---\n")); w != nil {
			t.Errorf("%s/%s: got %q, want no warnings", tc.typ, tc.name, w)
		}
	}
}
