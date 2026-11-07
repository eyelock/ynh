# ynh Architecture Reference

## Core flow

```
.agents/harness/plugin.json → resolve Git includes → assemble vendor config → launch vendor CLI
```

1. **Detect** harness format and load manifest (`internal/harness/`, `internal/plugin/`)
2. **Resolve** Git includes from local cache (`internal/resolver/`) — repos are pre-fetched at install time; run uses cache-only with fallback fetch on miss
3. **Assemble** vendor config into a content-addressed snapshot, `~/.ynh/run/.snapshots/<id-fsname>/<hash>/`, behind the stable link `~/.ynh/run/<id-fsname>` (e.g. `run/local--foo`) (`internal/assembler/`)
4. **Launch** vendor CLI, adapting to each vendor's capabilities (`internal/vendor/`)

## Package structure

```
cmd/ynh/                  CLI entry point and command handlers
internal/
  config/                 Global config (~/.ynh/) and path management
  harness/                Harness loading, format detection, name validation
  plugin/                 Harness manifest types (.agents/harness/plugin.json)
  resolver/               Git clone, cache, and content extraction
  assembler/              Build vendor config dir from resolved content
    delegates.go          Generate agent files for delegates_to
  symlink/                Symlink transaction log (~/.ynh/symlinks.json)
  vendor/                 Vendor adapter interface and implementations
    adapter.go            Interface definition + registry
    claude.go             Claude Code adapter (child process with --plugin-dir)
    codex.go              OpenAI Codex adapter (child process + symlinks)
    cursor.go             Cursor Agent adapter (child process + symlinks)
    copilot.go            GitHub Copilot CLI adapter (exec with --plugin-dir)
    opencode.go           OpenCode 2.x adapter (exec of `opencode --standalone` with OPENCODE_CONFIG_DIR)
    symlinks.go           Shared symlink install/clean helpers
    process.go            Child process management with signal forwarding
  exporter/               ynd export — vendor-native plugin directories
  agent/                  Autonomous agent loop (budget, checkpoint, convergence,
                          stuckness, sandbox, trajectory)
  gate/                   ynh check — runs sensors, applies tolerance, gates
  baseline/               Inherited-failure baselines and ratchets
  freshness/              Whether a files sensor's artifact still describes the tree
  clischema/              Embedded CLI JSON schemas
  jsonschema/             Schema validation used by ynd validate-output
  namespace/              Canonical harness ids
  migration/              Format migration chains: FormatChain on load, MigrateChain for ynd migrate
  registry/               Harness registries
  marketplace/            Vendor-native marketplace indexes
  sources/                Local harness source directories
  backend/                Local model backends (ynh backend)
  docs/                   Tests over the docs tree: links, anchors, tutorial names
  pathutil/               Path helpers
testdata/                 Test fixtures (export-harness, monorepo, etc.)
```

`ls internal/` is the authority — this listing has been stale before. The four
stages above are the original flow; everything from `agent/` down was added
later and is invisible in that diagram.

`agent/`, `baseline/`, `gate/` and `freshness/` are the loop that runs between
agent turns; they have their own reference in `agent-and-baseline.md`.

## Key design decisions

- **No build system on content** - artifacts are standard-format files, never transformed
- **Vendor is a deployment concern** - harnesses define what, adapters decide where/how
- **Git is the package manager** - no registry, content cached locally by URL+ref hash
- **Vendor-adaptive launch** - every vendor runs as a child process of ynh, which supervises it (`runChildProcess`: SIGINT left to the child, SIGTERM/SIGHUP forwarded, exit code passed through, child ended if ynh dies); Claude and Copilot load natively (`--plugin-dir`), OpenCode by `OPENCODE_CONFIG_DIR`, Codex/Cursor need symlinks
- **Content-addressed run snapshots** - each run assembles into a staging folder and seals it as a snapshot, `~/.ynh/run/.snapshots/<id-fsname>/<hash>/`, reused when the inputs match. `~/.ynh/run/<id-fsname>` (keyed by canonical id) is a symlink to the newest snapshot, swapped atomically, so a running session never sees a later run's writes. Snapshots are kept and never deleted yet (pruning is a later slice, ynw-u0n5.3); same-named installs don't clobber each other
- **Single manifest** - `.agents/harness/plugin.json` for all config (identity, includes, hooks, MCP servers, profiles). `.harness.json` is the legacy form: only `ynd migrate` converts it, and every read command refuses a tree that still uses it with that fix, leaving the tree untouched (#406). `.ynh-plugin/` is the pre-0.8 manifest directory: not read (#404), never created. A tree you own that has one is refused with `ynd migrate`; copies ynh owns (installs, cache clones) are renamed on load.

## Adapter interface

```go
type Adapter interface {
	Name() string
	DisplayName() string
	CLIName() string
	ConfigDir() string
	ArtifactDirs() map[string]string
	InstructionsFile() string
	NeedsSymlinks() bool
	Install(stagingDir string, projectDir string) ([]SymlinkEntry, error)
	Clean(entries []SymlinkEntry) error
	LaunchInteractive(configPath string, extraArgs []string) error
	LaunchNonInteractive(configPath string, prompt string, extraArgs []string) error
	LaunchWithInitialPrompt(configPath string, prompt string, extraArgs []string) error
	SupportsResume() bool
	ResolveLastSession(cwd string, notBefore time.Time) (string, error)
	LaunchResume(configPath string, sessionID string, prompt string, interactive bool, extraArgs []string) error
	GenerateSystemPrompt(content []byte) map[string][]byte
	ApplyRuntimeInstructions(runDir, text string) ([]string, error)
	GenerateHookConfig(hooks map[string][]plugin.HookEntry) (map[string][]byte, error)
	GenerateMCPConfig(servers map[string]plugin.MCPServer) (map[string][]byte, error)
	GeneratePluginManifest(hj *plugin.HarnessJSON, outputDir string) (map[string][]byte, error)
	PluginManifestDir() string
	ExportArtifactDirs() map[string]string
	SupportsExportDelegates() bool
	MarketplaceManifestDir() string
	GenerateMarketplaceIndex(cfg MarketplaceIndexConfig, plugins []MarketplacePluginInfo) ([]byte, error)
	AgentPluginLayout() AgentPluginLayout
}
```

**26 methods.** Regenerated from `internal/vendor/adapter.go`; that
declaration is the only authority. This copy has drifted before: it read 10,
11 and 18 methods in three different places while the interface had 24, which
hid the entire resume subsystem from anyone writing a new adapter, and later
read 25, without `AgentPluginLayout`, while the interface had 26.

One launch pattern: every vendor runs as a child process of ynh, supervised like every vendor, and ynh stays the parent until it exits. The deferred `snap.Release()` therefore holds the shared lock on the run snapshot for the child's whole life.
- **Claude, Copilot** (`NeedsSymlinks() = false`): child process with `--plugin-dir`. **OpenCode** (also `false`): child process of `opencode --standalone` with `OPENCODE_CONFIG_DIR`.
- **Codex/Cursor** (`NeedsSymlinks() = true`): symlink-based install into the project, planted through the stable link `run/<name>`.

New vendors: the adapter is one file in `internal/vendor/` that implements the interface and self-registers via `init()`, but a vendor also needs its per-vendor declarations (hook events, hook timeouts, session-start context, workspace) and the tables that read them, plus schema, image, parity, tests and docs. Follow the `add-vendor-adapter` skill, which lists every touch point.

## Error handling

Functions return errors. CLI `main()` handles display. Internal packages wrap with `fmt.Errorf("context: %w", err)`.

## Testing patterns

- `t.TempDir()` for filesystem isolation
- `t.Setenv("HOME", ...)` to isolate from real home
- Local Git repos created in tests for resolver testing
- Mock adapters for assembler testing
- Run with `make test` (race detection + coverage)
