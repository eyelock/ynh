# Claude Code (Anthropic) — Vendor Reference

## Documentation URLs

- CLI Reference: https://code.claude.com/docs/en/cli-reference
- Plugins Overview: https://code.claude.com/docs/en/plugins
- Plugins Reference: https://code.claude.com/docs/en/plugins-reference
- Plugin Marketplaces: https://code.claude.com/docs/en/plugin-marketplaces
- Hooks Guide: https://code.claude.com/docs/en/hooks-guide
- MCP Servers: https://code.claude.com/docs/en/mcp
- Settings: https://code.claude.com/docs/en/settings
- Subagents: https://code.claude.com/docs/en/sub-agents
- Official Plugins: https://github.com/anthropics/claude-plugins-official

## Plugin Format

Manifest: `.claude-plugin/plugin.json`
Only `name` is required. Optional: `version`, `description`, `author`, `homepage`, `repository`, `license`, `keywords`.

Component pointers in manifest (paths relative to plugin root, replace defaults):
- `skills` — path to skills directory
- `commands` — path to commands directory (legacy, prefer skills)
- `agents` — path to agents directory
- `hooks` — path to hooks config or inline object
- `mcpServers` — path to MCP config or inline object
- `lspServers` — path to LSP config or inline object
- `outputStyles` — path to output styles
- `userConfig` — user-configurable options (substituted into configs)

## Plugin Directory Structure

```
plugin-root/
  .claude-plugin/plugin.json    (manifest, only name required)
  skills/<name>/SKILL.md        (agent skills)
  commands/<name>.md            (legacy skills)
  agents/<name>.md              (subagents)
  hooks/hooks.json              (hook config)
  .mcp.json                     (MCP servers)
  .lsp.json                     (LSP servers)
  bin/                          (executables added to PATH)
  settings.json                 (only "agent" key supported)
```

## Hook Events (25)

SessionStart, UserPromptSubmit, PreToolUse, PermissionRequest, PermissionDenied,
PostToolUse, PostToolUseFailure, Notification, SubagentStart, SubagentStop,
TaskCreated, TaskCompleted, Stop, StopFailure, TeammateIdle, InstructionsLoaded,
ConfigChange, CwdChanged, FileChanged, WorktreeCreate, WorktreeRemove,
PreCompact, PostCompact, Elicitation, ElicitationResult, SessionEnd

## Hook Types

command, http, prompt, agent

## Hook Format (hooks/hooks.json or settings.json)

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {"type": "command", "command": "/path/to/script.sh", "timeout": 600}
        ]
      }
    ]
  }
}
```

## MCP Format (.mcp.json at plugin root or project root)

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

## Marketplace Format (.claude-plugin/marketplace.json)

```json
{
  "name": "marketplace-name",
  "owner": {"name": "Org"},
  "plugins": [
    {"name": "plugin-name", "source": "./plugins/plugin-name", "description": "...", "version": "1.0.0"}
  ]
}
```

## Key CLI Flags

- `--plugin-dir <path>` — load plugin for session (repeatable)
- `--add-dir <path>` — grant read access to directory
- `--append-system-prompt <text>` — inject instructions
- `--bare` — skip auto-discovery of hooks, skills, plugins, MCP
- `--mcp-config <path>` — load MCP servers from file
- `--permission-mode <mode>` — default, auto, plan, dontAsk, bypassPermissions
- `--dangerously-skip-permissions` — skip tool execution prompts

## Known Limitations for ynh

- `--plugin-dir` auto-activates skills/commands but NOT hooks/MCP (need `/plugin enable` + `/reload-plugins`)
- Plugin `settings.json` only supports `agent` key (not hooks)
- Claude doesn't read AGENTS.md natively — export writes CLAUDE.md with `@AGENTS.md` import to bridge this
- Environment vars available: `${CLAUDE_PLUGIN_ROOT}`, `${CLAUDE_PLUGIN_DATA}`
