# Hooks

Hooks are shell commands that vendors execute at specific lifecycle events during an agent session. They bridge the **guide layer** (what ynh manages) to the **sensor layer** (linters, tests, validators) by declaring *when* a command should run, without embedding the tool itself.

A harness declares hooks in `.agents/harness/plugin.json` at the top level. A harness it includes can contribute its own hooks too, if the include opts in; see [Hooks from Included Harnesses](#hooks-from-included-harnesses). At assembly time, ynh translates them into the vendor-native config format. A hook command is a regular shell command: a tool on the host machine, a script in the project, or a script the harness ships in its own tree. A command that starts with `./` names a script the harness ships; ynh carries it with the hooks and rewrites the path to reach it, in a session and in a plugin alike. See [Hook script paths](#hook-script-paths).

> **Note:** Hooks can vary by [profile](harnesses.md#profiles). When a profile is selected, its `hooks` field is merged per event: an event the profile declares replaces the default for that event, and events it does not declare are inherited. See [Profiles](profiles.md#merge-semantics).

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

Hooks are declared under the top-level `hooks` key in `.agents/harness/plugin.json`. Each event maps to an array of hook entries:

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

| Vendor | Format |
|--------|--------|
| Claude Code | Three-level nesting: event > matcher group > hook array |
| Cursor | Flat: event > hook array (with `"version": 1` required) |
| Codex | Three-level nesting: event > matcher group > hook array (same structure as Claude) |

Each vendor reads hooks from a different file depending on how the harness reaches it: a session in a project directory, or an installed plugin. ynh writes each file only where that vendor reads it:

| Vendor | Session (`ynh run`, `ynd preview`, `ynh agent`) | Plugin (`ynd export`, `ynd marketplace build`) |
|--------|--------------------------------------------------|-----------------------------------------------|
| Claude Code | `.claude/hooks/hooks.json`: `ynh run` passes `.claude/` as `--plugin-dir`, so this is `hooks/hooks.json` at that plugin's root | `hooks/claude.json`, named by `"hooks"` in `.claude-plugin/plugin.json` |
| Cursor | `.cursor/hooks.json`, the only project-level path Cursor reads ([cursor.com/docs/hooks](https://cursor.com/docs/hooks)) | `hooks/cursor.json`, named by `"hooks"` in `.cursor-plugin/plugin.json` |
| Codex | `.codex/hooks.json`, read from a trusted project's `.codex/` layer ([developers.openai.com/codex/hooks](https://developers.openai.com/codex/hooks)) | `hooks/codex.json`, named by `"hooks"` in `.codex-plugin/plugin.json` |
| Copilot | none (see [Vendor Translation](#vendor-translation)) | none of its own; see below |

The document is the same in a vendor's session file and its plugin file except, on Cursor and Codex, for commands that start with `./`, which reach the harness's script from a different root in each (see [Hook script paths](#hook-script-paths)). A plugin never reads a session path, so an export does not carry `.claude/hooks/`, `.cursor/hooks.json` or `.codex/hooks.json`, and a session assembly does not carry `hooks/<vendor>.json`.

#### Why a plugin's hooks are not in `hooks/hooks.json`

Every vendor's plugin loader has a default hooks file, `hooks/hooks.json` at the plugin root, and a manifest `"hooks"` field that can name another file. They differ in how the two combine:

| Vendor | Default plugin hooks file | Manifest `"hooks"` field | Source |
|--------|---------------------------|--------------------------|--------|
| Claude Code | `hooks/hooks.json` | Path, inline object or array. **Merged with** `hooks/hooks.json`, which loads whenever it exists | [code.claude.com/docs/en/plugins-reference](https://code.claude.com/docs/en/plugins-reference) |
| Cursor | `hooks/hooks.json` | Path or inline object. **Replaces** folder discovery: the default file is not also read | [cursor.com/docs/reference/plugins](https://cursor.com/docs/reference/plugins) |
| Codex | `hooks/hooks.json` | Path, inline object or array, in `.codex-plugin/plugin.json` for a legacy package. **Replaces** default-file discovery | [developers.openai.com/codex/plugins/build](https://developers.openai.com/codex/plugins/build) |
| Copilot CLI | `hooks.json` or `hooks/hooks.json` | Path or inline object | [docs.github.com: CLI plugin reference](https://docs.github.com/en/copilot/reference/cli-plugin-reference) |

A merged export and a marketplace package put every vendor's manifest in one plugin root, and the vendors' formats differ (Cursor's flat `beforeShellExecution` against Claude's nested `PreToolUse`). A shared `hooks/hooks.json` could therefore serve at most one of them, and because Claude Code loads that file even when its manifest names another, no other vendor's format could ever go there. So ynh writes no `hooks/hooks.json` into a plugin. Each vendor gets its own file, `hooks/<vendor>.json`, and its manifest names it in `"hooks"`; Cursor and Codex then read that file instead of the default, and Claude Code reads it alongside a default that is not there. No vendor reads another's format.

A single-vendor export uses the same layout, so every plugin ynh writes follows one rule and a per-vendor export can be combined with another by hand without a clash. A manifest names a hooks file only when the harness has hooks for that vendor, because a plugin loader rejects a `"hooks"` path that does not exist.

Copilot has no manifest of its own: it reads `.claude-plugin/plugin.json`, the file Claude writes, and ynh emits no Copilot hooks. A Copilot-only export therefore carries no hooks. In a merged package that also targets Claude, Copilot reads the same manifest, so it finds `hooks/claude.json`; Copilot accepts hooks in Claude's format, with PascalCase event names and Claude's matcher semantics ([docs.github.com: hooks configuration](https://docs.github.com/en/copilot/reference/hooks-configuration)).

### Hook script paths

A command such as `./scripts/guard.sh` names a script relative to the harness that declares it: `scripts/guard.sh` in the harness's own tree. Wherever the hooks end up, in a session's run directory or an installed plugin, the harness tree is not there, so ynh copies the script alongside the hooks and makes the command reach the copy. It does this for a command whose first characters are `./`, and only those; an absolute path (`/usr/local/bin/lint.sh`), a command already anchored to a variable (`$CLAUDE_PROJECT_DIR/x.sh`) and a PATH-style command (`make check`) are written exactly as declared.

| Vendor | Session (`ynh run`, `ynd preview`, `ynh agent`) | Plugin (`ynd export`, `ynd marketplace build`) |
|--------|--------------------------------------------------|-----------------------------------------------|
| Claude Code | `"${CLAUDE_PLUGIN_ROOT}"/scripts/guard.sh`, script copied to `.claude/scripts/guard.sh`: the `--plugin-dir` plugin root | `"${CLAUDE_PLUGIN_ROOT}"/scripts/guard.sh`: the installed plugin ([code.claude.com/docs/en/plugins-reference](https://code.claude.com/docs/en/plugins-reference)) |
| Cursor | `./scripts/guard.sh`, unchanged, script copied to `scripts/guard.sh` in the run directory | `"${CURSOR_PLUGIN_ROOT}"/scripts/guard.sh`, which Cursor expands in a plugin hook command ([cursor.com/docs/reference/plugins](https://cursor.com/docs/reference/plugins)) |
| Codex | `./scripts/guard.sh`, unchanged, script copied to `scripts/guard.sh` in the run directory | `"${PLUGIN_ROOT}"/scripts/guard.sh`; Codex exports `PLUGIN_ROOT` to plugin hook commands ([developers.openai.com/codex/plugins/build](https://developers.openai.com/codex/plugins/build)) |
| Copilot | no hooks | none of its own; in a merged package it reads `hooks/claude.json`, with Claude's `${CLAUDE_PLUGIN_ROOT}` (see below) |

The variable is quoted so an install path with spaces stays one word, as Claude Code's reference recommends.

**In a plugin, a `./` script is part of the plugin.** The plugin is installed somewhere else, away from the harness and from any project, so the only script its hooks can rely on is one that ships inside it. The export therefore copies each `./` script a hook runs from the harness directory to the same path in the plugin, keeping its mode: a harness with `scripts/guard.sh` and the hook `./scripts/guard.sh --strict` exports a plugin carrying `scripts/guard.sh`. Only the command's first word is taken as the script. A `./` script that is not a file in the harness, or that climbs out of it (`./../x.sh`), is not copied, and the export prints a warning naming it:

```
  warning: hook script ./scripts/missing.sh is not a file in the harness, so the plugin does not carry it
```

The hook is still written, anchored to the plugin root, so it will not find the script. To run a script that belongs to the project rather than to the plugin, anchor it yourself (`$CLAUDE_PROJECT_DIR/scripts/x.sh` on Claude Code) and it is left alone. A Copilot-only export carries no hooks, so it copies no scripts. `ynd marketplace build` copies scripts the same way and prints the same warnings on stderr, each prefixed with the entry's name (`warning: reviewer: hook script ...`).

Copilot's documentation does not say whether it expands `${CLAUDE_PLUGIN_ROOT}` in a hook command (it documents `${PLUGIN_ROOT}` and its `${CLAUDE_PLUGIN_ROOT}` alias for MCP servers), so a `./` script in a merged package is unverified on Copilot.

**In a session, a `./` script is part of the run directory.** `ynh run`, `ynd preview` and the agent loop copy each `./` script a hook runs into the run directory, by the same rules as an export (first word only, a regular file inside the harness, mode kept), and warn about any they cannot carry:

```
  warning: hook script ./scripts/missing.sh is not a file in the harness, so the session does not carry it
```

Where the copy goes follows where each vendor runs session hooks from:

- **Claude Code** reads the session hooks from the `.claude/` directory `ynh run` passes as `--plugin-dir`. Claude loads a `--plugin-dir` plugin in place and sets `${CLAUDE_PLUGIN_ROOT}` to it for that plugin's hook commands ([code.claude.com/docs/en/plugins/loading](https://code.claude.com/docs/en/plugins/loading)), so the script is copied to `.claude/scripts/guard.sh` and the command is anchored there. `ynh run` still starts Claude in your project, so the command no longer depends on the agent's working directory.
- **Cursor** runs project hooks from the project root ([cursor.com/docs/hooks](https://cursor.com/docs/hooks)), and **Codex** runs hooks in the session's working directory ([developers.openai.com/codex/hooks](https://developers.openai.com/codex/hooks)). `ynh run` launches both with the run directory as their working directory, so the script is copied to the run directory's root and the bare `./scripts/guard.sh` reaches it.

A `./` command therefore means the harness's script in a session too, not a file in your project. To run a script that belongs to the project, anchor it yourself (`$CLAUDE_PROJECT_DIR/scripts/x.sh` on Claude Code) or give an absolute path, and it is left alone. `ynh hook export`, which writes your project's own `.claude/settings.json`, is the exception: there the hooks belong to the project, and a `./` command is anchored to `$CLAUDE_PROJECT_DIR` (see [Running hooks in a plain Claude session](#running-hooks-in-a-plain-claude-session)).

### Running hooks in a plain Claude session

`ynh run` activates the harness's hooks through `--plugin-dir`, but that only covers sessions started by `ynh run`. To make hooks, and the sensors that depend on them, fire when you simply open the repo in Claude Code, declare them in the project's own `.claude/settings.json`, the file Claude auto-loads for every session in that directory. This is a separate deployment mode from `ynh run`:

| Mode | Hooks come from | When hooks fire |
|------|-----------------|-----------------|
| `ynh run` (staging dir + `--plugin-dir`) | assembled `.claude/hooks/hooks.json` | every `ynh run` session, no install step |
| Plain `claude` in the project | project `.claude/settings.json` | every session, automatically |

For an always-on, sensor-driven repo, declare the hooks once in `.agents/harness/plugin.json` (canonical names) and let ynh write them into the settings file:

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

1. **Use Claude-native event names and the nested shape** — `PreToolUse`, `PostToolUse`, `UserPromptSubmit`, `Stop`, each `{ "matcher": …, "hooks": [ { "type": "command", "command": … } ] }`. The canonical names (`before_tool`, `on_stop`, …) are valid **only** in `.agents/harness/plugin.json`; Claude silently ignores them in `settings.json`. Don't copy the `plugin.json` shape into `settings.json`.
2. **Anchor command paths to `$CLAUDE_PROJECT_DIR`** — `$CLAUDE_PROJECT_DIR/tools/hooks/foo.sh`. Claude runs each hook via `/bin/sh` in the **agent's current working directory**, not the project root, so a relative path like `./tools/hooks/foo.sh` silently breaks the moment the agent does `cd` into a subdirectory — and a *blocking* guard hook then fails open (stops guarding) without erroring. `$CLAUDE_PROJECT_DIR` is cwd-independent; it's also more portable than an absolute path, since `settings.json` is checked in and shared across machines.
3. **Keep the canonical declarations in `plugin.json` too** if you also use `ynh run` or `ynd export`, since they activate there through `--plugin-dir` for Claude, and for Codex and Cursor.

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

## Hooks from Included Harnesses

An included harness's hooks are **opt-in, per include**. A hook is command execution on every lifecycle event, so an include that could contribute one silently would turn composed content into an execution surface the root author never declared. The root author says yes, include by include, with `"hooks": true`:

```json
{
  "includes": [
    { "local": "../guard", "hooks": true },
    { "git": "github.com/acme/linters", "ref": "v1.2.0" }
  ]
}
```

```bash
ynh include add <harness> <url> --hooks     # sets "hooks": true
ynh profile include add <harness> <profile> <url> --hooks
```

`hooks` defaults to false. `ynh include add --replace --hooks` changes an existing include.

### What is carried

- **Only with consent.** An included harness's `hooks` reach the session only when the include entry that brought it in says `"hooks": true`. A picked include (`pick`) brings its hooks too when it consents.
- **At every link.** For a harness reached through another, consent must hold along the whole chain: the root's include of B says `"hooks": true`, and B's include of C says `"hooks": true`. If either link is missing, C's hooks are not carried.
- **After its profile.** If a namespaced profile was selected for the included harness (`--profile guard:strict`), its hooks are the result of that profile: an event the profile declares replaces the harness's own entries for that event, and events it does not declare are inherited (see [Profiles](profiles.md#merge-semantics)). That result is what is carried.
- **Merge order.** Per event, entries are appended: included harnesses first, in content order (dependencies before the harness that includes them), the root's own entries last. Nothing is de-duplicated: an identical entry from two sources runs twice.
- **Sensors and delegates are not carried.** Only hooks are opt-in. See [sensors](sensors.md#includes-root-only).

### Warning without consent

When an included harness declares hooks and its include does not consent, ynh does not fail. It prints one warning per such harness on stderr, every time the harness is assembled (`ynh run`, `ynh agent run`, `ynd preview`, `ynd export`):

```
  warning: included harness guard declares hooks (before_tool, on_stop) that are not active; add "hooks": true to its include to run them
```

The events are listed sorted. The source is the include's name as shown elsewhere, with the chain (`eyelock/a > eyelock/b`) for a harness reached through another. `ynd preview` also lists them after the assembled tree, under "Hooks from included harnesses:" (active) and "Hooks from included harnesses, not active" (declined), each with the event and its source.

### Script placement

A `./` command from an included harness names a script inside **that harness's** directory. ynh copies each such script into the run or plugin under a per-include subdirectory, `scripts/_include/<namespace>/`, keeping the script's path inside its harness, and rewrites the command to `./scripts/_include/<namespace>/<path>`. The vendor then anchors that command exactly as it does a root script (see [Hook script paths](#hook-script-paths)), so an included `./scripts/mark.sh` on Claude Code becomes `"${CLAUDE_PLUGIN_ROOT}"/scripts/_include/guard/scripts/mark.sh`, with the script at `.claude/scripts/_include/guard/scripts/mark.sh` in a session.

- `<namespace>` is the one [selection](profiles.md#profiles-and-focuses-of-included-harnesses) uses: the include's `as` alias, or the harness's name. When two harnesses that carry hooks share a namespace, each directory also carries a short hash of the include's chain, so `scripts/_include/guard-3fa9c01d/`; the hash depends only on where the harness came from, so it is the same on every run.
- The root's own scripts are unaffected, so a root `scripts/mark.sh` and an included `scripts/mark.sh` never collide.
- Only the command's first word is the script, and only a command starting with `./`. Absolute, variable-anchored (`$CLAUDE_PROJECT_DIR/x.sh`) and PATH-style commands are left exactly as declared.
- A script that climbs out of the included harness (`./../x.sh`) is an error naming the include, and nothing is written. A script the include names but does not ship as a regular file gets the same warning as a root script, naming the include: `hook script ./scripts/gone.sh is not a file in included harness guard, so the session does not carry it`.

### Export

`ynd export` and `ynd marketplace build` carry consented include hooks the same way: they are merged into the vendor's plugin hook file (`hooks/<vendor>.json`), ahead of the root's, and each script is copied to `scripts/_include/<namespace>/...` in the plugin, where the vendor's plugin-root variable reaches it. An include without consent adds nothing to the plugin, and the warning above is printed once for the export.

### `ynh hook export`

`ynh hook export` writes the project's own `.claude/settings.json`, where a `./` command is anchored to `$CLAUDE_PROJECT_DIR`, the project. An included harness's script is not in the project and a settings file cannot reach into the include's directory, so the export includes consented include hooks that are plain commands (absolute, `$CLAUDE_PROJECT_DIR`-anchored or PATH-style) and refuses, naming the include and the script, when one runs a `./` script. Either anchor the command in the included harness, or run the harness with `ynh run`, which does carry the script. Includes are resolved only when one of them says `"hooks": true`, so exporting a harness's own hooks still needs no network. An include without consent is warned about, as above.

## Portable Hook Script Advice

When writing hook scripts for use across vendors:

1. **Output correct JSON for the event type** — Claude expects `{"type": "command"}` wrapper; Cursor and Codex do not. Your *script output* (blocking messages) should be plain text or simple JSON that any vendor can display.
2. **Use exit code 2 for blocking**: Claude Code, Codex and Cursor treat exit code 2 as "block this action" on the events that can block. Copilot CLI does so only for `preToolUse` and `permissionRequest`; on its other events exit 2 is a warning, and a hook blocks through its JSON output instead.
3. **Include remediation instructions** — tell the agent how to fix the problem, not just that there is one.
4. **Keep scripts idempotent** — hooks may fire multiple times per session.
5. **Make command paths cwd-independent**: hooks run in the agent's current working directory, not the project root, and that cwd changes as the agent navigates. A relative command (`./tools/hooks/foo.sh`) breaks after any `cd`. Anchor to the vendor's project-root variable (`$CLAUDE_PROJECT_DIR` on Claude Code) or use an absolute path. For a session or a plugin export, ynh anchors a `./` script for you and ships the script alongside the hooks; see [Hook script paths](#hook-script-paths).

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
