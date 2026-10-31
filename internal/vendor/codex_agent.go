package vendor

import (
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
	var b strings.Builder
	fmt.Fprintf(&b, "name = %s\n", tomlString(name))
	fmt.Fprintf(&b, "description = %s\n", tomlString(description))
	fmt.Fprintf(&b, "developer_instructions = %s\n", tomlMultilineString(prompt))
	return name + ".toml", []byte(b.String()), nil
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
