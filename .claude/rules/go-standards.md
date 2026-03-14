Use Go 1.25+ idioms. Prefer standard library over external dependencies.

Return errors, don't panic. Wrap with context: `fmt.Errorf("doing thing: %w", err)`.

Tests use the standard `testing` package - no frameworks. Use `t.TempDir()` for filesystem isolation and `t.Setenv()` to avoid leaking state.
