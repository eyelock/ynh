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

Component pointers in manifest (paths relative to plugin root, must start with `./`). How
each combines with its default differs (CONFIRMED 2026-10-04, plugins-reference
"How each key combines with its default location"):
- `skills`: path to skills directory (adds to the default `skills/` scan)
- `commands`: path to commands directory (legacy, prefer skills; replaces the default)
- `agents`: path to agents directory (replaces the default)
- `hooks`: `.json` file path, inline object, or array of either. MERGES with
  `hooks/hooks.json`: the default file loads whenever it exists, even when the manifest
  names another. A hooks file wraps its event map in a top-level `"hooks"` key.
- `mcpServers`: `.json` file path, `.mcpb`/`.dxt` bundle path or URL, inline map, or
  array of any of them. MERGES with `.mcp.json`: the root `.mcp.json` loads first, then
  each declared shape in order, and a server name declared later replaces an earlier one.
  A named `.json` file is read as an `mcpServers` map (top-level `"mcpServers"` key, as
  in `.mcp.json`).
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
  hooks/hooks.json              (hook config, default; ynh exports use hooks/claude.json via the manifest)
  .mcp.json                     (MCP servers, default; ynh exports use mcp/claude.json via the manifest)
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

ynh writes this document to `.claude/.mcp.json` for a session (`ynh run` passes
`.claude/` to `--plugin-dir`, so it is that plugin's root `.mcp.json`) and to
`mcp/claude.json` in an exported plugin, named by the manifest's `mcpServers`
(#481). An export never uses the root `.mcp.json`: Codex keeps its own there in a
merged package, and Copilot reads it too. Because the root file loads first and
merges, Claude in a merged package also sees Codex's `.mcp.json`; the two
documents are byte-identical today, so the merge is a no-op.

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

## Hook Config Paths in ynh (#468, #469)

| ynh output | File | Generator |
|------------|------|-----------|
| `ynh run`, `ynd preview`, agent loop | `.claude/hooks/hooks.json` (`ynh run` passes `.claude/` as `--plugin-dir`, so this is that plugin's default `hooks/hooks.json`) | `Claude.GenerateHookConfig` |
| `ynd export -v claude`, `ynd marketplace build` (plugin) | `hooks/claude.json`, named by `"hooks"` in `.claude-plugin/plugin.json` | `Claude.GeneratePluginHookConfig`; `claudePluginManifest` adds the pointer when the file exists |

ynh never writes `hooks/hooks.json` into a plugin. Because Claude merges that default with
the manifest's `hooks`, a merged package holding Cursor's or Codex's file there would hand
Claude another vendor's format. Copilot renders the same `.claude-plugin/plugin.json` via
`claudePluginManifest`, so the pointer survives whichever adapter writes it last.

Hook command paths (#483, #495): a command starting with `./` names a script the harness
ships. Both `GenerateHookConfig` (session) and `GeneratePluginHookConfig` (plugin) anchor it
to `"${CLAUDE_PLUGIN_ROOT}"/` (`pluginRootCommand`). In a session that is the `.claude/`
directory `ynh run` passes as `--plugin-dir`, which Claude loads in place (CONFIRMED
2026-10-04, code.claude.com/docs/en/plugins/loading "In-place and copied plugins"), so
`assembler.WriteSessionHooks` copies the script there (`Claude.SessionHookScriptDir`). In a
plugin the exporter copies it to the plugin root. Both use `assembler.CopyHookScripts`,
which warns when the script is not a file in the harness. `ClaudeSettingsHooks`, used by
`ynh hook export` for the project's `.claude/settings.json`, anchors to
`$CLAUDE_PROJECT_DIR/` instead (`anchorHookCommand`). Absolute, variable-anchored and
PATH-style commands are untouched everywhere.

## Known Limitations for ynh

- `--plugin-dir` auto-activates skills/commands but NOT hooks/MCP (need `/plugin enable` + `/reload-plugins`)
- Plugin `settings.json` only supports `agent` key (not hooks)
- Claude doesn't read AGENTS.md natively — export writes CLAUDE.md with `@AGENTS.md` import to bridge this
- Environment vars available: `${CLAUDE_PLUGIN_ROOT}`, `${CLAUDE_PLUGIN_DATA}`
