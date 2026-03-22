# Contributing to ynh

## Architecture

ynh is a packaging and distribution tool. It has no runtime component - the AI vendor CLI (Claude, Codex, Cursor) handles all interaction. ynh's job is to resolve, assemble, and launch.

### Core Flow

```
.claude-plugin/plugin.json + metadata.json → resolve Git includes → assemble vendor config → launch vendor CLI
```

1. **Detect** the persona format and load its manifest (`internal/persona/`, `internal/plugin/`)
2. **Resolve** Git includes by cloning/caching repos (`internal/resolver/`)
3. **Assemble** a staging directory with vendor-specific layout (`internal/assembler/`)
4. **Launch** the vendor CLI, adapting to each vendor's capabilities (`internal/vendor/`)

### Package Structure

```
cmd/ynh/                  CLI entry point: persona manager (init, install, uninstall, update, run, ls, info, vendors, search, registry, image, status, prune)
cmd/ynd/                  CLI entry point: developer tools (create, lint, validate, fmt, compress, export, marketplace, inspect)
internal/
  config/                 Global config (~/.ynh/) and path management
  persona/                Persona loading, format detection, name validation
  plugin/                 Plugin format types (plugin.json + metadata.json)
  resolver/               Git clone, cache, and content extraction
  assembler/              Build vendor config dir from resolved content
  exporter/               Produce vendor-native plugin dirs from persona definitions
  marketplace/            Generate marketplace indexes from sets of personas/plugins
  registry/               Registry discovery: fetch, search, lookup across Git-hosted indexes
  symlink/                Symlink transaction log (~/.ynh/symlinks.json)
  vendor/                 Vendor adapter interface and implementations
    adapter.go            Interface definition + registry
    claude.go             Claude Code adapter (exec with --plugin-dir)
    codex.go              OpenAI Codex adapter (child process + symlinks)
    cursor.go             Cursor adapter (child process + symlinks)
    symlinks.go           Shared symlink install/clean helpers
    process.go            Child process management with signal forwarding
testdata/                 Test fixtures
docs/                     User guide (GitHub Pages)
```

### Key Design Decisions

**No build system on content.** Skills, agents, rules, and commands are standard-format files. ynh never transforms or wraps them. A skill from skills.sh works as-is.

**Vendor is a deployment concern, not a content concern.** Personas define what artifacts to include. The vendor adapter decides where to put them and how to launch. Adding a new vendor is one file implementing the `Adapter` interface.

**Git is the package manager.** No npm, no custom registry. Content lives in Git repos, versioned with Git tags, cached locally by hash.

**Personas compose from any source.** A persona can embed its own artifacts and pull from any number of Git repos. The `path` field supports monorepos.

**Vendor-adaptive launch.** Each vendor gets the strategy that matches its capabilities. Claude supports `--plugin-dir`, so ynh does a clean `exec` with no running process. Codex and Cursor lack native plugin loading, so ynh installs symlinks and manages a child process with signal forwarding. This pragmatic split avoids forcing a lowest-common-denominator approach.

**Plugin format.** Personas use the Claude Code plugin format (`.claude-plugin/plugin.json`) for the manifest, with a `metadata.json` sidecar for ynh-specific config under a `"ynh"` key. This makes personas first-class Claude Code plugins while keeping extensibility for other tools.

## Technologies

- **Go 1.25+** - single binary, no runtime dependencies
- **Git** - content resolution, caching, versioning
- **JSON** - persona manifests (`plugin.json` + `metadata.json`)
- **JSON** - all configuration (plugin manifests, metadata, global config)

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
```

### Two Binaries

The project produces two binaries:

- **`ynh`** (`cmd/ynh/`) - Persona manager for end users. Install, run, update, uninstall, search registries, and manage registry sources.
- **`ynd`** (`cmd/ynd/`) - Developer tools for persona authors. Scaffold, lint, validate, format, compress, inspect, export vendor-native plugins, and build marketplace indexes. LLM-powered commands (compress, inspect) delegate to vendor CLIs on PATH.

Both are built by `make build`, installed by `make install`, and released via goreleaser (single tag, both binaries, synced versions). They share `internal/config` for version injection but are otherwise independent.

### ynd Internals

ynd is self-contained in `cmd/ynd/` with its own command routing, file discovery, and signal scanning. Key patterns:

- **LLM integration** (`llm.go`): Compress and inspect shell out to vendor CLIs (`claude`, `codex`) via `queryLLM()`. Auto-detection tries each CLI on PATH.
- **Signal scanning** (`inspect.go`): Discovers project files by category (build, test, CI, lint, config) to provide context for LLM analysis.
- **Backup system** (`compress.go`): Backups are stored in `~/.ynd/backups/` mirroring the absolute file path. Override with `YND_BACKUP_DIR` env var (used in tests).
- **Vendor-aware output** (`inspect.go`): Inspect writes artifacts to `.{vendor}/` by default (e.g., `.claude/skills/`). Override with `-o`. Discovery searches both project root and all vendor dirs.
- **Export** (`export.go`): Produces vendor-native plugin directories from persona definitions. Resolves includes, applies pick filtering, generates vendor manifests. Supports per-vendor and merged output modes.
- **Marketplace** (`marketplace.go`): Builds marketplace indexes from `marketplace.json` config. Exports personas with merged manifests, copies standalone plugins, generates `marketplace.json` indexes for each vendor.

### Exporter

The exporter (`internal/exporter/`) takes the same inputs as the assembler but produces distributable, vendor-native plugin directories instead of runtime config.

**Key differences from assembler:**
- Output goes to plugin root (not inside `ConfigDir`)
- Generates vendor-specific manifests (`.claude-plugin/plugin.json`, `.cursor-plugin/plugin.json`)
- Codex export uses `.agents/skills/` layout (different from runtime `.codex/`)
- Supports merged mode (dual manifests in one directory) for marketplace builds

The exporter reuses `assembler.CopyPicked`, `CopyAllArtifacts`, `CopyFile`, and `BuildDelegateAgent` for content operations but owns its own layout decisions per vendor.

### Registry

The registry system (`internal/registry/`) enables persona discovery from Git-hosted indexes. A registry is a Git repo with a `registry.json` at its root. Registries are configured in `~/.ynh/config.json` and fetched/cached via `resolver.EnsureRepo`.

The install command uses a 6-rule disambiguation chain: local path → SSH URL → HTTPS URL → `name@registry` → Git shorthand → plain name registry search. See `cmd/ynh/install_resolve.go`.

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
}
```

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

The assembler builds a deterministic run directory (`~/.ynh/run/<name>/`) with the vendor's expected layout. It copies files from resolved content into the right artifact directories (e.g., `skills/` files go into `.claude/skills/`), and copies `instructions.md` to the vendor's project instructions file (e.g., `CLAUDE.md`). The run directory is cleaned and repopulated on each run. Two modes:

- **With pick list**: Only specified paths are copied
- **Without pick list**: All recognized artifact directories are scanned and copied

For delegates, the assembler generates a vendor-native agent file for each delegate persona, embedding the delegate's `plugin.json` description, `instructions.md`, rules, and skill list.

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
- `composed-persona/` - personal persona with embedded artifacts
- `team-persona/` - team persona for delegation testing
- `sample-persona/` - minimal self-contained persona
- `plugin-persona/` - persona in plugin format (`.claude-plugin/plugin.json` + `metadata.json`)

## Configuration

### Plugin Manifest (`.claude-plugin/plugin.json`)

```json
{
  "name": "my-persona",
  "version": "0.1.0",
  "description": "My coding persona"
}
```

The `name` field is required and becomes the launcher command name. Only fields from the Claude Code plugin schema belong here.

### Metadata Sidecar (`metadata.json`)

```json
{
  "ynh": {
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
      {"git": "github.com/user/other-persona"}
    ]
  }
}
```

ynh-specific config lives under the `"ynh"` key, keeping the file extensible for other tools.

### Install Lifecycle

There are two copies of `metadata.json` in a persona's life:

1. **Source copy** — git-tracked in the persona's repo. Author-managed. Contains `includes`, `delegates_to`, `default_vendor`.
2. **Installed copy** — at `~/.ynh/personas/<name>/metadata.json`. Created by `ynh install` via `copyTree`. Local-only, not git-tracked.

During install:
- `ynh install` copies the entire persona directory (including `metadata.json`) to `~/.ynh/personas/<name>/`.
- After the copy, ynh injects `installed_from` provenance into the installed `metadata.json`. This records where the persona was installed from (source type, URL/path, timestamp).
- ynh then pre-fetches all `includes` and `delegates_to` Git repos into `~/.ynh/cache/`. This ensures `ynh run` works offline and validates all Git refs at install time. If any fetch fails, the install fails with a clear error.
- The source `metadata.json` is never modified.

At runtime:
- `ynh run` reads the installed copy at `~/.ynh/personas/<name>/metadata.json` to resolve includes, delegates, and vendor settings.
- Cached repos are used as-is without hitting the network. If a cache entry is missing (e.g. manually cleared), ynh falls back to a network fetch with a warning.

The `installed_from` field looks like:

```json
{
  "ynh": {
    "installed_from": {
      "source_type": "git",
      "source": "github.com/eyelock/assistants",
      "path": "ynh/david",
      "installed_at": "2026-03-22T10:30:00Z"
    }
  }
}
```

Possible `source_type` values: `"local"`, `"git"`, `"registry"`. Registry installs also include `"registry_name"`.

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `YNH_HOME` | Override the ynh home directory | `~/.ynh` |
| `YND_BACKUP_DIR` | Override the ynd compress backup directory | `~/.ynd/backups` |

### Global Config (`~/.ynh/config.json`)

```json
{
  "default_vendor": "claude",
  "allowed_remote_sources": ["github.com/myorg/*"],
  "registries": [
    {"url": "github.com/myorg/persona-registry"}
  ]
}
```

### Directory Structure (`~/.ynh/`)

```
~/.ynh/
├── config.json               # Global configuration
├── symlinks.json             # Symlink transaction log (install/clean tracking)
├── personas/                  # Installed personas
│   └── david/
│       ├── .claude-plugin/
│       │   └── plugin.json
│       ├── metadata.json
│       ├── skills/
│       ├── agents/
│       ├── rules/
│       └── commands/
├── cache/                     # Cloned Git repos
│   └── eyelock--assistants--a1b2c3d4/
├── run/                       # Assembled vendor config (per persona, overwritten each run)
│   └── david/
│       ├── .claude/           # vendor config dir with assembled artifacts
│       └── CLAUDE.md          # vendor instructions file (from instructions.md)
└── bin/                       # Launcher scripts (add to PATH)
    └── david                  # -> exec ynh run david "$@"
```

## Using ynh's Own Persona

The ynh project ships its own persona (skills, agents, rules) for contributors. Since the persona is named `ynh` — which conflicts with the `ynh` binary in `~/.ynh/bin/` — it installs without a launcher script:

```bash
ynh install github.com/eyelock/ynh

# Or install from a monorepo subdirectory:
# ynh install github.com/org/monorepo --path personas/my-persona
# Installed persona "ynh"
#   Launcher: (skipped — conflicts with ynh binary, use "ynh run ynh")
```

To use it, run explicitly:

```bash
ynh run ynh
```

The skills and agents in this persona are development-focused and will be extracted to a separate repo in the future.

## Submitting Changes

1. Fork the repo
2. Create a feature branch
3. Make your changes
4. Run `make check` (format, lint, test, build)
5. Submit a pull request

All tests must pass. New features should include tests. Keep the code simple - this is a tool for developers, not a framework.
