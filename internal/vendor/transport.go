package vendor

// HasNativeSubagents reports whether the vendor has subagents a delegate can
// run as: Claude Code, Codex, Copilot CLI and OpenCode (each carrying only
// part of a harness, limit L12 on docs/vendor-parity.md). Cursor's are
// unverified (L35), so it counts as having none, as does any vendor not
// listed.
func HasNativeSubagents(name string) bool {
	switch name {
	case "claude", "codex", "copilot", "opencode":
		return true
	}
	return false
}
