package assembler

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
)

// SessionHookGenerator is what WriteSessionHooks needs from a vendor: its
// session hook files and its config directory.
type SessionHookGenerator interface {
	ConfigDir() string
	GenerateHookConfig(hooks map[string][]plugin.HookEntry) (map[string][]byte, error)
}

// SessionHookScriptDirer is implemented by a vendor whose session hook
// commands reach a "./" script somewhere other than the run directory's root:
// it returns that directory, relative to the run directory. Claude reads its
// session hooks as a --plugin-dir plugin rooted at .claude/, and anchors "./"
// to ${CLAUDE_PLUGIN_ROOT}. A vendor without it runs hooks from the run
// directory, where the session starts, so the bare "./" reaches the root.
type SessionHookScriptDirer interface {
	SessionHookScriptDir() string
}

// WriteSessionHooks writes a vendor's session hook files into dir, the run
// directory of `ynh run`, `ynd preview` or the agent loop, and copies in each
// script those hooks run by a "./" path, from the harness at harnessDir, to
// where the session's hook commands reach it (#495). It returns a warning for
// each script it could not carry. A vendor that writes no hooks (Copilot) needs
// none of their scripts.
func WriteSessionHooks(dir string, adapter SessionHookGenerator, harnessDir string, hs HookSet) ([]string, error) {
	files, err := adapter.GenerateHookConfig(hs.Hooks)
	if err != nil {
		return nil, fmt.Errorf("generating hook config: %w", err)
	}
	if len(files) == 0 {
		return nil, nil
	}
	for relPath, content := range files {
		absPath := filepath.Join(dir, relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return nil, fmt.Errorf("creating hook config dir: %w", err)
		}
		if err := os.WriteFile(absPath, content, 0o644); err != nil {
			return nil, fmt.Errorf("writing hook config %s: %w", relPath, err)
		}
	}
	scriptDir := dir
	if d, ok := adapter.(SessionHookScriptDirer); ok {
		scriptDir = filepath.Join(dir, d.SessionHookScriptDir())
	}
	warnings, err := CopyHookScripts(harnessDir, scriptDir, hs, "the session")
	if err != nil {
		return nil, err
	}
	return warnings, nil
}

// CopyHookScripts copies into destDir each script the hooks run by a "./"
// path: the command's first word, taken from the same path under harnessDir
// and written to the same path under destDir, keeping its mode. A plugin
// export (#483) and a session (#495) both anchor such a command to where the
// copy lands, so the script must travel with the hooks. A script that came
// from an included harness (hs.Scripts) is taken from that harness's
// directory instead, and lands at the path ComposeHooks gave it. A "./" script
// that is not a regular file in its harness, or that climbs out of it, cannot
// be carried and comes back as a warning naming carrier (for example "the
// plugin"), not an error: the output is still valid and the hook may be meant
// for a tree the harness does not own. Neither is a destination that is
// already there as anything but a regular file, so the copy never writes
// through a symlink. Other commands (absolute, variable-anchored, PATH-style)
// are not scripts the harness ships and are left alone. Nothing is deleted.
func CopyHookScripts(harnessDir, destDir string, hs HookSet, carrier string) ([]string, error) {
	events := make([]string, 0, len(hs.Hooks))
	for event := range hs.Hooks {
		events = append(events, event)
	}
	sort.Strings(events)

	var warnings []string
	seen := map[string]bool{}
	for _, event := range events {
		for _, entry := range hs.Hooks[event] {
			script, ok := hookScript(entry.Command)
			if !ok || seen[script] {
				continue
			}
			seen[script] = true
			// where names the harness the script belongs to in a warning.
			srcDir, srcScript, where := harnessDir, script, "the harness"
			if origin, ok := hs.Scripts[script]; ok {
				srcDir, srcScript, where = origin.Dir, origin.Script, "included harness "+origin.Source
			}
			destRel := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(script, "./")))
			srcRel := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(srcScript, "./")))
			if !filepath.IsLocal(srcRel) || !filepath.IsLocal(destRel) {
				warnings = append(warnings, fmt.Sprintf("hook script %s is outside %s, so %s cannot carry it", srcScript, where, carrier))
				continue
			}
			src := filepath.Join(srcDir, srcRel)
			if info, err := os.Lstat(src); err != nil || !info.Mode().IsRegular() {
				warnings = append(warnings, fmt.Sprintf("hook script %s is not a file in %s, so %s does not carry it", srcScript, where, carrier))
				continue
			}
			dst := filepath.Join(destDir, destRel)
			if info, err := os.Lstat(dst); err == nil && !info.Mode().IsRegular() {
				warnings = append(warnings, fmt.Sprintf("hook script %s would replace something that is not a file, so %s does not carry it", srcScript, carrier))
				continue
			}
			if err := CopyFile(src, dst); err != nil {
				return nil, fmt.Errorf("copying hook script %s: %w", srcScript, err)
			}
		}
	}
	return warnings, nil
}

// hookScript returns the script a hook command runs when it names one by a
// "./" path, the form a plugin or session anchors to where its scripts are:
// the command's first word.
func hookScript(cmd string) (string, bool) {
	fields := strings.Fields(cmd)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "./") {
		return "", false
	}
	return fields[0], true
}

// IncludeScriptDir is where, under a run directory or plugin root, the "./"
// scripts of an included harness's hooks are placed: one subdirectory per
// include, so they cannot collide with the root's scripts or each other's.
const IncludeScriptDir = "scripts/_include"

// ScriptOrigin says where a "./" script that ComposeHooks rewrote comes from:
// the directory of the included harness that declared it, the path it had
// there, and the include's source for a warning.
type ScriptOrigin struct {
	Dir    string
	Script string
	Source string
}

// HookSet is the hooks a session or plugin carries: the root harness's own,
// and those of the included harnesses that were given consent to run them.
type HookSet struct {
	// Hooks holds each event's entries: included harnesses first, in content
	// order (dependencies before the harness that includes them), the root's
	// own last. An included harness's "./" command is already rewritten to
	// its place under IncludeScriptDir.
	Hooks map[string][]plugin.HookEntry
	// Scripts maps each rewritten "./" script path in Hooks to where it is
	// copied from. The root's scripts are not in it.
	Scripts map[string]ScriptOrigin
	// Inactive holds one warning per included harness that declares hooks
	// without consent to run them.
	Inactive []string
	// FromIncludes lists, per event, each included harness's hooks that are
	// in Hooks, and NotActive those that are not, for a report of where the
	// hooks came from.
	FromIncludes, NotActive []HookOrigin
}

// HookOrigin is an included harness's hooks for one event, and the include
// they came from.
type HookOrigin struct {
	Event, Source string
}

// ComposeHooks merges the hooks of root with those of the included harnesses
// among resolved whose include chain consented to them (HooksActive). An
// included harness that declares hooks without consent adds a warning to
// Inactive and nothing else. A "./" hook script of an included harness names
// a script in that harness's own directory: it is rewritten to a path under
// IncludeScriptDir, named for the include's namespace (with a short hash of
// the include's chain added when two harnesses share one), so that carrying it
// beside the root's scripts cannot collide. A script that climbs out of the
// included harness is an error. Entries are not de-duplicated.
func ComposeHooks(root map[string][]plugin.HookEntry, resolved []resolver.ResolveResult) (HookSet, error) {
	var active, inactive []resolver.ResolveResult
	counted := map[string]bool{}
	for _, r := range resolved {
		if r.Harness == nil || countHooks(r.Harness) == 0 || counted[r.Content.BasePath] {
			continue
		}
		counted[r.Content.BasePath] = true
		if r.HooksActive {
			active = append(active, r)
		} else {
			inactive = append(inactive, r)
		}
	}

	hs := HookSet{Hooks: root}
	for _, r := range inactive {
		events := make([]string, 0, len(r.Harness.Hooks))
		for event, entries := range r.Harness.Hooks {
			if len(entries) > 0 {
				events = append(events, event)
			}
		}
		sort.Strings(events)
		for _, event := range events {
			hs.NotActive = append(hs.NotActive, HookOrigin{Event: event, Source: r.Chain})
		}
		hs.Inactive = append(hs.Inactive, fmt.Sprintf("included harness %s declares hooks (%s) that are not active; add \"hooks\": true to its include to run them", r.Chain, strings.Join(events, ", ")))
	}
	if len(active) == 0 {
		return hs, nil
	}

	shared := map[string]int{}
	for _, r := range active {
		shared[r.Namespace]++
	}
	merged := make(map[string][]plugin.HookEntry, len(root))
	hs.Scripts = map[string]ScriptOrigin{}
	for _, r := range active {
		dirName := r.Namespace
		if shared[r.Namespace] > 1 {
			dirName += fmt.Sprintf("-%x", sha256.Sum256([]byte(r.Chain)))[:9]
		}
		for _, event := range sortedEvents(r.Harness.Hooks) {
			if len(r.Harness.Hooks[event]) > 0 {
				hs.FromIncludes = append(hs.FromIncludes, HookOrigin{Event: event, Source: r.Chain})
			}
			for _, entry := range r.Harness.Hooks[event] {
				script, ok := hookScript(entry.Command)
				if ok {
					rel := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(script, "./")))
					if !filepath.IsLocal(rel) {
						return HookSet{}, fmt.Errorf("included harness %s: hook script %s is outside the harness", r.Chain, script)
					}
					placed := "./" + IncludeScriptDir + "/" + dirName + "/" + filepath.ToSlash(rel)
					entry.Command = placed + strings.TrimLeft(entry.Command, " \t")[len(script):]
					hs.Scripts[placed] = ScriptOrigin{Dir: r.Harness.Dir, Script: script, Source: r.Chain}
				}
				merged[event] = append(merged[event], entry)
			}
		}
	}
	for event, entries := range root {
		merged[event] = append(merged[event], entries...)
	}
	hs.Hooks = merged
	return hs, nil
}

func sortedEvents(hooks map[string][]plugin.HookEntry) []string {
	events := make([]string, 0, len(hooks))
	for event := range hooks {
		events = append(events, event)
	}
	sort.Strings(events)
	return events
}

// countHooks is how many hook entries h declares.
func countHooks(h *harness.Harness) int {
	n := 0
	for _, entries := range h.Hooks {
		n += len(entries)
	}
	return n
}

// WriteComposedSessionHooks composes the hooks of root and its included
// harnesses (see ComposeHooks) and writes them into dir as WriteSessionHooks
// does. It returns the warnings to print: first those for included harnesses
// whose hooks are not active, then for scripts that could not be carried.
func WriteComposedSessionHooks(dir string, adapter SessionHookGenerator, root *harness.Harness, resolved []resolver.ResolveResult) ([]string, error) {
	hs, err := ComposeHooks(root.Hooks, resolved)
	if err != nil {
		return nil, err
	}
	warnings := hs.Inactive
	if len(hs.Hooks) == 0 {
		return warnings, nil
	}
	written, err := WriteSessionHooks(dir, adapter, root.Dir, hs)
	if err != nil {
		return nil, err
	}
	return append(warnings, written...), nil
}
