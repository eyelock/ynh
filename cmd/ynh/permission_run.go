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

// The hook events the opened-mode check runs on (Claude Code 2.1.332, checked
// live, ynw-c5hme): SessionStart carries no permission_mode and nothing
// printed there stops a one-shot session, so the check is a UserPromptSubmit
// hook, which carries permission_mode and stops the session before any tool
// runs with exit 2, and a PreToolUse hook as a backstop, which denies the tool
// if the prompt hook was skipped.
const (
	openedModeUserPrompt = "user_prompt"
	openedModePreTool    = "pre_tool"
)

// openedModeSettings returns vendorArgs with the opened-mode check added for a
// Claude Code session (the check runs on every path: ynh run and both delegate
// transports, which end in a one-shot ynh run). The hooks run
// `ynh permission-check`, which compares the permission_mode Claude reports in
// the hook input with the one ynh chose and stops the session on a mismatch,
// with the message agent run gives. A run that passes its own --settings has
// the hooks merged into it (a JSON value, or a file's JSON passed inline); one
// whose --settings cannot be merged exactly is refused, never run unchecked.
// Two --settings do not merge in Claude Code (the last replaces the first), so
// the merge is into the one. Other vendors report no mode, so their args are
// returned unchanged.
func openedModeSettings(vendorName, expected string, vendorArgs []string) ([]string, error) {
	if vendorName != permission.Claude || expected == "" {
		return vendorArgs, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("finding the ynh binary for the opened-mode check: %w", err)
	}
	group := func(event, matcher string) map[string]any {
		g := map[string]any{"hooks": []any{map[string]any{
			"type": "command", "command": shellQuote(exe) + " permission-check --expect " + shellQuote(expected) + " --event " + event}}}
		if matcher != "" {
			g["matcher"] = matcher
		}
		return g
	}

	idx, inline := -1, false
	for i, a := range vendorArgs {
		if a == "--settings" || strings.HasPrefix(a, "--settings=") {
			if idx >= 0 {
				return nil, fmt.Errorf("refusing to run: more than one --settings is passed, so the opened-mode check cannot be added exactly; pass one --settings or none")
			}
			idx, inline = i, strings.HasPrefix(a, "--settings=")
		}
	}
	doc := map[string]any{}
	value := ""
	if idx >= 0 {
		if inline {
			value = strings.TrimPrefix(vendorArgs[idx], "--settings=")
		} else if idx+1 < len(vendorArgs) {
			value = vendorArgs[idx+1]
		} else {
			return nil, fmt.Errorf("refusing to run: --settings has no value, so the opened-mode check cannot be added")
		}
		raw := []byte(value)
		if !strings.HasPrefix(strings.TrimSpace(value), "{") {
			if raw, err = os.ReadFile(value); err != nil {
				return nil, fmt.Errorf("refusing to run: the --settings file %q cannot be read, so the opened-mode check cannot be added to it: %w", value, err)
			}
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("refusing to run: --settings is not a JSON object, so the opened-mode check cannot be added to it: %w", err)
		}
	}
	hooks, _ := doc["hooks"].(map[string]any)
	if hooks == nil {
		if doc["hooks"] != nil {
			return nil, fmt.Errorf("refusing to run: --settings has a hooks value that is not an object, so the opened-mode check cannot be added to it")
		}
		hooks = map[string]any{}
	}
	for ev, g := range map[string]map[string]any{
		"UserPromptSubmit": group(openedModeUserPrompt, ""),
		"PreToolUse":       group(openedModePreTool, "*"),
	} {
		if existing, bad := hooks[ev]; bad && existing != nil {
			if _, ok := existing.([]any); !ok {
				return nil, fmt.Errorf("refusing to run: --settings has a %s value that is not a list, so the opened-mode check cannot be added to it", ev)
			}
		}
		list, _ := hooks[ev].([]any)
		hooks[ev] = append(list, g)
	}
	doc["hooks"] = hooks
	merged, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	switch {
	case idx < 0:
		return append([]string{"--settings", string(merged)}, vendorArgs...), nil
	case inline:
		out := append([]string{}, vendorArgs...)
		out[idx] = "--settings=" + string(merged)
		return out, nil
	default:
		out := append([]string{}, vendorArgs...)
		out[idx+1] = string(merged)
		return out, nil
	}
}

// cmdPermissionCheck is the hook of the opened-mode check. It reads the hook's
// JSON input, and when Claude Code opened in a different permission mode than
// --expect, stops the session: on the user prompt it exits 2 with the message
// on stderr (which blocks the prompt before any tool runs), and on a tool use
// it denies the tool.
//
//	ynh permission-check --expect <mode> --event <user_prompt|pre_tool>
func cmdPermissionCheck(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) != 4 || args[0] != "--expect" || args[2] != "--event" || (args[3] != openedModeUserPrompt && args[3] != openedModePreTool) {
		return fmt.Errorf("usage: ynh permission-check --expect <mode> --event <user_prompt|pre_tool>")
	}
	var in struct {
		PermissionMode string `json:"permission_mode"`
	}
	if err := json.NewDecoder(stdin).Decode(&in); err != nil {
		return nil // not a hook input: nothing to compare
	}
	mismatch := permission.OpenedMismatch(args[1], in.PermissionMode)
	if mismatch == nil {
		return nil
	}
	if args[3] == openedModePreTool {
		if path := os.Getenv(permission.StopEnv); path != "" {
			_ = os.WriteFile(path, []byte(mismatch.Error()), 0o600)
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": mismatch.Error()}})
	}
	_, _ = fmt.Fprintln(stderr, mismatch)
	if path := os.Getenv(permission.StopEnv); path != "" {
		_ = os.WriteFile(path, []byte(mismatch.Error()), 0o600)
	}
	return &exitCodeError{code: 2, err: mismatch}
}

// stoppedByOpenedModeCheck turns a launch that the opened-mode check stopped
// into an error, so ynh exits nonzero and says why on stderr: Claude Code ends
// such a session with exit 0, and a script would read that as success. A launch
// that already failed keeps its own error.
func stoppedByOpenedModeCheck(stopFile string, launchErr error) error {
	if launchErr != nil || stopFile == "" {
		return launchErr
	}
	msg, err := os.ReadFile(stopFile)
	if err != nil || len(msg) == 0 {
		return nil
	}
	return fmt.Errorf("the session was stopped by the opened-mode check: %s", msg)
}
