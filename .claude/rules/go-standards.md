Use Go 1.25+ idioms. Prefer standard library over external dependencies. Zero external deps is a deliberate design choice.

Return errors, don't panic. Wrap with context: `fmt.Errorf("doing thing: %w", err)`. Handle errors once — don't log AND return.

Tests use the standard `testing` package — no frameworks. Use `t.TempDir()` for filesystem isolation and `t.Setenv()` to avoid leaking state. Table-driven tests with `t.Run()`.

Accept interfaces, return structs. Define interfaces where consumed, not where implemented. Don't create interfaces speculatively — wait for a second implementation or testing need.

No `utils`/`helpers`/`common` packages. No premature abstraction — three similar lines beat one speculative helper. No dead code — delete unused code completely.

Full coding standards: `.claude/skills/ynh-dev/references/coding-standards.md`
