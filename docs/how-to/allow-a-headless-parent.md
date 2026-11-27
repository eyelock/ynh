# Allow a headless parent to reach its delegates

Your parent runs headless (`claude -p`, `codex exec`, `copilot -p`, `opencode run`), and its model cannot
call a delegate, or a delegate's MCP tools are never called: the vendor denies a tool until it is allowed.
After these steps it can.

Which route the parent uses is its choice; this page covers both. For the tools and the route, see
[Delegates reference](../delegates-reference.md#the-ynh-delegates-mcp-server).

## Before you start

The parent has an `a2a` delegate that is installed and trusted ([Add a delegate](add-a-delegate.md)). For a
`native` delegate, go to [A native delegate on Claude Code](#a-native-delegate-on-claude-code).

## Steps

1. Run the check. Its warnings say which rule is missing for your vendor, with the fix:

   ```
   ynh doctor
   ```

   It reads the vendor's settings files, so it cannot see a rule you pass on a command line and warns
   anyway.

2. Allow the MCP tools (route 1), for the vendor your parent runs on. A flag goes after the harness and ynh
   passes it to the vendor, so for Claude Code `ynh run <parent> --allowedTools=mcp__plugin_<harness>_ynh-delegates "<prompt>"`
   works as an alternative to the settings entry:

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

> **Known issue.** A headless parent answers a delegate's question with its model, not a person
> ([known limit](../vendor-parity.md#l39-a-headless-parent-s-model-answers-a-delegate-s-question-not-a-person)).
> If the delegate's questions need a person, run the parent interactively.

## A native delegate on Claude Code

A `native` delegate runs as a subagent inside a Claude Code parent and brings its own MCP servers. A
headless parent calls their tools only with an allow rule for each server, by the name the delegate's
manifest gives it. `ynh doctor` does not check this one.

1. Find the delegate's server names: the keys of `mcp_servers` in its `.agents/harness/plugin.json`.

2. Allow each server as `mcp__<server>`. For a delegate with a server named `probe`, with the flag (ynh
   passes it to Claude Code; write it with `=` so the prompt is not read as its value):

   ```
   ynh run <parent> --allowedTools=mcp__probe "<prompt>"
   ```

   For several servers, separate them with commas: `--allowedTools=mcp__probe,mcp__tickets`. Or add
   `mcp__probe` to `permissions.allow` in `~/.claude/settings.json`, `.claude/settings.json` or
   `.claude/settings.local.json`. It is not the `mcp__plugin_<harness>_<server>` form, and it is not the
   `ynh-delegates` rule from step 2 above.

Without the rule, the delegate's server starts and none of its tools is called. Codex, Copilot CLI and
OpenCode carry no MCP servers into a native delegate, so on them there is nothing to allow
([delegates carry only part of a harness](../vendor-parity.md#l12-delegates-carry-only-part-of-a-harness)).

## Checked against

[Allow rules for the tools](../delegates-reference.md#allow-rules-for-the-tools) and
[The a2a CLI route](../delegates-reference.md#the-a2a-cli-route) at v0.18.0 (develop `4c05c4b5`),
2026-11-15, and `ynh run --help` for how a flag reaches the vendor. Not run on any vendor. The native
delegate section, against
[Allow rules for a native delegate's MCP servers](../delegates-reference.md#allow-rules-for-a-native-delegate-s-mcp-servers)
and limit L12 on develop `31909df7`, 2026-11-27; the `--allowedTools mcp__<server>` rule was shown live
on Claude Code in the v0.18.0 acceptance, 2026-11-26, and the `permissions.allow` form was not run.
