# ynh Coding Standards

## Package Design

- **Single responsibility per package** — name with one noun: `resolver`, `assembler`, `config`. If you need "and" to describe it, split it.
- **Dependency direction** — leaf packages (`config`, `plugin`) have zero internal deps. Core packages (`harness`, `resolver`) build on leaves. Orchestrators (`assembler`, `exporter`) coordinate cores. Never import upward.
- **No circular dependencies** — if two packages need each other, extract shared types into a third or use an interface to invert the direction.
- **No `utils`/`helpers`/`common` packages** — move the function to the package that uses it, or create a well-named package.
- **Default to `internal/`** — only expose packages when there is a proven external consumer.
- **Flat over nested** — don't create sub-packages until a directory has 15+ files.

## Interface Design

- **Accept interfaces, return structs** — function parameters take interfaces for flexibility and testability; return concrete types for discoverability and zero-value usefulness.
- **Small interfaces** — the Go stdlib averages 2 methods per interface. If yours has 5+ methods, question whether it should be split into composable pieces.
- **Define interfaces where consumed, not where implemented** — the consumer owns the contract. This is Go's expression of Dependency Inversion.
- **Don't create interfaces speculatively** — wait for a second implementation or a genuine testing need. Go's implicit interface satisfaction means you can extract an interface later without changing implementors.
- **No stutter** — `vendor.Adapter` not `vendor.VendorAdapter`; `config.Manager` not `config.ConfigManager`.

## Error Handling

- **Return errors, never panic** — `panic` is for unrecoverable programmer errors only (e.g., invalid regex in `init()`).
- **Wrap with context** — `fmt.Errorf("resolving include %s: %w", url, err)`. Use `%w` when callers may need to inspect the cause; `%v` to hide implementation details.
- **Lowercase, no punctuation** — error messages compose via wrapping: `"resolving include: cloning repo: permission denied"`.
- **Handle errors once** — don't log AND return. CLI `main()` handles all display. Internal packages just wrap and return.
- **Sentinel errors for known conditions** — `var ErrNotFound = errors.New("harness not found")`. Always prefix with `Err`.
- **Custom error types for structured data** — when callers need `errors.As()` to extract fields like file path or line number.
- **Use `errors.Is()` and `errors.As()`** — never `==` comparison or type assertions on potentially wrapped errors.

## Dependency Injection

- **Constructor injection for required deps** — explicit params in `NewFoo()` constructors. Compilation fails if you forget one.
- **Options structs for complex configuration** — the `ExportOptions` pattern: extensible without breaking the API, optional fields for optional features.
- **Function variables for test seams** — the `queryLLMFunc` pattern for single-function dependencies. Swap in tests, production default at package level.
- **No DI frameworks** — manual wiring in `main()` is explicit, debuggable, and has zero cost.
- **Functional options only when justified** — `WithTimeout()`, `WithLogger()` style is appropriate for public APIs with many optional params. Overkill for internal packages.

## Open/Closed Principle & Extensibility

- **Strategy + Registry for variation points** — vendor adapters are the model: define an interface, implement per variant, self-register via `init()`. Adding a vendor = one new file, zero changes to existing code.
- **No `switch` on type names in core code** — if you find yourself switching on vendor name in the assembler or exporter, push that behaviour into the adapter interface instead.
- **Don't create extension points speculatively** — if there's one implementation and no testing need, use a concrete type. You can always extract an interface later.

## Testing Standards

- **Standard `testing` package only** — no test frameworks.
- **Table-driven tests** — `[]struct{ name string; input string; want string }` with `t.Run(tt.name, ...)`.
- **`t.Helper()` in all test helpers** — so failure messages point to the caller, not the helper.
- **`t.TempDir()` for filesystem isolation** — auto-cleaned on test completion, no manual cleanup.
- **`t.Setenv()` for environment isolation** — auto-restored on test completion.
- **Test names describe the scenario** — `TestAssembler_PickSingleSkill`, `TestResolver_NonexistentPathErrors`, not `TestAssembler1`.
- **Mock adapters via interfaces** — `mockAdapter{}` implements the interface with minimal stubs. Only implement the methods the test exercises.
- **Function variable swapping for external deps** — assign `queryLLMFunc = func(...) { ... }` in test setup for deterministic behaviour.
- **Fixtures in `testdata/`** — Go toolchain ignores this directory during builds. Reference via relative paths.
- **Coverage target: 90%+** on testable code. `errcheck` is strict — all returned errors must be checked, including in tests.

## CLI Patterns

- **Subcommand routing via flag sets** — `flag.NewFlagSet(name, flag.ExitOnError)` per subcommand. Avoids global flag pollution.
- **Output to stdout, errors to stderr** — be well-behaved in pipelines. Normal output goes to `os.Stdout`, diagnostics and errors to `os.Stderr`.
- **Exit codes** — `0` success, `1` error, `2` usage error.
- **Flag definitions local to subcommand** — define flags in the function that handles the subcommand, not at package level.
- **Testable command functions** — accept `io.Writer` for output and return `error` or exit code so tests can capture output without touching stdout.

## Code Style

- **Go 1.25+ idioms** — use modern stdlib features. No compatibility shims for older Go versions.
- **Zero external dependencies** — standard library only. This is a deliberate design choice, not an accident.
- **`goimports` + `gofmt`** — enforced via `make format`. No manual formatting.
- **`errcheck` strict** — every returned error must be checked. No `_ = f.Close()` shortcuts.
- **No dead code** — if it's unused, delete it completely. No `_var` renames, `// removed` comments, or backward-compatibility re-exports.
- **No premature abstraction** — three similar lines of code is better than one speculative helper. Extract when there's a proven third use.
- **No speculative features** — build for what's needed now, not hypothetical futures. No feature flags, no "just in case" configurability.
- **Naming** — follows Go conventions: `MixedCaps` for exported, `mixedCaps` for unexported. Acronyms are all-caps (`URL`, `HTTP`). Short variable names in tight scopes (`r` for reader), descriptive names in wider scopes.

## File Organisation

- **One file per major concern in `cmd/`** — `install_resolve.go`, `image.go`, `signals.go`. Don't pile everything into `main.go`.
- **Keep files under 500 LOC** where practical — smaller files are easier to navigate and review. `main.go` at ~1150 LOC is the acknowledged exception (cohesive CLI routing).
- **Test files alongside source** — `foo_test.go` next to `foo.go`, same package.
- **Shared test helpers at package level** — `runGit()`, `mockAdapter{}` live in the test files of the package that uses them. Don't create a separate `testhelper` package.
- **`cmd/` packages are independent** — `cmd/ynh/` and `cmd/ynd/` share `internal/` packages but never import each other.
