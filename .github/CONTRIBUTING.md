# Contributing to ynh

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
# {"version": "dev-<branch>-<sha>", "capabilities": "0.2.0"}

# Downstream tooling on PATH now sees the dev build
```

Bump `CapabilitiesVersion` whenever you change a JSON shape, command name, or manifest field that downstream code decodes or depends on. Do **not** bump it for internal refactors, bug fixes, or additive fields that older clients can safely ignore.

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
  "focus": {
    "review": { "profile": "ci", "prompt": "Review staged changes for quality" },
    "docs": { "prompt": "Generate API documentation for all public interfaces" }
  }
}
```

`name` and `version` are required for installed harnesses. For project-local manifests (loaded via `--harness-file` or auto-discovered in cwd), they are optional. See the JSON schema at `docs/schema/harness.schema.json` for the full specification. See [Hooks](docs/hooks.md), [MCP Servers](docs/mcp.md), [Profiles](docs/profiles.md), and the focus tutorial (`docs/tutorial/14-focus.md`) for details.

### Focus Entries

Focus entries combine a profile + prompt for repeatable, non-interactive AI execution. When `ynh run --focus review` is invoked, ynh looks up the focus entry, applies its profile (if any), and sends its prompt to the vendor CLI via `LaunchNonInteractive`.

Profiles use merge semantics when applied — see `ResolveProfile()` in `internal/harness/harness.go`. Focus validation (`ValidateFocus()` in `internal/plugin/plugin.go`) checks that prompts are non-empty, and `ynd validate` cross-references profile names.

### Harness Resolution Order

When `ynh run` is invoked, the harness source is resolved in this order:

1. **Positional name**: `ynh run my-harness` → loads from `~/.ynh/harnesses/my-harness/.ynh-plugin/plugin.json`
2. **`--harness-file`**: `ynh run --harness-file path/.harness.json` → loads a legacy single-file manifest directly from the given path
3. **Auto-discovery**: bare `ynh run` → migrates the current working directory if needed, then loads `.ynh-plugin/plugin.json` from cwd

For `--harness-file` and auto-discovery, the harness is assembled into `~/.ynh/run/_inline-<hash>/` (hash of the source directory for stable run dirs).

### Install Lifecycle

A harness has two locations in its life:

1. **Source** — git-tracked in the harness's repo. Author-managed. The author writes `.ynh-plugin/plugin.json` containing `name`, `version`, `includes`, `delegates_to`, `default_vendor`, hooks, MCP servers, profiles, focuses.
2. **Installed copy** — at `~/.ynh/harnesses/<name>/`. Created by `ynh install`. Local-only, not git-tracked. Contains the copied source plus a separate `.ynh-plugin/installed.json` file written by ynh.

There are two install layouts on disk, chosen by command:

- **Tree-shaped** (`~/.ynh/harnesses/<name>/`) — created by `ynh install` for git and registry sources. The harness lives as a copy under `harnesses/`, with `.ynh-plugin/installed.json` recording provenance in-tree.
- **Pointer-shaped** (`~/.ynh/installed/<name>.json`) — created by `ynh fork`. The harness lives at a user-chosen path; the pointer file in `installed/` registers it under the YNH layer. No copy under `harnesses/` is made. Edits to the source tree are live to `ynh run`. The pointer file holds only registration metadata (name → source path → timestamp); provenance still lives in the source tree's `.ynh-plugin/installed.json`. `harness.Load(name)` checks pointers before tree directories.

During install:
- `ynh install` copies the entire harness directory (including the `.ynh-plugin/` directory) to `~/.ynh/harnesses/<name>/`.
- If the source uses the legacy `.harness.json` single-file format, the migration chain converts it to `.ynh-plugin/plugin.json` in place during install.
- ynh writes `~/.ynh/harnesses/<name>/.ynh-plugin/installed.json` recording install provenance — separate from the author-controlled `plugin.json`. This records where the harness was installed from (source type, URL/path, timestamp), and a `resolved[]` slice of per-include/per-delegate SHAs captured at fetch time.
- ynh then pre-fetches all `includes` and `delegates_to` Git repos into `~/.ynh/cache/`. This ensures `ynh run` works offline and validates all Git refs at install time. If any fetch fails, the install fails with a clear error.
- The source `.ynh-plugin/plugin.json` is never modified.

At runtime:
- `ynh run` reads the installed copy at `~/.ynh/harnesses/<name>/.ynh-plugin/plugin.json` to resolve includes, delegates, and vendor settings.
- Cached repos are used as-is without hitting the network. If a cache entry is missing (e.g. manually cleared), ynh falls back to a network fetch with a warning.

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
├── config.json               # Global configuration
├── symlinks.json             # Symlink transaction log (install/clean tracking)
├── harnesses/                  # Installed harnesses (tree-shaped: git, registry)
│   └── david/
│       ├── .ynh-plugin/
│       │   ├── plugin.json   # Author manifest (copied from source)
│       │   └── installed.json  # Install provenance (written by ynh install)
│       ├── skills/
│       ├── agents/
│       ├── rules/
│       └── commands/
├── installed/                  # Pointer files for forks (pointer-shaped: ynh fork)
│   └── researcher.json       # Registers a user-owned tree under <name>

├── cache/                     # Cloned Git repos
│   └── eyelock--assistants--a1b2c3d4/
├── run/                       # Assembled vendor config (per harness, overwritten each run)
│   ├── david/
│   │   ├── .claude/           # vendor config dir with assembled artifacts
│   │   └── CLAUDE.md          # vendor instructions file (from instructions.md)
│   └── _inline-a1b2c3d4/     # inline harness run dirs (--harness-file / auto-discovery)
└── bin/                       # Launcher scripts (add to PATH)
    └── david                  # -> exec ynh run david "$@"
```

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
