package vendor

import (
	"bytes"
	"fmt"
	"strings"
)

// DelegateAgentFile writes a delegate as a Codex custom agent: a TOML file in
// .codex/agents/ that sets the three fields every custom agent file must,
// name, description and developer_instructions
// (developers.openai.com/codex/subagents). Codex identifies the agent by its
// name field; the file is named after it too.
//
// Nothing else is set. A custom agent inherits the parent session's MCP
// servers, sandbox and approval policy, and its file cannot override them, so
// a delegate's own MCP servers have no field here.
func (c *Codex) DelegateAgentFile(name, description, prompt string) (string, []byte, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return "", nil, fmt.Errorf("delegate name %q cannot name a file", name)
	}
	return name + ".toml", codexAgentTOML(name, description, prompt, ""), nil
}

// TransformArtifact writes a harness's own agents (agents/<name>.md) as Codex
// custom agents. Codex reads only TOML files in .codex/agents/
// (developers.openai.com/codex/subagents), so a Markdown agent linked there
// would never be one. Other artifact types pass through unchanged.
func (c *Codex) TransformArtifact(artifactType, name string, data []byte) (string, []byte) {
	if artifactType != "agents" || !strings.HasSuffix(name, ".md") {
		return name, data
	}
	stem := strings.TrimSuffix(name, ".md")
	return stem + ".toml", codexAgent(stem, data)
}

// codexAgent renders a Markdown agent as a Codex custom agent with the three
// fields every one must set:
//
//   - name: the source's, else the file name. Codex identifies the agent by
//     it. The file keeps the source's name, so two agents never share one.
//   - description: the source's, else one that names the agent; Codex
//     requires the field.
//   - developer_instructions: the body after the frontmatter.
//
// One optional field carries over: effort becomes model_reasoning_effort,
// which a custom agent file may set, with the mapping ynh agent run uses for
// Codex (internal/agent/effort.go): low, medium and high pass through as they
// are. Claude Code's other levels (xhigh, max) are dropped, as ynh exposes
// only those three, so a level means the same thing on every backend.
//
// Every other source field is dropped, because none maps onto a custom
// agent's: Claude Code's model names (sonnet, inherit) are not Codex models,
// and Codex has no tool allowlist (tools, disallowedTools); a custom agent
// inherits the session's permissions and MCP servers. A source with no closed
// frontmatter block is all body.
func codexAgent(stem string, data []byte) []byte {
	name, description, effort, body := "", "", "", data
	if lines, rest, ok := splitFrontmatter(data); ok {
		body = bytes.TrimLeft(rest, "\r\n")
		for _, e := range frontmatterEntries(lines) {
			switch e.key {
			case "name":
				name = strings.TrimSpace(e.scalar())
			case "description":
				description = strings.Join(strings.Fields(e.scalar()), " ")
			case "effort":
				effort = codexReasoningEffort(strings.TrimSpace(e.scalar()))
			}
		}
	}
	if name == "" {
		name = stem
	}
	if description == "" {
		description = fmt.Sprintf("Agent %q.", name)
	}
	return codexAgentTOML(name, description, string(body), effort)
}

// codexReasoningEffort maps an agent's effort onto Codex's
// model_reasoning_effort: ynh's three levels (internal/agent/effort.go, which
// imports this package and so cannot be imported here) pass through, and any
// other level maps to nothing.
func codexReasoningEffort(level string) string {
	switch level {
	case "low", "medium", "high":
		return level
	}
	return ""
}

// codexAgentTOML encodes a custom agent's three required fields as its TOML
// file, and model_reasoning_effort when effort is set.
func codexAgentTOML(name, description, instructions, effort string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "name = %s\n", tomlString(name))
	fmt.Fprintf(&b, "description = %s\n", tomlString(description))
	if effort != "" {
		fmt.Fprintf(&b, "model_reasoning_effort = %s\n", tomlString(effort))
	}
	fmt.Fprintf(&b, "developer_instructions = %s\n", tomlMultilineString(instructions))
	return []byte(b.String())
}

// tomlString encodes s as a TOML basic string, on one line.
func tomlString(s string) string {
	return `"` + tomlEscape(s, false) + `"`
}

// tomlMultilineString encodes s as a TOML multi-line basic string, keeping its
// newlines and tabs as they are so the file reads as the prompt does. The
// newline after the opening delimiter is trimmed by TOML, and every quote is
// escaped, so no run of three can close the string early.
func tomlMultilineString(s string) string {
	return "\"\"\"\n" + tomlEscape(s, true) + `"""`
}

// tomlEscape escapes s for a TOML basic string. A basic string may not hold a
// quote, a backslash or a control character other than tab unescaped; a
// multi-line one may also hold a newline.
func tomlEscape(s string, multiline bool) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\t':
			b.WriteRune(r)
		case r == '\n' && multiline:
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
