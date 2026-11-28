package permission

import (
	"fmt"
	"strings"
)

// The vendors this package maps, by their adapter names.
const (
	Claude   = "claude"
	Codex    = "codex"
	Cursor   = "cursor"
	OpenCode = "opencode"
	Copilot  = "copilot"
)

// Mapping is what one level means on one vendor: arguments for its command
// line and variables for its environment. Both empty means the vendor is
// passed nothing for this level.
type Mapping struct {
	Args []string
	Env  []string
	// Provisional marks a cell not yet shown live (ynh #1058, P0). A caller
	// may print it; a golden test pins it so P0's findings are one edit here.
	Provisional bool
}

// RefusedError is a level a vendor cannot hold exactly. It is refused at
// start, never widened (D4).
type RefusedError struct {
	Vendor string
	Level  Level
	Why    string
}

func (e *RefusedError) Error() string {
	return fmt.Sprintf("%s cannot hold auto-approve %s exactly: %s", e.Vendor, e.Level, e.Why)
}

const noEditsOnly = "it has no mode that approves file edits while still withholding commands; use all, or a different vendor"

// opencodePermission is OpenCode's permission block, passed in
// OPENCODE_CONFIG_CONTENT. Without --standalone, `opencode run` goes through a
// background service that ignores it (P0, ynh #1059), so the mapping carries
// --standalone beside it.
func opencodePermission(edit, bash string) string {
	return fmt.Sprintf(`OPENCODE_CONFIG_CONTENT={"permission":{"edit":%q,"bash":%q}}`, edit, bash)
}

// For returns the mapping of level on vendor, or a *RefusedError when the
// vendor cannot hold it. It is the only place per-vendor permission arguments
// are written. Cells marked Provisional follow the design's table and wait on
// ynh #1058 P0 to be shown live.
func For(vendor string, level Level) (Mapping, error) {
	if !level.Valid() {
		return Mapping{}, fmt.Errorf("unknown auto-approve level %q (supported: %s)", string(level), Words)
	}
	switch vendor {
	case Claude:
		// none and edits hold (ynh #1059, ynw-sg5jw, ynw-22war, ynw-mpnjz):
		// sh -c, touch and mkdir are refused at edits, Write works.
		switch level {
		case None:
			return Mapping{Args: []string{"--permission-mode", "default"}}, nil
		case Edits:
			return Mapping{Args: []string{"--permission-mode", "acceptEdits", "--disallowedTools", "Bash"}}, nil
		}
		return Mapping{Args: []string{"--permission-mode", "bypassPermissions"}}, nil
	case Codex:
		switch level {
		case None:
			return Mapping{Args: []string{"--sandbox", "read-only"}}, nil
		case Edits:
			return Mapping{}, &RefusedError{vendor, level, noEditsOnly}
		}
		return Mapping{Args: []string{"--dangerously-bypass-approvals-and-sandbox"}}, nil
	case Cursor:
		switch level {
		case None:
			// Final (ynh #1059): --mode plan is the only setting that held none,
			// and a project allow list widened even that; --mode ask allowed the
			// edit. Print mode without --force still writes files.
			return Mapping{}, &RefusedError{vendor, level, "print mode without --force still writes files and a project allow list widens it; use all, or a different vendor"}
		case Edits:
			return Mapping{}, &RefusedError{vendor, level, noEditsOnly}
		}
		return Mapping{Args: []string{"--force"}}, nil
	case OpenCode:
		switch level {
		case None:
			return Mapping{Args: []string{"--standalone"}, Env: []string{opencodePermission("deny", "deny")}}, nil
		case Edits:
			return Mapping{Args: []string{"--standalone"}, Env: []string{opencodePermission("allow", "deny")}}, nil
		}
		return Mapping{Args: []string{"--auto"}}, nil
	case Copilot:
		switch level {
		case None:
			return Mapping{Args: []string{"--no-ask-user", "--deny-tool", "shell", "--deny-tool", "write"}}, nil
		case Edits:
			return Mapping{Args: []string{"--no-ask-user", "--allow-tool", "write", "--deny-tool", "shell"}}, nil
		}
		return Mapping{Args: []string{"--no-ask-user", "--allow-all-tools"}}, nil
	}
	return Mapping{}, fmt.Errorf("auto-approve is not supported by the %s vendor (supported: %s)",
		vendor, strings.Join([]string{Claude, Codex, Cursor, OpenCode, Copilot}, ", "))
}

// ClaudeMode returns the --permission-mode Claude Code is passed for level,
// and is what the opened-mode check compares against. "" for an invalid level.
func ClaudeMode(level Level) string {
	m, err := For(Claude, level)
	if err != nil || len(m.Args) < 2 {
		return ""
	}
	return m.Args[1]
}
