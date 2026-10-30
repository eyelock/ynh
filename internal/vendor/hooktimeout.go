package vendor

// hookTimeoutSupport returns, for a vendor, the native field a per-hook timeout
// is written to ("" when the vendor has none), the reason it has none, and the
// per-event caps (canonical event -> seconds) the vendor documents. Every
// vendor must declare the first two: scripts/hook-event-parity.sh reads the same
// declarations, so a vendor cannot be added without saying whether it supports
// a timeout.
func hookTimeoutSupport(vendorName string) (field, unsupported string, caps map[string]int, ok bool) {
	switch vendorName {
	case "claude":
		return claudeHookTimeoutField, claudeHookTimeoutUnsupported, claudeHookTimeoutCaps, true
	case "codex":
		return codexHookTimeoutField, codexHookTimeoutUnsupported, codexHookTimeoutCaps, true
	case "cursor":
		return cursorHookTimeoutField, cursorHookTimeoutUnsupported, cursorHookTimeoutCaps, true
	case "copilot":
		return copilotHookTimeoutField, copilotHookTimeoutUnsupported, copilotHookTimeoutCaps, true
	}
	return "", "", nil, false
}

// hookTimeoutValue is the number of seconds to write for a hook that asks for
// requested seconds on a canonical event: 0 when none was asked for or the
// vendor has no timeout field (HookWarnings says so), the vendor's cap when the
// request exceeds it (HookWarnings says so), otherwise the request.
func hookTimeoutValue(vendorName, event string, requested int) int {
	if requested <= 0 {
		return 0
	}
	field, _, caps, ok := hookTimeoutSupport(vendorName)
	if !ok || field == "" {
		return 0
	}
	if limit, capped := caps[event]; capped && requested > limit {
		return limit
	}
	return requested
}
