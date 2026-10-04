package assembler

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eyelock/ynh/internal/plugin"
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
func WriteSessionHooks(dir string, adapter SessionHookGenerator, harnessDir string, hooks map[string][]plugin.HookEntry) ([]string, error) {
	files, err := adapter.GenerateHookConfig(hooks)
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
	warnings, err := CopyHookScripts(harnessDir, scriptDir, hooks, "the session")
	if err != nil {
		return nil, err
	}
	return warnings, nil
}

// CopyHookScripts copies into destDir each script the hooks run by a "./"
// path: the command's first word, taken from the same path under harnessDir
// and written to the same path under destDir, keeping its mode. A plugin
// export (#483) and a session (#495) both anchor such a command to where the
// copy lands, so the script must travel with the hooks. A "./" script that is
// not a regular file in the harness, or that climbs out of it, cannot be
// carried and comes back as a warning naming carrier (for example "the
// plugin"), not an error: the output is still valid and the hook may be meant
// for a tree the harness does not own. Neither is a destination that is
// already there as anything but a regular file, so the copy never writes
// through a symlink. Other commands (absolute, variable-anchored, PATH-style)
// are not scripts the harness ships and are left alone. Nothing is deleted.
func CopyHookScripts(harnessDir, destDir string, hooks map[string][]plugin.HookEntry, carrier string) ([]string, error) {
	events := make([]string, 0, len(hooks))
	for event := range hooks {
		events = append(events, event)
	}
	sort.Strings(events)

	var warnings []string
	seen := map[string]bool{}
	for _, event := range events {
		for _, entry := range hooks[event] {
			script, ok := hookScript(entry.Command)
			if !ok || seen[script] {
				continue
			}
			seen[script] = true
			rel := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(script, "./")))
			if !filepath.IsLocal(rel) {
				warnings = append(warnings, fmt.Sprintf("hook script %s is outside the harness, so %s cannot carry it", script, carrier))
				continue
			}
			src := filepath.Join(harnessDir, rel)
			if info, err := os.Lstat(src); err != nil || !info.Mode().IsRegular() {
				warnings = append(warnings, fmt.Sprintf("hook script %s is not a file in the harness, so %s does not carry it", script, carrier))
				continue
			}
			dst := filepath.Join(destDir, rel)
			if info, err := os.Lstat(dst); err == nil && !info.Mode().IsRegular() {
				warnings = append(warnings, fmt.Sprintf("hook script %s would replace something that is not a file, so %s does not carry it", script, carrier))
				continue
			}
			if err := CopyFile(src, dst); err != nil {
				return nil, fmt.Errorf("copying hook script %s: %w", script, err)
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
