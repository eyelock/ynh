package main

import "github.com/eyelock/ynh/internal/harness"

// cursorTrustLaunchArgs is --trust ("Trust the current workspace without
// prompting", cursor-agent 2026.11.05), or nil. It is passed only when the
// installed harness is accepted and current in `ynh trust`, the same rule as
// codexHookTrustLaunchArgs and workspaceTrusted: an unreviewed, changed or
// path-run harness gets no grant, and Cursor asks as it always did.
func cursorTrustLaunchArgs(loaded *harness.Harness, id, vendorName string) []string {
	if vendorName != "cursor" || !workspaceTrusted(id, loaded) {
		return nil
	}
	return []string{"--trust"}
}
