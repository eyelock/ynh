# OpenAI Codex — Vendor Reference

## Documentation URLs

- Plugins Overview: https://developers.openai.com/codex/plugins
- Plugin Build Guide: https://developers.openai.com/codex/plugins/build
- Hooks: https://learn.chatgpt.com/docs/hooks (https://developers.openai.com/codex/hooks redirects there)
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
- Plugin: `hooks/hooks.json` at the plugin root by default; a `hooks` value (path, inline
  object or array) replaces that default-file discovery. A legacy package declares it in
  `.codex-plugin/plugin.json` (CONFIRMED 2026-10-04,
  developers.openai.com/codex/plugins/build "Bundled MCP servers and lifecycle hooks").
  Plugin hooks are non-managed and skipped until the user trusts them in `/hooks`.
- ynh writes `.codex/hooks.json` for a session (run, preview, agent loop) and
  `hooks/codex.json`, named by the manifest `hooks` field, in an export (#469)
- Plugin hook commands receive `PLUGIN_ROOT` and `PLUGIN_DATA`, plus `CLAUDE_PLUGIN_ROOT`
  and `CLAUDE_PLUGIN_DATA` for compatibility (CONFIRMED 2026-10-04,
  developers.openai.com/codex/plugins/build). Its "Hook paths start with `./`, resolve
  relative to the plugin root" is about the manifest's hooks file paths, not command
  strings, and the hook's working directory is not documented. So the plugin file
  anchors a `./` command to `"${PLUGIN_ROOT}"/` and the exporter copies the script into
  the plugin (#483). The session file keeps the command as written: hooks "run with the
  session `cwd` as their working directory" (CONFIRMED 2026-10-04,
  developers.openai.com/codex/hooks), `launchCodex` sets that to the run dir, and
  `assembler.WriteSessionHooks` copies the script to the run dir's root (#495)
- Feature flag required: `[features] codex_hooks = true` in `config.toml`
- Status: **Experimental**

## Hook Events (12)

SessionStart, SessionEnd, SubagentStart, PreToolUse, PermissionRequest, PostToolUse,
PreCompact, PostCompact, UserPromptSubmit, SubagentStop, Stop, Interrupt
(https://learn.chatgpt.com/docs/hooks). ynh maps six: `before_tool` PreToolUse,
`after_tool` PostToolUse, `before_prompt` UserPromptSubmit, `on_stop` Stop,
`on_session_start` SessionStart, `on_session_end` SessionEnd.

`SessionEnd`: main thread only (not subagents); runs when a conversation is archived or
deleted, when Codex closes normally, or after 30 minutes idle with no connected client.
Default timeout 1 s, maximum 3 s (`timeout` field, seconds; most other hooks default to
600). Always synchronous, even with `async`. Output is advisory. Its matcher filters
`reason`, currently always `other`. Matchers: tool name for PreToolUse, PostToolUse and
PermissionRequest; trigger for PreCompact and PostCompact; source (`startup`, `resume`,
`clear`, `compact`, `fork`) for SessionStart; none for UserPromptSubmit, Stop and
Interrupt. Non-managed hooks must be trusted via `/hooks` before they run.

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

## Runtime Custom Agents (Subagents)

Codex has subagents at runtime, outside plugins: custom agents, one standalone
TOML file each, in `.codex/agents/` for a project or `~/.codex/agents/` for the
user ([Codex's subagent docs](https://learn.chatgpt.com/docs/agent-configuration/subagents), where the old
developers.openai.com/codex/subagents link redirects).

- Every file must set `name`, `description` and `developer_instructions`, all strings. `name` identifies the agent; naming the file after it is the convention. The docs name only TOML files there, so the Markdown agent ynh writes for other vendors would not be one.
- A custom agent inherits the parent's permissions (sandbox and approvals), model provider and MCP servers; `mcp_servers` in the agent file does not override them.
- ynh writes each delegate (`delegates_to`) in a run as `.codex/agents/<name>.toml` with those three fields (`DelegateAgentFile` in `internal/vendor/codex_agent.go`, through the `DelegateAgentEncoder` interface in `internal/assembler/delegates.go`). A delegate's own MCP servers have no field, so ynh warns and the subagent has the session's servers instead (`docs/mcp.md`, "MCP Servers of Delegates").
- ynh writes a harness's own agents (`agents/<name>.md`) in a run as `.codex/agents/<name>.toml` too (`TransformArtifact` in `internal/vendor/codex_agent.go`, through the `ArtifactTransformer` interface in `internal/assembler/assembler.go`), sharing the delegates' TOML encoder. `name` and `description` come from the frontmatter (falling back to the file name, and to a description naming the agent), `developer_instructions` from the body. `effort` `low`/`medium`/`high` becomes `model_reasoning_effort` at the same level (the mapping in `internal/agent/effort.go` that `ynh agent run --effort` uses); `xhigh` and `max` are dropped, as ynh maps only those three. Codex has no tool allowlist, so `tools` and `disallowedTools` are dropped, and assembly prints a warning naming the agent for each one it declares (`ArtifactWarnings`, through the `ArtifactWarner` interface in `internal/assembler/assembler.go`). Assembly keys the warnings by the file written, so an agent the root overrides from an include warns once, about the root's copy. One part of `tools` is kept: a list that leaves out `Bash` (whole or as `Bash(...)`) writes `features.shell_tool = false`, so the agent has no shell tool. That a custom agent file honours `features.shell_tool` has not been checked live (ynw-s9h). Every other field is dropped: Claude Code's `model` names are not Codex models. A Codex export carries no agents (skipped with a warning).
- `ynd export` writes no Codex delegates (`SupportsExportDelegates()` is `false`): this is the run assembly only.
- Read from Codex's documentation; that a live `codex` picks up the agent has not been checked (ynw-s9h).

## What Codex Does NOT Support (in plugins)

- Agents/subagents — NOT supported in plugin format (runtime custom agents are, and a run writes a harness's agents as them; see above)
- Rules — NOT supported in plugin format
- Commands — NOT supported (use skills)
- Delegates — NOT exported in a plugin; a run writes them as custom agents (see above)
- LSP servers — NOT documented

## Known ynh Discrepancies (as of 2026-07-30)

All discrepancies noted on 2026-04-07 were fixed in the same PR (#23, "modernize Codex adapter to current plugin spec"):

- ynh generates `.codex-plugin/plugin.json` — OK
- The adapter reports two different directories: `PluginManifestDir()` is `.codex-plugin` and `MarketplaceManifestDir()` is `.agents/plugins`. Codex keeps the two apart, so neither can stand in for the other (#453)
- ynh exports skills to `skills/` at plugin root — OK
- ynh writes MCP to `.mcp.json` (JSON) — OK
- ynh includes Codex in marketplace generation (`.agents/plugins/marketplace.json`) and merged export — OK
- ynh hook format matches Codex spec — OK
- ynh hook path `.codex/hooks.json` matches Codex spec for a session: OK
- ynh plugin hook path: an export used to write `.codex/hooks.json`, which a Codex plugin never reads; it now writes `hooks/codex.json` and names it in the manifest `hooks` field (#469)

No known discrepancies at this time.
