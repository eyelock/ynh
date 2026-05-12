# Contributing to ynh

## Design Stance

These principles constrain what gets accepted into ynh. Read them before proposing a feature; they explain why some natural-looking ideas are out of scope.

- **Declarative-first.** Manifests describe; they do not execute. Proposals that require ynh to run code at agent runtime, mutate state mid-session, or own the agent loop belong in a runtime project, not here.
- **Vendor-neutral.** Every feature must translate cleanly into every supported vendor. A capability that exists in only one vendor either gets a portable abstraction or stays out.
- **Restraint over richness.** The canonical hook vocabulary, the freeform sensor `format` string, the absence of a `passed` boolean on `ynh sensors run` — these are not gaps. They are deliberate refusals to take on responsibility that belongs to the layer above (the loop driver) or below (the vendor runtime).
- **Structure where the layer above needs to discover.** When an external consumer needs to know about something — a sensor, a focus, a hook event — it goes in the manifest as structured data with CLI discovery. When the *agent* needs to know about something, it goes in `instructions.md` as prose. The two channels are complementary.

See [Harness Engineering §"Design Stance"](../docs/harness-engineering.md#design-stance--declarative-first-vendor-neutral) for the user-facing version of this rationale.

## Architecture

ynh is a packaging and distribution tool. It has no runtime component - the AI vendor CLI (Claude, Codex, Cursor) handles all interaction. ynh's job is to resolve, assemble, and launch.

### Core Flow

```
.ynh-plugin/plugin.json → resolve Git includes → assemble vendor config → launch vendor CLI
```

1. **Detect** the harness format and load its manifest (`internal/harness/`, `internal/plugin/`)
2. **Resolve** Git includes by cloning/caching repos (`internal/resolver/`)
3. **Assemble** a staging directory with vendor-specific layout (`internal/assembler/`)
4. **Launch** the vendor CLI, adapting to each vendor's capabilities (`internal/vendor/`)

### Package Structure

```
cmd/ynh/                  CLI entry point: harness template manager (init, install, uninstall, update, run, ls, info, vendors, search, registry, image, status, prune)
cmd/ynd/                  CLI entry point: developer tools (create, lint, validate, fmt, compress, export, marketplace, inspect, preview, diff)
internal/
  config/                 Global config (~/.ynh/) and path management
  harness/                Harness loading, name validation, namespaced storage
  migration/              Format migration filter chain (see Migration Chain below)
  plugin/                 Harness manifest types (.ynh-plugin/plugin.json, marketplace.json)
  resolver/               Git clone, cache, and content extraction
  assembler/              Build vendor config dir from resolved content
  exporter/               Produce vendor-native plugin dirs from harness definitions
  marketplace/            Generate marketplace indexes from sets of harnesses/plugins
  namespace/              @ syntax parsing, URL → namespace derivation, FS name encoding
  registry/               Registry discovery: fetch, search, lookup across Git-hosted indexes
  symlink/                Symlink transaction log (~/.ynh/symlinks.json)
  vendor/                 Vendor adapter interface and implementations
    adapter.go            Interface definition + registry
    claude.go             Claude Code adapter (exec with --plugin-dir)
    codex.go              OpenAI Codex adapter (child process + symlinks)
    cursor.go             Cursor adapter (child process + symlinks)
    symlinks.go           Shared symlink install/clean helpers
    process.go            Child process management with signal forwarding
    toml.go               Minimal TOML emitter for Codex MCP config
testdata/                 Test fixtures
docs/                     User guide (GitHub Pages)
```

### Key Design Decisions

**No build system on content.** Skills, agents, rules, and commands are standard-format files. ynh never transforms or wraps them. A skill from skills.sh works as-is.

**Vendor is a deployment concern, not a content concern.** Harnesses define what artifacts to include. The vendor adapter decides where to put them and how to launch. Adding a new vendor is one file implementing the `Adapter` interface.

**Git is the package manager.** No npm, no custom registry. Content lives in Git repos, versioned with Git tags, cached locally by hash.

**Harnesses compose from any source.** A harness can embed its own artifacts and pull from any number of Git repos. The `path` field supports monorepos.

**Vendor-adaptive launch.** Each vendor gets the strategy that matches its capabilities. Claude supports `--plugin-dir`, so ynh does a clean `exec` with no running process. Codex and Cursor lack native plugin loading, so ynh installs symlinks and manages a child process with signal forwarding. This pragmatic split avoids forcing a lowest-common-denominator approach.

**Single manifest.** Harnesses use a single `plugin.json` inside `.ynh-plugin/` as their manifest — all config (identity, includes, hooks, MCP servers, profiles) lives in one place.

**Migration via filter chain.** All backward compatibility lives in `internal/migration/`. Each migration is one struct implementing `Migrator` — its own `Applies` conditions and `Run` transform. Loaders call the chain once before reading; they never branch on old formats themselves. Removing support for a legacy format means deleting that one file and unregistering the struct. No other code changes.

## Harness Identity

Every installed harness has exactly one identity form: a path-shaped, host-prefixed **canonical id**. There is no fallback path; every code path that accepts a user-typed harness reference goes through the same lexical classifier and rejects everything else. This section codifies the rules so the next contributor doesn't accidentally re-introduce a bare-name fallback "for backwards compat" — the whole point of schema 2 is that those branches don't exist.

### The classification rule

`internal/namespace.Classify(ref)` is the single entry point. It returns `RefPath` / `RefID` / `RefInvalid`:

| Ref shape | Classification | Examples |
|---|---|---|
| Starts with `./`, `../`, `/`, `~/`, drive-letter | `RefPath` | `./planner`, `~/work/planner`, `/abs/path` |
| Slash-bearing, no path prefix, no `@` | `RefID` | `github.com/eyelock/assistants/planner`, `local/planner` |
| Anything else | `RefInvalid` | `planner` (bare), `planner@eyelock/assistants` (legacy `@`-form), `id@v1` (version pin slot, reserved) |

Classify is purely lexical — no `os.Stat`, no heuristic, no fallback. If you find yourself adding "if not found as id, try as bare name" anywhere outside `internal/migration/`, that's a regression of this rule.

### The "no fallback in code paths" contract

PR-canonical-3 deliberately removed every bare-name lookup from the resolver. `harness.LoadQualified` calls `LoadByID` directly when given a `RefID` and returns `BadRefError` for everything else. `harness.ResolveEditTarget` does the same. Under schema 2 there's exactly one valid ref shape per kind.

**Rules for adding a new ref-accepting command:**

1. Take the user input verbatim.
2. Call `namespace.Classify` once.
3. Switch on the result. `RefPath` → resolve via filesystem. `RefID` → call `harness.LoadByID`. `RefInvalid` → return `harness.BadRefError(ref)` unchanged.
4. Do not invent a new ref form. Do not accept "bare name as fallback."
5. If a downstream library you call wants to look something up by bare name, that library is wrong; convert to a canonical id at the boundary.

### Migration is the only place legacy is touched

`internal/migration/canonicalid.go` runs **once** on schema-1 homes via the auto-migration gate in `cmd/ynh/main.go`. It walks `~/.ynh/installed/`, `~/.ynh/harnesses/`, and `~/.ynh/bin/` exactly once, rewrites everything to schema 2, and stamps `~/.ynh/.schema-version`. After that, the rest of the codebase has no memory that legacy forms ever existed.

When you encounter pre-schema-2 layout in a non-migration code path: that's a bug. Fix the bug by reaching schema 2 before the read, not by teaching the reader about both formats.

### On-disk encoding

Canonical ids contain `/`, which is not a filesystem-safe character. The transliteration is mechanical:

| Layer | Form |
|---|---|
| User-typed at CLI / JSON envelope | `github.com/eyelock/assistants/planner` |
| Pointer file path | `~/.ynh/installed/github.com--eyelock--assistants--planner.json` |
| Tree-shaped install dir | `~/.ynh/harnesses/github.com--eyelock--assistants--planner/` |
| `installed.json` `id` field | `github.com/eyelock/assistants/planner` (canonical, never `--`-form) |

`namespace.IDToFSName` and `FSNameToID` are the two adapters between these forms. Users never type `--` on the CLI; ynh never accepts it as input. The transliteration is a one-direction encoding for filesystem safety, same way npm stores `@scope/pkg` under `node_modules/@scope/pkg/`.

### Reserved prefixes

- `local/` — installs that have no remote source (local paths, forks, `--url` aliases). The fork command defaults `--as` to `local/<source-name>`. The CLI accepts `local/<name>` as an id but **rejects it as an install source** (`ynh install local/foo` is an error pointing at "use a filesystem path").
- `<host-with-dot>/<...>` — registry / Git-URL-derived ids. The host segment must contain a `.` (else it's not a real hostname); the next two segments are org and repo.

### Install topologies

An installed harness lives on disk in one of two shapes. The shape is chosen at install time from the source kind, recorded in `installed.json.source_type`, and is the single source of truth for both reads (LoadByID) and writes (ResolveEditTarget) — they always resolve to the same directory, so the include/delegate edits a user makes are always visible to `ynh run`.

| Topology | `source_type` | Created by | On-disk shape | Reads & writes go to |
|---|---|---|---|---|
| **Pointer-form** | `local`, `source` | `ynh install /path`, `ynh install <name>` (sources: entry), `ynh fork` | A single pointer file at `~/.ynh/installed/<id-fsname>.json` carrying both the registration (id, name) and the full provenance (ref, sha, path, namespace, registry_name, forked_from, resolved). No content is copied. | The user's source tree at `installed.json.source`. |
| **Tree-form** | `git`, `registry` | `ynh install <git-url>`, `ynh install <name>` (registry) | Content copy at `~/.ynh/harnesses/<id-fsname>/` plus a sibling `.ynh-plugin/installed.json` with the provenance. | The copy under HarnessesDir. |

The single classifier `harness.IsLocalSource(ins *plugin.InstalledJSON)` discriminates the two — every code path that needs to choose "consult the user's source tree" vs "consult the install copy" routes through it. See `internal/harness/topology.go`.

**Why two topologies, not one:** remote installs need a local copy (we can't run `ynh run` if the network is down); local installs already have a local copy (the user's working tree), and any second copy is just drift waiting to happen. The split keeps remote installs self-contained while keeping local installs honest about where the canonical source is.

**Why no `installed.json` in the user's source tree:** for pointer-form installs, the provenance record is ynh-owned state — it has no business being in the user's git repository. The pointer file is the home for that data. Prior schemas left a `.ynh-plugin/installed.json` in the source tree; the schema-3 migration absorbs and removes it.

### Schema version contract

`~/.ynh/.schema-version` records the on-disk format version. Absent file means **schema 1** (legacy / pre-migration). Content `2` means canonical-id layout. Content `3` means pointer-form local installs (see above).

The `ynh ls` and `ynh info` JSON envelopes carry `schema_version` as a **dynamic** field (read from disk via `migration.ReadSchemaVersion(home)`, not the static `config.SchemaVersion` constant). Consumers like TermQ gate their behaviour on this — never on `capabilities`, which is a separate wire-contract version.

**When bumping the schema version:** add a new migration step in `internal/migration/` (one file per schema version — see `local_install_collapse.go` for the schema-3 example), make it write the literal new version at the end of its work (not `CurrentSchemaVersion` — that constant moves), bump `migration.CurrentSchemaVersion`, chain it into `autoMigrate` and `cmdMigrateTo` so a home at any older schema migrates through every step in order, and write tests for both the legacy → new round-trip and idempotent re-runs. The auto-migration gate runs on first invocation against a stale home; do not accept stale-home reads anywhere else.

## Versioning & Identifiers

ynh inherits the **git-as-package-manager** model from Claude Code's plugin marketplace: identity is a git ref, optionally anchored to a commit SHA. There is no semver-style version resolver. Several version-shaped fields exist in the schema; only some are load-bearing. The next contributor will be tempted to wire the cosmetic ones into resolution — don't.

### The four identifiers

| Identifier | Where | Load-bearing? |
|---|---|---|
| `marketplace.json` `harnesses[].version` | Registry / marketplace metadata | **No.** Cosmetic label, surfaced in `ynh search` output only. Never consulted at install or update time. |
| `source.ref` (branch, tag, or SHA string) | Marketplace `RemoteSource`, registry `Entry`, `installed.json` | **Yes — primary.** Drives `git fetch` / `git checkout`. Whatever string is here is what the user is tracking. |
| `source.sha` (40-char commit) | Marketplace `RemoteSource`, registry `Entry`, `installed.json` | **Yes — optional integrity pin.** When set alongside `ref`, install verifies the fetched HEAD matches and aborts on mismatch (`cmd/ynh/install_helpers.go:verifyResolvedSHA`). |
| `plugin.json` `version` | Harness's own manifest | Cosmetic, surfaced as `version_installed` in `ynh ls`. Authored by the harness, decoupled from any registry's `version` field. |

### The contract: ref is primary, sha is an optional pin

`ref` is the user's stated intent ("track `v1.0`", "track `develop`", "stay on this exact commit"). `sha`, when present, is a belt-and-braces check that the fetched bytes are the bytes the registry author signed off on. Three legitimate combinations:

| `ref` | `sha` | Behaviour |
|---|---|---|
| `"v1.0"` | _empty_ | Fetch the tag. Tracks tag updates. (Tag rewrites silently honoured.) |
| `"v1.0"` | `"abc123…"` | Fetch the tag. Verify HEAD == sha. Abort if mismatch — protects against tag rewrites and tampering. |
| `"abc123…"` (full SHA) | _empty_ | Fetch by SHA. Immutable. (Resolver mode 3, `cloneAtSHA`.) |

The third row is the "I don't trust the registry to keep tags stable" mode. The second is the standard published-release mode. The first is the lightweight mode for internal/developer registries.

### Don't add a `--version` flag

`ynh install` and `ynh delegate add` accept `--ref` and `--sha`; they do not accept `--version`. Adding a `--version` flag would imply ynh has a version resolver, which it does not. If a user wants "version 1.0," they pin `--ref v1.0`. Mapping a semver request to a ref is the registry's job, not ynh's.

The cosmetic `marketplace.json` `version` field is a known wart kept for display compatibility. It is not a roadmap signal that resolution-by-version is coming. If you find yourself reading `entry.Version` in resolver code, that's the regression this section exists to prevent.

### Guidance for downstream consumers

Tools that compose ynh harnesses (delegate sheets, dashboards, CI integrations) should follow the same model:

1. Default to `installed.json.ref` when proposing a delegate or include — that's the user's stated intent at install time.
2. Offer `installed.json.sha` as an opt-in **integrity pin** (typically a checkbox: "pin to exact commit"), not as the default.
3. Don't use the cosmetic `version` field for anything other than display.

The user-facing version of this guidance lives in [`docs/marketplace.md` § Pinning: refs and SHAs](../docs/marketplace.md#pinning-refs-and-shas).

## Technologies

- **Go 1.25+** - single binary, no runtime dependencies
- **Git** - content resolution, caching, versioning
- **JSON** - all configuration (harness manifests, global config)

## Development Setup

```bash
# Prerequisites + dev tools (Go, linter, formatter)
make deps

# Build and install binaries to ~/.ynh/bin/
make install

# Run all tests
make test

# Run tests for a specific package
make test FILE=./cmd/ynh
make test FILE=./cmd/ynd

# Format code
make format

# Lint
make lint

# Full CI pipeline (deps, format, lint, test, build)
make check

# E2E suite (release gate; not part of `make check`)
make e2e
```

### E2E test suite

`make e2e` runs an end-to-end test suite (~100 tests, ~1m wallclock) that exercises both binaries against SHA-pinned fixtures in [eyelock/assistants:e2e-fixtures/](https://github.com/eyelock/assistants/tree/develop/e2e-fixtures). Tests live in `test/e2e/` behind the `e2e` build tag and are **not** part of `make check` or `make test`.

**What the suite locks:**

- Every documented entry point on `ynh` and `ynd` (install, update, fork, delegate, include, run, vendors, sources, paths, status, prune, info, ls, image, search, registry; create, lint, validate, fmt, preview, export, compose, diff, migrate, marketplace, inspect)
- All three vendor adapters (Claude, Codex, Cursor) end-to-end: instructions files, hooks (with matchers + per-vendor event remapping), MCP servers (command + URL forms, env passthrough)
- Profile + focus resolution (hook replace + inherit, MCP deep-merge, mutex/unknown errors)
- Schema/security guards (path traversal, --ref + local, fork update, duplicate sources)
- JSON error envelope, override semantics (harness AGENTS.md beats include's), symlink stability across reinstall
- Local file:// registry support (registry add → search → install with namespace collision handling)

The suite is the release gate, not a per-PR gate:

| Trigger | Behaviour |
|---------|-----------|
| PR opened/updated targeting `main` | E2E must pass before merge (`.github/workflows/e2e.yml`) |
| Tag push `v*` | E2E runs as part of `release.yml`, blocking `goreleaser` |
| Manual `workflow_dispatch` | Ad-hoc "is develop healthy?" check before opening release PR |
| PR targeting `develop` | Not triggered — feature work stays fast |

Tests clone `eyelock/assistants` over the network and exercise the production binary built via `make build`. Fixture SHAs are pinned in `test/e2e/helpers.go`. When ynh's harness schema legitimately evolves, the same PR that changes the schema must update the affected fixtures in `eyelock/assistants:e2e-fixtures/` and bump the SHA constants.

**Local fixture iteration.** If you have an `eyelock/assistants` worktree checked out at the pinned SHA, point the suite at it to skip the per-test clone:

```bash
YNH_E2E_ASSISTANTS_PATH=/path/to/assistants/worktree make e2e
```

The worktree's HEAD must match `AssistantsFixturesSHA` in `helpers.go` — otherwise the suite fails fast (so you can't accidentally pass tests locally with a fixture state CI doesn't share). Iterating on fixtures? Set `YNH_E2E_FIXTURES_LOOSE=1` to bypass the SHA check while you work, but bump the pinned SHA before pushing.

See `.claude/plans/e2e-test-suite.md` for the architecture and coverage matrix.

### Testing Unreleased ynh Against Downstream Tooling

Downstream consumers (e.g. TermQ) gate on `ynh`'s wire-contract version, exposed as `capabilities` in `ynh version --format json`. Unlike `version` (release tag, injected via ldflags), `capabilities` is a source constant in `internal/config/config.go` — `make install` produces a developer build that honestly reports whatever contract the current branch implements.

To test a branch against downstream tooling without cutting a release:

```bash
# From this repo — build and install to ~/.ynh/bin/
make install

# Verify the contract
ynh version --format json
# {"version": "dev-<branch>-<sha>", "capabilities": "0.4.0"}

# Downstream tooling on PATH now sees the dev build
```

Bump `CapabilitiesVersion` (`internal/config/config.go`) whenever you change a JSON shape, command name, or manifest field that downstream code decodes or depends on. Do **not** bump it for internal refactors, bug fixes, or additive fields that older clients can safely ignore. Distinct from `SchemaVersion`, which is the on-disk format version of `~/.ynh` (see Harness Identity § Schema version contract); they bump independently.

### Two Binaries

The project produces two binaries:

- **`ynh`** (`cmd/ynh/`) - Harness manager for end users. Install, run, update, uninstall, search registries, and manage registry sources.
- **`ynd`** (`cmd/ynd/`) - Developer tools for harness authors. Scaffold, lint, validate, format, compress, inspect, export vendor-native plugins, and build marketplace indexes. LLM-powered commands (compress, inspect) delegate to vendor CLIs on PATH.

Both are built by `make build`, installed by `make install`, and released via goreleaser (single tag, both binaries, synced versions). They share `internal/config` for version injection but are otherwise independent.

### ynd Internals

ynd is self-contained in `cmd/ynd/` with its own command routing, file discovery, and signal scanning. Key patterns:

- **LLM integration** (`llm.go`): Compress and inspect shell out to vendor CLIs (`claude`, `codex`) via `queryLLM()`. Auto-detection tries each CLI on PATH.
- **Signal scanning** (`inspect.go`): Discovers project files by category (build, test, CI, lint, config) to provide context for LLM analysis.
- **Backup system** (`compress.go`): Backups are stored in `~/.ynd/backups/` mirroring the absolute file path. Override with `YND_BACKUP_DIR` env var (used in tests).
- **Vendor-aware output** (`inspect.go`): Inspect writes artifacts to `.{vendor}/` by default (e.g., `.claude/skills/`). Override with `-o`. Discovery searches both project root and all vendor dirs.
- **Export** (`export.go`): Produces vendor-native plugin directories from harness definitions. Resolves includes, applies pick filtering, generates vendor manifests. Supports per-vendor and merged output modes.
- **Marketplace** (`marketplace.go`): Builds marketplace indexes from `marketplace.json` config. Exports harnesses with merged manifests, copies standalone plugins, generates `marketplace.json` indexes for each vendor.

### Exporter

The exporter (`internal/exporter/`) takes the same inputs as the assembler but produces distributable, vendor-native plugin directories instead of runtime config.

**Key differences from assembler:**
- Output goes to plugin root (not inside `ConfigDir`)
- Generates vendor-specific manifests (`.claude-plugin/plugin.json`, `.cursor-plugin/plugin.json`)
- Codex export uses `.agents/skills/` layout (different from runtime `.codex/`)
- Supports merged mode (dual manifests in one directory) for marketplace builds

The exporter reuses `assembler.CopyPicked`, `CopyAllArtifacts`, `CopyFile`, and `BuildDelegateAgent` for content operations but owns its own layout decisions per vendor.

### Registry

The registry system (`internal/registry/`) enables harness discovery from Git-hosted indexes. A registry is a Git repo with a `.ynh-plugin/marketplace.json` at its root. Registries are configured in `~/.ynh/config.json` and fetched/cached via `resolver.EnsureRepo`.

The install command uses a 6-rule disambiguation chain: local path → SSH URL → HTTPS URL → `name@org/repo` → Git shorthand → plain name registry search. See `cmd/ynh/install_resolve.go`.

### Migration Chain

All backward compatibility lives in `internal/migration/`. The pattern:

```go
type Migrator interface {
    Applies(dir string) bool  // check conditions — should this run?
    Run(dir string) error     // perform the transform
    Description() string      // user-facing label for what was migrated
}

type Chain []Migrator

func (c Chain) Run(dir string) ([]string, error)  // returns descriptions of applied migrations
```

**Adding a migration:** create one file in `internal/migration/`, implement `Migrator`, add to `DefaultChain()`.

**Removing a migration:** delete the file and remove the struct from `DefaultChain()`. No other code changes — loaders never branch on old formats directly.

**Rule:** loaders (`internal/plugin`, `internal/registry`, `internal/harness`) call the migration chain before reading. They assume the new format. Legacy detection logic lives only in `Applies()` methods inside `internal/migration/`.

## Code Patterns

### Vendor Adapters

To add a new vendor, create a single file in `internal/vendor/` that implements the `Adapter` interface and self-registers via `init()`. No other wiring needed - rebuild and the vendor is available.

**Interface** (`internal/vendor/adapter.go`):

```go
type Adapter interface {
    Name() string                                                         // vendor identifier
    CLIName() string                                                      // CLI binary name (e.g. "claude", "agent")
    ConfigDir() string                                                    // e.g. ".myvendor"
    ArtifactDirs() map[string]string                                      // artifact type → directory name
    InstructionsFile() string                                             // project instructions filename
    NeedsSymlinks() bool                                                  // true if vendor needs symlink-based install
    Install(stagingDir, projectDir string) ([]SymlinkEntry, error)        // install artifacts to project
    Clean(entries []SymlinkEntry) error                                   // remove installed artifacts
    LaunchInteractive(configPath string, extraArgs []string) error        // start interactive session
    LaunchNonInteractive(configPath string, prompt string, extraArgs []string) error
    GenerateSystemPrompt(content []byte) map[string][]byte                // vendor-native instruction files
    GenerateHookConfig(hooks) (map[string][]byte, error)                  // vendor-native hook config
    GenerateMCPConfig(servers) (map[string][]byte, error)                 // vendor-native MCP config
    GeneratePluginManifest(hj, outputDir) (map[string][]byte, error)      // vendor-native plugin manifest
    ExportArtifactDirs() map[string]string                                // restricted dirs for export (nil = use ArtifactDirs)
    SupportsExportDelegates() bool                                        // true if vendor supports delegates
    MarketplaceManifestDir() string                                       // manifest dir for marketplace index
    GenerateMarketplaceIndex(cfg, plugins) ([]byte, error)                // vendor-native marketplace index
}
```

**Consumer-side narrow interfaces.** Packages that consume adapters define their own interfaces covering only the methods they need:

- `assembler.LayoutProvider` — `ConfigDir()`, `ArtifactDirs()`, `InstructionsFile()`
- `exporter.VendorExporter` — generation methods needed for export
- `exporter.SystemPromptGenerator` — `GenerateSystemPrompt()` only

This follows the "accept interfaces, return structs" principle and keeps coupling minimal.

**Two launch patterns exist:**

Vendors with native plugin support (like Claude's `--plugin-dir`) use `syscall.Exec` for a clean process replacement:

```go
func launchMyVendor(configPath string, extraArgs []string) error {
    bin, err := exec.LookPath("myvendor")
    if err != nil {
        return err
    }
    args := []string{"myvendor", "--plugin-dir", filepath.Join(configPath, ".myvendor")}
    args = append(args, extraArgs...)
    return syscall.Exec(bin, args, os.Environ())
}
```

Vendors that need symlinks use a managed child process with signal forwarding, so ynh stays alive for cleanup:

```go
func launchMyVendor(configPath string, extraArgs []string) error {
    bin, err := exec.LookPath("myvendor")
    if err != nil {
        return err
    }
    cmd := exec.Command(bin, extraArgs...)
    cmd.Dir = configPath
    return runChildProcess(cmd) // handles SIGINT/SIGTERM forwarding
}
```

The `init()` function registers the adapter automatically via Go's init mechanism. See `internal/vendor/claude.go`, `codex.go`, and `cursor.go` for working examples.

### Resolver

The resolver clones Git repos into `~/.ynh/cache/` using a deterministic directory name (`org--repo--hash`, with double hyphens for parsibility). Repos are shallow-cloned (`--depth 1`) for speed. The `path` field scopes into a subdirectory within the cloned repo for monorepo support.

### Assembler

The assembler builds a deterministic run directory (`~/.ynh/run/<name>/`) with the vendor's expected layout. It accepts the narrow `LayoutProvider` interface (not the full `Adapter`) and copies files from resolved content into the right artifact directories (e.g., `skills/` files go into `.claude/skills/`), and copies instructions (`instructions.md` or `AGENTS.md` as fallback) to the vendor's project instructions file (e.g., `CLAUDE.md`). After assembly, hook and MCP configs are generated via the adapter's `GenerateHookConfig()` and `GenerateMCPConfig()` methods, and plugin manifests via `GeneratePluginManifest()`. The run directory is cleaned and repopulated on each run. Two modes:

- **With pick list**: Only specified paths are copied
- **Without pick list**: All recognized artifact directories are scanned and copied

For delegates, the assembler generates a vendor-native agent file for each delegate harness, embedding the delegate's `plugin.json` description, `instructions.md`, rules, and skill list.

### Error Handling

Functions return errors rather than panicking. The CLI's `main()` function handles all error display. Internal packages wrap errors with context using `fmt.Errorf("context: %w", err)`.

### Testing

Tests use Go's standard `testing` package. No test frameworks.

- `t.TempDir()` for isolated filesystem tests
- `t.Setenv("HOME", ...)` to isolate config from the real home directory
- Local Git repos (created in tests) for resolver testing
- Mock adapters for assembler testing

Run with race detection and coverage:

```bash
go test ./... -cover -race
```

### Resolution and Assembly Test Matrix

The include/subpath logic has many combinations. Integration tests in `internal/assembler/` cover them all:

| Test | Repos | Subpath | Pick | What it verifies |
|------|-------|---------|------|-----------------|
| `PickSingleSkill` | 1 | yes | yes | Single skill from subpath, others excluded |
| `PickMixedArtifactTypes` | 1 | yes | yes | All 4 artifact types via pick from subpath |
| `NoPickIncludesAllArtifacts` | 1 | yes | no | Everything under subpath auto-discovered |
| `SameRepoMultipleSubpaths` | 1 | yes x2 | yes | Two different subpaths from same repo |
| `MixedWithAndWithoutPath` | 2 | mixed | yes | One include with subpath, one without |
| `PlusEmbeddedContent` | 1 | yes | yes | External subpath + local embedded artifacts |
| `NonexistentPathErrors` | 1 | invalid | - | Clean error for bad subpath |
| `RootNoPathFullInclude` | 1 | no | no | Full repo, no subpath, no pick |
| `DeeplyNested` | 1 | 4 levels | no | `org/division/team/ai-config` works |
| `ComplexComposition` | 3 | mixed | mixed | 3 repos, multiple subpaths, selective picks |
| `MultiSourceComposition` | 2+embed | mixed | yes | Integration using testdata fixtures |
| `MonorepoNoPickIncludesAll` | 1 | yes | no | Integration with testdata monorepo |
| `SkillsRepoFullInclude` | 1 | no | no | Integration with testdata skills-repo |
| `CrossVendorAssembly` | 1 | no | yes | Same content assembled for 3 vendors |

Test fixtures in `testdata/` simulate real-world sources:
- `skills-repo/` - standalone skills library with skills, agents, and rules
- `monorepo/` - company monorepo with AI config under `packages/ai-config/` and unrelated code alongside
- `composed-harness/` - personal harness with embedded artifacts
- `team-harness/` - team harness for delegation testing
- `sample-harness/` - minimal self-contained harness
- `plugin-harness/` - harness with author and keyword metadata fields

## Configuration

### Harness Manifest (`.ynh-plugin/plugin.json`)

```json
{
  "name": "my-harness",
  "version": "0.1.0",
  "description": "My coding harness",
  "default_vendor": "claude",
  "includes": [
    {
      "git": "github.com/user/repo",
      "ref": "v1.0.0",
      "path": "subdirectory",
      "pick": ["skills/commit", "agents/reviewer"]
    }
  ],
  "delegates_to": [
    {"git": "github.com/user/other-harness"}
  ],
  "hooks": {
    "before_tool": [{"matcher": "Bash", "command": "/path/to/lint.sh"}],
    "on_stop": [{"command": "/path/to/log.sh"}]
  },
  "mcp_servers": {
    "github": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": {"GITHUB_TOKEN": "${GITHUB_TOKEN}"}
    }
  },
  "profiles": {
    "ci": {
      "hooks": { "before_tool": [{"matcher": "Bash", "command": "/path/to/ci-lint.sh"}] },
      "mcp_servers": { "github": null }
    }
  },
  "focuses": {
    "review": { "profile": "ci", "prompt": "Review staged changes for quality" },
    "docs": { "prompt": "Generate API documentation for all public interfaces" }
  }
}
```

`name` and `version` are required for installed harnesses. For project-local manifests (loaded via `--harness-file` or auto-discovered in cwd), they are optional. See the JSON schema at `docs/schema/harness.schema.json` for the full specification. See [Hooks](docs/hooks.md), [MCP Servers](docs/mcp.md), [Profiles](docs/profiles.md), and the focus tutorial (`docs/tutorial/14-focus.md`) for details.

### Delegates: remote-only

Unlike `includes`, `delegates_to` entries must be remote git URLs. The `local` source form is not supported and the schema rejects it. Reasons:

- **Portability.** A delegate path baked into a committed `plugin.json` either breaks on another machine or — worse — silently resolves to a different harness with the same path. Delegates are part of a harness's public contract; they need a stable, shareable identity.
- **Identity.** ynh keys delegate matching, SHA backfill, and provenance on the git URL. A local path has no global identity, no SHA, and no ref to track.
- **Post-install resolution.** A relative path written during authoring (`./sibling-harness`) does not resolve from the installed location (`~/.ynh/harnesses/<id>/`). Working pre-install but broken post-install is a worse failure mode than not supporting it at all.

To iterate on a delegate locally, install it (`ynh install <path>`), then reference it by its canonical id from the parent harness — or test the parent with `ynd preview` against an in-tree manifest.

### Focus Entries

Focus entries combine a profile + prompt for repeatable, non-interactive AI execution. When `ynh run --focus review` is invoked, ynh looks up the focus entry, applies its profile (if any), and sends its prompt to the vendor CLI via `LaunchNonInteractive`.

Profiles use merge semantics when applied — see `ResolveProfile()` in `internal/harness/harness.go`. Focus validation (`ValidateFocus()` in `internal/plugin/plugin.go`) checks that prompts are non-empty, and `ynd validate` cross-references profile names.

### Harness Resolution Order

When `ynh run` is invoked, the harness source is resolved in this order:

1. **Positional canonical id**: `ynh run local/my-harness` (or `ynh run github.com/org/repo/name`) → `harness.LoadQualified` classifies the ref via `namespace.Classify`, then `LoadByID` reads the schema-2 install at `~/.ynh/harnesses/<idfsname>/.ynh-plugin/plugin.json` (or the pointer file at `~/.ynh/installed/<idfsname>.json` for forks). Bare names (`ynh run my-harness`) are rejected with `BadRefError`.
2. **`--harness-file`**: `ynh run --harness-file path/.harness.json` → loads a legacy single-file manifest directly from the given path. Path-based, no canonical-id classification.
3. **Auto-discovery**: bare `ynh run` → migrates the current working directory if needed, then loads `.ynh-plugin/plugin.json` from cwd.

For `--harness-file` and auto-discovery, the harness is assembled into `~/.ynh/run/_inline-<hash>/` (hash of the source directory for stable run dirs). For positional refs, the run-dir is named after the manifest's bare `Name` field — keeping `~/.ynh/run/` paths flat (no `/` characters).

### Install Lifecycle

A harness has two locations in its life:

1. **Source** — git-tracked in the harness's repo. Author-managed. The author writes `.ynh-plugin/plugin.json` containing `name`, `version`, `includes`, `delegates_to`, `default_vendor`, hooks, MCP servers, profiles, focuses.
2. **Installed copy** — at `~/.ynh/harnesses/<idfsname>/` where `<idfsname>` is the canonical id with `/` → `--` (e.g. `github.com--eyelock--assistants--planner`). Created by `ynh install`. Local-only, not git-tracked. Contains the copied source plus a separate `.ynh-plugin/installed.json` file written by ynh.

There are two install layouts on disk, chosen by command:

- **Tree-shaped** (`~/.ynh/harnesses/<idfsname>/`) — created by `ynh install` for git and registry sources. The harness lives as a copy under `harnesses/`, with `.ynh-plugin/installed.json` recording provenance in-tree (including the canonical `id` field).
- **Pointer-shaped** (`~/.ynh/installed/<idfsname>.json`) — created by `ynh fork`. The harness lives at a user-chosen path; the pointer file in `installed/` registers it under the YNH layer using the same id-keyed transliteration. No copy under `harnesses/` is made. Edits to the source tree are live to `ynh run`. The pointer file holds registration metadata (`id`, `name`, source path, timestamp); provenance still lives in the source tree's `.ynh-plugin/installed.json`. `harness.LoadByID(id)` checks pointers before tree directories.

Both layouts are id-keyed under schema 2. The schema-1 layouts (`harnesses/<name>/` flat, `harnesses/<ns--repo>/<name>/` two-level, `installed/<name>.json` name-keyed) are converted in place by the migration in `internal/migration/canonicalid.go`. See § Harness Identity above for the full classification + on-disk encoding rules.

During install:
- `ynh install` copies the entire harness directory (including the `.ynh-plugin/` directory) to `~/.ynh/harnesses/<idfsname>/`. The id is derived from the recorded source URL plus the harness name via `namespace.CanonicalID(sourceURL, name)`.
- For canonical-id install sources (`ynh install github.com/eyelock/assistants/researcher`), `cmdInstall` synthesizes the clone URL from the first three segments and uses `sources.Discover` to find a manifest matching the trailing segment within the cloned repo.
- If the source uses the legacy `.harness.json` single-file format, the migration chain converts it to `.ynh-plugin/plugin.json` in place during install.
- ynh writes `~/.ynh/harnesses/<idfsname>/.ynh-plugin/installed.json` recording install provenance — separate from the author-controlled `plugin.json`. This records where the harness was installed from (source type, URL/path, timestamp), and a `resolved[]` slice of per-include/per-delegate SHAs captured at fetch time.
- ynh then pre-fetches all `includes` and `delegates_to` Git repos into `~/.ynh/cache/`. This ensures `ynh run` works offline and validates all Git refs at install time. If any fetch fails, the install fails with a clear error.
- ynh stamps `~/.ynh/.schema-version` to the current schema version after a successful install, so subsequent commands skip the auto-migrate gate cleanly.
- The source `.ynh-plugin/plugin.json` is never modified.

At runtime:
- `ynh run` reads the installed copy at `~/.ynh/harnesses/<idfsname>/.ynh-plugin/plugin.json` to resolve includes, delegates, and vendor settings. Run-dir naming uses the bare `name` field from the manifest, not the canonical id, so paths under `~/.ynh/run/` stay flat.
- Cached repos are used as-is without hitting the network. If a cache entry is missing (e.g. manually cleared), ynh falls back to a network fetch with a warning.
- Launchers at `~/.ynh/bin/<name>` invoke `ynh run "<canonical-id>" "$@"` — the schema-2 resolver rejects bare names, so the embedded ref must be the canonical id.

`installed.json` looks like:

```json
{
  "source_type": "git",
  "source": "github.com/eyelock/assistants",
  "path": "ynh/david",
  "installed_at": "2026-03-22T10:30:00Z",
  "resolved": [
    {"git": "github.com/example/repo", "path": "skills", "sha": "a1b2c3d..."}
  ]
}
```

Possible `source_type` values: `"local"`, `"git"`, `"registry"`. Registry installs also include `"registry_name"`. Forks include a `"forked_from"` block recording upstream provenance.

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `YNH_HOME` | Override the ynh home directory | `~/.ynh` |
| `YNH_VENDOR` | Fallback for `-v` flag | _(none)_ |
| `YNH_PROFILE` | Fallback for `--profile` flag | _(none)_ |
| `YNH_FOCUS` | Fallback for `--focus` flag | _(none)_ |
| `YNH_HARNESS_FILE` | Fallback for `--harness-file` flag | _(none)_ |
| `YNH_HARNESS` | Fallback for `--harness` flag (ynd) | _(none)_ |
| `YND_BACKUP_DIR` | Override the ynd compress backup directory | `~/.ynd/backups` |

### Global Config (`~/.ynh/config.json`)

```json
{
  "default_vendor": "claude",
  "allowed_remote_sources": ["github.com/myorg/*"],
  "registries": [
    {"url": "github.com/myorg/harness-registry"}
  ]
}
```

### Directory Structure (`~/.ynh/`)

```
~/.ynh/
├── .schema-version           # On-disk format version (2 = canonical-id layout)
├── .migration-manifest.json  # Last migration's old_id→new_id map (after first migrate)
├── .quarantine/              # Entries migration couldn't convert (--skip-broken)
│   └── broken/
├── config.json               # Global configuration
├── symlinks.json             # Symlink transaction log (install/clean tracking)
├── harnesses/                # Installed harnesses (tree-shaped: git, registry)
│   ├── github.com--eyelock--assistants--david/   # canonical-id-keyed (id with / → --)
│   │   ├── .ynh-plugin/
│   │   │   ├── plugin.json   # Author manifest (copied from source)
│   │   │   └── installed.json  # Install provenance (id, source URL, SHA, timestamp)
│   │   ├── skills/
│   │   ├── agents/
│   │   ├── rules/
│   │   └── commands/
│   └── local--my-harness/    # local installs land under "local/" namespace
├── installed/                # Pointer files for forks (id-keyed)
│   └── local--researcher.json    # registers a user-owned tree under canonical id
├── cache/                    # Cloned Git repos (URL-derived hash, not id-keyed)
│   └── eyelock--assistants--a1b2c3d4/
├── run/                      # Assembled vendor config (keyed by manifest Name, not id)
│   ├── david/
│   │   ├── .claude/          # vendor config dir with assembled artifacts
│   │   └── CLAUDE.md         # vendor instructions file (from instructions.md)
│   └── _inline-a1b2c3d4/     # inline harness run dirs (--harness-file / auto-discovery)
└── bin/                      # Launcher scripts (add to PATH)
    └── david                 # -> exec ynh run "github.com/eyelock/assistants/david" "$@"
```

The `<idfsname>` directory naming uses the canonical id with `/` → `--` (one-direction encoding for filesystem safety). Users never type `--` on the CLI; ynh never accepts it. See § Harness Identity → On-disk encoding for the full rule.

## Using ynh's Own Harness

The ynh project ships its own harness (skills, agents, rules) for both contributors and new users:

```bash
ynh install github.com/eyelock/ynh
ynh-guide
```

Inside the session, `/ynh-create-harness` walks you through creating your own harness. The development-focused skills (`ynh-dev`, `vendor-adapters`, etc.) live in `.claude/` and are loaded natively by Claude — they're not part of the installable harness.

## Submitting Changes

1. Fork the repo
2. Create a feature branch
3. Make your changes
4. Run `make check` (format, lint, test, build)
5. Submit a pull request

All tests must pass. New features should include tests. Keep the code simple - this is a tool for developers, not a framework.
