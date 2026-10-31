package telemetry

//go:generate go run ./gen

// The names ynh emits are generated from its Weaver registry,
// telemetry/registry (ynr ADR-007), into names_gen.go: one constant per
// attribute, span, event and enum value. Code uses those and never a string
// literal, so a name the registry does not declare does not compile.
// `ynh telemetry registry --format json` prints the registry.

// SemconvVersion is the semantic-conventions version ynh follows for the
// standard names, the version the registry's manifest pins. It is 1.41.0, not
// the SDK's newer default, because 1.41.0 is the last version whose Go
// package carries the gen_ai.* names, which are still in development upstream.
const SemconvVersion = "1.41.0"

// ScopeName is the instrumentation scope of everything ynh emits.
const ScopeName = "github.com/eyelock/ynh"
