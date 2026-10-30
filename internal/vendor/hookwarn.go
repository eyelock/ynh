package vendor

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/eyelock/ynh/internal/plugin"
)

// hookEventSupport returns, for a vendor, the canonical hook events it maps
// (to its native name) and the ones it declares unsupported (with the reason).
// Every canonical event must be in exactly one of the two for every vendor:
// scripts/hook-event-parity.sh reads the same declarations and fails the build
// when one is in neither, so a new event cannot be forgotten on one vendor.
func hookEventSupport(vendorName string) (native, unsupported map[string]string, ok bool) {
	switch vendorName {
	case "claude":
		return claudeHookEventMap, claudeUnsupportedHookEvents, true
	case "codex":
		return codexHookEventMap, codexUnsupportedHookEvents, true
	case "cursor":
		return cursorHookEventMap, cursorUnsupportedHookEvents, true
	case "copilot":
		return copilotHookEventMap, copilotUnsupportedHookEvents, true
	}
	return nil, nil, false
}

// HookWarnings returns one warning for each way a vendor will not carry the
// canonical hooks as declared: an event it cannot express (the hook is left
// out of its config), and a matcher it cannot honour. It never fails: the
// output is still valid, so assembly, export and `ynd validate` print these
// without stopping. A vendor ynh has no hook map for returns nothing.
func HookWarnings(vendorName string, hooks map[string][]plugin.HookEntry) []string {
	native, unsupported, ok := hookEventSupport(vendorName)
	if !ok {
		return nil
	}
	var warnings []string
	for _, event := range slices.Sorted(maps.Keys(hooks)) {
		if len(hooks[event]) == 0 || !plugin.ValidHookEvents[event] {
			continue
		}
		nativeEvent, mapped := native[event]
		if !mapped {
			msg := fmt.Sprintf("hook event %s has no equivalent on %s, so it is not written", event, vendorName)
			if reason := unsupported[event]; reason != "" {
				msg += ": " + reason
			}
			warnings = append(warnings, msg)
			continue
		}
		warnings = append(warnings, matcherWarnings(vendorName, event, nativeEvent, hooks[event])...)
		warnings = append(warnings, timeoutWarnings(vendorName, event, hooks[event])...)
	}
	return warnings
}

// timeoutWarnings covers a per-hook timeout: a vendor with no timeout field
// runs the hook with its own default, and a vendor that caps the event below
// the request gets the cap. Both say so, naming the event, the vendor and the
// numbers, so a declared timeout is never dropped or changed silently.
func timeoutWarnings(vendorName, event string, entries []plugin.HookEntry) []string {
	field, reason, caps, ok := hookTimeoutSupport(vendorName)
	if !ok {
		return nil
	}
	var warnings []string
	for _, e := range entries {
		if e.Timeout <= 0 {
			continue
		}
		if field == "" {
			msg := fmt.Sprintf("hook timeout %ds on %s is not supported on %s, so its default applies instead", e.Timeout, event, vendorName)
			if reason != "" {
				msg += ": " + reason
			}
			warnings = append(warnings, msg)
			continue
		}
		if limit, capped := caps[event]; capped && e.Timeout > limit {
			warnings = append(warnings, fmt.Sprintf("hook timeout %ds on %s exceeds the %ds %s allows for it, so %ds is written", e.Timeout, event, limit, vendorName, limit))
		}
	}
	return warnings
}

// matcherWarnings covers the vendors that scope a matcher on some native
// events only. Claude and Codex pass a matcher through on every event.
func matcherWarnings(vendorName, event, nativeEvent string, entries []plugin.HookEntry) []string {
	var scoped map[string]bool
	switch vendorName {
	case "cursor":
		scoped = cursorMatcherEvents
	case "copilot":
		scoped = copilotMatcherEvents
	default:
		return nil
	}
	var warnings []string
	for _, e := range entries {
		if e.Matcher == "" {
			continue
		}
		if !scoped[nativeEvent] {
			warnings = append(warnings, fmt.Sprintf("hook matcher %q on %s is ignored on %s: %s documents a matcher for tool events only, so the hook runs unfiltered", e.Matcher, event, vendorName, nativeEvent))
			continue
		}
		// Cursor matches its own tool types, not Claude's tool names.
		if vendorName == "cursor" && usesClaudeToolName(e.Matcher) {
			warnings = append(warnings, fmt.Sprintf("hook matcher %q on %s is written to Cursor as-is, but Cursor matches tool types such as Shell, Read and Write, so a Claude tool name like Bash will not match", e.Matcher, event))
		}
	}
	return warnings
}

// usesClaudeToolName reports whether a matcher names a Claude Code tool whose
// Cursor tool type differs (Bash is Shell; Edit and MultiEdit are Write).
func usesClaudeToolName(matcher string) bool {
	for _, tok := range strings.FieldsFunc(matcher, func(r rune) bool { return r == '|' || r == ' ' }) {
		switch tok {
		case "Bash", "Edit", "MultiEdit":
			return true
		}
	}
	return false
}
