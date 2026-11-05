# Vendor Feature Parity

What each vendor receives from a harness, on each surface ynh has: `ynh run`, `ynh agent run` and `ynd export`. Every cell that is not **Y** names a limit, and every limit says whose it is.

- **ynh version:** v0.17.0, as on `develop` at [f2822a1](https://github.com/eyelock/ynh/commit/f2822a1), which includes [ynh #721](https://github.com/eyelock/ynh/pull/721), [ynh #725](https://github.com/eyelock/ynh/pull/725), [ynh #726](https://github.com/eyelock/ynh/pull/726) and [ynh #727](https://github.com/eyelock/ynh/pull/727).
- **Checked on:** 2026-11-05. The verification is recorded in [ynh #716](https://github.com/eyelock/ynh/issues/716) and [ynh #735](https://github.com/eyelock/ynh/issues/735); each limit below links the pull request or run that proved it. Each column was checked by its own run:
  - **Claude Code** 2.1.318, **Codex** (codex-cli 0.166.0), **Copilot CLI** 1.0.105 and **OpenCode** 2.2.3: live runs through ynh at `develop` [958b7a3](https://github.com/eyelock/ynh/commit/958b7a3), and on the branches of the four pull requests above for the cells they changed.
  - **Cursor**, `ynd export` column: a package loaded live into cursor-agent 2026.11.05-86047df, at `develop` [c29ebd6](https://github.com/eyelock/ynh/commit/c29ebd6), whose export code is the same as at f2822a1. The record is on [ynh #735](https://github.com/eyelock/ynh/issues/735).
  - **Cursor**, `ynh run` and `ynh agent run` columns: not run live in this verification. cursor-agent stops at its Workspace Trust prompt in each harness's run directory ([Vendor Notes](vendors.md#vendor-notes)). Those cells are U where their limit says so; the others come from the pull requests their limits and feature pages cite, on cursor-agent 2026.10.22-27dcf5f, and were not re-checked.

This page is the only copy of this table. Other pages link here rather than repeat it. How each vendor is launched, and where its files go, is in [Vendor Support](vendors.md).

## Legend

| Mark | Meaning |
|---|---|
| **Y** | Works. |
| **P** | Works, with a limit. The limit says what is missing and whose it is. |
| **N** | Does not work, and is ynh's to fix. The limit names its issue and milestone, or says there is none yet. |
| **X** | Does not work because the vendor cannot do it. The limit gives the vendor's evidence. |
| **U** | Unverified. The limit says what blocks verification. |
| – | Does not apply: the surface does not exist for that vendor. |

A gap is marked **N** until there is evidence that the vendor cannot do it. Limit ids (`L1`, `L2`, ...) are never reused: a limit that is fixed keeps its id and says what fixed it.

In the [ynd export](#ynd-export) matrix, a cell says what `ynd export` writes into the package, in the form the vendor documents. Whether the vendor then loads the package is its own row, the first: it is Y where a package was loaded into the vendor live. An **X** below it is something a live load showed the vendor ignores, or that the vendor has no place for.

Columns are in the order `ynh vendors` lists the vendors.

## ynh run

What a harness delivers when you launch it with `ynh run` (or its launcher), interactive or one-shot.

| Feature | Claude Code | Codex | Copilot CLI | Cursor | OpenCode |
|---|---|---|---|---|---|
| Instructions | Y | Y | Y | Y | Y |
| Project instructions (the project's own `AGENTS.md` or `CLAUDE.md`) | Y | Y | Y | U [L1](#l1-project-instructions-on-cursor) | Y |
| Skills | Y | Y | Y | Y | Y |
| Agents | Y | P [L31](#l31-codex-agents-cannot-restrict-tools) | Y | Y | Y |
| Rules | P [L2](#l2-rules-are-always-on) | P [L2](#l2-rules-are-always-on) | P [L2](#l2-rules-are-always-on) | Y | P [L2](#l2-rules-are-always-on) |
| Commands | Y | P [L3](#l3-codex-commands-arrive-as-skills) | P [L32](#l32-copilot-commands-under-ynh-run) | Y | P [L4](#l4-opencode-commands-in-a-one-shot-prompt) |
| MCP servers | Y | Y | Y | Y | Y |
| MCP isolation (`mcp_isolation`, `--isolated-mcp`) | Y | N [L6](#l6-mcp-isolation) | P [L6](#l6-mcp-isolation) | N [L6](#l6-mcp-isolation) | N [L6](#l6-mcp-isolation) |
| Hooks | Y | P [L7](#l7-codex-hooks-need-an-accepted-harness) | Y | P [L8](#l8-cursor-print-mode-skips-two-hooks) | P [L9](#l9-opencode-has-no-session-end-event) |
| Session context (`on_session_start` output) | Y | P [L7](#l7-codex-hooks-need-an-accepted-harness) | Y | Y | Y |
| Profiles | Y | Y | Y | Y | Y |
| Focuses, includes, sensors | Y | Y | Y | Y | Y |
| Workspace folders | Y | P [L10](#l10-codex-workspace-folders-on-resume) | Y | U [L11](#l11-cursor-workspace-folders) | Y |
| `delegates_to` | P [L12](#l12-delegates-carry-only-part-of-a-harness) | P [L12](#l12-delegates-carry-only-part-of-a-harness) | P [L12](#l12-delegates-carry-only-part-of-a-harness) | P [L12](#l12-delegates-carry-only-part-of-a-harness) | N [L12](#l12-delegates-carry-only-part-of-a-harness) |
| Resume (`--resume`, with or without a prompt) | Y | P [L30](#l30-codex-resumes-a-prompt-one-shot-only) | Y | Y | Y |
| Telemetry from `ynh run` | N [L13](#l13-ynh-run-emits-no-telemetry) | N [L13](#l13-ynh-run-emits-no-telemetry) | N [L13](#l13-ynh-run-emits-no-telemetry) | N [L13](#l13-ynh-run-emits-no-telemetry) | N [L13](#l13-ynh-run-emits-no-telemetry) |

## ynh agent run

Where the backends of `ynh agent run` differ. Its flags and behaviour are in [The Agent Loop](agent.md).

| Feature | Claude Code | Codex | Copilot CLI | Cursor | OpenCode |
|---|---|---|---|---|---|
| Backend (`--backend`) | Y | Y | N [L14](#l14-copilot-is-not-a-backend) | Y | Y |
| Hooks | Y | N [L15](#l15-a-codex-worker-runs-no-hooks) | – | P [L8](#l8-cursor-print-mode-skips-two-hooks) | P [L9](#l9-opencode-has-no-session-end-event) |
| Session context (`on_session_start` output) | Y | N [L15](#l15-a-codex-worker-runs-no-hooks) | – | Y | Y |
| MCP isolation (`mcp_isolation`) | Y | N [L6](#l6-mcp-isolation) | – | N [L6](#l6-mcp-isolation) | N [L6](#l6-mcp-isolation) |
| Effort (`--effort`, `agent.effort`) | Y | Y | – | X [L16](#l16-cursor-and-opencode-have-no-effort-setting) | X [L16](#l16-cursor-and-opencode-have-no-effort-setting) |
| `--auto-approve edits` | Y | X [L17](#l17-auto-approve-edits) | – | X [L17](#l17-auto-approve-edits) | X [L17](#l17-auto-approve-edits) |
| `--auto-approve all` | Y | Y | – | Y | Y |
| `--sandbox srt` | Y | N [L18](#l18-sandbox-srt) | – | N [L18](#l18-sandbox-srt) | N [L18](#l18-sandbox-srt) |
| Telemetry (ynh's own spans and events) | Y | Y | – | Y | Y |
| Vendor telemetry through the relay (`--telemetry-relay`) | Y | Y | – | N [L19](#l19-vendor-telemetry-through-the-relay) | N [L19](#l19-vendor-telemetry-through-the-relay) |

## ynd export

What a package from `ynd export` carries, and whether the vendor loads it. The file each row becomes is in [Export Output by Vendor](vendors.md#export-output-by-vendor).

| Feature | Claude Code | Codex | Copilot CLI | Cursor | OpenCode |
|---|---|---|---|---|---|
| The vendor loads the package | Y | Y | Y | Y | X [L27](#l27-opencode-reads-no-packages) |
| Skills | Y | Y | Y | Y | – |
| Agents | Y | X [L22](#l22-a-codex-plugin-carries-only-skills-hooks-and-mcp-servers) | Y | Y | – |
| Rules | X [L23](#l23-rules-in-claude-code-and-copilot-packages) | X [L22](#l22-a-codex-plugin-carries-only-skills-hooks-and-mcp-servers) | X [L23](#l23-rules-in-claude-code-and-copilot-packages) | P [L26](#l26-cursor-packages-drop-instructions-rule-bodies-and-commands) | – |
| Commands | Y | X [L22](#l22-a-codex-plugin-carries-only-skills-hooks-and-mcp-servers) | P [L24](#l24-commands-in-a-copilot-package) | P [L26](#l26-cursor-packages-drop-instructions-rule-bodies-and-commands) | – |
| Instructions | X [L20](#l20-claude-code-and-copilot-plugins-carry-no-instructions) | X [L21](#l21-a-codex-plugin-carries-no-instructions) | X [L20](#l20-claude-code-and-copilot-plugins-carry-no-instructions) | X [L34](#l34-a-cursor-plugin-carries-no-instructions) | – |
| Hooks | Y | P [L25](#l25-codex-package-hooks-and-mcp-tools-need-approval) | Y | P [L8](#l8-cursor-print-mode-skips-two-hooks) | – |
| MCP servers | Y | P [L25](#l25-codex-package-hooks-and-mcp-tools-need-approval) | Y | P [L33](#l33-cursor-package-mcp-tools-need-approval-in-print-mode) | – |
| Delegates | P [L12](#l12-delegates-carry-only-part-of-a-harness) | X [L22](#l22-a-codex-plugin-carries-only-skills-hooks-and-mcp-servers) | P [L12](#l12-delegates-carry-only-part-of-a-harness) | P [L12](#l12-delegates-carry-only-part-of-a-harness) | – |
| Merged export (`--merged`) | Y | Y | Y | Y | X [L27](#l27-opencode-reads-no-packages) |
| Marketplace index (`ynd marketplace build`) | Y | Y | P [L28](#l28-copilot-marketplace-index) | Y | X [L27](#l27-opencode-reads-no-packages) |
| Agent Plugins package (`--format agent-plugin`) | P [L29](#l29-agent-plugins-on-claude-code-and-cursor) | Y | Y | P [L29](#l29-agent-plugins-on-claude-code-and-cursor) | X [L27](#l27-opencode-reads-no-packages) |

## Limits

### L1: Project instructions on Cursor

- **What:** whether Cursor reads the project's own instructions file under `ynh run` is unverified.
- **Owner:** unknown until verified.
- **Evidence:** the 2026-11-05 run stopped at cursor-agent's Workspace Trust prompt for the run directory, so no Cursor run completed. The same run showed Claude Code, Codex, Copilot CLI and OpenCode reading it. The run at `develop` c29ebd6 (cursor-agent 2026.11.05-86047df) was blocked the same way: each harness's run directory needs its own Workspace Trust ([Vendor Notes](vendors.md#vendor-notes)).
- **Tracking:** [ynh #716](https://github.com/eyelock/ynh/issues/716), v0.17.0. Verified live once Cursor's Workspace Trust is granted for the harness's run directory.
- **Workaround:** none recorded.
- **Detail:** [Project Instructions](artifacts.md#project-instructions).

### L2: Rules are always on

- **What:** on Claude Code, Codex and Copilot CLI, rules reach the model only by being appended to the instructions it reads. On OpenCode they are composed into `.opencode/AGENTS.md`. Either way a rule is always on, with no path scoping.
- **Owner:** the vendor. None of them reads a harness's rules from where ynh loads the harness; OpenCode 2 has no rules at all.
- **Evidence:** [ynh #695](https://github.com/eyelock/ynh/pull/695).
- **Tracking:** none.
- **Workaround:** on Cursor, rules are native `.mdc` files and keep their scoping.
- **Detail:** [Rules](artifacts.md#rules).

### L3: Codex commands arrive as skills

- **What:** Codex reads no command files from a project, so under `ynh run` each `commands/<name>.md` is written as a skill, `.codex/skills/<name>/SKILL.md`. Codex lists it as `<harness>:<name>` and you invoke it by name, not as a slash command. Frontmatter other than `description` is dropped, and `$ARGUMENTS` is not substituted. When the harness also has a skill with the same name, the skill is kept and the command is not written, with the warning `command "<name>": a skill of the same name exists, so the command is not written as a skill`.
- **Owner:** the vendor.
- **Evidence:** live on codex-cli 0.166.0, 2026-11-05, [ynh #721](https://github.com/eyelock/ynh/pull/721).
- **Tracking:** [ynh #720](https://github.com/eyelock/ynh/issues/720), v0.17.0.
- **Workaround:** write the command as a skill.
- **Detail:** [Commands](artifacts.md#commands).

### L4: OpenCode commands in a one-shot prompt

- **What:** `ynh run` with a prompt uses `opencode run`, which has no flag to run a command, so `/name` reaches the model as plain text. Commands do run through OpenCode's command API (`GET /api/command`, `POST /api/session/{id}/command`). The interactive TUI was not driven.
- **Owner:** the vendor.
- **Evidence:** live on OpenCode 2.2.3, 2026-11-05, [ynh #707](https://github.com/eyelock/ynh/pull/707).
- **Tracking:** none. Expanding `/name` inside ynh before `opencode run` was deferred past v0.17.0.
- **Workaround:** ask for the command in words, or ship it as a skill.
- **Detail:** [Vendor Notes](vendors.md#vendor-notes).

### L5: Codex MCP servers did not get env_passthrough (fixed)

- **What:** under Codex, a harness's MCP server did not receive the variables named in `env_passthrough`, so a server configured through one, such as ynm with `YNM_HOME`, fell back to its default (for ynm, the real `~/.ynm`). Fixed: ynh now names each `env_passthrough` variable to Codex with `-c mcp_servers.<name>.env_vars=[...]` for every stdio server, on one-shot, interactive, resumed and `ynh agent run` launches.
- **Owner:** ynh.
- **Evidence:** live on codex-cli 0.166.0, 2026-11-05: `YNM_HOME=<scratch> ynh run <harness> -v codex`, then ynm's `memory_status`, reported the default store before the fix and the scratch store after it, also with `--resume` and `ynh agent run --backend codex` ([ynh #726](https://github.com/eyelock/ynh/pull/726)). The interactive launch is covered by a launch-arguments test, not a live run.
- **Tracking:** [ynh #723](https://github.com/eyelock/ynh/issues/723), v0.17.0, fixed by [ynh #726](https://github.com/eyelock/ynh/pull/726).
- **Workaround:** none needed. A pre-assembled run directory (a `ynh image`) built without the passthrough names file forwards none.
- **Detail:** [Credentials](mcp.md#credentials-var-and-env-passthrough).

### L6: MCP isolation

- **What:** isolation runs with only the harness's MCP servers. Under `ynh run`, Copilot CLI is given `--disable-builtin-mcps`, but servers in `~/.copilot/mcp-config.json` still load. Codex, Cursor and OpenCode get no isolation. Under `ynh agent run` it applies on the Claude Code backend only. ynh prints a `warning:` line and launches normally.
- **Owner:** not established: the record does not show whether these vendors can do it.
- **Evidence:** [Running with only the harness's servers](mcp.md#running-with-only-the-harness-s-servers).
- **Tracking:** [ynh #731](https://github.com/eyelock/ynh/issues/731), v0.18.0.
- **Workaround:** none recorded.
- **Detail:** [Running with only the harness's servers](mcp.md#running-with-only-the-harness-s-servers).

### L7: Codex hooks need an accepted harness

- **What:** Codex runs a hook only when it trusts it. `ynh run` grants that trust for one run, and only for an installed harness that is accepted and unchanged in `ynh trust`. Otherwise Codex skips the hooks, and with them the `on_session_start` context, and ynh prints a line saying so.
- **Owner:** ynh, by design: it grants trust only to a harness you have accepted.
- **Evidence:** codex-cli 0.166.0, [ynh #701](https://github.com/eyelock/ynh/pull/701).
- **Tracking:** none.
- **Workaround:** `ynh trust accept local/<name>`. A harness run from a path (`ynh run ./dir`) must be installed first.
- **Detail:** [Codex hook trust](hooks.md#codex-hook-trust).

### L8: Cursor print mode skips two hooks

- **What:** when Cursor runs non-interactively (`ynh run` with a prompt, and every `ynh agent run --backend cursor` turn), `before_prompt` and `on_stop` do not fire. The other four events do. ynh warns, naming each event. The hooks of a package from `ynd export -v cursor` behave the same in `cursor-agent -p`: four of the six fire.
- **Owner:** the vendor. `cursor-agent -p` run directly, without ynh, skips them too.
- **Evidence:** cursor-agent 2026.10.22-27dcf5f, 2026-11-05, [ynh #710](https://github.com/eyelock/ynh/pull/710). For a package: live on cursor-agent 2026.11.05-86047df, 2026-11-05, at `develop` c29ebd6 ([ynh #735](https://github.com/eyelock/ynh/issues/735)).
- **Tracking:** none.
- **Workaround:** none verified. The interactive CLI is untested.
- **Detail:** [Hooks Cursor's CLI does not run](hooks.md#hooks-cursor-s-cli-does-not-run).

### L9: OpenCode has no session-end event

- **What:** `on_session_end` never fires on OpenCode. The other five events do.
- **Owner:** the vendor: OpenCode 2 has no such event.
- **Evidence:** [Events a vendor cannot express](hooks.md#events-a-vendor-cannot-express).
- **Tracking:** none.
- **Workaround:** none.
- **Detail:** [OpenCode hooks are a generated plugin](hooks.md#opencode-hooks-are-a-generated-plugin).

### L10: Codex workspace folders on resume

- **What:** `codex exec resume` rejects `--add-dir`, so a resume with a prompt passes the granted folders as `writable_roots`. That key takes effect only under the `workspace-write` sandbox. `codex exec` defaults to read-only, and ynh does not change Codex's sandbox; it warns on stderr instead.
- **Owner:** the vendor.
- **Evidence:** codex-cli 0.166.0, [ynh #700](https://github.com/eyelock/ynh/pull/700).
- **Tracking:** none.
- **Workaround:** run Codex under the `workspace-write` sandbox, for example with `sandbox_mode` in Codex's `config.toml`.
- **Detail:** [Per vendor](workspace.md#per-vendor).

### L11: Cursor workspace folders

- **What:** Cursor receives `--add-dir` for each granted folder, but whether the grant changes what Cursor can reach is unverified.
- **Owner:** unknown until verified.
- **Evidence:** in print mode with `--sandbox enabled`, Cursor wrote outside the project with or without the flag, so no folder was found that it cannot reach without it ([ynh #698](https://github.com/eyelock/ynh/pull/698)). The 2026-11-05 run stopped at Cursor's Workspace Trust prompt. The run at `develop` c29ebd6 (cursor-agent 2026.11.05-86047df) was blocked the same way: each harness's run directory needs its own Workspace Trust ([Vendor Notes](vendors.md#vendor-notes)).
- **Tracking:** [ynh #716](https://github.com/eyelock/ynh/issues/716), v0.17.0. Verified live once Cursor's Workspace Trust is granted for the harness's run directory.
- **Workaround:** none needed to launch; the folder is passed either way.
- **Detail:** [Per vendor](workspace.md#per-vendor).

### L12: Delegates carry only part of a harness

- **What:** a delegate reaches the vendor as an agent carrying its instructions. On Claude Code it also carries its MCP servers. On no vendor does it carry its hooks or workspace folders. On OpenCode a delegate does not run, and `ynd export -v opencode` leaves delegates out with the warning `opencode: skipping N delegates (not supported)`. In an export, delegates are carried the same way: instructions, and on Claude Code MCP servers.
- **Owner:** ynh.
- **Evidence:** [MCP Servers of Delegates](mcp.md#mcp-servers-of-delegates).
- **Tracking:** [ynh #729](https://github.com/eyelock/ynh/issues/729), v0.18.0.
- **Workaround:** for everything else, include the harness (`includes`) rather than delegating to it.
- **Detail:** [delegates_to](harnesses.md#delegates-to-optional).

### L13: ynh run emits no telemetry

- **What:** `ynh run` writes no OpenTelemetry on any vendor. Only `ynh agent run` does.
- **Owner:** ynh.
- **Evidence:** [Telemetry](telemetry.md).
- **Tracking:** [ynh #673](https://github.com/eyelock/ynh/issues/673), v0.19.0, for Claude Code. [ynh #732](https://github.com/eyelock/ynh/issues/732), v0.19.0, for Codex, Copilot CLI, Cursor and OpenCode.
- **Workaround:** run the harness with `ynh agent run`.
- **Detail:** [Telemetry](telemetry.md).

### L14: Copilot is not a backend

- **What:** `ynh agent run --backend` accepts `claude`, `codex`, `cursor` and `opencode`. There is no Copilot backend, so the rest of the Copilot column does not apply.
- **Owner:** ynh.
- **Evidence:** [Flags](agent.md#flags).
- **Tracking:** [ynh #425](https://github.com/eyelock/ynh/issues/425), no milestone.
- **Workaround:** run the harness on Copilot with `ynh run`.
- **Detail:** [The Agent Loop](agent.md).

### L15: A Codex worker runs no hooks

- **What:** `ynh agent run --backend codex` does not pass Codex the trust overrides that `ynh run` passes ([L7](#l7-codex-hooks-need-an-accepted-harness)), so a harness's hooks, and its `on_session_start` context, do not fire.
- **Owner:** ynh.
- **Evidence:** [Codex hook trust](hooks.md#codex-hook-trust).
- **Tracking:** [ynh #730](https://github.com/eyelock/ynh/issues/730), v0.18.0.
- **Workaround:** none.
- **Detail:** [Codex hook trust](hooks.md#codex-hook-trust).

### L16: Cursor and OpenCode have no effort setting

- **What:** any effort level on the `cursor` or `opencode` backend is refused, so a harness that sets `agent.effort` cannot run on them.
- **Owner:** the vendor. `cursor-agent --help` (2026.10.01) has no effort flag; OpenCode writes a model variant into the model id instead.
- **Evidence:** [Effort](agent.md#effort).
- **Tracking:** none.
- **Workaround:** choose the model or variant with `--model` (on OpenCode, `--model provider/model#variant`).
- **Detail:** [Effort](agent.md#effort).

### L17: auto-approve edits

- **What:** `--auto-approve edits` is an error on `codex`, `cursor` and `opencode`.
- **Owner:** the vendor: none of them has a mode that approves file edits while still asking before commands.
- **Evidence:** [Permissions and `--auto-approve`](agent.md#permissions-and-auto-approve).
- **Tracking:** none.
- **Workaround:** `--auto-approve all`, inside containment you own.
- **Detail:** [Permissions and `--auto-approve`](agent.md#permissions-and-auto-approve).

### L18: sandbox srt

- **What:** `--sandbox srt` is honoured by the `claude` backend only. On `codex`, `cursor` and `opencode` it is an error, not a warning.
- **Owner:** not established.
- **Evidence:** [Redaction](agent.md#redaction).
- **Tracking:** [ynh #734](https://github.com/eyelock/ynh/issues/734), no milestone.
- **Workaround:** run the worker inside containment you configure yourself.
- **Detail:** [What `--sandbox srt` allows](agent.md#what-sandbox-srt-allows).

### L19: Vendor telemetry through the relay

- **What:** the telemetry relay collects the vendor's own telemetry for Claude Code and Codex only.
- **Owner:** not established: the record names no OTLP export in Cursor or OpenCode.
- **Evidence:** [Vendor telemetry through the relay](telemetry.md#vendor-telemetry-through-the-relay).
- **Tracking:** [ynh #732](https://github.com/eyelock/ynh/issues/732), v0.19.0.
- **Workaround:** none. ynh's own spans and events are still written.
- **Detail:** [Vendor telemetry through the relay](telemetry.md#vendor-telemetry-through-the-relay).

### L20: Claude Code and Copilot plugins carry no instructions

- **What:** a Claude Code or Copilot CLI plugin loaded with `--plugin-dir` ignores an instructions file inside it, so `ynd export -v claude` and `ynd export -v copilot` write no `AGENTS.md` or `CLAUDE.md` and warn.
- **Owner:** the vendor.
- **Evidence:** live on Claude Code 2.1.318 and Copilot CLI 1.0.105, 2026-11-05: the package's skill was read, and a codeword in its instructions was not, [ynh #727](https://github.com/eyelock/ynh/pull/727).
- **Tracking:** [ynh #722](https://github.com/eyelock/ynh/issues/722), v0.17.0.
- **Workaround:** put the instructions in the installing project's own instructions file. Under `ynh run` they do reach both vendors.
- **Detail:** [Export Output by Vendor](vendors.md#export-output-by-vendor).

### L21: A Codex plugin carries no instructions

- **What:** a Codex plugin ignores its `AGENTS.md`, so `ynd export -v codex` writes none and warns.
- **Owner:** the vendor.
- **Evidence:** live on codex-cli 0.166.0, 2026-11-05, [ynh #711](https://github.com/eyelock/ynh/pull/711).
- **Tracking:** none.
- **Workaround:** put the instructions in the installing project's `AGENTS.md`. Under `ynh run` they do reach Codex.
- **Detail:** [Export Output by Vendor](vendors.md#export-output-by-vendor).

### L22: A Codex plugin carries only skills, hooks and MCP servers

- **What:** a Codex export carries no agents, rules, commands or delegates. ynd leaves each out with a warning.
- **Owner:** the vendor: Codex plugins have none of these.
- **Evidence:** [Export Output by Vendor](vendors.md#export-output-by-vendor).
- **Tracking:** none.
- **Workaround:** ship commands as skills ([L3](#l3-codex-commands-arrive-as-skills)). Under `ynh run`, agents and rules do reach Codex.
- **Detail:** [Export Output by Vendor](vendors.md#export-output-by-vendor).

### L23: Rules in Claude Code and Copilot packages

- **What:** an export cannot carry rules to Claude Code or Copilot CLI. Under `ynh run` rules ride in the instructions ([L2](#l2-rules-are-always-on)), and a package cannot carry instructions to these vendors ([L20](#l20-claude-code-and-copilot-plugins-carry-no-instructions)). Claude Code does not read the `rules/` folder of a plugin.
- **Owner:** the vendor.
- **Evidence:** [Rules](artifacts.md#rules).
- **Tracking:** none.
- **Workaround:** put the rules in the installing project's own instructions file.
- **Detail:** [Rules](artifacts.md#rules).

### L24: Commands in a Copilot package

- **What:** `ynd export -v copilot` ships a harness's `commands/`, and Copilot CLI loads them as skills: the model reaches a command by name through the skill tool. Copilot does not expand `/name` itself, as under `ynh run` ([L32](#l32-copilot-commands-under-ynh-run)). Until [ynh #733](https://github.com/eyelock/ynh/issues/733) the export left commands out, with a warning that they were not supported.
- **Owner:** the vendor: Copilot CLI has no user-definable slash commands ([github/copilot-cli#618](https://github.com/github/copilot-cli/issues/618)).
- **Evidence:** live on Copilot CLI 1.0.105, 2026-11-05: a `ynd export -v copilot` package carrying `commands/` answered `skill(canary-cmd)` with the command's codeword; the same package without `commands/` had no such skill.
- **Tracking:** [ynh #733](https://github.com/eyelock/ynh/issues/733), v0.17.0, fixed.
- **Workaround:** ask for the command by name; ship it as a skill for the same result.
- **Detail:** [Export Output by Vendor](vendors.md#export-output-by-vendor).

### L25: Codex package hooks and MCP tools need approval

- **What:** Codex reads a plugin's hooks only after you trust them with `/hooks`. A one-shot `codex exec` refuses a plugin's MCP tool call until the server is approved.
- **Owner:** the vendor.
- **Evidence:** live on codex-cli 0.166.0, 2026-11-05, [ynh #711](https://github.com/eyelock/ynh/pull/711).
- **Tracking:** none.
- **Workaround:** trust the hooks with `/hooks`. Approve the server's tools in Codex's `config.toml` with `[plugins."<name>@<marketplace>".mcp_servers.<server>] default_tools_approval_mode = "approve"` (a `-c` override of the same key does not take).
- **Detail:** [Export Output by Vendor](vendors.md#export-output-by-vendor).

### L26: Cursor packages drop instructions, rule bodies and commands

- **What:** cursor-agent loads a package from `ynd export -v cursor`, and its skills (an included one too), its agent and its session context arrive. Two parts do not reach the model (a third, instructions, is [L34](#l34-a-cursor-plugin-carries-no-instructions)):
  - **Rules:** a rule with `alwaysApply: true` is offered only as a rule the model can request, without its body.
  - **Commands:** a command (`/name`) is not reached in print mode (`cursor-agent -p`). Interactive use is unverified.

  The package's hooks are [L8](#l8-cursor-print-mode-skips-two-hooks), and its MCP servers [L33](#l33-cursor-package-mcp-tools-need-approval-in-print-mode).
- **Owner:** not established. Whether a Cursor plugin can carry instructions, always-applied rules and commands is settled from Cursor's plugin docs in [ynh #739](https://github.com/eyelock/ynh/issues/739).
- **Evidence:** live on cursor-agent 2026.11.05-86047df, 2026-11-05, at `develop` [c29ebd6](https://github.com/eyelock/ynh/commit/c29ebd6), with the package in a folder granted Workspace Trust (an untrusted control folder was refused). Recorded on [ynh #735](https://github.com/eyelock/ynh/issues/735). Until this run no Cursor package had been loaded live.
- **Tracking:** [ynh #739](https://github.com/eyelock/ynh/issues/739), v0.17.0.
- **Workaround:** run the harness on Cursor with `ynh run`.
- **Detail:** [Export Output by Vendor](vendors.md#export-output-by-vendor).

### L27: OpenCode reads no packages

- **What:** OpenCode has no plugin manifest and no marketplace format, and does not read Agent Plugins packages. `ynd export -v opencode` writes the run-directory layout (artifacts, `AGENTS.md`, `.opencode/opencode.json`, the generated hooks plugin), but nothing installs it. Rules are written as files, not composed into `AGENTS.md`. Delegates are left out with a warning. `--merged` and `ynd marketplace build` add only OpenCode's `.opencode/` MCP and hooks files.
- **Owner:** the vendor.
- **Evidence:** [Export Output by Vendor](vendors.md#export-output-by-vendor).
- **Tracking:** none.
- **Workaround:** run the harness on OpenCode with `ynh run`.
- **Detail:** [Export Output by Vendor](vendors.md#export-output-by-vendor).

### L28: Copilot marketplace index

- **What:** Copilot CLI installs a marketplace from `ynd marketplace build` (`.github/plugin/marketplace.json`) and loads its plugin. The skills (an included one too), the agent, all six hooks and the command (as a skill, from the merged tree's `commands/`) arrive. The MCP server loads and offers its tool, but a one-shot run refuses the call because there is no one to approve it. Approving it in an interactive session has not been shown.
- **Owner:** the vendor: a one-shot Copilot run does not approve an installed plugin's MCP tool.
- **Evidence:** live on Copilot CLI 1.0.105, 2026-11-05: the MCP server's tool was offered and refused ("no one to approve"), so its codeword did not come back.
- **Tracking:** [ynh #735](https://github.com/eyelock/ynh/issues/735), v0.17.0.
- **Workaround:** none verified.
- **Detail:** [Marketplace & Distribution](marketplace.md).

### L29: Agent Plugins on Claude Code and Cursor

- **What:** Claude Code does not load the Agent Plugins format, so `--format agent-plugin` adds Claude Code's own plugin files at the package root, as a compatibility layer. Cursor loads only the portable core: it has published no extension namespace, and does not expand `${PLUGIN_ROOT}` or `${PLUGIN_DATA}`.
- **Owner:** the vendor.
- **Evidence:** [Agent Plugins Export](vendors.md#agent-plugins-export).
- **Tracking:** none.
- **Workaround:** for Claude Code, use the compatibility layer, or `ynd export -v claude`.
- **Detail:** [Agent Plugins Export](vendors.md#agent-plugins-export).

### L30: Codex resumes a prompt one-shot only

- **What:** `ynh run --resume` with a prompt runs on Codex as a one-shot `codex exec resume`. With `--interactive` it is refused before launch: `codex cannot resume a session interactively with a prompt; drop --resume or the prompt`. A resume without a prompt, and a one-shot resume with one, work.
- **Owner:** the vendor: Codex's interactive resume documents no prompt argument.
- **Evidence:** [Resuming a Session](focus.md#resuming-a-session).
- **Tracking:** none.
- **Workaround:** resume one-shot with the prompt, or resume interactively and type the prompt into the session.
- **Detail:** [Resuming a Session](focus.md#resuming-a-session).

### L31: Codex agents cannot restrict tools

- **What:** a harness's agent becomes a Codex custom agent (`.codex/agents/<name>.toml`), which has no tool allowlist, so its `tools` and `disallowedTools` are dropped and the agent can use every tool the session has. ynh prints a warning naming the agent for each list. When `tools` leaves out `Bash`, the agent file sets `features.shell_tool = false`; whether a custom agent honours that has not been checked live. An agent's Claude Code `model` does not carry over either.
- **Owner:** the vendor: Codex custom agents have no field that restricts tools ([Codex's subagent docs](https://learn.chatgpt.com/docs/agent-configuration/subagents)).
- **Evidence:** [Symlink Installation](vendors.md#symlink-installation-codex-cursor).
- **Tracking:** none.
- **Workaround:** do not rely on an agent's tool list for containment on Codex; restrict the session itself (Codex's sandbox and approval settings).
- **Detail:** [Symlink Installation](vendors.md#symlink-installation-codex-cursor).

### L32: Copilot commands under ynh run

- **What:** under `ynh run`, a harness's command is reached on Copilot CLI as a skill, by name: the model calls the skill tool, which loads `.copilot/commands/<name>.md`. Copilot does not expand a one-shot `/name` itself. The interactive slash command was not checked.
- **Owner:** the vendor: Copilot CLI has no user-definable slash commands ([github/copilot-cli#618](https://github.com/github/copilot-cli/issues/618)).
- **Evidence:** live on Copilot CLI 1.0.105, 2026-11-05: a harness's command was reached both by naming it and as a one-shot `/name` prompt, through the skill tool; a control harness with no `commands/` failed both ways. The interactive path was blocked by Copilot's folder trust prompt.
- **Tracking:** none.
- **Workaround:** ask for the command by name; ship it as a skill for the same result.
- **Detail:** [Commands](artifacts.md#commands).

### L33: Cursor package MCP tools need approval in print mode

- **What:** a Cursor package's MCP server loads and lists its tool, but in print mode (`cursor-agent -p`) a call to the tool is refused until it is approved.
- **Owner:** not established. Whether cursor-agent can approve a plugin's MCP tool for a one-shot run has not been checked.
- **Evidence:** live on cursor-agent 2026.11.05-86047df, 2026-11-05, at `develop` [c29ebd6](https://github.com/eyelock/ynh/commit/c29ebd6) ([ynh #735](https://github.com/eyelock/ynh/issues/735)).
- **Tracking:** none.
- **Workaround:** none verified.
- **Detail:** [Export Output by Vendor](vendors.md#export-output-by-vendor).

### L34: A Cursor plugin carries no instructions

- **What:** a Cursor plugin loaded with `--plugin-dir` does not deliver an instructions file inside it, so `ynd export -v cursor` writes no `.cursorrules` or `AGENTS.md` and warns.
- **Owner:** the vendor. Cursor's plugin docs list rules, commands, agents, skills, hooks and MCP servers as the components, and no instructions file ([Plugins](https://cursor.com/docs/plugins), [Plugins reference](https://cursor.com/docs/reference/plugins)); `AGENTS.md` is a project-root file ([Rules](https://cursor.com/docs/context/rules)).
- **Evidence:** live on cursor-agent 2026.11.05, 2026-11-05: a codeword set only in the package's `.cursorrules` and `AGENTS.md` was not returned in 3 runs, while the skill's, agent's and hook's were.
- **Tracking:** [ynh #739](https://github.com/eyelock/ynh/issues/739), v0.17.0.
- **Workaround:** put the instructions in the installing project's `AGENTS.md`. Under `ynh run` they do reach Cursor.
- **Detail:** [Export Output by Vendor](vendors.md#export-output-by-vendor).
