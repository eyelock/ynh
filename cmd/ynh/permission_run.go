package main

import (
	"encoding/json"
	"fmt"
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
	From  string
	// Args and Env are what the vendor is passed; empty when the project's own
	// mode governs.
	Args []string
	Env  []string
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
	base := permission.Step{Level: permission.Level(p.AutoApproveOf()), From: permission.FromDefault}
	if p.AutoApprove != "" {
		base.From = permission.FromHarness
		if isDelegate {
			base.From = permission.FromDelegateDefault
		}
	}
	reqFrom := permission.FromFlag
	if s := getenv(permission.SourceEnv); isDelegate && s != "" {
		reqFrom = s
	}
	cur, err := base.Ask(permission.Level(flag), reqFrom)
	if err != nil {
		return nil, fmt.Errorf("--auto-approve: %w", err)
	}
	limit, err := permission.CapFrom(getenv)
	if err != nil {
		return nil, err
	}
	cur = cur.Narrowed(permission.Step{Level: limit, From: permission.FromCallerCap})

	res, err := permission.Resolve(dir, vendorName, cur.Level)
	if err != nil {
		return nil, err
	}
	out := &runPermission{Level: res.Level, From: cur.From}
	if res.FromProject() {
		out.From = permission.FromProject
		out.Lines = append(out.Lines, res.Line)
	}
	if err := permission.CheckRoot(vendorName, res.Level, euid, getenv); err != nil {
		return nil, err
	}
	if !res.FromProject() {
		m, err := permission.For(vendorName, res.Level)
		if err != nil {
			return nil, fmt.Errorf("--auto-approve %s (%s): %w", res.Level, cur.From, err)
		}
		out.Args, out.Env = m.Args, m.Env
	}
	out.Lines = append([]string{fmt.Sprintf("auto-approve: %s (%s)", out.Level, out.From)}, out.Lines...)
	return out, nil
}

// applyEnv sets the vendor environment of the permission and the cap every
// session this run starts inherits (D6). OpenCode's permission block is merged
// into any OPENCODE_CONFIG_CONTENT already set.
func (rp *runPermission) applyEnv() error {
	for _, kv := range rp.Env {
		k, v, _ := strings.Cut(kv, "=")
		if k == "OPENCODE_CONFIG_CONTENT" {
			merged, err := mergeJSONObject(os.Getenv(k), v)
			if err != nil {
				return fmt.Errorf("your %s is not a JSON object: %w", k, err)
			}
			v = merged
		}
		if err := os.Setenv(k, v); err != nil {
			return err
		}
	}
	return os.Setenv(permission.CapEnv, string(rp.Level))
}

// mergeJSONObject returns existing with the top-level keys of add set over it.
func mergeJSONObject(existing, add string) (string, error) {
	obj := map[string]json.RawMessage{}
	if existing != "" {
		if err := json.Unmarshal([]byte(existing), &obj); err != nil {
			return "", err
		}
	}
	var over map[string]json.RawMessage
	if err := json.Unmarshal([]byte(add), &over); err != nil {
		return "", err
	}
	for k, v := range over {
		obj[k] = v
	}
	b, err := json.Marshal(obj)
	return string(b), err
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
	base := permission.Step{Level: permission.Level(delegate.AutoApproveOf()), From: permission.FromDelegateDefault}
	if delegate.AutoApprove == "" {
		base.From = permission.FromDefault
	}
	cur, err := base.Ask(permission.Level(entry.AutoApprove), permission.FromEntry)
	if err != nil {
		return cur, fmt.Errorf("delegates_to %s: auto_approve: %w", entry.Label(), err)
	}
	limit, err := permission.CapFrom(getenv)
	if err != nil {
		return cur, err
	}
	return cur.Narrowed(permission.Step{Level: limit, From: permission.FromCallerCap}), nil
}
