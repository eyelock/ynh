package main

// cursorTrustLaunchArgs is --trust ("Trust the current workspace without
// prompting", cursor-agent 2026.11.05), or nil. It is passed only when the
// installed harness is accepted and current in `ynh trust`, the same rule as
// codexHookTrustLaunchArgs, all reading grantsFor: an unreviewed, changed or
// path-run harness gets no grant, and Cursor asks as it always did.
func cursorTrustLaunchArgs(adm Admission, vendorName string) []string {
	if !grantsFor(adm, vendorName, false, false).CursorTrust {
		return nil
	}
	return []string{"--trust"}
}
