# CLI Reference

Centralized reference for both `ynh` and `ynd` binaries — environment variables, flags, and resolution order.

## Environment Variables

| Variable | Default | Scope | Fallback For |
|----------|---------|-------|-------------|
| `YNH_HOME` | `~/.ynh` | `ynh` (global) | — |
| `YNH_VENDOR` | _(none)_ | `ynh run`, `ynd preview/export/create/compress/inspect/marketplace` | `-v` flag |
| `YNH_PROFILE` | _(none)_ | `ynh run`, `ynd export/preview/diff` | `--profile` flag |
| `YNH_HARNESS` | _(none)_ | `ynd preview/export/diff/validate/lint/fmt` | `--harness` flag / positional arg |
| `YNH_YES` | _(none)_ | `ynd compress/inspect` | `-y` flag |
| `CI` | _(none)_ | `ynd compress/inspect` | (lowest priority skip-confirm) |
| `YND_BACKUP_DIR` | `~/.ynd/backups` | `ynd compress` | — |

**Note:** `YNH_VENDOR` is not used by `ynd diff` — diff always compares across multiple vendors and a single vendor value is not meaningful. Use `-v` with a comma-separated list instead.

## Priority Rules

| Setting | Resolution Order |
|---------|-----------------|
| Vendor | `-v` flag > `YNH_VENDOR` > harness `default_vendor` > global config |
| Profile | `--profile` flag > `YNH_PROFILE` > no profile (top-level) |
| Harness source | `--harness` flag > `YNH_HARNESS` > positional arg > `.` (CWD) or error |
| Non-interactive | `-y` flag > `YNH_YES` > `CI` |

The harness source defaults to `.` (CWD) for `validate`, `lint`, and `fmt`. For `preview`, `export`, and `diff` it is an error if no source is specified.

## ynh Commands

| Command | Key Flags |
|---------|-----------|
| `ynh install <source>` | `--path`, `-v` |
| `ynh run [harness]` | `-v`, `--profile`, `--install`, `--clean` |
| `ynh uninstall <harness>` | |
| `ynh update [harness]` | |
| `ynh ls` | `--format <text\|json>` |
| `ynh info <harness>` | `--format <text\|json>` |
| `ynh vendors` | |
| `ynh search <query>` | |
| `ynh registry <subcommand>` | |
| `ynh image <subcommand>` | |
| `ynh paths` | `--format <text\|json>` |
| `ynh status` | |
| `ynh prune` | |

## ynd Commands

| Command | Key Flags |
|---------|-----------|
| `ynd create <type> <name>` | |
| `ynd validate [path]` | `--harness` |
| `ynd lint [path]` | `--harness` |
| `ynd fmt [path]` | `--harness` |
| `ynd compress [files...]` | `-v`, `-y`, `--restore`, `--list-backups`, `--pick` |
| `ynd inspect` | `-v`, `-y`, `-o` |
| `ynd preview <source>` | `-v`, `-o`, `--harness`, `--profile` |
| `ynd diff <source>` | `-v`, `--harness`, `--profile` |
| `ynd export <source>` | `-v`, `-o`, `--harness`, `--profile`, `--path`, `--merged`, `--clean` |
| `ynd marketplace build` | `-v`, `-o`, `--clean` |

See [ynd Developer Tools](ynd.md) for detailed command documentation.

## Structured Output

Commands that take `--format json` emit machine-readable output conforming to [Structured CLI Output](cli-structured.md). Current emitters:

| Command | Structured fields |
|---------|-------------------|
| `ynh info <name>` | Single harness object: `name`, `version`, `description`, `default_vendor`, `path`, `installed_from`, `manifest` (raw `.harness.json` body) |
| `ynh ls` | Array of harness objects: `name`, `version`, `description`, `default_vendor`, `path`, `installed_from`, `artifacts`, `includes`, `delegates_to` |
| `ynh paths` | `home`, `config`, `harnesses`, `symlinks`, `cache`, `run`, `bin` — all absolute paths resolved for the current `$YNH_HOME` |

Human-readable tabwriter output remains the default for every command. Structured mode is strictly opt-in.
