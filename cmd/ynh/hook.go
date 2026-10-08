package main

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
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
	"settings": filepath.Join((&vendor.Claude{}).ConfigDir(), "settings.json"),       // committed, team-wide
	"local":    filepath.Join((&vendor.Claude{}).ConfigDir(), "settings.local.json"), // gitignored, personal
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
	hooks, err := exportableHooks(dir, hj.Hooks)
	if err != nil {
		return err
	}
	if len(hooks) == 0 {
		return fmt.Errorf("harness %q declares no hooks to export", harnessRef)
	}

	// ClaudeSettingsHooks emits a single {"hooks": {...}} document; pull out
	// the translated hooks object to merge into the target settings file.
	gen, err := vendor.ClaudeSettingsHooks(hooks)
	if err != nil {
		return err
	}
	var genDoc map[string]any
	if gen != nil {
		if err := json.Unmarshal(gen, &genDoc); err != nil {
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

// exportableHooks adds to a harness's own hooks those of the included
// harnesses that consented to them ("hooks": true on the include, at every
// link). Includes are only resolved when one of the harness's includes
// consents, so exporting a harness's own hooks needs no network. A settings
// file belongs to the project and cannot reach into an included harness's
// directory, so an included hook that runs a "./" script is an error naming
// the include: write the command absolute or anchored to $CLAUDE_PROJECT_DIR
// in the included harness, or run it through `ynh run`.
func exportableHooks(dir string, own map[string][]plugin.HookEntry) (map[string][]plugin.HookEntry, error) {
	h, err := harness.LoadDir(dir)
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(h.Includes, func(inc harness.Include) bool { return inc.Hooks }) {
		return own, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	resolved, err := resolver.Resolve(h, cfg)
	if err != nil {
		return nil, fmt.Errorf("resolving includes: %w", err)
	}
	hs, err := assembler.ComposeHooks(own, resolved)
	if err != nil {
		return nil, err
	}
	for _, w := range hs.Inactive {
		fmt.Fprintf(os.Stderr, "  warning: %s\n", w)
	}
	if len(hs.Scripts) > 0 {
		origin := hs.Scripts[slices.Sorted(maps.Keys(hs.Scripts))[0]]
		return nil, fmt.Errorf("included harness %s runs the script %s from its own directory, which a project settings file cannot reach; use an absolute or $CLAUDE_PROJECT_DIR command in that harness, or run it through ynh run", origin.Source, origin.Script)
	}
	return hs.Hooks, nil
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
