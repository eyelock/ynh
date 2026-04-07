# OpenAI Codex — Vendor Reference

## Documentation URLs

- Plugins Overview: https://developers.openai.com/codex/plugins
- Plugin Build Guide: https://developers.openai.com/codex/plugins/build
- Hooks: https://developers.openai.com/codex/hooks
- CLI Reference: https://developers.openai.com/codex
- GitHub Repo: https://github.com/openai/codex
- Hook Schemas: https://github.com/openai/codex/tree/main/codex-rs/hooks/schema/generated

## Plugin Format

Manifest: `.codex-plugin/plugin.json` (required)
Required fields: `name`, `version`, `description`.
Optional: `author`, `homepage`, `repository`, `license`, `keywords`, `skills`, `mcpServers`, `apps`, `interface`.

Component pointers in manifest:
- `skills` — path to skills directory (e.g. `"./skills/"`)
- `mcpServers` — path to MCP config (e.g. `"./.mcp.json"`)
- `apps` — path to app config (e.g. `"./.app.json"`) — Codex-specific, no equivalent in Claude/Cursor
- `interface` — display metadata for marketplace (displayName, shortDescription, category, brandColor, logos, screenshots)

## Plugin Directory Structure

```
plugin-root/
  .codex-plugin/plugin.json     (manifest, required)
  skills/<name>/SKILL.md        (agent skills)
  .mcp.json                     (MCP servers)
  .app.json                     (app connectors — Codex-specific)
  assets/                       (icons, logos, screenshots)
```

## Hook Config Paths

- Repo-level: `<repo>/.codex/hooks.json`
- User-level: `~/.codex/hooks.json`
- Feature flag required: `[features] codex_hooks = true` in `config.toml`
- Status: **Experimental**

## Hook Events (5)

SessionStart, PreToolUse, PostToolUse, UserPromptSubmit, Stop

## Hook Types

command only (no http, prompt, or agent)

## Hook Format (.codex/hooks.json)

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "/path/to/script.py",
            "statusMessage": "Checking command",
            "timeout": 600
          }
        ]
      }
    ],
    "SessionStart": [
      {
        "matcher": "startup|resume",
        "hooks": [
          {"type": "command", "command": "/path/to/init.py", "statusMessage": "Loading session"}
        ]
      }
    ]
  }
}
```

Extra fields vs Claude: `statusMessage` (display text), `timeoutSec` (alias for timeout).
Matchers: PreToolUse/PostToolUse filter on tool_name. SessionStart filters on source (startup|resume). UserPromptSubmit/Stop: matcher not supported.
Multiple matching hooks run **concurrently** (not sequentially).
Output **must** be JSON on stdout when exit 0.

## MCP Format (.mcp.json at plugin root)

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

## Marketplace Format (.agents/plugins/marketplace.json)

```json
{
  "name": "local-example-plugins",
  "interface": {"displayName": "Local Example Plugins"},
  "plugins": [
    {
      "name": "my-plugin",
      "source": {"source": "local", "path": "./plugins/my-plugin"},
      "policy": {"installation": "AVAILABLE", "authentication": "ON_INSTALL"},
      "category": "Productivity"
    }
  ]
}
```

Index paths:
- Repo: `$REPO_ROOT/.agents/plugins/marketplace.json`
- Personal: `~/.agents/plugins/marketplace.json`
- Plugin cache: `~/.codex/plugins/cache/$MARKETPLACE/$PLUGIN/$VERSION/`

Note: Different format from Claude/Cursor — uses `source` object with `source`/`path`, has `policy` object.

## Key CLI Details

- Binary: `codex`
- Non-interactive: `codex exec "prompt"`
- Requires git working tree — use `--skip-git-repo-check` to bypass
- Plugin on/off: `~/.codex/config.toml` under `[plugins."name@marketplace"]`
- User config: `~/.codex/config.toml` or `config.yaml` or `config.json`

## What Codex Does NOT Support (in plugins)

- Agents/subagents — NOT supported in plugin format
- Rules — NOT supported in plugin format
- Commands — NOT supported (use skills)
- Delegates — NOT supported
- LSP servers — NOT documented

## Known ynh Discrepancies (as of 2026-04-07)

- ynh does NOT generate `.codex-plugin/plugin.json` — NEEDS FIX
- ynh exports skills to `.agents/skills/` — should be `skills/` at plugin root — NEEDS FIX
- ynh writes MCP to `.codex/config.toml` (TOML) — should be `.mcp.json` (JSON) — NEEDS FIX
- ynh excludes Codex from marketplace generation — Codex now supports it — NEEDS FIX
- ynh hook format matches Codex spec — OK
- ynh hook path `.codex/hooks.json` matches Codex spec — OK
