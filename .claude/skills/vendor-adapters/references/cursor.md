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

- Plugin: `hooks/hooks.json` (inside plugin dir)
- Project: `.cursor/settings.json` (committable)
- Project-local: `.cursor/settings.local.json` (gitignored)
- User: `~/.cursor/settings.json`

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
`hooks/hooks.json` at plugin root. ynh's Cursor adapter (`Cursor.GenerateHookConfig`)
now writes both paths with identical content.

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
FIXED: ynh writes both — `Cursor.GenerateMCPConfig` emits identical content to both paths.

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
- Hooks: YES — FIXED: ynh writes both `.cursor/hooks.json` (project) and `hooks/hooks.json` (plugin root), same format/event names in both
- MCP: YES — FIXED: ynh writes both `.cursor/mcp.json` (project) and `mcp.json` (plugin root, no dot)
- Marketplace: YES (.cursor-plugin/marketplace.json)
- .agents/skills/: PARTIAL — Cursor reads `.agents/skills/` but NOT `.agents/rules/` or other subdirs

## Known ynh Discrepancies (as of 2026-08-19)

All four tracked discrepancies (#196, #197, #198, #200) are resolved as of this note —
see the FIXED/CONFIRMED markers above.
