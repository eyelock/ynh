package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/permission"
)

// runPermission is the permission a one-shot run opens at, resolved from the
// harness's level, the delegates_to entry and per-call request or flag (which
// may only narrow it), the session that called it (D6) and the project's own
// mode (D5).
type runPermission struct {
	Level permission.Level
	From  permission.Source
	// Args and Env are what the vendor is passed: the level's mapping, or the
	// project's own mode explicitly when it narrowed the level (D5), so a
	// user-level setting cannot widen the run.
	Args []string
	Env  []string
	// OpenedMode is the --permission-mode Claude Code is expected to report
	// ("" for other vendors); the opened-mode check compares against it.
	OpenedMode string
	// Lines are the sentences printed to stderr: the level and its source, and
	// the project's line when it narrowed the level.
	Lines []string
}

// resolveRunPermission resolves the level of a one-shot run of harness p on
// vendorName in folder dir. flag is --auto-approve, which may only narrow. A
// delegate is run with the level its delegation resolved, and the source of
// it in permission.SourceEnv, so isDelegate names the harness's declaration
// delegate_default.
func resolveRunPermission(p *harness.Harness, vendorName, flag, dir string, isDelegate bool, getenv func(string) string, euid int) (*runPermission, error) {
	// The words of auto_approve_from are permission's, in one place.
	base := permission.Step{Level: permission.Level(p.AutoApproveOf()), From: permission.SourceDefault}
	if p.AutoApprove != "" {
		base.From = permission.SourceHarness
		if isDelegate {
			base.From = permission.SourceDelegateDefault
		}
	}
	reqFrom := permission.SourceFlag
	if s := getenv(permission.SourceEnv); isDelegate && s != "" {
		reqFrom = permission.Source(s)
	}
	cur, err := base.Ask(permission.Level(flag), reqFrom)
	if err != nil {
		return nil, fmt.Errorf("--auto-approve: %w", err)
	}
	limit, err := permission.CapFrom(getenv)
	if err != nil {
		return nil, err
	}
	cur = cur.Narrowed(permission.Step{Level: limit, From: permission.SourceCallerCap})

	res, err := permission.Resolve(dir, vendorName, cur.Level)
	if err != nil {
		return nil, err
	}
	out := &runPermission{Level: res.Level, From: cur.From}
	if res.FromProject() {
		out.From = permission.SourceProject
		out.Lines = append(out.Lines, res.Line)
	}
	if err := permission.CheckRoot(vendorName, res.Level, euid, getenv); err != nil {
		return nil, err
	}
	m, err := res.Mapping(vendorName)
	if err != nil {
		return nil, fmt.Errorf("--auto-approve %s (%s): %w", res.Level, cur.From, err)
	}
	out.Args, out.Env, out.OpenedMode = m.Args, m.Env, res.OpenedMode(vendorName)
	out.Lines = append([]string{fmt.Sprintf("auto-approve: %s (%s)", out.Level, out.From)}, out.Lines...)
	return out, nil
}

// applyEnv sets the vendor environment of the permission and the cap every
// session this run starts inherits (D6). The permission block of OpenCode is
// merged into OPENCODE_CONFIG_CONTENT by permission.MergeEnv, which keeps the
// caller's own nested keys.
func (rp *runPermission) applyEnv() error {
	if len(rp.Env) > 0 {
		merged, err := permission.MergeEnv(os.Environ(), rp.Env)
		if err != nil {
			return err
		}
		for _, kv := range rp.Env {
			name, _, _ := strings.Cut(kv, "=")
			for i := len(merged) - 1; i >= 0; i-- {
				if v, ok := strings.CutPrefix(merged[i], name+"="); ok {
					if err := os.Setenv(name, v); err != nil {
						return err
					}
					break
				}
			}
		}
	}
	return os.Setenv(permission.CapEnv, string(rp.Level))
}

// capInteractive sets the cap of an interactive session run with
// --auto-approve (S4): no flag is passed to the vendor and a person still
// approves, but every delegate the session starts opens no wider than level.
func capInteractive(level string) error {
	l, err := permission.Parse(level)
	if err != nil {
		return fmt.Errorf("--auto-approve: %w", err)
	}
	if limit, err := permission.CapFrom(os.Getenv); err != nil {
		return err
	} else if limit != "" {
		l = permission.Narrow(l, limit)
	}
	return os.Setenv(permission.CapEnv, string(l))
}

// delegateStep is the most a delegates_to entry's delegate may open at: the
// delegate harness's own level (its ceiling), narrowed by the entry (a wider
// entry is refused, D2) and by the calling session's level (D6). A per-call
// request narrows it further, at the call.
func delegateStep(delegate *harness.Harness, entry harness.Delegate, getenv func(string) string) (permission.Step, error) {
	base := permission.Step{Level: permission.Level(delegate.AutoApproveOf()), From: permission.SourceDelegateDefault}
	if delegate.AutoApprove == "" {
		base.From = permission.SourceDefault
	}
	cur, err := base.Ask(permission.Level(entry.AutoApprove), permission.SourceEntry)
	if err != nil {
		return cur, fmt.Errorf("delegates_to %s: auto_approve: %w", entry.Label(), err)
	}
	limit, err := permission.CapFrom(getenv)
	if err != nil {
		return cur, err
	}
	return cur.Narrowed(permission.Step{Level: limit, From: permission.SourceCallerCap}), nil
}

// openedModeSettings returns the arguments that make a Claude Code session
// check the mode it opened in (the opened-mode check, on every one-shot path:
// ynh run and both delegate transports, which end in a one-shot ynh run). A
// SessionStart hook runs `ynh permission-check`, which compares the
// permission_mode Claude reports in the hook input with the one ynh chose and
// stops the session on a mismatch, with the message agent run gives. It is nil
// for other vendors, and for a run that already carries its own --settings,
// which ynh will not override: that run says so on stderr.
func openedModeSettings(vendorName, expected string, vendorArgs []string) ([]string, error) {
	if vendorName != permission.Claude || expected == "" {
		return nil, nil
	}
	for _, a := range vendorArgs {
		if a == "--settings" || strings.HasPrefix(a, "--settings=") {
			fmt.Fprintln(os.Stderr, "ynh: this run passes its own --settings, so the mode Claude opens in is not checked")
			return nil, nil
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("finding the ynh binary for the opened-mode check: %w", err)
	}
	settings, err := json.Marshal(map[string]any{"hooks": map[string]any{"SessionStart": []any{map[string]any{
		"hooks": []any{map[string]any{"type": "command", "command": shellQuote(exe) + " permission-check --expect " + shellQuote(expected)}}}}}})
	if err != nil {
		return nil, err
	}
	return []string{"--settings", string(settings)}, nil
}

// cmdPermissionCheck is the SessionStart hook of the opened-mode check. It
// reads the hook's JSON input, and when Claude Code opened in a different
// permission mode than --expect, stops the session.
//
//	ynh permission-check --expect <mode>
func cmdPermissionCheck(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) != 2 || args[0] != "--expect" {
		return fmt.Errorf("usage: ynh permission-check --expect <mode>")
	}
	var in struct {
		PermissionMode string `json:"permission_mode"`
	}
	if err := json.NewDecoder(stdin).Decode(&in); err != nil {
		return nil // not a hook input: nothing to compare
	}
	if err := permission.OpenedMismatch(args[1], in.PermissionMode); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return json.NewEncoder(stdout).Encode(map[string]any{"continue": false, "stopReason": err.Error()})
	}
	return nil
}
