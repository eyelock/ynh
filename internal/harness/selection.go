package harness

import (
	"fmt"
	"strings"
)

// Selection is what --profile and --focus values pick across a harness and
// the harnesses it includes.
//
// A value with no colon belongs to the root harness, exactly as it always
// has. A value of the form "namespace:name" belongs to the included harness
// that answers to that namespace: its own name, or the "as" alias of the
// include that reached it. Colons are not legal in harness names, so a colon
// can never be part of either half.
type Selection struct {
	// Profile is the root's profile, empty for none.
	Profile string
	// Included maps a namespace to the profile applied to that included
	// harness alone.
	Included map[string]string
	// FocusNS and FocusName are a namespaced focus ("github:triage"): the
	// included harness's focus, whose prompt is the run's prompt and whose
	// profile, if any, applies to that harness.
	FocusNS, FocusName string
}

// SplitQualified splits a "namespace:name" value. A value without a colon is
// not qualified and comes back as name alone. More than one colon, or an
// empty half, is an error.
func SplitQualified(value string) (ns, name string, err error) {
	if !strings.Contains(value, ":") {
		return "", value, nil
	}
	ns, name, _ = strings.Cut(value, ":")
	if ns == "" || name == "" || strings.Contains(name, ":") {
		return "", "", fmt.Errorf("invalid selection %q: expected name or namespace:name", value)
	}
	return ns, name, nil
}

// ParseSelection builds a Selection from the values of --profile (repeated)
// and the value of --focus. At most one profile may be unqualified and at
// most one per namespace; a repeat is an error. A qualified focus is parsed
// into FocusNS and FocusName; an unqualified one is the root's, which the
// caller resolves as before, and is left out.
func ParseSelection(profiles []string, focus string) (Selection, error) {
	var sel Selection
	for _, v := range profiles {
		ns, name, err := SplitQualified(v)
		if err != nil {
			return Selection{}, err
		}
		if ns == "" {
			if sel.Profile != "" {
				return Selection{}, fmt.Errorf("--profile given twice for the root harness (%q and %q): at most one unqualified profile", sel.Profile, name)
			}
			sel.Profile = name
			continue
		}
		if prev, dup := sel.Included[ns]; dup {
			return Selection{}, fmt.Errorf("--profile given twice for namespace %q (%q and %q): at most one profile per included harness", ns, prev, name)
		}
		if sel.Included == nil {
			sel.Included = map[string]string{}
		}
		sel.Included[ns] = name
	}
	if focus != "" {
		ns, name, err := SplitQualified(focus)
		if err != nil {
			return Selection{}, err
		}
		if ns != "" {
			sel.FocusNS, sel.FocusName = ns, name
		}
	}
	return sel, nil
}

// HasProfile reports whether any --profile value was given, for the root or
// for an included harness.
func (s Selection) HasProfile() bool {
	return s.Profile != "" || len(s.Included) > 0
}
