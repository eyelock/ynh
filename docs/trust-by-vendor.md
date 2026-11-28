<!-- Reference. Each row was run on a build of ynh v0.18.0 (develop), 2026-11-27, with stand-in vendor CLIs on PATH that print the arguments ynh passes them, comparing an installed, accepted harness with the same harness run from its folder. -->

# What an Accepted Harness Gets, by Vendor

Some approvals let a run act without asking you. ynh gives them only to a harness that is **trusted**:
installed, and accepted (or with nothing to accept). A run from a folder is never trusted, because ynh
does not check its own commands.

## On every vendor

| What | Accepted, installed harness | Run from a folder |
|---|---|---|
| Runs at all | yes | yes; ynh prints `<name> runs from a folder, so its own commands are not trust-checked; the commands its remote includes bring are.` |
| The workspace folders it asks for, when no workspace allow-list is set | granted: `workspace: granted <folder> (harness trust-accepted)` | not granted: `warning: workspace: "<folder>" not granted: ... a harness run from a path has no trust record` |
| `ynh agent run --harness <harness> --auto-approve edits` or `all` | runs | refused, exit 1: `Error: refusing --auto-approve edits: <name> runs from a folder, which is not trust-checked, so nothing it executes is vouched for.` |

## By vendor

| Vendor | What an accepted, installed harness gets on a one-shot run | A run from a folder |
|---|---|---|
| Claude Code | the harness's own MCP servers' tools allowed (`--allowedTools=mcp__plugin_<harness>_<server>`, or `mcp__<server>` when the harness isolates its MCP servers), unless the manifest or the profile that runs sets `mcp_manual_approval` | none: Claude asks, or allow the tools yourself with `-- --allowedTools=...` or in your Claude settings |
| Codex | its own MCP servers' tools approved (`-c mcp_servers.<server>.default_tools_approval_mode="approve"`), unless the manifest sets `mcp_manual_approval`; its hooks trusted (`-c hooks.state=...`) | neither: `MCP tools are not pre-approved for <name>`, and `warning: codex: hooks not trusted, so Codex will skip them` |
| Copilot CLI | the run, with `--allow-all-tools`, which approves every tool | refused, exit 1: `Error: refusing to run <name> one-shot on copilot: a run from a folder is not trust-checked, and copilot's one-shot mode approves every tool.` |
| Cursor | `--trust`, so Cursor's Workspace Trust does not prompt | no `--trust` |
| OpenCode | nothing beyond every vendor's | the same |

A one-shot Claude Code run of a trusted harness, and an `ynh agent run` worker on Claude Code, get the
allow rule for the harness's own MCP servers: see [MCP Servers](mcp.md#claude-code-format).

The vendor is not part of what you accept: one acceptance holds on every vendor, and what it unlocks
is the row for the vendor the run uses.

## Related

- [Why ynh asks you to trust a harness](why-trust.md#runs-from-a-folder): why a folder run is never trusted.
- [MCP Servers](mcp.md), [Hooks: Codex hook trust](hooks.md#codex-hook-trust), [Workspace Folders](workspace.md) and [Agent Loop](agent.md).
