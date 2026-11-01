package harness

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// DelegateFocus is a focus a delegates_to entry names: its prompt becomes
// standing instructions in the delegate's agent, under its name. The calling
// session still gives the delegate its task.
type DelegateFocus struct {
	Name   string
	Prompt string
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
		return resolved, &DelegateFocus{Name: d.Focus, Prompt: focus.Prompt}, nil
	}
	return h, nil, nil
}

// CheckDeclared reports whether the profile or focus the entry names is one
// the delegate harness dh declares, and, for a focus, that the profile the
// focus names is declared too. It is the check ynd validate and ynh delegate
// add share; the message reads as a continuation of the entry's label. An
// entry naming neither passes.
func (d Delegate) CheckDeclared(dh *Harness) error {
	if d.Profile != "" {
		if _, ok := dh.Profiles[d.Profile]; !ok {
			return fmt.Errorf("references profile %q, which delegate %s does not define (%s)", d.Profile, dh.Name, declaredNames(dh.Profiles, "profiles"))
		}
		return nil
	}
	if d.Focus == "" {
		return nil
	}
	focus, ok := dh.Focuses[d.Focus]
	if !ok {
		return fmt.Errorf("references focus %q, which delegate %s does not define (%s)", d.Focus, dh.Name, declaredNames(dh.Focuses, "focuses"))
	}
	if focus.Profile != "" {
		if _, ok := dh.Profiles[focus.Profile]; !ok {
			return fmt.Errorf("focus %q of delegate %s names profile %q, which it does not define", d.Focus, dh.Name, focus.Profile)
		}
	}
	return nil
}

// declaredNames lists a map's keys for a message: "it declares: a, b", or
// "it declares no <what>".
func declaredNames[V any](m map[string]V, what string) string {
	if len(m) == 0 {
		return "it declares no " + what
	}
	return "it declares: " + strings.Join(slices.Sorted(maps.Keys(m)), ", ")
}
