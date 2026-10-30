package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"

	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/vendor"
)

// hookVendorWarnings reports each hook the vendors a harness targets will not
// carry as declared (vendor.HookWarnings): an event a vendor cannot express, or
// a matcher it ignores. They are warnings, never errors: the manifest is valid
// and every other vendor still gets the hook. The targeted vendors are the
// harness's default_vendor, or every registered vendor when it names none.
// Hooks are read from the top level and from each profile.
func hookVendorWarnings(hj map[string]any) []string {
	vendors := vendor.Available()
	if v, _ := hj["default_vendor"].(string); v != "" {
		vendors = []string{v}
	}

	scopes := []struct {
		label string
		hooks map[string][]plugin.HookEntry
	}{{"", decodeHooks(hj)}}
	if profiles, ok := hj["profiles"].(map[string]any); ok {
		for _, name := range slices.Sorted(maps.Keys(profiles)) {
			if pm, ok := profiles[name].(map[string]any); ok {
				scopes = append(scopes, struct {
					label string
					hooks map[string][]plugin.HookEntry
				}{fmt.Sprintf("profile %q: ", name), decodeHooks(pm)})
			}
		}
	}

	var warnings []string
	for _, s := range scopes {
		for _, v := range vendors {
			for _, w := range vendor.HookWarnings(v, s.hooks) {
				warnings = append(warnings, s.label+w)
			}
		}
	}
	return warnings
}

// decodeHooks reads the "hooks" object of a manifest or profile, skipping
// whatever is malformed: validateHarnessHooks reports that as an error.
func decodeHooks(m map[string]any) map[string][]plugin.HookEntry {
	raw, ok := m["hooks"].(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string][]plugin.HookEntry, len(raw))
	for event, v := range raw {
		data, err := json.Marshal(v)
		if err != nil {
			continue
		}
		var entries []plugin.HookEntry
		if json.Unmarshal(data, &entries) == nil {
			out[event] = entries
		}
	}
	return out
}

// printHookWarnings prints the hook warnings for the manifest at path to
// stderr, for `ynd lint`. They do not count as issues.
func printHookWarnings(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var hj map[string]any
	if json.Unmarshal(data, &hj) != nil {
		return
	}
	for _, w := range hookVendorWarnings(hj) {
		fmt.Fprintf(os.Stderr, "%s: warning: %s\n", path, w)
	}
}
