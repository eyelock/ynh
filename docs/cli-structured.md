# Structured CLI Output

Conventions governing machine-readable output from `ynh` and `ynd` commands. Applies to every command that exposes a structured-output mode.

## Why

Human-readable tabwriter output is the default for every command — it is what a user at a terminal wants, and it stays that way. But a growing set of use cases need stable, parseable output:

- CI scripts (detecting whether a harness is installed, enumerating vendors, reading resolved paths)
- Shell automation (pipe `ynh paths` into `jq`, feed `ynh ls` into a worktree picker, etc.)
- IDE and editor integrations that treat the CLI as their source of truth rather than re-parsing on-disk files
- Troubleshooting — asking the CLI "what do you think is where?" is more reliable than guessing

Structured output exists to serve those consumers without breaking the humans-first default.

## Format

**JSON.** One format, everywhere. No YAML, no TOML, no per-command bespoke shapes.

- Fields use `snake_case` — matching `.harness.json` and `config.json`.
- Output is written to `stdout` as a single top-level value (object or array), terminated by a newline. No banners, no prompts, no progress chatter on `stdout`.
- Progress or informational messages, when emitted by a structured-output command, go to `stderr` and are advisory only. Consumers parse `stdout`.
- Arrays are emitted even when empty (`[]`, not omitted). Required object fields are always present.
- Optional object fields are **omitted when unset, never serialised as `null`**. Consumers can safely treat "field missing" as "field unset" without also checking for a `null` value. This matches Go's `omitempty` behaviour, which every emitter uses.

## Opt-in flag

Structured output is always **opt-in** via a single flag:

```
--format json
```

- `--format text` is the default and explicit equivalent of omitting the flag.
- Other values are rejected with a non-zero exit and an error on `stderr`.
- No `-o json`, no bare `--json`, no per-command variants. One convention, one flag name.
- The flag is **space-separated only**: `--format json`, not `--format=json`. This matches every existing flag in `ynh` and `ynd`. The `=` form is rejected as an unknown flag.

Commands that do not yet have structured output do not accept `--format`; the flag is added per-command as structured mode is implemented.

## Error envelope

When a command invoked with `--format json` fails:

- Exit code is non-zero (conventional: `1` for user/runtime errors, `2` for usage errors).
- `stdout` is empty, or contains a partial result only if the command explicitly documents streaming semantics (none do today).
- `stderr` contains a single JSON object:

```json
{
  "error": {
    "code": "<short-stable-identifier>",
    "message": "<human-readable description>"
  }
}
```

- `code` values are stable identifiers (e.g. `not_found`, `invalid_input`, `config_error`) — consumers may branch on them.
- `message` is for humans; do not parse.
- Additional fields may be added to the `error` object over time (additive-compat; see below).

When the same command is invoked without `--format json`, errors remain human-readable on `stderr` as they do today.

## Compatibility policy

**Additive-compat within a major version.** Consumers can rely on:

- Fields present today will remain present with the same meaning.
- New optional fields may be added at any time.
- Field removals, renames, or semantic changes require a major version bump.
- Enum-valued fields (e.g. `installed_from.type`) may gain new values within a major version. Consumers **must** tolerate unknown enum values — treat them as "something I don't recognise" rather than erroring.

Pre-1.0 caveat: breaking changes remain possible across minor versions, but will be called out in release notes and avoided where practical. Once `ynh` reaches 1.0, the policy above is binding.

## Field naming conventions

- `snake_case` for all keys.
- Paths are absolute, fully resolved — no `~`, no relative fragments. Consumers receive exactly what they can pass to `os.Open` or its equivalent.
- Timestamps are ISO 8601 in UTC (`2026-04-15T12:34:56Z`). No Unix epochs.
- Booleans are `true` / `false`, never `0` / `1` or `"yes"` / `"no"`.
- Vendor and adapter IDs use the canonical short form (`claude`, `codex`, `cursor`) — the same identifiers used in `.harness.json` and on the `-v` flag.

## Scope

This document governs *output* shape and stability. It does not govern:

- On-disk file formats (`.harness.json`, `config.json`, `symlinks.json`) — those have their own schemas under `docs/schema/`.
- Command-line argument shape — flags and positional args are part of each command's own contract.
- Log or diagnostic output from long-running operations (e.g. Git clones) — that remains human-oriented text on `stderr`.

Each structured-output command documents its own field reference in `docs/reference.md` or its command page. This doc is the overarching rule set they all conform to.
