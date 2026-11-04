package main

import (
	"fmt"
	"os"

	"github.com/eyelock/ynh/internal/harness"
)

// resolveSelection turns the --profile values, repeated one per harness, and
// the --focus value into a harness.Selection, for the commands that assemble a
// harness. YNH_PROFILE is the single-value fallback when no flag was given.
//
// A focus of the root is resolved here, since its profile is the root's
// profile and is applied before includes resolve; a focus of an included
// harness is left in the selection for the resolver, which alone can find the
// harness. --focus and --profile exclude each other, as a focus carries its
// own profile.
func resolveSelection(srcDir string, profiles []string, focusName string) (harness.Selection, error) {
	together := fmt.Errorf("cannot use --focus and --profile together (focus includes a profile)")
	if focusName != "" && len(profiles) > 0 {
		return harness.Selection{}, together
	}
	if len(profiles) == 0 {
		if env := os.Getenv("YNH_PROFILE"); env != "" {
			profiles = []string{env}
		}
	}
	if focusName != "" && len(profiles) > 0 {
		return harness.Selection{}, together
	}

	sel, err := harness.ParseSelection(profiles, focusName)
	if err != nil {
		return harness.Selection{}, err
	}
	if focusName == "" || sel.FocusNS != "" {
		return sel, nil
	}

	// A focus of the root: load the harness to look up the focus entry.
	h, _, loadErr := loadHarnessForPreview(srcDir)
	if loadErr != nil {
		return harness.Selection{}, fmt.Errorf("loading harness for focus resolution: %w", loadErr)
	}
	focus, ok := h.Focuses[focusName]
	if !ok {
		return harness.Selection{}, fmt.Errorf("focus %q not defined in harness", focusName)
	}
	sel.Profile = focus.Profile
	return sel, nil
}
