package permission

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/eyelock/ynh/internal/vendor"
)

// ProjectMode is a permission mode the working folder's own project settings
// choose for a vendor's CLI, placed on the none/edits/all order.
type ProjectMode struct {
	Level   Level
	File    string // the settings file
	Setting string // the setting and its value, as written
	// Args are the arguments that open the run in this mode, explicitly, so a
	// wider user-level setting cannot widen it (D5): the project's own words
	// where ynh can place them, else the narrowest level's arguments.
	Args []string
	// Opened is the --permission-mode Claude Code is passed, which the
	// opened-mode check compares its session against ("" for other vendors).
	Opened string
}

// Describe names the file and the setting, for the line a run prints.
func (p ProjectMode) Describe() string { return fmt.Sprintf("%s sets %s", p.File, p.Setting) }

// Where each vendor's project mode sits on the none/edits/all order (D5).
// Provisional until ynh #1058 P0 writes the order from live runs; this is the
// one place to correct it. A value not listed is treated as none, the
// narrowest, so a mode ynh cannot place never widens a run.
var (
	claudeModeLevel = map[string]Level{
		"default": None, "plan": None, "dontAsk": None,
		"acceptEdits":       Edits,
		"bypassPermissions": All,
	}
	codexSandboxLevel = map[string]Level{
		"read-only": None, "workspace-write": Edits, "danger-full-access": All,
	}
	codexApprovalLevel = map[string]Level{
		"untrusted": None, "on-request": Edits, "on-failure": Edits,
	}
)

// Project reports the permission mode the project in dir chooses for vendor,
// or nil when it sets none. Only project and project-local files in dir are
// read: user-level settings are the operator's own defaults, which ynh's flags
// override. It touches nothing.
func Project(dir, vendorName string) (*ProjectMode, error) {
	switch vendorName {
	case Claude:
		for _, name := range []string{"settings.json", "settings.local.json"} {
			pm, err := claudeProject(filepath.Join(dir, (&vendor.Claude{}).ConfigDir(), name))
			if pm != nil || err != nil {
				return pm, err
			}
		}
	case Codex:
		return codexProject(filepath.Join(dir, (&vendor.Codex{}).ConfigDir(), "config.toml"))
	}
	// cursor's project file carries only allow and deny lists, which it still
	// applies under --force; opencode's rules still apply under --auto. Neither
	// has a project approval mode.
	return nil, nil
}

func claudeProject(path string) (*ProjectMode, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var s struct {
		Permissions struct {
			DefaultMode string `json:"defaultMode"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	m := s.Permissions.DefaultMode
	if m == "" {
		return nil, nil
	}
	// A mode ynh cannot place (such as auto, which may approve commands) is
	// opened as default, the narrowest, and reported as none.
	lvl, ok := claudeModeLevel[m]
	word := m
	if !ok {
		lvl, word = None, "default"
	}
	args := []string{"--permission-mode", word}
	if lvl == Edits {
		// A project's edits is exactly the edits mapping: file edits, no commands.
		if em, err := For(Claude, Edits); err == nil {
			args = em.Args
		}
	}
	return &ProjectMode{Level: lvl, File: path, Setting: fmt.Sprintf("permissions.defaultMode %q", m),
		Args: args, Opened: word}, nil
}

// codexProject reads a top-level sandbox_mode or approval_policy. Only
// top-level keys count: a key under [profiles.x] applies only when that
// profile is selected. An approval_policy of "never" asks nothing and so
// places no bound on its own, as does an unplaced key's absence.
func codexProject(path string) (*ProjectMode, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var found *ProjectMode
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			break
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.HasPrefix(line, "#") {
			continue
		}
		key = strings.Trim(strings.TrimSpace(key), `"`)
		value = strings.TrimSpace(value)
		word := strings.Trim(value, `"'`)
		var lvl Level
		var args []string
		switch key {
		case "sandbox_mode":
			lvl = codexSandboxLevel[word]
			args = []string{"--sandbox", word}
		case "approval_policy":
			if word == "never" {
				continue
			}
			lvl = codexApprovalLevel[word]
			args = []string{"-c", fmt.Sprintf("approval_policy=%q", word)}
		default:
			continue
		}
		if lvl == "" {
			// A value ynh cannot place opens at none, explicitly.
			lvl, args = None, []string{"--sandbox", "read-only"}
		}
		pm := &ProjectMode{Level: lvl, File: path, Setting: key + " = " + value, Args: args}
		if found == nil {
			found = pm
			continue
		}
		// Both keys are passed, so the session opens at the narrower of them.
		found.Args = append(found.Args, pm.Args...)
		found.Setting += "; " + pm.Setting
		if pm.Level.Narrower(found.Level) {
			found.Level = pm.Level
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	// Always pass --sandbox, whatever the project sets: a project that sets
	// only approval_policy leaves the sandbox to the user's own config, which
	// may be danger-full-access. The narrowest, read-only, is passed then.
	if found != nil && !slices.Contains(found.Args, "--sandbox") {
		found.Args = append(found.Args, "--sandbox", "read-only")
		found.Level = None
	}
	return found, nil
}

// Resolution is the level a run opens at once the project's mode is weighed.
type Resolution struct {
	// Level is what the run opens at.
	Level Level
	// Project is the project mode that narrowed it, or nil.
	Project *ProjectMode
	// Line is the sentence a run prints when the project narrowed it, or "".
	Line string
}

// FromProject reports whether the project's mode set the level, so ynh passes
// the vendor the project's mode explicitly (Args) in place of the level's.
func (r Resolution) FromProject() bool { return r.Project != nil }

// Mapping is what the vendor is passed for this resolution: the project's
// mode, explicitly, when it narrowed the level, else the level's own mapping.
func (r Resolution) Mapping(vendor string) (Mapping, error) {
	if r.FromProject() {
		return Mapping{Args: r.Project.Args}, nil
	}
	return For(vendor, r.Level)
}

// OpenedMode is the --permission-mode a Claude Code session is expected to
// report: the project's, when it narrowed the level, else the level's.
func (r Resolution) OpenedMode(vendor string) string {
	if vendor != Claude {
		return ""
	}
	if r.FromProject() {
		return r.Project.Opened
	}
	return ClaudeMode(r.Level)
}

// Resolve weighs level against the project's mode in dir (D5). A project mode
// narrower than level wins: the run opens at the project's mode, passed to the
// vendor explicitly (so a user-level setting cannot widen it), and Line names the file and setting. A wider project mode
// loses to ynh's explicit level. A project mode never refuses a run.
func Resolve(dir, vendorName string, level Level) (Resolution, error) {
	pm, err := Project(dir, vendorName)
	if err != nil {
		return Resolution{}, fmt.Errorf("checking project settings: %w", err)
	}
	if pm == nil || !pm.Level.Narrower(level) {
		return Resolution{Level: level}, nil
	}
	return Resolution{
		Level:   pm.Level,
		Project: pm,
		Line: fmt.Sprintf("auto-approve: opening at the project's mode (%s) rather than %s: %s",
			pm.Level, level, pm.Describe()),
	}, nil
}
