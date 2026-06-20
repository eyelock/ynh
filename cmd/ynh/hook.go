package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/vendor"
)

func cmdHook(args []string) error {
	return cmdHookTo(args, os.Stdout)
}

func cmdHookTo(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ynh hook <add|remove|export>")
	}
	switch args[0] {
	case "add":
		return cmdHookAdd(args[1:], stdout)
	case "remove":
		return cmdHookRemove(args[1:], stdout)
	case "export":
		return cmdHookExport(args[1:], stdout)
	default:
		return fmt.Errorf("unknown hook subcommand: %s\nUsage: ynh hook <add|remove|export>", args[0])
	}
}

func cmdHookAdd(args []string, stdout io.Writer) error {
	var opts harness.HookAddOptions
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--matcher":
			if i+1 >= len(args) {
				return fmt.Errorf("--matcher requires a value")
			}
			i++
			opts.Matcher = args[i]
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown flag: %s", args[i])
			}
			positional = append(positional, args[i])
		}
	}
	if len(positional) != 3 {
		return fmt.Errorf("usage: ynh hook add <harness> <event> <command> [--matcher <pattern>]")
	}
	harnessRef, event, command := positional[0], positional[1], positional[2]

	dir, _, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}
	if err := harness.AddHook(dir, event, command, opts); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Added hook (event %s)\n", event)
	return nil
}

func cmdHookRemove(args []string, stdout io.Writer) error {
	var positional []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return fmt.Errorf("unknown flag: %s", a)
		}
		positional = append(positional, a)
	}
	if len(positional) != 3 {
		return fmt.Errorf("usage: ynh hook remove <harness> <event> <index>")
	}
	harnessRef, event, idxStr := positional[0], positional[1], positional[2]
	index, err := strconv.Atoi(idxStr)
	if err != nil {
		return fmt.Errorf("hook index must be an integer: %s", idxStr)
	}

	dir, _, rErr := harness.ResolveEditTarget(harnessRef)
	if rErr != nil {
		return rErr
	}
	if err := harness.RemoveHook(dir, event, index); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Removed hook %d (event %s)\n", index, event)
	return nil
}

// hookExportTargets maps the --target value to the project-relative settings
// file Claude Code auto-loads in a plain session.
var hookExportTargets = map[string]string{
	"settings": filepath.Join(".claude", "settings.json"),       // committed, team-wide
	"local":    filepath.Join(".claude", "settings.local.json"), // gitignored, personal
}

func cmdHookExport(args []string, stdout io.Writer) error {
	// settings/local are Claude-only files, so claude is the only meaningful
	// vendor here; -v stays accepted for explicitness and clear errors.
	vendorName := vendor.DefaultName
	var target string
	var dryRun bool
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-v", "--vendor":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires a value", args[i])
			}
			i++
			vendorName = args[i]
		case "--target":
			if i+1 >= len(args) {
				return fmt.Errorf("--target requires a value (settings or local)")
			}
			i++
			target = args[i]
		case "--dry-run":
			dryRun = true
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown flag: %s", args[i])
			}
			positional = append(positional, args[i])
		}
	}
	if len(positional) != 1 {
		return fmt.Errorf("usage: ynh hook export <harness> [-v <vendor>] --target <settings|local> [--dry-run]")
	}
	harnessRef := positional[0]

	relFile, ok := hookExportTargets[target]
	if !ok {
		return fmt.Errorf("--target must be 'settings' (.claude/settings.json) or 'local' (.claude/settings.local.json); got %q", target)
	}
	// settings/local are Claude's auto-loaded project files. Other vendors
	// auto-load their hooks via symlink install, so there is nothing to export.
	if vendorName != "claude" {
		return fmt.Errorf("hook export --target is Claude-specific; vendor %q auto-loads hooks from its config dir when installed", vendorName)
	}

	dir, _, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}
	hj, err := plugin.LoadPluginJSON(dir)
	if err != nil {
		return err
	}
	if len(hj.Hooks) == 0 {
		return fmt.Errorf("harness %q declares no hooks to export", harnessRef)
	}

	adapter, err := vendor.Get(vendorName)
	if err != nil {
		return err
	}
	gen, err := adapter.GenerateHookConfig(hj.Hooks)
	if err != nil {
		return err
	}
	// GenerateHookConfig emits a single {"hooks": {...}} document; pull out the
	// translated hooks object to merge into the target settings file.
	var genDoc map[string]any
	for _, data := range gen {
		if err := json.Unmarshal(data, &genDoc); err != nil {
			return fmt.Errorf("parsing generated hook config: %w", err)
		}
	}
	genHooks, _ := genDoc["hooks"].(map[string]any)
	if len(genHooks) == 0 {
		return fmt.Errorf("harness %q declares no hooks for vendor %q", harnessRef, vendorName)
	}

	// Load the existing settings file (if any) so non-hook keys are preserved.
	settings := map[string]any{}
	existed := false
	if data, rerr := os.ReadFile(relFile); rerr == nil {
		existed = true
		if uerr := json.Unmarshal(data, &settings); uerr != nil {
			return fmt.Errorf("%s is not valid JSON: %w", relFile, uerr)
		}
	}

	added := mergeHookEntries(settings, genHooks)

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling settings: %w", err)
	}
	out = append(out, '\n')

	if dryRun {
		_, _ = fmt.Fprintf(stdout, "# dry run — would write %s (%d new hook group(s))\n", relFile, added)
		_, _ = stdout.Write(out)
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(relFile), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(relFile), err)
	}
	if err := os.WriteFile(relFile, out, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", relFile, err)
	}

	verb := "Updated"
	if !existed {
		verb = "Created"
	}
	_, _ = fmt.Fprintf(stdout, "%s %s (%d new hook group(s) from %s)\n", verb, relFile, added, hj.Name)
	return nil
}

// mergeHookEntries unions the generated per-event hook groups into the
// settings map's "hooks" key, skipping any group already present verbatim
// (so re-running is idempotent and a user's own hooks are never removed).
// Non-hook keys in settings are left untouched. Returns the count of groups
// newly added.
func mergeHookEntries(settings map[string]any, generated map[string]any) int {
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}

	events := make([]string, 0, len(generated))
	for e := range generated {
		events = append(events, e)
	}
	sort.Strings(events)

	added := 0
	for _, event := range events {
		genArr, _ := generated[event].([]any)
		existing, _ := hooks[event].([]any)
		for _, group := range genArr {
			if !containsEquivalent(existing, group) {
				existing = append(existing, group)
				added++
			}
		}
		hooks[event] = existing
	}
	settings["hooks"] = hooks
	return added
}

// containsEquivalent reports whether arr already holds a value deeply equal to v.
func containsEquivalent(arr []any, v any) bool {
	for _, e := range arr {
		if reflect.DeepEqual(e, v) {
			return true
		}
	}
	return false
}
