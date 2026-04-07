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

IMPORTANT: ynh currently uses the legacy flat format with the plugin event names
(beforeShellExecution, afterFileEdit, beforeSubmitPrompt, stop) and writes to
`.cursor/hooks.json`. This needs verification — does Cursor read `.cursor/hooks.json`
the same way as `hooks/hooks.json` inside a plugin? The event name mapping may differ.

## MCP Format

Project: `.cursor/mcp.json`
User: `~/.cursor/mcp.json`
Plugin: `mcp.json` (at plugin root, NO dot prefix — differs from Claude's `.mcp.json`)

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
ynh currently writes `.md` rules — Cursor expects `.mdc` with frontmatter. NEEDS RESEARCH on whether plain `.md` rules are read.

## Key CLI Details

- Binary: `agent` (installed via `curl https://cursor.com/install -fsS | bash`)
- Non-interactive: `agent -p "prompt"`
- Environment vars: `$CURSOR_PROJECT_DIR`, `$CURSOR_ENV_FILE`, `$CURSOR_WORKSPACE_DIR`

## What Cursor Supports That ynh Maps

- Skills: YES (skills/<name>/SKILL.md)
- Agents/subagents: YES (agents/<name>.md) — delegation support NEEDS RESEARCH
- Rules: YES (.cursor/rules/<name>.mdc) — format mismatch with ynh (.md vs .mdc)
- Commands: YES (commands/<name>.md)
- Hooks: YES (two formats — plugin vs settings)
- MCP: YES (.cursor/mcp.json or mcp.json in plugin)
- Marketplace: YES (.cursor-plugin/marketplace.json)
- .agents/skills/: PARTIAL — Cursor reads `.agents/skills/` but NOT `.agents/rules/` or other subdirs

## Known ynh Discrepancies (as of 2026-04-07)

- ynh writes hooks to `.cursor/hooks.json` with flat format — may need plugin-format `hooks/hooks.json` for plugins
- ynh writes rules as `.md` — Cursor expects `.mdc` with frontmatter (globs, alwaysApply)
- ynh writes MCP to `.cursor/mcp.json` — correct for project, but plugin format uses `mcp.json` (no dot prefix)
- Cursor delegation/subagent support needs further research
