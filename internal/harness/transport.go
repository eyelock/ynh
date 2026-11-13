package harness

import (
	"fmt"

	"github.com/eyelock/ynh/internal/vendor"
)

// The transports of a delegates_to entry: how the parent reaches the
// delegate. A2A runs it as its own detached ynh session; native hands it to
// the vendor's own subagents.
const (
	TransportA2A    = "a2a"
	TransportNative = "native"
)

// TransportOf is the transport of the delegates_to entry d: the entry's own
// value, else the harness's default, else a2a.
func (h *Harness) TransportOf(d Delegate) string {
	switch {
	case d.Transport != "":
		return d.Transport
	case h.Transport != "":
		return h.Transport
	}
	return TransportA2A
}

// CheckTransports reports a transport that is not a2a or native, on the
// harness or on a delegates_to entry.
func (h *Harness) CheckTransports() error {
	if err := checkTransportValue(h.Transport); err != nil {
		return fmt.Errorf("transport: %w", err)
	}
	for i, d := range h.DelegatesTo {
		if err := checkTransportValue(d.Transport); err != nil {
			return fmt.Errorf("delegates_to[%d] (%s): transport: %w", i, d.Label(), err)
		}
	}
	return nil
}

func checkTransportValue(v string) error {
	switch v {
	case "", TransportA2A, TransportNative:
		return nil
	}
	return fmt.Errorf("%q is not a transport; use %q or %q", v, TransportA2A, TransportNative)
}

// CheckDelegateTransports refuses a delegates_to entry whose transport is
// native on a vendor with no native subagents, with the fix. There is no
// silent fallback to a2a. It also refuses a transport that is neither a2a nor
// native.
func (h *Harness) CheckDelegateTransports(vendorName string) error {
	if err := h.CheckTransports(); err != nil {
		return fmt.Errorf("%s: %w", h.Name, err)
	}
	if vendorName == "" {
		return nil
	}
	// A vendor ynh does not know is left to the command that launches it,
	// which refuses it by name.
	a, err := vendor.Get(vendorName)
	if err != nil || a.SupportsNativeSubagents() {
		return nil
	}
	for i, d := range h.DelegatesTo {
		if h.TransportOf(d) == TransportNative {
			return fmt.Errorf("%s: delegates_to[%d] (%s) has transport native, and %s has no native subagents\n  Fix: set transport: a2a on this delegate, or run on a vendor with subagents",
				h.Name, i, d.Label(), vendorName)
		}
	}
	return nil
}
