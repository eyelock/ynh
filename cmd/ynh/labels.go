package main

import (
	"fmt"
	"os"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/labels"
)

// resolveRunLabels is the run's labels: config.json, then the root harness's
// manifest, then YNH_LABELS, then --label, later winning per key. The set is
// validated here, so a run whose labels break ynr's rule 10 is refused
// before it starts (docs/telemetry.md, "Run labels").
//
// root is the harness being run, as installed; the labels of harnesses it
// includes are never read. A nil root contributes none.
//
// The merged set is then exported as YNH_LABELS to this process's
// environment, so every process ynh starts inherits it: the vendor CLI, and
// the `ynh check` children. An empty set removes the variable, so a child
// never sees a label the run dropped.
func resolveRunLabels(flags []string, root *harness.Harness) (labels.Set, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	src := labels.Sources{
		Config: cfg.Labels,
		Env:    os.Getenv(labels.Env),
		Flags:  flags,
	}
	if root != nil {
		src.Manifest = root.Labels
	}
	set, err := labels.Resolve(src, labels.Home())
	if err != nil {
		return nil, err
	}
	if len(set) == 0 {
		_ = os.Unsetenv(labels.Env)
	} else {
		_ = os.Setenv(labels.Env, labels.Encode(set))
	}
	return set, nil
}
