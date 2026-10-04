# Cursor — Vendor Reference

## Documentation URLs

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

## Hook Events (25 — same as Claude Code)

SessionStart, UserPromptSubmit, PreToolUse, PermissionRequest, PermissionDenied,
PostToolUse, PostToolUseFailure, Notification, SubagentStart, SubagentStop,
TaskCreated, TaskCompleted, Stop, StopFailure, TeammateIdle, InstructionsLoaded,
ConfigChange, CwdChanged, FileChanged, WorktreeCreate, WorktreeRemove,
PreCompact, PostCompact, Elicitation, ElicitationResult, SessionEnd

## Hook Types

command, http, prompt, agent (same as Claude Code)

## Hook Formats (TWO different formats)

**Plugin hooks/hooks.json** — flat/legacy format with lowercase event names:
```json
{
  "hooks": {
    "beforeShellExecution": [
      {"command": "./scripts/validate-shell.sh", "matcher": "rm|curl|wget"}
    ],
    "afterFileEdit": [
      {"command": "./scripts/format-code.sh"}
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
`hooks/hooks.json` at plugin root. ynh renders the same document for both and writes
each to its own context only (see the table under Hook Config Paths). ynh's plugin
file is `hooks/cursor.json` via the manifest `hooks` field, not the default
`hooks/hooks.json`: Claude Code always loads a plugin root's `hooks/hooks.json`, so in
a merged package it would read Cursor's format (#469).

Full supported event list confirmed via docs: `sessionStart`, `sessionEnd`,
`preToolUse`, `postToolUse`, `postToolUseFailure`, `subagentStart`, `subagentStop`,
`beforeShellExecution`, `afterShellExecution`, `beforeMCPExecution`,
`afterMCPExecution`, `beforeReadFile`, `afterFileEdit`, `beforeSubmitPrompt`,
`preCompact`, `stop`, `afterAgentResponse`, `afterAgentThought` (plus Tab hooks
`beforeTabFileRead`/`afterTabFileEdit` and app-lifecycle `workspaceOpen`, not
currently mapped by ynh). ynh's canonical map covers five events:
`before_tool`, `after_tool`, `before_prompt`, `on_stop` and `on_session_start`.

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
`Cursor.TransformArtifact`) renames `.md` → `.mdc` and injects
`description`/`alwaysApply: true` frontmatter at copy time (both `ynh run` staging and
`ynh export`). No `globs` is emitted — ynh has no per-rule glob metadata to source it
from.

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
