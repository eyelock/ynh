package permission

import "fmt"

// CheckRoot refuses a Claude Code run at level all as root (N4), before
// Claude starts: Claude refuses bypassPermissions as root itself, and ynh
// says so first, once, with the fixes. euid is the caller's effective uid,
// and getenv are passed in so the check is a pure function. Claude Code itself
// allows bypassPermissions as root when IS_SANDBOX=1 or CLAUDE_CODE_BUBBLEWRAP
// is set (P0, ynh #1059, not run live), so ynh honours both.
func CheckRoot(vendor string, level Level, euid int, getenv func(string) string) error {
	if vendor != Claude || level != All || euid != 0 {
		return nil
	}
	if getenv("IS_SANDBOX") == "1" || getenv("CLAUDE_CODE_BUBBLEWRAP") != "" {
		return nil
	}
	return fmt.Errorf("claude code refuses auto-approve all as root (--permission-mode bypassPermissions). Fix one of:\n" +
		"  - run as a non-root user\n" +
		"  - use ynh's agent image, which runs as uid 1001\n" +
		"  - narrow the run with --auto-approve edits\n" +
		"  - in a container that is the sandbox, set IS_SANDBOX=1 (or CLAUDE_CODE_BUBBLEWRAP), which Claude Code honours")
}

// CheckOpened compares the mode a session reports it opened in with the one
// ynh chose, where the vendor reports it (Claude Code, in its init event). A
// mismatch means a setting overrode the choice, so the level does not apply.
// An empty want or an empty opened (the vendor reported nothing) passes.
func CheckOpened(vendor string, level Level, opened string) error {
	if vendor != Claude || opened == "" {
		return nil
	}
	return OpenedMismatch(ClaudeMode(level), opened)
}

// OpenedMismatch is CheckOpened on the --permission-mode words themselves.
func OpenedMismatch(want, opened string) error {
	if want == "" || opened == "" || opened == want {
		return nil
	}
	return fmt.Errorf("asked for --permission-mode %s but the session started in %q; a managed or user setting "+
		"(such as permissions.disableBypassPermissionsMode) overrides it, so --auto-approve does not apply", want, opened)
}
