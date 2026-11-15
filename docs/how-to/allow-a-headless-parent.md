# Allow a headless parent to reach its delegates

Your parent runs headless (`claude -p`, `codex exec`, `copilot -p`, `opencode run`), and its model cannot
call a delegate: the vendor denies the tool until it is allowed. After these steps it can.

Which route the parent uses is its choice; this page covers both. For the tools and the route, see
[Delegates reference](../delegates-reference.md#the-ynh-delegates-mcp-server).

## Before you start

The parent has an `a2a` delegate that is installed and trusted ([Add a delegate](add-a-delegate.md)).

## Steps

1. Run the check. Its warnings say which rule is missing for your vendor, with the fix:

   ```
   ynh doctor
   ```

   It reads the vendor's settings files, so it cannot see a rule you pass on a command line and warns
   anyway.

2. Allow the MCP tools (route 1), for the vendor your parent runs on:

   | Parent | What to do |
   |---|---|
   | Claude Code | add `mcp__plugin_<harness>_ynh-delegates` to `permissions.allow` in `~/.claude/settings.json`, `.claude/settings.json` or `.claude/settings.local.json`; for the harness `boss` that is `mcp__plugin_boss_ynh-delegates`. Or pass `--allowedTools mcp__plugin_<harness>_ynh-delegates` |
   | Codex | nothing: ynh sets `mcp_servers.ynh-delegates.default_tools_approval_mode="approve"` for a one-shot run, unless your config or `-c` already sets it |
   | Copilot CLI | allow the tool in `~/.copilot/config.json`, `~/.copilot/settings.json` or `.copilot/settings.json`, or pass `--allow-tool=ynh-delegates` |
   | OpenCode | nothing |
   | Cursor | not known: the route has not been run on Cursor |

3. Allow the shell route instead (route 2) if the parent uses the `a2a` CLI:

   | Parent | What to do |
   |---|---|
   | Claude Code | `Bash(a2a:*)` in `permissions.allow`, or `--allowedTools 'Bash(a2a:*)'` |
   | Copilot CLI | `--allow-tool 'shell(a2a:*)'` |
   | Codex | in `~/.codex/config.toml`, `sandbox_mode = "workspace-write"` and, under `[sandbox_workspace_write]`, `network_access = true` |
   | OpenCode | nothing |

   One rule covers every command, for any number of delegates. For the `a2a` binary itself and the Codex
   cost, see [Reach delegates with the a2a CLI](reach-delegates-with-the-a2a-cli.md).

4. Run `ynh doctor` again. A rule given on a command line is still warned about; the check is only for
   settings files.

## If a delegate has a question

A headless parent answers a delegate's question with its model, not a person. See
[L39](../vendor-parity.md#l39-a-headless-parent-s-model-answers-a-delegate-s-question-not-a-person).

## Checked against

[Allow rules for the tools](../delegates-reference.md#allow-rules-for-the-tools) and
[The a2a CLI route](../delegates-reference.md#the-a2a-cli-route) at v0.18.0 (develop `4c05c4b5`),
2026-11-15; not run by Visconti on any vendor. The record does not say how a flag is handed through
`ynh run` to a vendor; use the settings files above if unsure.
