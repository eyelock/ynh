package delegation

import (
	"fmt"
	"strings"
)

// The model and the reasoning effort a delegate opens with are chosen by the
// parent, from four places. Highest first:
//
//  1. the parent's per-call choice, on the delegate_to tool;
//  2. the parent's per-entry default, on its delegates_to entry;
//  3. the delegate harness's own default (default_model, default_effort);
//  4. the vendor's default, which is nothing passed to the vendor.
//
// Model and effort follow the same order, each on its own: a call that names
// only an effort leaves the model to the entry, the delegate or the vendor.

// Where a resolved model or effort came from (Setting.From).
const (
	FromCall            = "call"
	FromEntry           = "entry"
	FromDelegateDefault = "delegate_default"
	FromVendorDefault   = "vendor_default"
	// FromLiveSession is for a task queued at a live interactive session: it
	// runs with that session's own model and effort, which ynh does not know,
	// so the record names the source and claims no value.
	FromLiveSession = "live_session"
)

// Froms is every word a model_from or effort_from can carry, in the order of
// the layers, so the schemas built from it cannot drift from the code.
var Froms = []string{FromCall, FromEntry, FromDelegateDefault, FromVendorDefault, FromLiveSession}

// The neutral effort levels, the same as `ynh agent run --effort`.
const (
	EffortLow    = "low"
	EffortMedium = "medium"
	EffortHigh   = "high"
)

// Setting is one resolved value and where it came from. From is
// FromVendorDefault, and Value empty, when no layer named one.
type Setting struct {
	Value string
	From  string
}

// Resolve picks the highest layer that names a value.
func Resolve(call, entry, delegateDefault string) Setting {
	switch {
	case call != "":
		return Setting{call, FromCall}
	case entry != "":
		return Setting{entry, FromEntry}
	case delegateDefault != "":
		return Setting{delegateDefault, FromDelegateDefault}
	}
	return Setting{"", FromVendorDefault}
}

// maxModelLen bounds a model name.
const maxModelLen = 200

// CheckModel refuses a model name that could not be one: empty is allowed
// (none named), a name that starts with "-" or holds white space or a control
// character is not, so a model can never be read by the vendor as a flag.
func CheckModel(v string) error {
	if v == "" {
		return nil
	}
	if len(v) > maxModelLen {
		return fmt.Errorf("model is longer than %d characters", maxModelLen)
	}
	if strings.HasPrefix(v, "-") {
		return fmt.Errorf("model %q starts with \"-\"", v)
	}
	for _, r := range v {
		if r <= ' ' || r == 0x7f {
			return fmt.Errorf("model %q holds white space or a control character", v)
		}
	}
	return nil
}

// CheckEffort refuses an effort that is not low, medium or high. Empty is
// allowed (none named).
func CheckEffort(v string) error {
	switch v {
	case "", EffortLow, EffortMedium, EffortHigh:
		return nil
	}
	return fmt.Errorf("effort %q is not one of low, medium or high", v)
}

// VendorEffortArgs is the argument list that gives the vendor CLI the effort,
// or an error when the vendor has no setting for it: an effort is never
// silently dropped. Empty effort gives no arguments.
func VendorEffortArgs(vendor, effort string) ([]string, error) {
	if effort == "" {
		return nil, nil
	}
	switch vendor {
	case "claude":
		return []string{"--effort", effort}, nil
	case "codex":
		return []string{"-c", `model_reasoning_effort="` + effort + `"`}, nil
	}
	return nil, fmt.Errorf("effort %q cannot be honoured: %s has no reasoning effort setting (claude and codex do)", effort, vendor)
}
