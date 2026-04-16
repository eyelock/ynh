# Tutorial 16: Structured Output

Use `--format json` to get machine-readable output from ynh commands. Useful for scripts, CI pipelines, shell automation, and IDE integrations.

## Prerequisites

Make sure `ynh` is installed and on your PATH. See the [install instructions](tutorial/README.md) if you haven't set up yet.

The `ynh paths` examples work with just ynh itself. The `ynh ls` examples need at least one harness installed — if you completed Tutorial 1, `my-harness` will be available.

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

## T16.8: List installed harnesses — JSON

`ynh ls` also supports `--format json`. First, make sure a harness is installed:

```bash
rm -rf /tmp/ynh-tutorial
mkdir -p /tmp/ynh-tutorial/my-harness/skills/greet

cat > /tmp/ynh-tutorial/my-harness/.harness.json << 'EOF'
{
  "name": "my-harness",
  "version": "0.1.0",
  "description": "Tutorial harness",
  "default_vendor": "claude"
}
EOF

cat > /tmp/ynh-tutorial/my-harness/skills/greet/SKILL.md << 'EOF'
---
name: greet
description: Say hello.
---
Say hello.
EOF

ynh install /tmp/ynh-tutorial/my-harness
```

Now list in JSON:

```bash
ynh ls --format json
```

Expected (timestamps and paths will differ):
```json
[
  {
    "name": "my-harness",
    "version": "0.1.0",
    "description": "Tutorial harness",
    "default_vendor": "claude",
    "path": "/Users/<you>/.ynh/harnesses/my-harness",
    "installed_from": {
      "source_type": "local",
      "source": "/tmp/ynh-tutorial/my-harness",
      "installed_at": "<timestamp>"
    },
    "artifacts": {
      "skills": 1,
      "agents": 0,
      "rules": 0,
      "commands": 0
    },
    "includes": [],
    "delegates_to": []
  }
]
```

Key points:
- Output is a JSON **array** — even with one harness.
- `path` is the absolute path to the installed harness directory.
- `artifacts` always includes all four counts (never omitted when zero).
- `includes` and `delegates_to` are always present, even when empty (`[]`).
- `description` is omitted if the harness has none (not present as `""`).

## T16.9: List harnesses — extract with jq

Get just the names:

```bash
ynh ls --format json | jq -r '.[].name'
```

Expected:
```
my-harness
```

Check if a specific harness is installed:

```bash
ynh ls --format json | jq -e '.[] | select(.name == "my-harness")' > /dev/null && echo "installed" || echo "not installed"
```

Expected:
```
installed
```

## T16.10: Empty list — JSON

With no harnesses installed, the JSON output is an empty array:

```bash
ynh uninstall my-harness
ynh ls --format json
```

Expected:
```json
[]
```

Not `null`, not omitted — a clean empty array. Reinstall for subsequent tutorials:

```bash
ynh install /tmp/ynh-tutorial/my-harness
```

## T16.11: YNH_HOME override

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

## Clean up

```bash
ynh uninstall my-harness 2>/dev/null
rm -rf /tmp/ynh-tutorial
```

## What you learned

- `--format json` gives machine-readable JSON on stdout; `--format text` is the default
- The flag is space-separated only (`--format json`, not `--format=json`)
- `ynh paths --format json` emits a single object; `ynh ls --format json` emits an array
- Empty arrays are `[]`, never `null` or omitted
- Optional fields like `description` are omitted when unset, never serialised as `""`
- Pipe to `jq` for extraction — names, paths, counts, install status checks
- Errors in text mode go to stderr as plain `Error:` lines
- Errors in JSON mode go to stderr as a structured envelope: `{"error":{"code":"...","message":"..."}}`
- Stdout is always empty on error — scripts can check exit code and parse stderr
- `code` values are stable identifiers for scripting; `message` is for humans
- `YNH_HOME` changes all resolved paths — useful for testing and multi-environment setups
- These conventions apply to every command that supports `--format json`

## Next

[Tutorial 12: Delegation](tutorial/04-delegation.md) — chain harnesses together as subagents.

The `--format json` pattern established here will appear on additional commands as structured output is added. See [Structured CLI Output](cli-structured.md) for the full conventions.
