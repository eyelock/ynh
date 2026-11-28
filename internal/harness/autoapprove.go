package harness

import "fmt"

// The words of a permission level, widest last. A harness's level is how
// much it may do without a person approving, when it is run unattended: a
// delegate session, a one-shot `ynh run` or `ynh agent run`.
const (
	AutoApproveNone  = "none"
	AutoApproveEdits = "edits"
	AutoApproveAll   = "all"
)

// AutoApproveDefault is the level an accepted harness that declares none
// opens at when run unattended (ynh #1058, D3).
const AutoApproveDefault = AutoApproveAll

// The places a harness's level can come from (AutoApproveOrigin).
const (
	AutoApproveFromHarness = "harness"
	AutoApproveFromEntry   = "entry"
	AutoApproveFromDefault = "default"
)

// AutoApproveOf is the harness's level as it opens when run unattended: its
// declared one, else the default.
func (h *Harness) AutoApproveOf() string {
	if h.AutoApprove != "" {
		return h.AutoApprove
	}
	return AutoApproveDefault
}

// AutoApproveOrigin says where AutoApproveOf got its value: the harness's own
// declaration, or the default.
func (h *Harness) AutoApproveOrigin() string {
	if h.AutoApprove != "" {
		return AutoApproveFromHarness
	}
	return AutoApproveFromDefault
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
	switch v {
	case "", AutoApproveNone, AutoApproveEdits, AutoApproveAll:
		return nil
	}
	return fmt.Errorf("%q is not a level; use %q, %q or %q", v, AutoApproveNone, AutoApproveEdits, AutoApproveAll)
}
