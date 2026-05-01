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
| `ynh fork <name>` | `--to <path>`, `--name <new>`, `--format <text\|json>` |
| `ynh ls` | `--format <text\|json>` |
| `ynh info <harness>` | `--format <text\|json>` |
| `ynh vendors` | `--format <text\|json>` |
| `ynh search [query]` | `--format <text\|json>` |
| `ynh delegate add <harness> <url>` | `--ref`, `--path` |
| `ynh delegate remove <harness> <url>` | `--path` |
| `ynh delegate update <harness> <url>` | `--from-path`, `--path`, `--ref` |
| `ynh include add <harness> <url>` | `--path`, `--pick`, `--ref`, `--replace` |
| `ynh include remove <harness> <url>` | `--path` |
| `ynh include update <harness> <url>` | `--from-path`, `--path`, `--pick`, `--ref` |
| `ynh sources add <path>` | `--name`, `--description` |
| `ynh sources list` | `--format <text\|json>` |
| `ynh sources remove <name>` | |
| `ynh registry add <url>` | |
| `ynh registry list` | `--format <text\|json>` |
| `ynh registry remove <url>` | |
| `ynh registry update` | |
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
| `ynd compose <source>` | `--harness`, `--profile`, `--format <text\|json>` |
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
| `ynd compose` | Composed harness: `name`, `version`, `description`, `default_vendor`, `artifacts` (with source), `includes`, `delegates_to`, `hooks`, `mcp_servers`, `profiles`, `focuses`, `counts` |
| `ynh fork <name>` | Envelope (`capabilities`, `ynh_version`, `name`, `path`, `installed_from`) — see [ynh fork output](#ynh-fork-output) below |
| `ynh info <name>` | Envelope (`capabilities`, `ynh_version`, `harness`) wrapping a single harness object — see [Envelope and harness fields](#envelope-and-harness-fields) below |
| `ynh ls` | Envelope (`capabilities`, `ynh_version`, `harnesses`) wrapping an array of harness objects — same shape as `ynh info`, plus `artifacts`, minus `manifest` |
| `ynh paths` | `home`, `config`, `harnesses`, `symlinks`, `cache`, `run`, `bin` — all absolute paths resolved for the current `$YNH_HOME` |
| `ynh search [query]` | Array of result objects: `name`, `description`, `keywords`, `repo`, `path`, `vendors`, `version`, `from` (`type`, `name`) |
| `ynh vendors` | Array of vendor objects: `name`, `display_name`, `cli`, `config_dir`, `available` (bool) |
| `ynh version` / `ynd version` | `version` (release), `capabilities` (wire-contract). See [Wire-contract capability](cli-structured.md#wire-contract-capability-version---format-json). |
| `ynh sources list` | Array of source objects: `name`, `path`, `description`, `harnesses` (discovery count) |

Human-readable tabwriter output remains the default for every command. Structured mode is strictly opt-in.

### Envelope and harness fields

`ynh ls --format json` and `ynh info <name> --format json` share the same envelope shape:

```json
{
  "capabilities": "0.3.0",
  "ynh_version": "0.x.y",
  "harnesses": [ /* ynh ls — array */ ]
}
```

`ynh info` returns `"harness": { … }` (singular) instead of `"harnesses"`, with the same per-harness fields plus `manifest` (the raw `.ynh-plugin/plugin.json` body).

Per-harness fields:

| Field | Description |
|-------|-------------|
| `name` | Harness name |
| `kind` | Install kind: `local-fork` (pointer-shaped, registered via `ynh fork`), `local`, `registry`, `git`, or `-` for pre-migration entries |
| `version_installed` | Version recorded in the harness manifest |
| `version_available` | Latest version known upstream — **omitted** if `--check-updates` was not passed or the upstream check failed (the "unknown" state) |
| `description` | Optional human description |
| `default_vendor` | Vendor for `ynh run` without `-v` |
| `path` | Absolute path to the installed harness directory |
| `ref_installed` | Currently installed Git ref or SHA — omitted when there is no Git provenance |
| `ref_available` | Latest Git SHA known upstream — same omission rules as `version_available`. For registry harnesses this is the registry entry's recorded SHA (may lag the actual source); for git harnesses it equals `sha_available` |
| `sha_available` | Live upstream SHA from a fresh `git ls-remote` against `installed_from.source` at the recorded `installed_from.ref` — answers "has the source moved since I installed?" independently of registry freshness. Omitted with the same rules as `ref_available` |
| `is_pinned` | `true` iff `ref_installed` matches `^[0-9a-f]{7,40}$` (resolved SHA). Tags and branches are floating |
| `installed_from` | Provenance object — `source_type`, `source`, optional `ref`, `sha`, `path`, `registry_name`, `installed_at`, `forked_from` |
| `installed_from.ref` | Branch/tag/SHA recorded at install time — the ref this harness actually tracks. Empty for pre-migration installs (re-run `ynh update <name>` to backfill) |
| `installed_from.sha` | Resolved commit SHA at install time. Empty for pre-migration installs |
| `installed_from.forked_from` | Upstream a forked harness was copied from — `source_type`, `source`, `version`, `sha`, optional `ref`, `path`, `registry_name`. Absent on non-fork installs |
| `artifacts` | (`ynh ls` only) Counts: `skills`, `agents`, `rules`, `commands` |
| `includes` | Array of include objects: `git`, `ref_installed`, `ref_available`, `is_pinned`, optional `path`, `pick` |
| `delegates_to` | Array of delegate objects: `git`, `ref_installed`, `ref_available`, `is_pinned`, optional `path` |
| `manifest` | (`ynh info` only) Raw `.ynh-plugin/plugin.json` body, JSON-compacted |

#### `--check-updates` flag

`ynh info` and `ynh ls` accept `--check-updates` together with `--format json`. The flag opts in to upstream lookups for `version_available`, `ref_available`, and `sha_available`. Without it, those fields are always omitted (the "unknown" three-state).

What gets probed:

- **Includes and delegates** (per `git`, with a remote URL): `git ls-remote` against the upstream URL, targeting the recorded install ref (`installed.json.resolved[].ref`) — the branch the cache actually tracks. Falls back to the manifest ref for pre-migration installs. **Pinned entries** (`is_pinned: true`) probe `HEAD` instead of re-resolving the pinned SHA — otherwise a pinned entry could never appear behind upstream.
- **Registry-installed harnesses**: configured registries are walked. The matching entry's version → `version_available`; the entry's recorded SHA → `ref_available` (maintainer-controlled, may be stale). A live `ls-remote` against `installed_from.source` at `installed_from.ref` → `sha_available`.
- **Git-installed harnesses**: live `ls-remote` against `installed_from.source` at the recorded ref → `ref_available` and `sha_available` (identical for this source type).
- **Local-only harnesses** (no `installed_from`, or `source_type: "local"` without a remote): no probe possible — fields stay omitted.

> Note: `--check-updates` performs network calls. Failures degrade silently — fields are simply omitted, the command does not error. Default `info` and `ls` calls (without the flag) remain offline, fast, and deterministic. Probes run concurrently (bounded fan-out) so a multi-include harness does not serialize the network.

Three-state rendering on the consumer side (TermQ and similar):

- Field omitted ⇒ **unknown** (probe failed, not requested, or no upstream)
- Field present and equal to `*_installed` ⇒ **up-to-date**
- Field present and different from `*_installed` ⇒ **update available**

The `is_pinned` rule is the same on harnesses and includes:

- `ref_installed` matches `^[0-9a-f]{7,40}$` ⇒ `"is_pinned": true` (a resolved SHA)
- Anything else (tag, branch, `main`, `HEAD`, empty) ⇒ `"is_pinned": false`

### ynh fork output

`ynh fork <name> --format json` returns a single result object (not an array):

```json
{
  "capabilities": "0.3.0",
  "ynh_version": "0.x.y",
  "name": "demo",
  "path": "/absolute/path/to/fork",
  "installed_from": {
    "source_type": "local",
    "source": "/absolute/path/to/fork",
    "installed_at": "<timestamp>",
    "forked_from": {
      "source_type": "git",
      "source": "github.com/org/demo",
      "sha": "abc123",
      "version": "0.1.0"
    }
  }
}
```

`forked_from` captures the upstream provenance of the harness at the time of the fork. If the source harness had no upstream (a bare local install), `forked_from.source_type` is `"local"` and `forked_from.source` is the installed directory path.

`ynh fork` self-registers the new fork: a pointer file at `~/.ynh/installed/<name>.json` records `<path>` as the install location, and a launcher script is generated at `~/.ynh/bin/<name>`. The fork is immediately visible in `ynh ls` and runnable via `ynh run <name>` — no follow-up `ynh install` needed. Edits to the source tree are live; YNH never copies it.

`ynh fork` refuses to register if a flat install of the same name already exists (either a pointer or a tree at `~/.ynh/harnesses/<name>/`). Namespaced installs of the same name are unaffected — they remain accessible via `name@org/repo`. Uninstall the conflicting flat install first if you want the fork to take that name.

**`--name <new>`** registers the fork under a different name without uninstalling the source — the common case where a user wants to keep the upstream installed and fork a copy alongside it. The fork tree's `.ynh-plugin/plugin.json` is rewritten so its `name` field matches the registration; upstream identity survives in `installed_from.forked_from`. The new name is validated against the same regex as harness names (`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`). If `--to` is omitted, the default destination uses the new name (`<cwd>/<new>`).

`ynh uninstall <name>` for a fork removes the pointer file (and launcher, run dir, sources entry) but leaves the source tree on disk — the user owns it. To delete the tree as well, remove the directory after uninstalling.

If the source path recorded in a pointer no longer exists when the fork is loaded, `ynh` prints an actionable error directing the user to either restore the directory or run `ynh uninstall <name>`. There is no auto-relocate in this version.

`ynh update` refuses to run on a fork and prints an explanatory error. To incorporate upstream changes, re-install the original and fork again.
