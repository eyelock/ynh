package harness

import (
	"fmt"

	"github.com/eyelock/ynh/internal/permission"
)

// AutoApproveDefault is the level an accepted harness that declares none
// opens at when run unattended (ynh #1058, D3).
const AutoApproveDefault = permission.All

// AutoApproveOf is the harness's level as it opens when run unattended: its
// declared one, else the default.
func (h *Harness) AutoApproveOf() permission.Level {
	if h.AutoApprove != "" {
		return permission.Level(h.AutoApprove)
	}
	return AutoApproveDefault
}

// AutoApproveOrigin says where AutoApproveOf got its value: the harness's own
// declaration, or the default.
func (h *Harness) AutoApproveOrigin() permission.Source {
	if h.AutoApprove != "" {
		return permission.SourceHarness
	}
	return permission.SourceDefault
}

// CheckAutoApprove reports a level that is not none, edits or all, on the
// harness or on a delegates_to entry, and a level on a native entry, which
// has no session of its own to apply it to.
func (h *Harness) CheckAutoApprove() error {
	if err := checkAutoApproveValue(h.AutoApprove); err != nil {
		return fmt.Errorf("auto_approve: %w", err)
	}
	for i, d := range h.DelegatesTo {
		if err := checkAutoApproveValue(d.AutoApprove); err != nil {
			return fmt.Errorf("delegates_to[%d] (%s): auto_approve: %w", i, d.Label(), err)
		}
	}
	return h.CheckNativeAutoApprove()
}

// CheckNativeAutoApprove refuses auto_approve on a native delegates_to entry.
// A level is applied exactly or refused (D4): a native delegate is a subagent
// in the parent's own session, with no launch of its own, so it runs at the
// parent's level and an entry's level cannot be applied. Every loader runs
// this, so every path that loads a harness to run, accept, check, update,
// show or bake it refuses the same way, with the same message.
func (h *Harness) CheckNativeAutoApprove() error {
	for i, d := range h.DelegatesTo {
		if d.AutoApprove != "" && h.TransportOf(d) == TransportNative {
			return fmt.Errorf("%s: delegates_to[%d] (%s): auto_approve is not accepted on a native delegate: it runs in the parent's session at the parent's level, so the level cannot be applied\n"+
				"  Fix: narrow the parent with its own auto_approve, or set transport: a2a on this entry", h.Name, i, d.Label())
		}
	}
	return nil
}

func checkAutoApproveValue(v string) error {
	if v == "" {
		return nil
	}
	_, err := permission.Parse(v)
	return err
}
