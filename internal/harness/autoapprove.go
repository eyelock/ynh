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
// harness or on a delegates_to entry.
func (h *Harness) CheckAutoApprove() error {
	if err := checkAutoApproveValue(h.AutoApprove); err != nil {
		return fmt.Errorf("auto_approve: %w", err)
	}
	for i, d := range h.DelegatesTo {
		if err := checkAutoApproveValue(d.AutoApprove); err != nil {
			return fmt.Errorf("delegates_to[%d] (%s): auto_approve: %w", i, d.Label(), err)
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
