# Cursor — Vendor Reference

## Documentation URLs

- Hooks: https://cursor.com/docs/hooks
- Plugin Template: https://github.com/cursor/plugin-template
- Official Plugins Repo: https://github.com/cursor/plugins
- Marketplace: https://cursor.com/marketplace
- MCP Servers: https://docs.cursor.com/advanced/mcp
- Rules (.mdc format): https://docs.cursor.com/advanced/rules
- CLI Install: https://cursor.com/cli
- Forum: .agents/ support: https://forum.cursor.com/t/support-for-agent-folder-compatibility/154167

Note: docs.cursor.com aggressively rate-limits programmatic access. Manual browsing may be needed.

## Plugin Format

Manifest: `.cursor-plugin/plugin.json`
Required fields: `name`, `version`, `description`.
Optional: `displayName`, `author`, `license`, `keywords`, `logo`.

## Plugin Directory Structure

```
plugin-root/
  .cursor-plugin/plugin.json   (manifest)
  skills/<name>/SKILL.md        (agent skills)
  rules/<name>.mdc              (rules with frontmatter)
  agents/<name>.md              (subagents)
  commands/<name>.md             (commands)
  hooks/hooks.json              (hook config)
  mcp.json                      (MCP servers — note: no dot prefix)
  scripts/                      (hook scripts)
  assets/                       (logos, icons)
```

## Hook Config Paths

CONFIRMED 2026-10-04 (cursor.com/docs/hooks, cursor.com/docs/reference/plugins):

- Project: `.cursor/hooks.json` (a `hooks.json` anywhere else in the project is not loaded)
- User: `~/.cursor/hooks.json`
- Enterprise: `/Library/Application Support/Cursor/hooks.json` (macOS), `/etc/cursor/hooks.json` (Linux/WSL), `C:\ProgramData\Cursor\hooks.json` (Windows)
- Plugin: `hooks/hooks.json` at the plugin root, or a path or inline object in the manifest's `hooks` field. A manifest `hooks` value replaces folder discovery, so the default file is then not read. A plugin's `.cursor/hooks.json` is not read.

ynh writes each path only where Cursor reads it (#454, #469):

| ynh output | File | Generator |
|------------|------|-----------|
| `ynh run`, `ynd preview`, agent loop (project dir) | `.cursor/hooks.json` | `Cursor.GenerateHookConfig` |
| `ynd export -v cursor`, `ynd marketplace build` (plugin) | `hooks/cursor.json`, named by `"hooks"` in `.cursor-plugin/plugin.json` | `Cursor.GeneratePluginHookConfig`, picked by the exporter through its `PluginHookGenerator` interface; `Cursor.GeneratePluginManifest` adds the pointer when the file exists |

Hook command paths (#483). CONFIRMED 2026-10-04 (cursor.com/docs/reference/plugins):
"Cursor expands `${CURSOR_PLUGIN_ROOT}` and `${CLAUDE_PLUGIN_ROOT}` to the plugin's
install path in `command`, `args`, `env` values, and `cwd`"; it does not expand the
standard's `${PLUGIN_ROOT}`. The docs' hook example uses bare `./scripts/...` but never
states the hook's working directory, so ynh does not rely on it: the plugin file anchors a
`./` command to `"${CURSOR_PLUGIN_ROOT}"/` and the exporter copies the script into the
plugin. The session file keeps the command as written: "Project hooks run from the
project root" (CONFIRMED 2026-10-04, cursor.com/docs/hooks), `launchCursor` starts the
agent in the run dir, and `assembler.WriteSessionHooks` copies the script to the run dir's
root (#495; see docs/hooks.md "Hook script paths").

## Hook Events (18 agent events, plus Tab and app-lifecycle ones; NOT the same as Claude Code)

Agent: sessionStart, sessionEnd, preToolUse, postToolUse, postToolUseFailure,
subagentStart, subagentStop, beforeShellExecution, afterShellExecution,
beforeMCPExecution, afterMCPExecution, beforeReadFile, afterFileEdit,
beforeSubmitPrompt, preCompact, stop, afterAgentResponse, afterAgentThought.
Tab: beforeTabFileRead, afterTabFileEdit. App lifecycle: workspaceOpen.
(cursor.com/docs/hooks, checked 2026-10-30.) Event names are camelCase; Claude's
PascalCase names appear in Cursor only as matcher values (`Stop`, `UserPromptSubmit`).

ynh maps six: `before_tool` preToolUse, `after_tool` postToolUse, `before_prompt`
beforeSubmitPrompt, `on_stop` stop, `on_session_start` sessionStart, `on_session_end`
sessionEnd. before_tool and after_tool used to map to beforeShellExecution and
afterFileEdit (shell commands and file edits only); preToolUse and postToolUse fire for
any tool (Shell, Read, Write, Grep, Delete, Task, MCP), so the events now mean what they
mean on Claude and Codex.

Matcher (regex; empty or `*` matches everything): preToolUse, postToolUse and
postToolUseFailure match the tool type (`Shell`, `Read`, `Write`, `Grep`, `Delete`,
`Task`, `MCP:<tool_name>`); beforeShellExecution the full command; stop the word `Stop`;
beforeSubmitPrompt the word `UserPromptSubmit`. **Not documented** for sessionStart,
sessionEnd, preCompact, beforeMCPExecution. ynh writes a matcher on preToolUse and
postToolUse only, and warns when a Claude tool name (`Bash`, `Edit`, `MultiEdit`) is used
there, since Cursor calls the shell tool `Shell`.

`sessionEnd`: fires when a composer conversation ends; fire-and-forget (response logged,
not used); input `session_id`, `reason` (completed, aborted, error, window_close,
user_close), `duration_ms`, `is_background_agent`, `final_status`, `error_message`.
**Not available to cloud agents** (tied to the IDE session); fires on self-hosted workers
when the session's claim is released. **CLI behaviour is not specified** in the docs.
`sessionStart` is fire-and-forget too, not available to cloud agents.

**CONFIRMED live (cursor-agent 2026.10.01-e373342, 2026-10-30, `ynh run -v cursor --trust --
"<prompt>"`, CLI print mode, all six events in `.cursor/hooks.json`):** `sessionStart` and
`sessionEnd` fire from the CLI; `preToolUse` with no matcher fires for every tool type seen
(Grep, Read, Shell) and `postToolUse` fires; a `Shell` matcher fires only for shell calls and
a `Bash` matcher never fires (ynh's warning printed); hook scripts run with the run dir as
cwd, so a bare `./scripts/x.sh` works. **`beforeSubmitPrompt` and `stop` did NOT fire in print
mode.** ynh warns when launching Cursor non-interactively (`LaunchNonInteractive`, and the
agent loop's `--print` worker) with those hooks assembled (`vendor.CursorPrintModeWarnings`).
UNVERIFIED: interactive CLI and IDE behaviour, including whether those two fire there.

## Hook Types

command, prompt (cursor.com/docs/hooks); the older "same as Claude" claim of http and
agent types is not in the current page.

## Hook Formats (TWO different formats)

**Plugin hooks/hooks.json** — flat/legacy format with lowercase event names:
```json
{
  "hooks": {
    "preToolUse": [
      {"command": "./scripts/validate-shell.sh", "matcher": "Shell"}
    ],
    "postToolUse": [
      {"command": "./scripts/format-code.sh", "matcher": "Write"}
    ],
    "stop": [
      {"command": "./scripts/audit.sh"}
    ]
  }
}
```

**Settings.json** — three-level format with PascalCase event names (same as Claude):
```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {"type": "command", "command": "/path/to/script.sh", "timeout": 60}
        ]
      }
    ]
  }
}
```

CONFIRMED (cursor.com/docs/hooks, cursor.com/docs/reference/plugins): both locations
use the SAME flat/lowercase-camelCase format and event names — only the path differs.
Project-level `.cursor/hooks.json` (also `.cursor/hooks.json` gitignored-local,
`~/.cursor/hooks.json` user, and OS-specific enterprise paths) vs plugin-format
`hooks/hooks.json` at plugin root. ynh renders the same document for both (except `./`
commands, anchored to the plugin root in the plugin file) and writes
each to its own context only (see the table under Hook Config Paths). ynh's plugin
file is `hooks/cursor.json` via the manifest `hooks` field, not the default
`hooks/hooks.json`: Claude Code always loads a plugin root's `hooks/hooks.json`, so in
a merged package it would read Cursor's format (#469).

The full event list is under "Hook Events" above. ynh's canonical map covers six events:
`before_tool`, `after_tool`, `before_prompt`, `on_stop`, `on_session_start` and
`on_session_end`.

## MCP Format

Project: `.cursor/mcp.json`
User: `~/.cursor/mcp.json`
Plugin: `mcp.json` (at plugin root, NO dot prefix — differs from Claude's `.mcp.json`)
CONFIRMED 2026-10-04 (cursor.com/docs/context/mcp, cursor.com/docs/reference/plugins):
a project reads only `.cursor/mcp.json` (no root `mcp.json`); a plugin discovers
`mcp.json` at its root automatically, or a custom path named by `mcpServers` in
`.cursor-plugin/plugin.json`. ynh writes each only where it is read (#470):
`Cursor.GenerateMCPConfig` returns `.cursor/mcp.json` (run, preview, agent loop),
`Cursor.GeneratePluginMCPConfig` returns `mcp.json` (export, marketplace). Same
document, one renderer.

```json
{
  "mcpServers": {
    "name": {
      "command": "npx",
      "args": ["-y", "@scope/server"],
      "env": {"KEY": "value"}
    }
  }
}
```

Supports: stdio, SSE, streamable HTTP transports. OAuth authentication supported.

## Marketplace Format (.cursor-plugin/marketplace.json)

```json
{
  "name": "cursor-plugins",
  "owner": {"name": "Cursor", "email": "plugins@cursor.com"},
  "metadata": {"description": "..."},
  "plugins": [
    {"name": "plugin-name", "source": "plugin-name", "description": "..."}
  ]
}
```

Install command: `/add-plugin` in editor

## Rules Format (.mdc)

Path: `.cursor/rules/<name>.mdc`
Legacy: `.cursorrules` (project root, deprecated but still read)

```yaml
---
description: Baseline coding standards
globs: "*.ts,*.tsx"
alwaysApply: true
---

- Prefer small, focused changes
- Write tests for new functions
```

Frontmatter fields: `description`, `globs` (file pattern), `alwaysApply` (boolean).
CONFIRMED (cursor.com/docs/advanced/rules): plain `.md` files in `.cursor/rules` are
silently ignored. ynh's Cursor adapter (`internal/vendor/cursor.go`,
`Cursor.TransformArtifact`) renames `.md` → `.mdc` and writes one frontmatter block
at copy time (both `ynh run` staging and `ynh export`). A source rule's own
frontmatter is merged into that block, never left in the body as a second one
(#532): `description` from the source, else the humanised file name; `globs` from
the source's `globs`, else its `paths` (Claude Code's rule scoping), comma-joined;
`alwaysApply` from the source when it is a boolean, else `false` with globs and
`true` without. Other source fields are dropped. See `docs/artifacts.md#rules`.

## Key CLI Details

- Binary: `agent` (installed via `curl https://cursor.com/install -fsS | bash`)
- Non-interactive: `agent -p "prompt"`
- Environment vars: `$CURSOR_PROJECT_DIR`, `$CURSOR_ENV_FILE`, `$CURSOR_WORKSPACE_DIR`

## What Cursor Supports That ynh Maps

- Skills: YES (skills/<name>/SKILL.md)
- Agents/subagents: YES (agents/<name>.md) — CONFIRMED (cursor.com/docs/subagents):
  reads `name`/`description` frontmatter (required — `description` drives delegation
  routing), plus optional `model`/`readonly`/`is_background`. ynh's delegate generator
  (`internal/assembler/delegates.go`) already emits `name`+`description`.
- Rules: YES (.cursor/rules/<name>.mdc) — FIXED: ynh now writes `.mdc` with frontmatter
- Commands: YES (commands/<name>.md)
- Hooks: YES. `.cursor/hooks.json` in the run assembly, `hooks/cursor.json` (named by the manifest `hooks` field) in an export, same format and event names; never both in one output (#454, #469)
- MCP: YES. `.cursor/mcp.json` in the run assembly, `mcp.json` (plugin root, no dot) in an export, same content; never both in one output (#470)
- Marketplace: YES (.cursor-plugin/marketplace.json)
- .agents/skills/: PARTIAL — Cursor reads `.agents/skills/` but NOT `.agents/rules/` or other subdirs

## Known ynh Discrepancies (as of 2026-08-19)

All four tracked discrepancies (#196, #197, #198, #200) are resolved as of this note —
see the FIXED/CONFIRMED markers above.
