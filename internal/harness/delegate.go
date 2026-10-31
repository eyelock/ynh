package harness

import (
	"fmt"
	"maps"
	"slices"
)

// DelegateFocus is a focus a delegates_to entry names: its prompt becomes
// standing instructions in the delegate's agent, under its name. The calling
// session still gives the delegate its task.
type DelegateFocus struct {
	Name    string
	Profile string // the profile the focus binds, "" for none
	Prompt  string
}

// CheckSelection reports a delegates_to entry that names both a profile and
// a focus. A focus already carries its profile, so the two cannot both be
// given.
func (d Delegate) CheckSelection() error {
	if d.Profile != "" && d.Focus != "" {
		return fmt.Errorf("delegate %s names both profile %q and focus %q; a focus already carries its profile, so name one", d.Label(), d.Profile, d.Focus)
	}
	return nil
}

// Label names the delegate's source for messages: its git source, with its
// path when it has one.
func (d Delegate) Label() string {
	if d.Path != "" {
		return d.Git + "//" + d.Path
	}
	return d.Git
}

// Select applies the entry's selection to the delegate harness h loaded from
// it. With a profile, h comes back with that profile resolved
// (ResolveProfile). With a focus, the focus's profile, if it has one, is
// resolved the same way, and the focus is returned for the agent's standing
// instructions. With neither, h is returned as it is. A name the delegate
// harness does not declare is an error naming the delegate. launch_focus is
// never consulted: a delegate runs a focus only when its entry names one.
func (d Delegate) Select(h *Harness) (*Harness, *DelegateFocus, error) {
	if err := d.CheckSelection(); err != nil {
		return nil, nil, err
	}
	switch {
	case d.Profile != "":
		resolved, err := ResolveProfile(h, d.Profile)
		if err != nil {
			return nil, nil, fmt.Errorf("delegate %s: %w", h.Name, err)
		}
		return resolved, nil, nil
	case d.Focus != "":
		focus, ok := h.Focuses[d.Focus]
		if !ok {
			if len(h.Focuses) == 0 {
				return nil, nil, fmt.Errorf("delegate %s: focus %q not defined in its manifest (it declares no focuses)", h.Name, d.Focus)
			}
			return nil, nil, fmt.Errorf("delegate %s: focus %q not defined in its manifest (available: %v)", h.Name, d.Focus, slices.Sorted(maps.Keys(h.Focuses)))
		}
		resolved, err := ResolveProfile(h, focus.Profile)
		if err != nil {
			return nil, nil, fmt.Errorf("delegate %s: focus %q: %w", h.Name, d.Focus, err)
		}
		return resolved, &DelegateFocus{Name: d.Focus, Profile: focus.Profile, Prompt: focus.Prompt}, nil
	}
	return h, nil, nil
}
