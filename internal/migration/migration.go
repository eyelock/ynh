// Package migration provides a filter chain for format migrations.
//
// Each migration is a single struct implementing Migrator. Loaders call
// FormatChain().Run(dir) before reading and never branch on old formats;
// `ynd migrate` runs MigrateChain(), the only chain that rewrites a tree
// ynh did not install.
//
// Removing support for a legacy format means deleting the migrator file and
// unregistering the struct from the chains. No other code changes.
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

// DefaultChain returns the full migration chain including storage relocation.
//
// Order matters: HarnessFormatMigrator must run before HarnessStorageMigrator
// so that .agents/harness/installed.json exists when namespace inference runs.
// ManifestDirMigrator runs first so every later step sees the canonical
// manifest directory.
func DefaultChain() Chain {
	return Chain{
		ManifestDirMigrator{},
		HarnessFormatMigrator{},
		RegistryFormatMigrator{},
		HarnessStorageMigrator{},
	}
}

// FormatChain returns the format-only migration chain (no storage relocation),
// which every command runs before it loads a harness or registry.
//
// It rewrites only installs under config.HarnessesDir(), ynh's own copies.
// Nothing outside them is ever written (#406): a .ynh-plugin/ tree is read
// through the fallback with a deprecation warning, and a legacy .harness.json
// or registry.json makes Run return an error naming `ynd migrate`, because
// ynh no longer reads either format. See MigrateChain.
func FormatChain() Chain {
	return Chain{
		ManifestDirMigrator{},
		HarnessFormatMigrator{},
		RegistryFormatMigrator{},
	}
}

// MigrateChain is FormatChain for `ynd migrate`, the one caller acting on an
// explicit request from a tree's owner, behind its confirmation. It is the
// only chain that rewrites source trees: it renames .ynh-plugin/ to
// .agents/harness/ and converts .harness.json and registry.json.
func MigrateChain() Chain {
	return Chain{
		ManifestDirMigrator{SourceTrees: true},
		HarnessFormatMigrator{SourceTrees: true},
		RegistryFormatMigrator{SourceTrees: true},
	}
}
