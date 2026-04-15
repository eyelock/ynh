# Tutorial 16: Structured Output

Use `--format json` to get machine-readable output from ynh commands. Useful for scripts, CI pipelines, shell automation, and IDE integrations.

## Prerequisites

Make sure `ynh` is installed and on your PATH. See the [install instructions](tutorial/README.md) if you haven't set up yet.

No harnesses need to be installed — `ynh paths` works with just ynh itself.

## How structured output works

Every ynh command defaults to human-readable text. Commands that support structured output accept:

```
--format json     ← machine-readable JSON on stdout
--format text     ← human-readable text (the default)
```

The flag is **space-separated only** — `--format json`, not `--format=json`. This matches every other flag in ynh.

## T16.1: Show resolved paths — text

`ynh paths` reports every path root ynh uses for the current environment:

```bash
ynh paths
```

Expected:
```
home       /Users/<you>/.ynh
config     /Users/<you>/.ynh/config.json
harnesses  /Users/<you>/.ynh/harnesses
symlinks   /Users/<you>/.ynh/symlinks.json
cache      /Users/<you>/.ynh/cache
run        /Users/<you>/.ynh/run
bin        /Users/<you>/.ynh/bin
```

Seven rows, tab-aligned. These are the same values ynh uses internally — no guessing at `$YNH_HOME` or platform defaults.

## T16.2: Show resolved paths — JSON

```bash
ynh paths --format json
```

Expected:
```json
{
  "home": "/Users/<you>/.ynh",
  "config": "/Users/<you>/.ynh/config.json",
  "harnesses": "/Users/<you>/.ynh/harnesses",
  "symlinks": "/Users/<you>/.ynh/symlinks.json",
  "cache": "/Users/<you>/.ynh/cache",
  "run": "/Users/<you>/.ynh/run",
  "bin": "/Users/<you>/.ynh/bin"
}
```

A single JSON object on stdout. All paths are absolute. Keys are `snake_case`. Output ends with a newline.

## T16.3: Pipe to jq

Extract a single path for use in a script:

```bash
ynh paths --format json | jq -r '.harnesses'
```

Expected:
```
/Users/<you>/.ynh/harnesses
```

Count installed harnesses:

```bash
ls "$(ynh paths --format json | jq -r '.harnesses')" 2>/dev/null | wc -l | tr -d ' '
```

Expected: the number of harnesses you have installed (possibly `0`).

## T16.4: Explicit text format

`--format text` is the default and produces identical output to omitting the flag:

```bash
ynh paths --format text
```

Expected: same tabwriter output as T16.1.

## T16.5: Error handling — text mode

When `--format json` is **not** active, errors come back as plain text on stderr, prefixed with `Error:`:

```bash
ynh paths --format yaml
```

Expected:
```
Error: invalid --format value "yaml" (want text or json)
```

Exit code is `1`. No JSON anywhere — plain text for humans.

```bash
ynh paths --nope
```

Expected:
```
Error: unknown flag: --nope
```

## T16.6: Error handling — JSON error envelope

When `--format json` **is** active and an error occurs, ynh writes a structured error envelope to **stderr** (not stdout). Stdout is empty:

```bash
ynh paths --format json extra 2>/dev/null
```

Expected: no output (stdout is empty on error).

```bash
ynh paths --format json extra 2>&1 1>/dev/null
```

Expected (on stderr):
```json
{"error":{"code":"invalid_input","message":"unexpected argument: extra"}}
```

The envelope has two fields:
- `code` — a stable identifier safe to branch on in scripts (`invalid_input`, `not_found`, `config_error`, `io_error`)
- `message` — human-readable, do not parse

Extract the error code in a script:

```bash
ynh paths --format json extra 2>&1 1>/dev/null | jq -r '.error.code'
```

Expected:
```
invalid_input
```

## T16.7: Space-separated flags only

ynh flags are always space-separated. The `=` form is rejected:

```bash
ynh paths --format=json
```

Expected:
```
Error: unknown flag: --format=json
```

Always use `--format json` (with a space).

## T16.8: YNH_HOME override

Paths reflect the active `YNH_HOME`, which is useful for testing or multi-environment setups:

```bash
YNH_HOME=/tmp/ynh-custom ynh paths --format json
```

Expected:
```json
{
  "home": "/tmp/ynh-custom",
  "config": "/tmp/ynh-custom/config.json",
  "harnesses": "/tmp/ynh-custom/harnesses",
  "symlinks": "/tmp/ynh-custom/symlinks.json",
  "cache": "/tmp/ynh-custom/cache",
  "run": "/tmp/ynh-custom/run",
  "bin": "/tmp/ynh-custom/bin"
}
```

All seven paths shift to the overridden root.

## What you learned

- `--format json` gives machine-readable JSON on stdout; `--format text` is the default
- The flag is space-separated only (`--format json`, not `--format=json`)
- JSON output is a single object (or array) terminated by a newline — pipe to `jq` for extraction
- Errors in text mode go to stderr as plain `Error:` lines
- Errors in JSON mode go to stderr as a structured envelope: `{"error":{"code":"...","message":"..."}}`
- Stdout is always empty on error — scripts can check exit code and parse stderr
- `code` values are stable identifiers for scripting; `message` is for humans
- `YNH_HOME` changes all resolved paths — useful for testing and multi-environment setups
- These conventions apply to every command that supports `--format json`, not just `ynh paths`

## Next

[Tutorial 12: Delegation](tutorial/04-delegation.md) — chain harnesses together as subagents.

The `--format json` pattern established here will appear on additional commands as structured output is added. See [Structured CLI Output](cli-structured.md) for the full conventions.
