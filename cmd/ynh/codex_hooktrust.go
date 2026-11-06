package main

import (
	"fmt"
	"io"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/vendor"
)

// codexHookTrustLaunchArgs is the Codex overrides that trust the run
// directory as a project and each hook ynh wrote there, or nil. It grants
// only when the installed harness is accepted and current in `ynh trust`, the
// same rule as workspaceTrusted: an unreviewed, changed or path-run harness
// gets no grant, so Codex skips its hooks, and one line on stderr says so and
// what to run. A harness with no hooks needs no grant and prints nothing.
func codexHookTrustLaunchArgs(loaded *harness.Harness, id, vendorName, runDir string, stderr io.Writer) []string {
	if !codexHooksTrusted(loaded, id, vendorName, runDir, stderr) {
		return nil
	}
	return vendor.CodexHookTrustArgs(runDir)
}

// codexHooksTrusted reports whether Codex may run the hooks ynh wrote in
// runDir: the harness has some, and it is accepted and current in `ynh trust`.
// When it has some and is not, it says so on stderr, once. `ynh run` and
// `ynh agent run` share this rule.
func codexHooksTrusted(loaded *harness.Harness, id, vendorName, runDir string, stderr io.Writer) bool {
	if vendorName != "codex" {
		return false
	}
	if vendor.CodexHookTrustArgs(runDir) == nil {
		return false
	}
	if workspaceTrusted(id, loaded) {
		return true
	}
	if id == "" {
		_, _ = fmt.Fprintln(stderr, "  warning: codex: hooks not trusted, so Codex will skip them: a harness run from a path has no trust record; install it and run 'ynh trust accept local/<name>'")
	} else {
		_, _ = fmt.Fprintf(stderr, "  warning: codex: hooks not trusted, so Codex will skip them: run 'ynh trust accept %s'\n", id)
	}
	return false
}
