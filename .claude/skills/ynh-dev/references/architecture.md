# ynh Architecture Reference

## Core flow

```
.ynh-plugin/plugin.json → resolve Git includes → assemble vendor config → launch vendor CLI
```

1. **Detect** harness format and load manifest (`internal/harness/`, `internal/plugin/`)
2. **Resolve** Git includes from local cache (`internal/resolver/`) — repos are pre-fetched at install time; run uses cache-only with fallback fetch on miss
3. **Assemble** vendor config into `~/.ynh/run/<id-fsname>/` (e.g. `run/local--foo/`) (`internal/assembler/`)
4. **Launch** vendor CLI, adapting to each vendor's capabilities (`internal/vendor/`)

## Package structure

```
cmd/ynh/                  CLI entry point and command handlers
internal/
  config/                 Global config (~/.ynh/) and path management
  harness/                Harness loading, format detection, name validation
  plugin/                 Harness manifest types (.ynh-plugin/plugin.json)
  resolver/               Git clone, cache, and content extraction
  assembler/              Build vendor config dir from resolved content
    delegates.go          Generate agent files for delegates_to
  symlink/                Symlink transaction log (~/.ynh/symlinks.json)
  vendor/                 Vendor adapter interface and implementations
    adapter.go            Interface definition + registry
    claude.go             Claude Code adapter (exec with --plugin-dir)
    codex.go              OpenAI Codex adapter (child process + symlinks)
    cursor.go             Cursor Agent adapter (child process + symlinks)
    copilot.go            GitHub Copilot CLI adapter (exec with --plugin-dir)
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
  migration/              Format migration chain (.harness.json -> plugin.json)
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
- **Vendor-adaptive launch** - Claude and Copilot use `syscall.Exec` (native `--plugin-dir`), Codex/Cursor use child process with signal forwarding (symlink-based install)
- **Deterministic run dir** - `~/.ynh/run/<id-fsname>/` (keyed by canonical id) overwritten each run (no temp dir leaks; same-named installs don't clobber each other)
- **Single manifest** - `.ynh-plugin/plugin.json` for all config (identity, includes, hooks, MCP servers, profiles). `.harness.json` is the legacy form; `ynd migrate` converts it and `DetectFormat` runs the migration chain transparently.

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
	LaunchResume(configPath string, sessionID string, extraArgs []string) error
	GenerateSystemPrompt(content []byte) map[string][]byte
	ApplyRuntimeInstructions(runDir, text string) ([]string, error)
	GenerateHookConfig(hooks map[string][]plugin.HookEntry) (map[string][]byte, error)
	GenerateMCPConfig(servers map[string]plugin.MCPServer) (map[string][]byte, error)
	GeneratePluginManifest(hj *plugin.HarnessJSON, outputDir string) (map[string][]byte, error)
	ExportArtifactDirs() map[string]string
	SupportsExportDelegates() bool
	MarketplaceManifestDir() string
	GenerateMarketplaceIndex(cfg MarketplaceIndexConfig, plugins []MarketplacePluginInfo) ([]byte, error)
}
```

**24 methods.** Regenerated from `internal/vendor/adapter.go` — that
declaration is the only authority. This copy has drifted before (it read 10,
11 and 18 methods in three different places while the interface had 24), which
hid the entire resume subsystem from anyone writing a new adapter.

Two launch patterns:
- **Claude, Copilot** (`NeedsSymlinks() = false`): `syscall.Exec` with `--plugin-dir` for clean process replacement. No ynh process running.
- **Codex/Cursor** (`NeedsSymlinks() = true`): Child process via `os/exec.Command` with signal forwarding (`SIGINT`/`SIGTERM`). ynh stays alive for cleanup.

New vendors: create one file in `internal/vendor/`, implement the interface, self-register via `init()`.

## Error handling

Functions return errors. CLI `main()` handles display. Internal packages wrap with `fmt.Errorf("context: %w", err)`.

## Testing patterns

- `t.TempDir()` for filesystem isolation
- `t.Setenv("HOME", ...)` to isolate from real home
- Local Git repos created in tests for resolver testing
- Mock adapters for assembler testing
- Run with `make test` (race detection + coverage)
