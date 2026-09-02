# Hooks

Hooks are shell commands that vendors execute at specific lifecycle events during an agent session. They bridge the **guide layer** (what ynh manages) to the **sensor layer** (linters, tests, validators) by declaring *when* a command should run, without embedding the tool itself.

A harness declares hooks in `.ynh-plugin/plugin.json` at the top level. At assembly time, ynh translates them into the vendor-native config format. The hook scripts themselves live outside the harness — they are regular shell commands or scripts on the host machine.

> **Note:** Hooks can vary by [profile](harnesses.md#profiles). When a profile is selected, its `hooks` field replaces the top-level hooks entirely.

## Why Hooks Matter

Martin Fowler's harness engineering framework distinguishes feedforward controls (guides) from feedback controls (sensors). Hooks are the connection point: a harness declares "run this linter before every tool use" and the vendor runtime enforces it. The harness author defines the *intent*; the hook script provides the *mechanism*.

OpenAI's harness engineering guidance emphasizes that hook blocking messages should contain **agent-legible remediation instructions** — when a hook blocks an action, the error output should tell the agent *what to do differently*, not just *what went wrong*.

## Canonical Events

ynh defines five canonical hook events. Each vendor translates these to its native event names.

| Canonical Event | Description |
|----------------|-------------|
| `before_tool` | Runs before a tool/command is invoked. Can block execution. |
| `after_tool` | Runs after a tool/command completes. Can reject the result. |
| `before_prompt` | Runs before a user prompt is submitted to the model. |
| `on_stop` | Runs when the agent finishes responding. On Claude Code this fires at the **end of every turn**, not once at session end — see [on_stop output semantics](#on-stop-output-semantics-claude). |
| `on_session_start` | Runs when a session/agent starts. Codex supports filtering on `source` (`startup`\|`resume`) via the hook entry's existing `matcher` field. |

## Manifest Format

Hooks are declared under the top-level `hooks` key in `.ynh-plugin/plugin.json`. Each event maps to an array of hook entries:

```json
{
  "name": "my-harness",
  "version": "0.1.0",
  "hooks": {
    "before_tool": [
      {
        "matcher": "Bash",
        "command": "/usr/local/bin/check-dangerous-commands.sh"
      }
    ],
    "after_tool": [
      {
        "command": "/usr/local/bin/run-linter.sh"
      }
    ],
    "on_stop": [
      {
        "command": "/usr/local/bin/cleanup.sh"
      }
    ]
  }
}
```

Each hook entry has:

| Field | Required | Description |
|-------|----------|-------------|
| `command` | Yes | Shell command to execute |
| `matcher` | No | Tool name pattern to scope the hook (only meaningful for `before_tool` and `after_tool`) |

## Vendor Translation

Each vendor uses different event names and config file formats. **GitHub Copilot CLI is not in this table** — Copilot hooks silently no-op in folders the CLI hasn't marked as trusted, and no flag exists to grant that trust per-invocation, so `ynh`-assembled hook config would appear to work but never actually fire. Copilot's adapter always emits no hook config rather than shipping something misleadingly inert. See the Copilot row in [Vendor Support](vendors.md#vendor-notes) for detail.

### Event Name Mapping

| Canonical | Claude Code | Cursor | Codex |
|-----------|-------------|--------|-------|
| `before_tool` | `PreToolUse` | `beforeShellExecution` | `PreToolUse` |
| `after_tool` | `PostToolUse` | `afterFileEdit` | `PostToolUse` |
| `before_prompt` | `UserPromptSubmit` | `beforeSubmitPrompt` | `UserPromptSubmit` |
| `on_stop` | `Stop` | `stop` | `Stop` |
| `on_session_start` | `SessionStart` | `sessionStart` | `SessionStart` |

### Config File Locations

| Vendor | File | Format |
|--------|------|--------|
| Claude Code | `.claude/hooks/hooks.json` | Three-level nesting: event > matcher group > hook array |
| Cursor | `.cursor/hooks.json` | Flat: event > hook array (with `"version": 1` required) |
| Codex | `.codex/hooks.json` | Three-level nesting: event > matcher group > hook array (same structure as Claude) |

### Claude Code Runtime Limitation

Claude Code's `--plugin-dir` flag (used by `ynh run` for Claude) only auto-activates **skills and commands** from plugins. Hooks and MCP servers in `--plugin-dir` plugins are **not activated** at runtime — they require the plugin to be formally installed via `/plugin install`. See [Claude Code plugin docs](https://code.claude.com/docs/en/plugins).

This means hooks and MCP servers defined in `.ynh-plugin/plugin.json` are correctly **assembled and exported** by ynh, but are **not active during `ynh run` sessions** with Claude. They work correctly with Codex and Cursor (which use symlink-based installation into the project directory).

Hooks and MCP servers in exported plugins (`ynd export`) work as expected when the plugin is installed via Claude Code's `/plugin install` command.

### Running hooks in a plain Claude session

Because `--plugin-dir` hooks don't auto-activate, the way to make hooks — and the sensors that depend on them — fire when you simply open the repo in Claude Code is to declare them in the project's own `.claude/settings.json`, the file Claude auto-loads for every session in that directory. This is a separate deployment mode from `ynh run`:

| Mode | Hooks come from | When hooks fire |
|------|-----------------|-----------------|
| `ynh run` (staging dir + `--plugin-dir`) | assembled `.claude/hooks/hooks.json` | only after `/plugin install` (Claude limitation); Codex/Cursor activate via symlink |
| Plain `claude` in the project | project `.claude/settings.json` | every session, automatically |

For an always-on, sensor-driven repo, declare the hooks once in `.ynh-plugin/plugin.json` (canonical names) and let ynh write them into the settings file:

```bash
ynh hook export <harness> --target settings   # → .claude/settings.json (committed, team-wide)
ynh hook export <harness> --target local      # → .claude/settings.local.json (gitignored, personal)
ynh hook export <harness> --target settings --dry-run   # preview, write nothing
```

`hook export` translates canonical events to Claude-native names, applies the nested shape, and anchors relative command paths to `$CLAUDE_PROJECT_DIR` (see rules below). It **merges** — non-hook keys (`permissions`, `env`, …) and your own existing hooks are preserved, and re-running adds nothing already present, so it's safe to run repeatedly. `--target` is required; there is no default, so you always choose committed vs. personal explicitly.

Then confirm the wiring:

```bash
ynh doctor   # among its checks: .claude/settings.json + settings.local.json for the traps below
```

`ynh doctor`'s hook-wiring check flags canonical names that leaked into a settings file (where Claude ignores them), cwd-relative hook commands, and a project with no settings file at all (hooks declared but not wired).

If you hand-author the settings file instead, three rules:

1. **Use Claude-native event names and the nested shape** — `PreToolUse`, `PostToolUse`, `UserPromptSubmit`, `Stop`, each `{ "matcher": …, "hooks": [ { "type": "command", "command": … } ] }`. The canonical names (`before_tool`, `on_stop`, …) are valid **only** in `.ynh-plugin/plugin.json`; Claude silently ignores them in `settings.json`. Don't copy the `plugin.json` shape into `settings.json`.
2. **Anchor command paths to `$CLAUDE_PROJECT_DIR`** — `$CLAUDE_PROJECT_DIR/tools/hooks/foo.sh`. Claude runs each hook via `/bin/sh` in the **agent's current working directory**, not the project root, so a relative path like `./tools/hooks/foo.sh` silently breaks the moment the agent does `cd` into a subdirectory — and a *blocking* guard hook then fails open (stops guarding) without erroring. `$CLAUDE_PROJECT_DIR` is cwd-independent; it's also more portable than an absolute path, since `settings.json` is checked in and shared across machines.
3. **Keep the canonical declarations in `plugin.json` too** if you also use `ynh run` or `ynd export` — they activate there via `/plugin install`, Codex, or Cursor.

Example `.claude/settings.json` (what `hook export` produces):

```json
{
  "hooks": {
    "PostToolUse": [
      { "matcher": "Edit|Write", "hooks": [ { "type": "command", "command": "$CLAUDE_PROJECT_DIR/tools/hooks/after-edit-sensor.sh" } ] }
    ],
    "Stop": [
      { "hooks": [ { "type": "command", "command": "$CLAUDE_PROJECT_DIR/tools/hooks/on-stop-sensors.sh" } ] }
    ]
  }
}
```

The `Stop` entry above needs the loop-guard and output-routing discipline described in [on_stop output semantics](#on-stop-output-semantics-claude).

### Claude Code Format

Claude uses a three-level structure. Hook entries are grouped by matcher, and each group contains an array of inner hooks:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          { "type": "command", "command": "/usr/local/bin/check-dangerous-commands.sh" }
        ]
      }
    ]
  }
}
```

### Cursor Format

Cursor uses a flat structure with a required `"version": 1` field. Matchers are not supported — all hooks for an event fire unconditionally:

```json
{
  "version": 1,
  "hooks": {
    "beforeShellExecution": [
      { "command": "/usr/local/bin/check-dangerous-commands.sh" }
    ]
  }
}
```

### Codex Format

Codex uses the same three-level nesting structure as Claude. Hook entries are grouped by matcher, and each group contains an array of inner hooks:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          { "type": "command", "command": "/usr/local/bin/check-dangerous-commands.sh" }
        ]
      }
    ]
  }
}
```

## Blocking Hooks

A hook can block an action by using exit code 2. To provide the agent with context about why the action was blocked, the hook script should output a JSON object or a text message to stdout.

### Portable Hook Script Pattern

To write a hook that works across all vendors, output remediation instructions and exit with code 2:

```bash
#!/bin/bash
# check-dangerous-commands.sh — block destructive git operations
if echo "$@" | grep -qE 'git (push --force|reset --hard|clean -fd)'; then
  echo '{"error": "Destructive git operation blocked. Use --no-force or create a backup branch first."}' >&2
  exit 2
fi
exit 0
```

The blocking message should be **agent-legible**: tell the agent what to do instead, not just what failed. For example, "Use git push without --force" rather than "Force push not allowed."

## on_stop Output Semantics (Claude)

The `Stop` hook is the subtlest event, and it behaves differently from the other three in ways that matter for sensor sweeps.

**It fires every turn, not once per session.** On Claude Code, `Stop` runs each time the agent finishes responding — not at session end. An on-stop sensor sweep therefore runs *per turn*, so it must be cheap and idempotent.

**Its stdout does not reach the model.** For a `Stop` hook, `stdout` + `exit 0` goes to the transcript (user-visible), **not** into the model's context. A sweep that prints its verdict to stdout and exits 0 runs every turn but the agent never sees the result. To surface a verdict to the model you must either:

- write the message to **stderr** and `exit 2` — Claude feeds stderr to the model and blocks the stop, or
- print JSON `{"decision": "block", "reason": "…"}` to stdout.

This is why a `PostToolUse` sensor that exits 2 + stderr reaches the model, while an on-stop one that prints to stdout appears dead.

**A naive `exit 2` infinite-loops.** Blocking the stop makes the agent continue; at the next stop the hook fires and blocks again, forever. Claude passes `stop_hook_active: true` in the hook's stdin JSON when the current continuation was itself caused by a stop-hook block. The script must read it and **not block again**.

### Canonical on_stop sensor-sweep template

```bash
#!/usr/bin/env bash
set -uo pipefail
# Claude passes the hook payload as JSON on stdin. stop_hook_active=true means we are
# already continuing because of a prior block — blocking again would loop forever.
# Absent (and stdin is a tty) when the script is run manually.
if [ -t 0 ]; then HOOK_INPUT=""; else HOOK_INPUT=$(cat 2>/dev/null || true); fi
STOP_ACTIVE=$(printf '%s' "$HOOK_INPUT" | python3 -c 'import json,sys
try:    print(str(json.load(sys.stdin).get("stop_hook_active", False)).lower())
except: print("false")' 2>/dev/null || echo false)

# … run sensors, compute fail_count and fail_detail, print a human summary to stdout …

if [ "$fail_count" -gt 0 ] && [ "$STOP_ACTIVE" != "true" ]; then
  { echo "sensor sweep: $fail_count FAILING — fix before ending the turn:"; printf '%s' "$fail_detail"; } >&2
  exit 2   # surfaces to the model AND blocks the stop, exactly once
fi
exit 0     # green → quiet, end normally
```

Cursor's `stop` and Codex's `Stop` route output and guard against loops differently; verify per vendor before relying on this exact pattern elsewhere.

## Root-Harness-Only Rule

Only the root harness's hooks are used. An included harness contributes `skills/`, `agents/`, `rules/` and `commands/` — files — and nothing else.

Nothing is *dropped*: `resolveWith` iterates includes flat, with no recursion, and returns file paths. **It never opens an included harness's `plugin.json`,** so a hook declared there is never read in the first place. Root-only is a property of the resolver, not a filter applied afterwards.

That is deliberate. A hook is command execution on every lifecycle event, so an include that could contribute one would turn inert composed content into an execution surface the root author never declared.

If an included harness needs hooks, copy its hook declarations into the root harness's `.ynh-plugin/plugin.json`. Merging that copy at authoring time — generated, labelled blocks with drift detection — is the agreed direction rather than resolving includes at run time.

## Portable Hook Script Advice

When writing hook scripts for use across vendors:

1. **Output correct JSON for the event type** — Claude expects `{"type": "command"}` wrapper; Cursor and Codex do not. Your *script output* (blocking messages) should be plain text or simple JSON that any vendor can display.
2. **Use exit code 2 for blocking** — all three vendors recognize exit code 2 as "block this action."
3. **Include remediation instructions** — tell the agent how to fix the problem, not just that there is one.
4. **Keep scripts idempotent** — hooks may fire multiple times per session.
5. **Make command paths cwd-independent** — hooks run in the agent's current working directory, not the project root, and that cwd changes as the agent navigates. A relative command (`./tools/hooks/foo.sh`) breaks after any `cd`. Anchor to the vendor's project-root variable — `$CLAUDE_PROJECT_DIR` on Claude Code — or use an absolute path.

## Pairing with Sensors

Hooks fire mid-session (push) and can produce artifacts that [sensors](sensors.md) declare a contract over (pull). The most common production pattern is `after_tool` writing a results file that a `files`-sourced sensor reads — implicit coupling by shared file path. See [Sensors §"Relationship to hooks"](sensors.md#relationship-to-hooks) for the full push/pull comparison and the canonical pairing pattern.

## CLI Editing

Hooks can be added and removed from the command line as well as authored directly in the manifest. The CLI distinguishes harness-level (default) hooks from profile-level overrides:

```bash
# Top-level harness hooks
ynh hook add <harness> <event> "<command>" [--matcher <pattern>]
ynh hook remove <harness> <event> <index>

# Profile-level hooks (override the harness-level set when the profile is active)
ynh profile hook add <harness> <profile> <event> "<command>" [--matcher <pattern>]
ynh profile hook remove <harness> <profile> <event> <index>
```

`<event>` is validated against the canonical set: `before_tool`, `after_tool`, `before_prompt`, `on_stop`, `on_session_start`. `<index>` is zero-based. When the last entry for an event is removed, the event key is dropped from the manifest entirely.

To translate a harness's declared hooks into a Claude settings file (so they fire in a plain session — see [Running hooks in a plain Claude session](#running-hooks-in-a-plain-claude-session)):

```bash
ynh hook export <harness> --target <settings|local> [-v claude] [--dry-run]
```

And to check that the project's settings files are correctly wired:

```bash
ynh doctor
```

See [reference.md](reference.md) for the complete flag matrix and [profiles.md](profiles.md#cli-editing) for the surrounding profile-editor surface.

## See Also

- [Hooks](tutorial/hooks.md) — step-by-step walkthrough
- [Sensors](sensors.md) — observation surfaces a loop driver consumes
- [Harness Engineering](harness-engineering.md) — how hooks bridge guides to sensors
- [Vendor Support](vendors.md) — vendor capabilities and differences
