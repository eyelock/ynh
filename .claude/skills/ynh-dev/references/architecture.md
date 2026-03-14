# ynh Architecture Reference

## Core flow

```
.claude-plugin/plugin.json + metadata.json → resolve Git includes → assemble vendor config → launch vendor CLI
```

1. **Detect** persona format and load manifest (`internal/persona/`, `internal/plugin/`)
2. **Resolve** Git includes by cloning/caching repos (`internal/resolver/`)
3. **Assemble** vendor config into `~/.ynh/run/<name>/` (`internal/assembler/`)
4. **Launch** vendor CLI, adapting to each vendor's capabilities (`internal/vendor/`)

## Package structure

```
cmd/ynh/                  CLI entry point and command handlers
internal/
  config/                 Global config (~/.ynh/) and path management
  persona/                Persona loading, format detection, name validation
  plugin/                 Plugin format types (plugin.json + metadata.json)
  resolver/               Git clone, cache, and content extraction
  assembler/              Build vendor config dir from resolved content
    delegates.go          Generate agent files for delegates_to
  symlink/                Symlink transaction log (~/.ynh/symlinks.json)
  vendor/                 Vendor adapter interface and implementations
    adapter.go            Interface definition + registry
    claude.go             Claude Code adapter (exec with --plugin-dir)
    codex.go              OpenAI Codex adapter (child process + symlinks)
    cursor.go             Cursor Agent adapter (child process + symlinks)
    symlinks.go           Shared symlink install/clean helpers
    process.go            Child process management with signal forwarding
testdata/                 Test fixtures (sample-persona, monorepo, etc.)
```

## Key design decisions

- **No build system on content** - artifacts are standard-format files, never transformed
- **Vendor is a deployment concern** - personas define what, adapters decide where/how
- **Git is the package manager** - no registry, content cached locally by URL+ref hash
- **Vendor-adaptive launch** - Claude uses `syscall.Exec` (native `--plugin-dir`), Codex/Cursor use child process with signal forwarding (symlink-based install)
- **Deterministic run dir** - `~/.ynh/run/<name>/` overwritten each run (no temp dir leaks)
- **Plugin format** - `.claude-plugin/plugin.json` for the manifest (strict vendor schema), `metadata.json` sidecar for ynh config under a `"ynh"` key

## Adapter interface

```go
type Adapter interface {
    Name() string                                                         // vendor identifier
    CLIName() string                                                      // CLI binary name (e.g. "claude", "agent")
    ConfigDir() string                                                    // e.g. ".claude"
    ArtifactDirs() map[string]string                                      // artifact type -> directory name
    InstructionsFile() string                                             // project instructions filename
    NeedsSymlinks() bool                                                  // true if vendor needs symlink-based install
    Install(stagingDir, projectDir string) ([]SymlinkEntry, error)        // install artifacts to project
    Clean(entries []SymlinkEntry) error                                   // remove installed artifacts
    LaunchInteractive(configPath string, extraArgs []string) error        // start interactive session
    LaunchNonInteractive(configPath string, prompt string, extraArgs []string) error
}
```

Two launch patterns:
- **Claude** (`NeedsSymlinks() = false`): `syscall.Exec` with `--plugin-dir` for clean process replacement. No ynh process running.
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
