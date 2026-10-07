package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/vendor"
)

// Auto-approve levels for `ynh agent run --auto-approve`.
//
// By default ynh passes no permission flag: the worker gets whatever the
// vendor CLI and the project grant it. --auto-approve exists for runs inside
// containment the operator owns (a container, an egress policy, a diff gate,
// human review). It is a run-time grant only: the harness manifest cannot set
// it, and a resume does not restore it.
const (
	// AutoApproveEdits approves file edits without prompting. Commands are
	// not approved.
	AutoApproveEdits = "edits"
	// AutoApproveAll approves everything the worker asks to do.
	AutoApproveAll = "all"
)

// validateAutoApprove rejects a level the chosen backend cannot honour
// exactly. A backend with no honest equivalent for a level is an error, never
// a silent widening (edits mapped to something that also approves commands)
// or narrowing (a grant that quietly does not apply).
func validateAutoApprove(level, backend string) error {
	switch level {
	case "":
		return nil
	case AutoApproveEdits, AutoApproveAll:
	default:
		return fmt.Errorf("unknown --auto-approve level %q (supported: edits, all)", level)
	}
	switch backend {
	case "claude":
		return nil
	case "codex", "cursor":
		if level == AutoApproveEdits {
			return fmt.Errorf(
				"%s cannot auto-approve edits only: it has no mode that approves file edits while still withholding commands; "+
					"use --auto-approve all or a different backend", backend)
		}
		return nil
	default:
		return fmt.Errorf("--auto-approve is not supported by the %s backend (supported: claude, codex, cursor)", backend)
	}
}

// claudePermissionMode is the claude --permission-mode a level maps to.
//
//	edits: acceptEdits (file edits approved, commands still need approval)
//	all:   bypassPermissions (every permission check skipped)
func claudePermissionMode(level string) string {
	switch level {
	case AutoApproveEdits:
		return "acceptEdits"
	case AutoApproveAll:
		return "bypassPermissions"
	}
	return ""
}

// projectPermissionSetting reports whether the project in dir chooses its own
// permission mode for backend's vendor CLI. It returns a reason naming the
// file and the setting, or "" when the project sets none. The project's choice
// wins over --auto-approve, so a non-empty reason refuses the run.
//
// Only project and project-local files in dir are read. User-level settings
// are not consulted: they are the operator's own defaults, which the flag is
// the operator's way of overriding for one run.
//
// It is a pure function of the directory's contents and touches nothing.
func projectPermissionSetting(dir, backend string) (string, error) {
	switch backend {
	case "claude":
		for _, name := range []string{"settings.json", "settings.local.json"} {
			reason, err := claudeDefaultMode(filepath.Join(dir, (&vendor.Claude{}).ConfigDir(), name))
			if reason != "" || err != nil {
				return reason, err
			}
		}
		return "", nil
	case "codex":
		return codexProjectPolicy(filepath.Join(dir, (&vendor.Codex{}).ConfigDir(), "config.toml"))
	default:
		// cursor's project file (.cursor/cli.json) carries only allow and
		// deny lists, which cursor still applies under --force ("unless
		// explicitly denied"). Its approval mode is a global setting only.
		return "", nil
	}
}

// claudeDefaultMode reads permissions.defaultMode from a claude settings file.
func claudeDefaultMode(path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	var settings struct {
		Permissions struct {
			DefaultMode string `json:"defaultMode"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return "", fmt.Errorf("parsing %s: %w", path, err)
	}
	if m := settings.Permissions.DefaultMode; m != "" {
		return fmt.Sprintf("%s sets permissions.defaultMode %q", path, m), nil
	}
	return "", nil
}

// codexProjectPolicy reports a top-level approval_policy or sandbox_mode in a
// project's .codex/config.toml. Only top-level keys are read: a key under a
// table such as [profiles.x] applies only when that profile is selected.
func codexProjectPolicy(path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			break // the first table header ends the top level
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.HasPrefix(line, "#") {
			continue
		}
		key = strings.Trim(strings.TrimSpace(key), `"`)
		if key == "approval_policy" || key == "sandbox_mode" {
			return fmt.Sprintf("%s sets %s = %s", path, key, strings.TrimSpace(value)), nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return "", nil
}

// checkProjectPermissions refuses --auto-approve when the project in dir has
// made its own permission choice for backend.
func checkProjectPermissions(level, backend, dir string) error {
	if level == "" {
		return nil
	}
	reason, err := projectPermissionSetting(dir, backend)
	if err != nil {
		return fmt.Errorf("--auto-approve: checking project settings: %w", err)
	}
	if reason != "" {
		return fmt.Errorf("--auto-approve %s refused: the project chooses its own permission mode (%s); "+
			"the project's choice wins, so remove the setting or run without --auto-approve", level, reason)
	}
	return nil
}
