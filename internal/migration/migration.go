// Package migration provides a filter chain for transparent format migrations.
//
// Each migration is a single struct implementing Migrator. Loaders call
// DefaultChain().Run(dir) before reading — they never branch on old formats.
// Removing support for a legacy format means deleting the migrator file and
// unregistering the struct from DefaultChain. No other code changes.
package migration

// Migrator is a single format migration step.
type Migrator interface {
	// Applies reports whether this migration should run on dir.
	Applies(dir string) bool
	// Run performs the in-place migration on dir.
	Run(dir string) error
	// Description returns a short user-facing label for what was migrated.
	Description() string
}

// Chain is an ordered list of migrators.
type Chain []Migrator

// Run applies each migrator whose Applies returns true, in order.
// Returns descriptions of the migrations that were applied.
func (c Chain) Run(dir string) ([]string, error) {
	var applied []string
	for _, m := range c {
		if m.Applies(dir) {
			if err := m.Run(dir); err != nil {
				return applied, err
			}
			applied = append(applied, m.Description())
		}
	}
	return applied, nil
}

// DefaultChain returns the standard migration chain in dependency order.
//
// Order matters: HarnessFormatMigrator must run before HarnessStorageMigrator
// so that .ynh-plugin/installed.json exists when namespace inference runs.
func DefaultChain() Chain {
	return Chain{
		HarnessFormatMigrator{},
		RegistryFormatMigrator{},
		HarnessStorageMigrator{},
	}
}
