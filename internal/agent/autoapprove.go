package agent

import (
	"errors"
	"fmt"
	"os"

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

// permissionArgs returns the vendor arguments for level, or nil for no level
// or a level the project's mode already governs (the vendor is passed nothing
// and the project's own setting applies). Arguments already present in args
// (OpenCode's --standalone) are not repeated.
func permissionArgs(backend string, opts StartOptions, args []string) []string {
	if opts.AutoApprove == "" || opts.PermissionFromProject {
		return nil
	}
	m, err := permission.For(backend, permission.Level(opts.AutoApprove))
	if err != nil {
		return nil
	}
	var out []string
	for _, a := range m.Args {
		if a == "--standalone" && containsArg(args, a) {
			continue
		}
		out = append(out, a)
	}
	return out
}

// permissionEnv returns the vendor environment for level, as permissionArgs.
func permissionEnv(backend string, opts StartOptions) []string {
	if opts.AutoApprove == "" || opts.PermissionFromProject {
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

// claudePermissionMode is the --permission-mode a level maps to, or "" for
// none given or a level the project's mode governs.
func claudePermissionMode(opts StartOptions) string {
	if opts.AutoApprove == "" || opts.PermissionFromProject {
		return ""
	}
	return permission.ClaudeMode(permission.Level(opts.AutoApprove))
}

// resolveProjectPermissions weighs the level against the project's own mode
// (D5). A narrower project mode wins: the level becomes the project's, the
// vendor is passed nothing, and the line is printed. It returns the level to
// run at and whether the project governs it.
func resolveProjectPermissions(level, backend, dir string, stderr interface{ Write([]byte) (int, error) }) (string, bool, error) {
	if level == "" {
		return "", false, nil
	}
	res, err := permission.Resolve(dir, backend, permission.Level(level))
	if err != nil {
		return "", false, fmt.Errorf("--auto-approve: %w", err)
	}
	if res.FromProject() && stderr != nil {
		_, _ = fmt.Fprintln(stderr, res.Line)
	}
	return string(res.Level), res.FromProject(), nil
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
