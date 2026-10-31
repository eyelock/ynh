package vendor

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
)

// DelegateAgentFile writes a delegate as a Codex custom agent: a TOML file in
// .codex/agents/ that sets the three fields every custom agent file must,
// name, description and developer_instructions
// (learn.chatgpt.com/docs/agent-configuration/subagents). Codex identifies the agent by its
// name field; the file is named after it too.
//
// Nothing else is set. A custom agent inherits the parent session's MCP
// servers, sandbox and approval policy, and its file cannot override them, so
// a delegate's own MCP servers have no field here.
func (c *Codex) DelegateAgentFile(name, description, prompt string) (string, []byte, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return "", nil, fmt.Errorf("delegate name %q cannot name a file", name)
	}
	return name + ".toml", codexAgentTOML(codexAgentFields{name: name, description: description, instructions: prompt}), nil
}

// TransformArtifact writes a harness's own agents (agents/<name>.md) as Codex
// custom agents. Codex reads only TOML files in .codex/agents/
// (learn.chatgpt.com/docs/agent-configuration/subagents), so a Markdown agent linked there
// would never be one. Other artifact types pass through unchanged.
func (c *Codex) TransformArtifact(artifactType, name string, data []byte) (string, []byte) {
	if artifactType != "agents" || !strings.HasSuffix(name, ".md") {
		return name, data
	}
	stem := strings.TrimSuffix(name, ".md")
	return stem + ".toml", codexAgentTOML(parseCodexAgent(stem, data))
}

// ArtifactWarnings returns what a run loses when TransformArtifact writes an
// agent as a Codex custom agent: one warning, naming the agent, for each of
// tools and disallowedTools it declares. A custom agent cannot restrict its
// tools, so both lists are dropped. When tools leaves out Bash, the warning
// says the shell tool is turned off instead (features.shell_tool = false),
// the one part of the list Codex can honour. Other artifacts have none.
func (c *Codex) ArtifactWarnings(artifactType, name string, data []byte) []string {
	if artifactType != "agents" || !strings.HasSuffix(name, ".md") {
		return nil
	}
	a := parseCodexAgent(strings.TrimSuffix(name, ".md"), data)
	var warnings []string
	if a.hasTools {
		msg := fmt.Sprintf("agent %q: Codex custom agents cannot restrict tools, so its tools list is dropped", a.name)
		if a.shellOff {
			msg += "; it leaves out Bash, so the agent's shell tool is turned off (features.shell_tool = false)"
		}
		warnings = append(warnings, msg)
	}
	if a.hasDisallowed {
		warnings = append(warnings, fmt.Sprintf("agent %q: Codex custom agents cannot restrict tools, so its disallowedTools list is dropped", a.name))
	}
	return warnings
}

// codexAgentFields are what a Codex custom agent file carries.
type codexAgentFields struct {
	name, description, instructions string
	effort                          string // model_reasoning_effort; empty for none
	shellOff                        bool   // features.shell_tool = false
	hasTools, hasDisallowed         bool   // the source declared these lists
}

// parseCodexAgent reads a Markdown agent into a Codex custom agent's fields.
// Three are set on every one:
//
//   - name: the source's, else the file name. Codex identifies the agent by
//     it. The file keeps the source's name, so two agents never share one.
//   - description: the source's, else one that names the agent; Codex
//     requires the field.
//   - developer_instructions: the body after the frontmatter.
//
// Two optional ones carry over. effort becomes model_reasoning_effort, which
// a custom agent file may set, with the mapping ynh agent run uses for Codex
// (internal/agent/effort.go): low, medium and high pass through as they are.
// Claude Code's other levels (xhigh, max) are dropped, as ynh exposes only
// those three, so a level means the same thing on every backend. And a tools
// list that leaves out Bash sets features.shell_tool = false, so an agent
// that was not to run shell commands has no shell tool.
//
// Every other source field is dropped, because none maps onto a custom
// agent's: Claude Code's model names (sonnet, inherit) are not Codex models,
// and Codex has no tool allowlist (tools, disallowedTools); a custom agent
// inherits the session's permissions and MCP servers. ArtifactWarnings
// reports the dropped tool lists. A source with no closed frontmatter block
// is all body.
func parseCodexAgent(stem string, data []byte) codexAgentFields {
	var a codexAgentFields
	body := data
	if lines, rest, ok := splitFrontmatter(data); ok {
		body = bytes.TrimLeft(rest, "\r\n")
		for _, e := range frontmatterEntries(lines) {
			switch e.key {
			case "name":
				a.name = strings.TrimSpace(e.scalar())
			case "description":
				a.description = strings.Join(strings.Fields(e.scalar()), " ")
			case "effort":
				a.effort = codexReasoningEffort(strings.TrimSpace(e.scalar()))
			case "tools":
				if tools := agentToolList(e); len(tools) > 0 {
					a.hasTools = true
					a.shellOff = !slices.ContainsFunc(tools, isBashTool)
				}
			case "disallowedTools":
				a.hasDisallowed = len(agentToolList(e)) > 0
			}
		}
	}
	if a.name == "" {
		a.name = stem
	}
	if a.description == "" {
		a.description = fmt.Sprintf("Agent %q.", a.name)
	}
	a.instructions = string(body)
	return a
}

// agentToolList reads a Claude Code agent's tools or disallowedTools: a
// comma-separated scalar ("Read, Grep, Bash") or a YAML list. An entry such
// as "Bash(git status:*)" has a comma only inside its parentheses, which the
// split respects.
func agentToolList(e frontmatterEntry) []string {
	var out []string
	for _, item := range e.list() {
		depth, start := 0, 0
		for i, r := range item {
			switch {
			case r == '(':
				depth++
			case r == ')':
				depth--
			case r == ',' && depth == 0:
				if t := strings.TrimSpace(item[start:i]); t != "" {
					out = append(out, t)
				}
				start = i + 1
			}
		}
		if t := strings.TrimSpace(item[start:]); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// isBashTool reports whether a tools entry grants Claude Code's Bash tool,
// whole ("Bash") or for some commands ("Bash(git:*)").
func isBashTool(tool string) bool {
	return tool == "Bash" || strings.HasPrefix(tool, "Bash(")
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
// file, with model_reasoning_effort when effort is set and
// features.shell_tool = false when the shell is off. The dotted key sets the
// features table without a [features] header, so every key stays at the top
// level before the multi-line instructions.
func codexAgentTOML(a codexAgentFields) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "name = %s\n", tomlString(a.name))
	fmt.Fprintf(&b, "description = %s\n", tomlString(a.description))
	if a.effort != "" {
		fmt.Fprintf(&b, "model_reasoning_effort = %s\n", tomlString(a.effort))
	}
	if a.shellOff {
		b.WriteString("features.shell_tool = false\n")
	}
	fmt.Fprintf(&b, "developer_instructions = %s\n", tomlMultilineString(a.instructions))
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
