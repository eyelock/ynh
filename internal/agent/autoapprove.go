package agent

import (
	"errors"
	"fmt"
	"os"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/permission"
)

// Auto-approve levels for `ynh agent run --auto-approve`. The words, their
// order, the per-vendor arguments, the project-mode comparison and the root
// check live in internal/permission, shared with ynh run and delegates
// (ynh #1058, D7); this file is agent run's thin use of it.
//
// By default ynh passes no permission flag: the worker gets whatever the
// vendor CLI and the project grant it. --auto-approve may be given to run
// inside containment the operator owns, and may be none, edits or all. It is a
// run-time grant only: a resume does not restore it.
const (
	AutoApproveNone  = string(permission.None)
	AutoApproveEdits = string(permission.Edits)
	AutoApproveAll   = string(permission.All)
)

// validateAutoApprove rejects a level the chosen backend cannot hold exactly.
// A level with no honest equivalent is an error, never a silent widening.
func validateAutoApprove(level, backend string) error {
	if level == "" {
		return nil
	}
	l, err := permission.Parse(level)
	if err != nil {
		return fmt.Errorf("--auto-approve: %w", err)
	}
	if backend == permission.Copilot {
		return fmt.Errorf("--auto-approve is not supported by the %s backend (supported: claude, codex, cursor, opencode)", backend)
	}
	if _, err := permission.For(backend, l); err != nil {
		var refused *permission.RefusedError
		if asRefused(err, &refused) {
			return fmt.Errorf("--auto-approve %s: %w", level, err)
		}
		return fmt.Errorf("--auto-approve is not supported by the %s backend (supported: claude, codex, cursor, opencode)", backend)
	}
	return nil
}

// permissionArgs returns the vendor arguments for the run's level, or the
// project's own mode when it narrowed the level (D5): either way the vendor is
// passed an explicit mode, so a user-level setting cannot widen the run. It is
// nil when no level was asked for. Arguments already present in args
// (OpenCode's --standalone) are not repeated.
func permissionArgs(backend string, opts StartOptions, args []string) []string {
	if opts.AutoApprove == "" {
		return nil
	}
	margs := opts.ProjectArgs
	if margs == nil {
		m, err := permission.For(backend, permission.Level(opts.AutoApprove))
		if err != nil {
			return nil
		}
		margs = m.Args
	}
	var out []string
	for _, a := range margs {
		if a == "--standalone" && containsArg(args, a) {
			continue
		}
		out = append(out, a)
	}
	return out
}

// permissionEnv returns the vendor environment for the run's level. A
// project-narrowed run passes arguments only.
func permissionEnv(backend string, opts StartOptions) []string {
	if opts.AutoApprove == "" || opts.ProjectArgs != nil {
		return nil
	}
	m, err := permission.For(backend, permission.Level(opts.AutoApprove))
	if err != nil {
		return nil
	}
	return m.Env
}

func containsArg(args []string, a string) bool {
	for _, x := range args {
		if x == a {
			return true
		}
	}
	return false
}

// claudePermissionMode is the --permission-mode the session is expected to
// open in, which the opened-mode check compares against: the project's when it
// narrowed the level, else the level's. "" when no level was asked for.
func claudePermissionMode(opts StartOptions) string {
	if opts.AutoApprove == "" {
		return ""
	}
	if opts.ProjectMode != "" {
		return opts.ProjectMode
	}
	return permission.ClaudeMode(permission.Level(opts.AutoApprove))
}

// resolveProjectPermissions weighs the level against the project's own mode
// (D5). A narrower project mode wins: the level becomes the project's, the
// vendor is passed that mode explicitly, and the line is printed.
func resolveProjectPermissions(level, backend, dir string, stderr interface{ Write([]byte) (int, error) }) (permission.Resolution, error) {
	if level == "" {
		return permission.Resolution{}, nil
	}
	res, err := permission.Resolve(dir, backend, permission.Level(level))
	if err != nil {
		return res, fmt.Errorf("--auto-approve: %w", err)
	}
	if res.FromProject() && stderr != nil {
		_, _ = fmt.Fprintln(stderr, res.Line)
	}
	return res, nil
}

// checkRoot is the root refusal for Claude Code, before it starts.
func checkRoot(level, backend string) error {
	if level == "" {
		return nil
	}
	return permission.CheckRoot(backend, permission.Level(level), os.Geteuid(), os.Getenv)
}

func asRefused(err error, target **permission.RefusedError) bool {
	return errors.As(err, target)
}

// settleAutoApprove weighs opts.AutoApprove against the project's own
// permission mode (D5) and checks the root refusal (N4), once the level is
// known.
func settleAutoApprove(opts *RunOptions) error {
	res, err := resolveProjectPermissions(opts.AutoApprove, opts.Backend, opts.WorktreeDir, opts.Stderr)
	if err != nil {
		return err
	}
	opts.AutoApprove = string(res.Level)
	opts.ProjectArgs, opts.ProjectMode = nil, ""
	if res.FromProject() {
		opts.ProjectArgs, opts.ProjectMode = res.Project.Args, res.Project.Opened
	}
	// Every delegate the worker starts opens no wider than this run does (D6).
	if opts.AutoApprove != "" && opts.HarnessName != "" {
		if err := os.Setenv(permission.CapEnv, opts.AutoApprove); err != nil {
			return err
		}
	}
	if err := checkRoot(opts.AutoApprove, opts.Backend); err != nil {
		return &ExitError{Code: ExitRefused, Message: err.Error()}
	}
	return nil
}

// agentHarnessStep is the level a worker of an accepted harness opens at: the
// harness's own, narrowed by --auto-approve (which may only narrow) and by the
// calling session's cap.
func agentHarnessStep(h *harness.Harness, flag string) (permission.Step, error) {
	from := permission.SourceHarness
	if h.AutoApprove == "" {
		from = permission.SourceDefault
	}
	step := permission.Step{Level: h.AutoApproveOf(), From: from}
	step, err := step.Ask(permission.Level(flag), permission.SourceFlag)
	if err != nil {
		return step, fmt.Errorf("--auto-approve: %w", err)
	}
	limit, err := permission.CapFrom(os.Getenv)
	if err != nil {
		return step, err
	}
	return step.Narrowed(permission.Step{Level: limit, From: permission.SourceCallerCap}), nil
}
