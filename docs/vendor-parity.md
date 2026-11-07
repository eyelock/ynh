# Vendor Feature Parity

What each vendor receives from a harness, on each surface ynh has: `ynh run`, `ynh agent run` and `ynd export`. Every cell that is not **Y** names a limit, and every limit says whose it is.

- **ynh version:** v0.17.0, as on `release/v0.17.0` [3916dd5](https://github.com/eyelock/ynh/commit/3916dd5): `develop` at [d71026a](https://github.com/eyelock/ynh/commit/d71026a) with the harness manifests stamped 0.17.0. Since `develop` [f2822a1](https://github.com/eyelock/ynh/commit/f2822a1), which included [ynh #721](https://github.com/eyelock/ynh/pull/721), [ynh #725](https://github.com/eyelock/ynh/pull/725), [ynh #726](https://github.com/eyelock/ynh/pull/726) and [ynh #727](https://github.com/eyelock/ynh/pull/727), it adds [ynh #728](https://github.com/eyelock/ynh/pull/728), [ynh #736](https://github.com/eyelock/ynh/pull/736), [ynh #741](https://github.com/eyelock/ynh/pull/741), [ynh #742](https://github.com/eyelock/ynh/pull/742), [ynh #743](https://github.com/eyelock/ynh/pull/743), [ynh #749](https://github.com/eyelock/ynh/pull/749), [ynh #750](https://github.com/eyelock/ynh/pull/750) and [ynh #752](https://github.com/eyelock/ynh/pull/752).
- **Checked on:** 2026-11-05. The verification is recorded in [ynh #716](https://github.com/eyelock/ynh/issues/716) and [ynh #735](https://github.com/eyelock/ynh/issues/735); each limit below links the pull request or run that proved it. Each column was checked by its own run:
  - **Claude Code** 2.1.318, **Codex** (codex-cli 0.166.0), **Copilot CLI** 1.0.105 and **OpenCode** 2.2.3: live runs through ynh at `develop` [958b7a3](https://github.com/eyelock/ynh/commit/958b7a3), and on the branches of the four pull requests above for the cells they changed. Copilot CLI's marketplace index ([L28](#l28-copilot-marketplace-index)) was checked live at `develop` [1c68ebe](https://github.com/eyelock/ynh/commit/1c68ebe). The release verification re-ran the cells of L6, L9, L12, L29, L30, L31 and L37 live on the same four versions at `release/v0.17.0` [3916dd5](https://github.com/eyelock/ynh/commit/3916dd5), 2026-11-05 (the release verification, in the body of [ynh #753](https://github.com/eyelock/ynh/pull/753)).
  - **Cursor**, `ynd export` column: a package loaded live into cursor-agent 2026.11.05-86047df, at `develop` [c29ebd6](https://github.com/eyelock/ynh/commit/c29ebd6), whose export code is the same as at f2822a1. The record is on [ynh #735](https://github.com/eyelock/ynh/issues/735). [ynh #742](https://github.com/eyelock/ynh/pull/742), merged at `develop` [fbe9e14](https://github.com/eyelock/ynh/commit/fbe9e14), then stopped the export writing `.cursorrules` and `AGENTS.md`. A live load of the package it writes is deferred to a later release.
  - **Cursor**, `ynh run` and `ynh agent run` columns: not run live in this verification. Their live check is deferred to a later release. Four `ynh run` cells were shown live before it, through `ynh run -v cursor`, and were not re-checked: Hooks ([ynh #621](https://github.com/eyelock/ynh/pull/621), cursor-agent 2026.10.01-e373342, and [L8](#l8-cursor-print-mode-skips-two-hooks)), Session context ([ynh #627](https://github.com/eyelock/ynh/pull/627)), Resume ([ynh #714](https://github.com/eyelock/ynh/pull/714), cursor-agent 2026.10.22) and focuses ([Focus](focus.md), cursor-agent 2026.10.22). Every other Cursor cell on these two surfaces is U ([L35](#l35-cursor-under-ynh-run-not-shown-live), [L36](#l36-the-cursor-backend-not-shown-live)), or N or X with its own limit.

This page is the only copy of this table. Other pages link here rather than repeat it. `make check-vendor-parity` checks the page against ynh (`scripts/parity-page.sh`, [ynh #718](https://github.com/eyelock/ynh/issues/718)): it fails when a vendor that `ynh vendors` reports has no column, when a cell is not a legend mark or a non-Y cell names no limit, when a limit has no Owner or Evidence, when an N limit links no issue, when a Hooks, Session context or Workspace folders cell contradicts what the vendor's adapter declares, and when another page holds a copy of the table. How each vendor is launched, and where its files go, is in [Vendor Support](vendors.md).

## Legend

| Mark | Meaning |
|---|---|
| **Y** | Works. |
| **P** | Works, with a limit. The limit says what is missing and whose it is. |
| **N** | Does not work, and is ynh's to fix. The limit names its issue and milestone, or says there is none yet. |
| **X** | Does not work because the vendor cannot do it. The limit gives the vendor's evidence. |
| **U** | Unverified. The limit says what was checked, and that the live check is deferred to a later release. |
| – | Does not apply: the surface does not exist for that vendor. |

A gap is marked **N** until there is evidence that the vendor cannot do it. Limit ids (`L1`, `L2`, ...) are never reused: a limit that is fixed keeps its id and says what fixed it.

In the [ynd export](#ynd-export) matrix, a cell says what `ynd export` writes into the package, in the form the vendor documents. Whether the vendor then loads the package is its own row, the first: it is Y where a package was loaded into the vendor live. An **X** below it is something a live load showed the vendor ignores, or that the vendor has no place for.

Columns are in the order `ynh vendors` lists the vendors.

## ynh run

What a harness delivers when you launch it with `ynh run` (or its launcher), interactive or one-shot.

| Feature | Claude Code | Codex | Copilot CLI | Cursor | OpenCode |
|---|---|---|---|---|---|
| Instructions | Y | Y | Y | U [L35](#l35-cursor-under-ynh-run-not-shown-live) | Y |
| Project instructions (the project's own `AGENTS.md` or `CLAUDE.md`) | Y | Y | Y | U [L1](#l1-project-instructions-on-cursor) | Y |
| Skills | Y | Y | Y | U [L35](#l35-cursor-under-ynh-run-not-shown-live) | Y |
| Agents | Y | P [L31](#l31-codex-agents-cannot-restrict-tools) | Y | U [L35](#l35-cursor-under-ynh-run-not-shown-live) | Y |
| Rules | P [L2](#l2-rules-are-always-on) | P [L2](#l2-rules-are-always-on) | P [L2](#l2-rules-are-always-on) | U [L35](#l35-cursor-under-ynh-run-not-shown-live) | P [L2](#l2-rules-are-always-on) |
| Commands | Y | P [L3](#l3-codex-commands-arrive-as-skills) | P [L32](#l32-copilot-commands-under-ynh-run) | U [L35](#l35-cursor-under-ynh-run-not-shown-live) | P [L4](#l4-opencode-commands-in-a-one-shot-prompt) |
| MCP servers | Y | Y | Y | U [L35](#l35-cursor-under-ynh-run-not-shown-live) | Y |
| MCP isolation (`mcp_isolation`, `--isolated-mcp`) | Y | Y | Y | X [L6](#l6-mcp-isolation) | Y |
| Hooks | Y | P [L7](#l7-codex-hooks-need-an-accepted-harness) | Y | P [L8](#l8-cursor-print-mode-skips-two-hooks) | P [L9](#l9-opencode-has-no-session-end-event) |
| Session context (`on_session_start` output) | Y | P [L7](#l7-codex-hooks-need-an-accepted-harness) | Y | Y | Y |
| Profiles | Y | Y | Y | U [L35](#l35-cursor-under-ynh-run-not-shown-live) | Y |
| Focuses, includes, sensors | Y | Y | Y | U [L35](#l35-cursor-under-ynh-run-not-shown-live) | Y |
| Workspace folders | Y | P [L10](#l10-codex-workspace-folders-on-resume) | Y | U [L11](#l11-cursor-workspace-folders) | Y |
| `delegates_to` | P [L12](#l12-delegates-carry-only-part-of-a-harness) | P [L12](#l12-delegates-carry-only-part-of-a-harness) | P [L12](#l12-delegates-carry-only-part-of-a-harness) | U [L35](#l35-cursor-under-ynh-run-not-shown-live) | P [L12](#l12-delegates-carry-only-part-of-a-harness) |
| Resume (`--resume`, with or without a prompt) | Y | P [L30](#l30-codex-resumes-a-prompt-one-shot-only) | Y | Y | Y |
| Telemetry from `ynh run` | N [L13](#l13-ynh-run-emits-no-telemetry) | N [L13](#l13-ynh-run-emits-no-telemetry) | N [L13](#l13-ynh-run-emits-no-telemetry) | N [L13](#l13-ynh-run-emits-no-telemetry) | N [L13](#l13-ynh-run-emits-no-telemetry) |
| Vendor telemetry through the relay (`--telemetry-relay`) | N [L13](#l13-ynh-run-emits-no-telemetry) | N [L13](#l13-ynh-run-emits-no-telemetry) | Y | X [L19](#l19-vendor-telemetry-through-the-relay) | X [L19](#l19-vendor-telemetry-through-the-relay) |

## ynh agent run

Where the backends of `ynh agent run` differ. Its flags and behaviour are in [The Agent Loop](agent.md).

| Feature | Claude Code | Codex | Copilot CLI | Cursor | OpenCode |
|---|---|---|---|---|---|
| Backend (`--backend`) | Y | Y | U [L14](#l14-the-copilot-backend-not-shown-live) | U [L36](#l36-the-cursor-backend-not-shown-live) | Y |
| Hooks | Y | Y | U [L14](#l14-the-copilot-backend-not-shown-live) | U [L36](#l36-the-cursor-backend-not-shown-live) | P [L9](#l9-opencode-has-no-session-end-event) |
| Session context (`on_session_start` output) | Y | Y | U [L14](#l14-the-copilot-backend-not-shown-live) | U [L36](#l36-the-cursor-backend-not-shown-live) | Y |
| MCP isolation (`mcp_isolation`) | Y | Y | U [L14](#l14-the-copilot-backend-not-shown-live) | X [L6](#l6-mcp-isolation) | Y |
| Effort (`--effort`, `agent.effort`) | Y | Y | U [L14](#l14-the-copilot-backend-not-shown-live) | X [L16](#l16-cursor-and-opencode-have-no-effort-setting) | X [L16](#l16-cursor-and-opencode-have-no-effort-setting) |
| `--auto-approve edits` | Y | X [L17](#l17-auto-approve-edits) | X [L17](#l17-auto-approve-edits) | X [L17](#l17-auto-approve-edits) | X [L17](#l17-auto-approve-edits) |
| `--auto-approve all` | Y | Y | U [L14](#l14-the-copilot-backend-not-shown-live) | U [L36](#l36-the-cursor-backend-not-shown-live) | Y |
| `--sandbox srt` | Y | N [L18](#l18-sandbox-srt) | N [L18](#l18-sandbox-srt) | N [L18](#l18-sandbox-srt) | N [L18](#l18-sandbox-srt) |
| Telemetry (ynh's own spans and events) | Y | Y | U [L14](#l14-the-copilot-backend-not-shown-live) | U [L36](#l36-the-cursor-backend-not-shown-live) | Y |
| Vendor telemetry through the relay (`--telemetry-relay`) | Y | Y | N [L19](#l19-vendor-telemetry-through-the-relay) | X [L19](#l19-vendor-telemetry-through-the-relay) | X [L19](#l19-vendor-telemetry-through-the-relay) |

## ynd export

What a package from `ynd export` carries, and whether the vendor loads it. The file each row becomes is in [Export Output by Vendor](vendors.md#export-output-by-vendor).

| Feature | Claude Code | Codex | Copilot CLI | Cursor | OpenCode |
|---|---|---|---|---|---|
| The vendor loads the package | Y | Y | Y | Y | X [L27](#l27-opencode-reads-no-packages) |
| Skills | Y | Y | Y | Y | – |
| Agents | Y | X [L22](#l22-a-codex-plugin-carries-only-skills-hooks-and-mcp-servers) | Y | Y | – |
| Rules | X [L23](#l23-rules-in-claude-code-and-copilot-packages) | X [L22](#l22-a-codex-plugin-carries-only-skills-hooks-and-mcp-servers) | X [L23](#l23-rules-in-claude-code-and-copilot-packages) | U [L26](#l26-cursor-packages-drop-rule-bodies-and-commands-need-a-slash-prompt) | – |
| Commands | Y | X [L22](#l22-a-codex-plugin-carries-only-skills-hooks-and-mcp-servers) | P [L24](#l24-commands-in-a-copilot-package) | P [L26](#l26-cursor-packages-drop-rule-bodies-and-commands-need-a-slash-prompt) | – |
| Instructions | X [L20](#l20-claude-code-and-copilot-plugins-carry-no-instructions) | X [L21](#l21-a-codex-plugin-carries-no-instructions) | X [L20](#l20-claude-code-and-copilot-plugins-carry-no-instructions) | X [L34](#l34-a-cursor-plugin-carries-no-instructions) | – |
| Hooks | Y | P [L25](#l25-codex-package-hooks-and-mcp-tools-need-approval) | Y | P [L8](#l8-cursor-print-mode-skips-two-hooks) | – |
| MCP servers | Y | P [L25](#l25-codex-package-hooks-and-mcp-tools-need-approval) | P [L37](#l37-copilot-package-mcp-tools-need-approval) | U [L33](#l33-cursor-package-mcp-tools-need-approval-in-print-mode) | – |
| Delegates | P [L12](#l12-delegates-carry-only-part-of-a-harness) | X [L22](#l22-a-codex-plugin-carries-only-skills-hooks-and-mcp-servers) | P [L12](#l12-delegates-carry-only-part-of-a-harness) | P [L12](#l12-delegates-carry-only-part-of-a-harness) | – |
| Merged export (`--merged`) | Y | Y | Y | Y | X [L27](#l27-opencode-reads-no-packages) |
| Marketplace index (`ynd marketplace build`) | Y | Y | Y [L28](#l28-copilot-marketplace-index) | Y | X [L27](#l27-opencode-reads-no-packages) |
| Agent Plugins package (`--format agent-plugin`) | P [L29](#l29-agent-plugins-on-claude-code-and-cursor) | Y | Y | U [L29](#l29-agent-plugins-on-claude-code-and-cursor) | X [L27](#l27-opencode-reads-no-packages) |

## Limits

### L1: Project instructions on Cursor

- **What:** whether Cursor reads the project's own instructions file under `ynh run` is unverified. The live check is deferred to a later release.
- **Owner:** unknown until verified.
- **Evidence:** checked: Cursor's docs say `AGENTS.md` is read from a project's root and subdirectories ([Rules](https://cursor.com/docs/context/rules)). Under `ynh run`, Cursor starts in the harness's run directory, not the project ([Vendor Notes](vendors.md#vendor-notes)), so the docs do not settle whether the project's own file is read. The live runs on 2026-11-05 showed Claude Code, Codex, Copilot CLI and OpenCode reading the project's file under `ynh run`. Deferred: a live `ynh run -v cursor` that shows whether Cursor reads it. No Cursor run under `ynh run` completed in this verification.
- **Tracking:** [ynh #751](https://github.com/eyelock/ynh/issues/751), v0.18.0: the live check is deferred to a later release. [ynh #716](https://github.com/eyelock/ynh/issues/716) records the v0.17.0 verification.
- **Workaround:** none recorded.
- **Detail:** [Project Instructions](artifacts.md#project-instructions).

### L2: Rules are always on

- **What:** on Claude Code, Codex and Copilot CLI, rules reach the model only by being appended to the instructions it reads. On OpenCode they are composed into `.opencode/AGENTS.md`. Either way a rule is always on, with no path scoping.
- **Owner:** the vendor. None of them reads a harness's rules from where ynh loads the harness; OpenCode 2 has no rules at all.
- **Evidence:** [ynh #695](https://github.com/eyelock/ynh/pull/695).
- **Tracking:** none.
- **Workaround:** on Cursor, ynh writes rules as native `.mdc` files that keep their scoping ([Rules](artifacts.md#rules)); whether Cursor applies them under `ynh run` is unverified ([L35](#l35-cursor-under-ynh-run-not-shown-live)).
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

- **What:** isolation runs with only the harness's MCP servers. It works under `ynh run` on Claude Code, Codex, Copilot CLI and OpenCode, and under `ynh agent run` on the Claude Code, Codex and OpenCode backends. It does not work on Cursor, which gets a `warning:` line and loads your own servers too.
- **Owner:** the vendor for Cursor, whose cells are **X**. On the other cells there is no limit left.
- **Evidence:** live runs on Codex, Copilot CLI and OpenCode, and Cursor's documentation:
  - **Codex**, codex-cli 0.166.0, live through `ynh run`, 2026-11-06: a probe MCP server in `$CODEX_HOME/config.toml` started without isolation and did not with `--isolated-mcp`, while the harness's own probe server started in both. Codex has `codex mcp list --json` and `-c mcp_servers.<name>.enabled=false`; `codex exec --ignore-user-config` exists too but only on `exec`, so it cannot serve an interactive launch.
  - **Copilot CLI**, 1.0.106, live through `ynh run`, 2026-11-06: the same probe in `$COPILOT_HOME/mcp-config.json`, with the same result, using `copilot mcp list --json` and `--disable-mcp-server <name>`. This replaces the 1.0.105 result of the release verification, where only the built-ins could be turned off.
  - **OpenCode**, 2.2.3, live through `ynh run`, 2026-11-06: a server in the global `opencode.json` did not start even without isolation, since `OPENCODE_CONFIG_DIR` replaces the global config directory (`opencode debug config` lists no global source when it is set). A server in the project's `opencode.json` started without isolation and did not with it, and the harness's own server started in both. The "no isolation" claim recorded before was wrong for the global config.
  - **`ynh agent run`**, live 2026-11-06 with a harness that sets `mcp_isolation`, a probe server in the operator's Codex config and another in the worktree's OpenCode `opencode.json`: on the Codex backend the operator's probe started without isolation and not with it; on the OpenCode backend the worktree's probe started without it and not with it; the harness's probe started in every run. The worker's environment is smaller than ynh's, so the vendor is asked which servers it would load under the worker's environment and in its worktree.
  - **Cursor**, cursor-agent 2026.11.05-86047df, **from documentation, not a live run** (no Cursor licence was available): [Cursor's MCP page](https://cursor.com/docs/mcp) documents `~/.cursor/mcp.json` and `.cursor/mcp.json` and only an in-app toggle for turning servers off. `cursor-agent --help` has no flag for it (`--approve-mcps` only approves); `cursor-agent mcp disable <name>` persists to the user's own state, which a launch must not rewrite; and the bundle reads no environment variable that moves its config. The live check is deferred with the rest of the Cursor `ynh run` column ([L35](#l35-cursor-under-ynh-run-not-shown-live)).
- **Tracking:** [ynh #731](https://github.com/eyelock/ynh/issues/731), v0.18.0.
- **Workaround:** none for Cursor.
- **Detail:** [Running with only the harness's servers](mcp.md#running-with-only-the-harness-s-servers).

### L7: Codex hooks need an accepted harness

- **What:** Codex runs a hook only when it trusts it. `ynh run` and `ynh agent run --backend codex` grant that trust for one run (see [L15](#l15-a-codex-worker-ran-no-hooks-fixed) and [Codex hook trust](hooks.md#codex-hook-trust)), under the same rule: only an installed harness that is accepted and unchanged in `ynh trust`. Otherwise Codex skips the hooks, and with them the `on_session_start` context, and ynh prints a line saying so.
- **Owner:** ynh, by design: it grants trust only to a harness you have accepted.
- **Evidence:** codex-cli 0.166.0, [ynh #701](https://github.com/eyelock/ynh/pull/701).
- **Tracking:** none.
- **Workaround:** `ynh trust accept local/<name>`. A harness run from a path (`ynh run ./dir`) must be installed first.
- **Detail:** [Codex hook trust](hooks.md#codex-hook-trust).

### L8: Cursor print mode skips two hooks

- **What:** when Cursor runs non-interactively (`ynh run` with a prompt), `before_prompt` and `on_stop` do not fire. `ynh agent run --backend cursor` runs Cursor the same way, but that is not shown live ([L36](#l36-the-cursor-backend-not-shown-live)). The other four events do. ynh warns, naming each event. The hooks of a package from `ynd export -v cursor` behave the same in `cursor-agent -p`: four of the six fire.
- **Owner:** the vendor. `cursor-agent -p` run directly, without ynh, skips them too.
- **Evidence:** cursor-agent 2026.10.22-27dcf5f, 2026-11-05, [ynh #710](https://github.com/eyelock/ynh/pull/710). For a package: live on cursor-agent 2026.11.05-86047df, 2026-11-05, at `develop` c29ebd6 ([ynh #735](https://github.com/eyelock/ynh/issues/735)). Under `ynh agent run --backend cursor` it has not been shown live ([L36](#l36-the-cursor-backend-not-shown-live)).
- **Tracking:** none.
- **Workaround:** none verified. The interactive CLI is untested.
- **Detail:** [Hooks Cursor's CLI does not run](hooks.md#hooks-cursor-s-cli-does-not-run).

### L9: OpenCode has no session-end event

- **What:** `on_session_end` never fires on OpenCode. The other five events do.
- **Owner:** the vendor: OpenCode 2 has no such event.
- **Evidence:** live on OpenCode 2.2.3, 2026-11-05, at `release/v0.17.0` [3916dd5](https://github.com/eyelock/ynh/commit/3916dd5) (the release verification, in the body of [ynh #753](https://github.com/eyelock/ynh/pull/753)), on both surfaces. `ynh run -v opencode` printed `warning: hook event on_session_end has no equivalent on opencode, so it is not written`, and the hook log shows `on_session_start`, `before_prompt`, `before_tool`, `after_tool` and `on_stop`, with no `on_session_end`. `ynh agent run --backend opencode` printed the same warning and fired `on_session_start`. Background: [Events a vendor cannot express](hooks.md#events-a-vendor-cannot-express).
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

- **What:** Cursor receives `--add-dir` for each granted folder, but whether the grant changes what Cursor can reach is unverified. The live check is deferred to a later release.
- **Owner:** unknown until verified.
- **Evidence:** verified: Cursor's own help documents the flag (`agent --help`, 2026.10.22: "Add an additional workspace root directory", repeatable), and ynh passes it ([Per vendor](workspace.md#per-vendor)). On cursor-agent 2026.10.22-27dcf5f, in print mode with `--sandbox enabled`, Cursor wrote outside the project with or without the flag, so no folder was found that it cannot reach without it ([ynh #698](https://github.com/eyelock/ynh/pull/698)). Deferred: a live `ynh run -v cursor` that shows what the grant changes. No Cursor run under `ynh run` completed in the live runs on 2026-11-05.
- **Tracking:** [ynh #751](https://github.com/eyelock/ynh/issues/751), v0.18.0: the live check is deferred to a later release. [ynh #716](https://github.com/eyelock/ynh/issues/716) records the v0.17.0 verification.
- **Workaround:** none needed to launch; the folder is passed either way.
- **Detail:** [Per vendor](workspace.md#per-vendor).

### L12: Delegates carry only part of a harness

- **What:** a delegate reaches the vendor as an agent carrying its instructions. On Claude Code it also carries its MCP servers. On no vendor does it carry its hooks or workspace folders. Under `ynh run`, OpenCode offers a delegate as a subagent the task tool can call, with its instructions only. `ynd export -v opencode` leaves delegates out with the warning `opencode: skipping N delegates (not supported)`. In an export, a delegate carries only its instructions; on Claude Code its MCP servers are written into the agent, but Claude Code ignores them in a plugin agent (ynd warns).
- **Owner:** ynh.
- **Evidence:** under `ynh run`, live on Claude Code 2.1.318, codex-cli 0.166.0, Copilot CLI 1.0.105 and OpenCode 2.2.3, 2026-11-05, at `release/v0.17.0` [3916dd5](https://github.com/eyelock/ynh/commit/3916dd5) (the release verification, in the body of [ynh #753](https://github.com/eyelock/ynh/pull/753)): a delegate's codeword, set only in its instructions, came back on Claude Code, Codex and Copilot. On Claude Code the delegate's MCP server started (its log shows `initialize` and `tools/list`), and the one-shot run refused the tool call for permission. Codex, Copilot and OpenCode printed `warning: delegate <name> declares MCP servers (<server>) that <vendor> subagents cannot carry; they are not available to it`. The delegate's hooks fired on none of the four. On OpenCode the delegate was not offered as a subagent type (only `explore` and `general`) because its agent file had no `mode`, which OpenCode reads as primary. It is written with `mode: all` now, and a live run on OpenCode 2.2.3, 2026-11-06, on [ynh #762](https://github.com/eyelock/ynh/pull/762) at 84b9e44, called the delegate through the task tool and got its codeword back from it. In an export, the same verification found that Claude Code ignores the MCP servers written into a plugin agent. Background: [MCP Servers of Delegates](mcp.md#mcp-servers-of-delegates).
- **Tracking:** [ynh #729](https://github.com/eyelock/ynh/issues/729), v0.18.0.
- **Workaround:** for everything else, include the harness (`includes`) rather than delegating to it.
- **Detail:** [delegates_to](harnesses.md#delegates-to-optional).

### L13: ynh run emits no telemetry

- **What:** `ynh run` writes no OpenTelemetry on any vendor. Only `ynh agent run` does.
- **Owner:** ynh.
- **Evidence:** [Telemetry](telemetry.md).
- **Tracking:** [ynh #673](https://github.com/eyelock/ynh/issues/673), v0.19.0, for Claude Code; [ynh #782](https://github.com/eyelock/ynh/issues/782) for Codex. Cursor and OpenCode: none yet, and their telemetry through the relay is [L19](#l19-vendor-telemetry-through-the-relay). [ynh #732](https://github.com/eyelock/ynh/issues/732) collects Copilot CLI's own telemetry under `ynh run` through the relay ([L19](#l19-vendor-telemetry-through-the-relay)) and does not make `ynh run` write its own.
- **Workaround:** run the harness with `ynh agent run`.
- **Detail:** [Telemetry](telemetry.md).

### L14: The Copilot backend, not shown live

- **What:** `ynh agent run --backend copilot` runs the worker with Copilot CLI's `copilot -p`, one process per turn, resumed by session id. Whether it runs a whole loop, and whether hooks, session context, MCP isolation, effort, `--auto-approve all` and ynh's own telemetry work on it, is unverified. The live check is deferred.
- **Owner:** unknown until verified.
- **Evidence:** checked, by code and tests rather than a live loop: the output shapes (`--output-format json`) were captured from Copilot CLI 1.0.107 and are the fixtures of [ynh #789](https://github.com/eyelock/ynh/pull/789); a signed-out run was captured and is an authentication failure (exit 20); `--allow-all-tools`, `--reasoning-effort` and `--resume` were checked against `copilot help`. Token usage assumes the session's totals carry across `--resume`, which a signed-in two-process probe has yet to confirm.
- **Tracking:** [ynh #790](https://github.com/eyelock/ynh/issues/790), milestone v0.20.0.
- **Workaround:** run the harness on Copilot with `ynh run`.
- **Detail:** [The Agent Loop](agent.md).

### L15: A Codex worker ran no hooks (fixed)

- **What:** `ynh agent run --backend codex` did not pass Codex the trust overrides that `ynh run` passes ([L7](#l7-codex-hooks-need-an-accepted-harness)), so a harness's hooks, and its `on_session_start` context, did not fire. Fixed: for an accepted harness, ynh now hands the worker the hooks inline as `-c hooks.<Event>=[...]` overrides, each trusted by its hash, on every turn. A worker runs in the task's worktree, not the run directory, so Codex never loads the run directory's `.codex/hooks.json`; the overrides do not depend on the working directory. A harness that is not accepted gets none, with the same line on stderr as `ynh run`.
- **Owner:** ynh.
- **Evidence:** live on Codex (codex-cli 0.166.0), 2026-11-06: an installed harness with an `on_session_start` hook that prints a marker and touches a file. Before `ynh trust accept` the worker printed the not-trusted line and the file was not created; after it the hook ran, the marker reached the model and appeared in the trajectory, and `~/.codex/config.toml` was byte-identical afterwards. See [Codex hook trust](hooks.md#codex-hook-trust).
- **Tracking:** [ynh #730](https://github.com/eyelock/ynh/issues/730), v0.18.0, fixed by [ynh #759](https://github.com/eyelock/ynh/pull/759).
- **Workaround:** none needed.
- **Detail:** [Codex hook trust](hooks.md#codex-hook-trust).

### L16: Cursor and OpenCode have no effort setting

- **What:** any effort level on the `cursor` or `opencode` backend is refused, so a harness that sets `agent.effort` cannot run on them.
- **Owner:** the vendor. `cursor-agent --help` (2026.10.01) has no effort flag; OpenCode writes a model variant into the model id instead.
- **Evidence:** [Effort](agent.md#effort).
- **Tracking:** none.
- **Workaround:** choose the model or variant with `--model` (on OpenCode, `--model provider/model#variant`).
- **Detail:** [Effort](agent.md#effort).

### L17: auto-approve edits

- **What:** `--auto-approve edits` is an error on `codex`, `cursor`, `opencode` and `copilot`.
- **Owner:** the vendor: none of them has a mode that approves file edits while still asking before commands. Copilot's nearest is `--allow-tool='write'`, which also approves a shell command's output redirection into a file (for example `cat a > b`), so it is wider than file edits.
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

- **What:** the telemetry relay collects the vendor's own telemetry under `ynh agent run` for Claude Code and Codex, and under `ynh run` for Copilot CLI. Cursor and OpenCode cannot send it to the run's relay.
- **Owner:** the vendor. Cursor documents no OTLP export. OpenCode's `opencode run` is a thin client of a shared `opencode serve` that never sees the run's environment.
- **Evidence:** Copilot CLI 1.0.106, live under `ynh run --telemetry-relay` with a real `ynr relay`, 2026-11-07: `invoke_agent` and `chat` spans and `gen_ai.*` metrics reached the spool, with no prompt, response or path, in a trace of Copilot's own ([Copilot CLI](telemetry.md#what-copilot-cli-receives)). OpenCode 2.2.3, live, 2026-11-06: only the client's startup spans reach the relay, and its `cli starting` log record carries the whole command line, the prompt included, which [ynr ADR-006, rule 10](https://github.com/eyelock/ynr/blob/develop/docs/adr/006-sibling-instrumentation-contract.md) forbids. Cursor, cursor-agent 2026.11.05: its [documentation](https://cursor.com/docs/cli/overview) and installed bundle show no OTLP export setting; not verified live, as there is no licence.
- **Tracking:** [ynh #732](https://github.com/eyelock/ynh/issues/732), v0.19.0, for Copilot CLI. A per-run `opencode serve`, which would give OpenCode the run's environment, is [ynh #771](https://github.com/eyelock/ynh/issues/771), unscheduled.
- **Workaround:** none. ynh's own spans and events are still written under `ynh agent run`.
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

### L26: Cursor packages drop rule bodies, and commands need a slash prompt

- **What:** cursor-agent loads a package from `ynd export -v cursor`, and its skills (an included one too), its agent and its session context arrive. Instructions are [L34](#l34-a-cursor-plugin-carries-no-instructions). Two other parts have gaps:
  - **Rules:** unverified. In the live runs a rule with `alwaysApply: true` was offered only as a rule the model can request, without its body. Whether that is Cursor's or ynh's, and a live run of the rule from the package ynh now writes, are deferred to a later release.
  - **Commands:** in print mode (`cursor-agent -p`) a command is reached when the prompt is `/name`: `/canary-cmd` returned its codeword, `FIG-CMD-67`, with `--plugin-dir`. A prose request for the command did not use it, and one `/name` run answered an unrelated report, so it is not yet reliable. Interactive use is unverified.

  The package's hooks are [L8](#l8-cursor-print-mode-skips-two-hooks), and its MCP servers [L33](#l33-cursor-package-mcp-tools-need-approval-in-print-mode).

  What was loaded live is the package written at `develop` c29ebd6. [ynh #742](https://github.com/eyelock/ynh/pull/742) then stopped the export writing `.cursorrules` and `AGENTS.md` ([L34](#l34-a-cursor-plugin-carries-no-instructions)). A live load of the package it writes is deferred to a later release.
- **Owner:** rules: not established. Cursor's [plugin reference](https://cursor.com/docs/reference/plugins) says a plugin carries `rules/*.mdc` and `alwaysApply: true` applies to all files, yet the rule did not reach the model in further runs, via `--plugin-dir` (with and without `rules` and `commands` named in the manifest) and as a local install, so it is not yet clear whether Cursor or ynh owns this. Commands: the vendor carries them, per the same reference.
- **Evidence:** live on cursor-agent 2026.11.05-86047df, 2026-11-05, at `develop` [c29ebd6](https://github.com/eyelock/ynh/commit/c29ebd6), with the package in a folder granted Workspace Trust (an untrusted control folder was refused). Recorded on [ynh #735](https://github.com/eyelock/ynh/issues/735). Until this run no Cursor package had been loaded live. The rule and command re-runs are on cursor-agent 2026.11.05, 2026-11-05, in the same trusted folder.
- **Tracking:** [ynh #739](https://github.com/eyelock/ynh/issues/739). Deferred to a later release: the rules investigation, and a live load of the package after [ynh #742](https://github.com/eyelock/ynh/pull/742).
- **Workaround:** none verified. Under `ynh run` on Cursor, rules and commands are unverified too ([L35](#l35-cursor-under-ynh-run-not-shown-live)).
- **Detail:** [Export Output by Vendor](vendors.md#export-output-by-vendor).

### L27: OpenCode reads no packages

- **What:** OpenCode has no plugin manifest and no marketplace format, and does not read Agent Plugins packages. `ynd export -v opencode` writes the run-directory layout (artifacts, `AGENTS.md`, `.opencode/opencode.json`, the generated hooks plugin), but nothing installs it. Rules are written as files, not composed into `AGENTS.md`. Delegates are left out with a warning. `--merged` and `ynd marketplace build` add only OpenCode's `.opencode/` MCP and hooks files.
- **Owner:** the vendor.
- **Evidence:** [Export Output by Vendor](vendors.md#export-output-by-vendor).
- **Tracking:** none.
- **Workaround:** run the harness on OpenCode with `ynh run`.
- **Detail:** [Export Output by Vendor](vendors.md#export-output-by-vendor).

### L28: Copilot marketplace index

- **What:** works. Copilot CLI installs a marketplace from `ynd marketplace build` (`.github/plugin/marketplace.json`) and loads its plugin. The skills (an included one too), the agent, all six hooks and the command (as a skill, from the merged tree's `commands/`) arrive. The MCP server loads and offers its tool; in an interactive session Copilot asks before the first call, and once the call is approved the tool runs. A one-shot run refuses the call, because there is no one to approve it ([L37](#l37-copilot-package-mcp-tools-need-approval)).
- **Owner:** the vendor, for the one-shot refusal: a one-shot Copilot run does not approve an installed plugin's MCP tool.
- **Evidence:** live on Copilot CLI 1.0.105, 2026-11-05, with `ynd` built from `develop` [1c68ebe](https://github.com/eyelock/ynh/commit/1c68ebe), in a scratch Copilot home:
  - `copilot plugin marketplace add <dir>`, where `<dir>` is the local directory `ynd marketplace build -v copilot` wrote, then `copilot plugin install <plugin>@<marketplace>`: both succeeded with no prompt, and `copilot plugin list` shows the plugin as loaded from a local marketplace directory, never copied.
  - In an interactive session, the session's `events.jsonl` records `permission.requested` (`permissionMode` `manual`), then `permission.completed` approved with `decisionSource` `human_response`, then the tool's result, `MCP codeword: PLUM-872`, a codeword set fresh for this run. The canary MCP server's own log has the `tools/call` line.
  - In a one-shot run on the same day, Copilot offered the tool and refused the call ("no one to approve"), so its codeword did not come back.
- **Tracking:** [ynh #735](https://github.com/eyelock/ynh/issues/735), v0.17.0: verified, so this closes it.
- **Workaround:** use an interactive session to call the tool. An approval given there for the location carries over to later one-shot runs in the same Copilot home and folder (shown on Copilot CLI 1.0.106).
- **Detail:** [Marketplace & Distribution](marketplace.md).

### L29: Agent Plugins on Claude Code and Cursor

- **What:** Claude Code loads only the skills of a portable Agent Plugins package; agents, hooks and MCP servers need the compatibility layer that `--format agent-plugin -v claude` adds (Claude Code's own plugin files at the package root). On Cursor it is unverified: by its docs it loads only the portable core, since it has published no extension namespace and does not expand `${PLUGIN_ROOT}` or `${PLUGIN_DATA}`. The live check on Cursor is deferred to a later release.
- **Owner:** the vendor.
- **Evidence:** Claude Code: live on 2.1.318, 2026-11-05, at `release/v0.17.0` [3916dd5](https://github.com/eyelock/ynh/commit/3916dd5) (the release verification, in the body of [ynh #753](https://github.com/eyelock/ynh/pull/753)); with the compatibility layer all six hooks fired. Cursor: [Agent Plugins Export](vendors.md#agent-plugins-export), from docs; deferred: a live load.
- **Tracking:** [ynh #751](https://github.com/eyelock/ynh/issues/751), for the Cursor live check, deferred to a later release. None for Claude Code.
- **Workaround:** for Claude Code, use the compatibility layer, or `ynd export -v claude`.
- **Detail:** [Agent Plugins Export](vendors.md#agent-plugins-export).

### L30: Codex resumes a prompt one-shot only

- **What:** `ynh run --resume` with a prompt runs on Codex as a one-shot `codex exec resume`. With `--interactive` it is refused before Codex runs (ynh has printed `Launching codex...` by then): `codex cannot resume a session interactively with a prompt; drop --resume or the prompt`, exit status 1. A resume without a prompt, and a one-shot resume with one, work.
- **Owner:** the vendor: Codex's interactive resume documents no prompt argument.
- **Evidence:** live on codex-cli 0.166.0, 2026-11-05, at `release/v0.17.0` [3916dd5](https://github.com/eyelock/ynh/commit/3916dd5) (the release verification, in the body of [ynh #753](https://github.com/eyelock/ynh/pull/753)): `ynh run <harness> -v codex --resume --interactive -- hello` printed `Launching codex...`, then `Error: codex cannot resume a session interactively with a prompt; drop --resume or the prompt`, and exited 1. Background: [Resuming a Session](focus.md#resuming-a-session).
- **Tracking:** none.
- **Workaround:** resume one-shot with the prompt, or resume interactively and type the prompt into the session.
- **Detail:** [Resuming a Session](focus.md#resuming-a-session).

### L31: Codex agents cannot restrict tools

- **What:** a harness's agent becomes a Codex custom agent (`.codex/agents/<name>.toml`), which has no tool allowlist, so its `tools` and `disallowedTools` are dropped and the agent can use every tool the session has. ynh prints a warning naming the agent for each list. When `tools` leaves out `Bash`, the agent file sets `features.shell_tool = false`; whether a custom agent honours that has not been checked live. An agent's Claude Code `model` does not carry over either.
- **Owner:** the vendor: Codex custom agents have no field that restricts tools ([Codex's subagent docs](https://learn.chatgpt.com/docs/agent-configuration/subagents)).
- **Evidence:** live on codex-cli 0.166.0, 2026-11-05, at `release/v0.17.0` [3916dd5](https://github.com/eyelock/ynh/commit/3916dd5) (the release verification, in the body of [ynh #753](https://github.com/eyelock/ynh/pull/753)): `ynh run -v codex` with an agent whose `tools` leaves out `Bash` printed `warning: agent "canary-agent": Codex custom agents cannot restrict tools, so its tools list is dropped; it leaves out Bash, so the agent's shell tool is turned off (features.shell_tool = false)`. Whether the agent honours `features.shell_tool = false` was not tested. Background: [Symlink Installation](vendors.md#symlink-installation-codex-cursor).
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

- **What:** a Cursor package's MCP server loads and lists its tool, but in print mode (`cursor-agent -p`) a call to the tool is refused until it is approved. Whether a call succeeds once the server is approved is unverified: that live call is deferred to a later release.
- **Owner:** the vendor: approval is the person's, and a package cannot carry it. cursor-agent keeps approvals per project in the user's home (`~/.cursor/projects/<project>/mcp-approvals.json`, one `<server>-<hash>` entry per approved server, written by `agent mcp enable <server>`). Its other switches are also the person's: `--approve-mcps` on the run, and an `Mcp(<server>:<tool>)` entry in `permissions.allow` of `~/.cursor/cli-config.json` or the project's `.cursor/cli.json` ([Cursor CLI permissions](https://cursor.com/docs/cli/reference/permissions)). A Cursor plugin's documented components are rules, skills, agents, commands, MCP servers and hooks ([Cursor plugins](https://cursor.com/docs/plugins)), and its manifest reference lists no field or file that carries approval or permissions ([Plugins reference](https://cursor.com/docs/reference/plugins)), so none of them can write one. That reference says a plugin's MCP servers show as `plugin-<plugin>-<server>` in approval prompts, but it does not say whether `agent mcp enable` or the approvals file takes that name; which identifier it takes is still open.
- **Evidence:** the refusal: live on cursor-agent 2026.11.05-86047df, 2026-11-05, at `develop` [c29ebd6](https://github.com/eyelock/ynh/commit/c29ebd6) ([ynh #735](https://github.com/eyelock/ynh/issues/735)). The mechanisms: `agent --help` (`--approve-mcps`: "Automatically approve all MCP servers"; `agent mcp enable`: "Add an MCP server to the local approved list") and a scratch run with `HOME` pointed at a scratch folder, where `agent mcp enable canary` wrote `mcp-approvals.json` as above. Deferred to a later release: a live call, with an approval in place (such as `--approve-mcps`), that comes back with the tool's result.
- **Tracking:** [ynh #744](https://github.com/eyelock/ynh/issues/744). The live call is deferred to a later release.
- **Workaround:** approve the server yourself: run `cursor-agent -p --approve-mcps ...`, or run `agent mcp enable <server>` once in the project. Neither is verified against a plugin's server in a live call.
- **Detail:** [Export Output by Vendor](vendors.md#export-output-by-vendor).

### L34: A Cursor plugin carries no instructions

- **What:** a Cursor plugin loaded with `--plugin-dir` does not deliver an instructions file inside it, so `ynd export -v cursor` writes no `.cursorrules` or `AGENTS.md` and warns.
- **Owner:** the vendor. Cursor's plugin docs list rules, commands, agents, skills, hooks and MCP servers as the components, and no instructions file ([Plugins](https://cursor.com/docs/plugins), [Plugins reference](https://cursor.com/docs/reference/plugins)); `AGENTS.md` is a project-root file ([Rules](https://cursor.com/docs/context/rules)).
- **Evidence:** live on cursor-agent 2026.11.05, 2026-11-05: a codeword set only in the package's `.cursorrules` and `AGENTS.md` was not returned in 3 runs, while the skill's, agent's and hook's were.
- **Tracking:** [ynh #739](https://github.com/eyelock/ynh/issues/739), v0.17.0.
- **Workaround:** put the instructions in the installing project's `AGENTS.md`. Under `ynh run`, ynh writes them to `.cursorrules` in the run directory; whether they reach Cursor that way is unverified ([L35](#l35-cursor-under-ynh-run-not-shown-live)).
- **Detail:** [Export Output by Vendor](vendors.md#export-output-by-vendor).

### L35: Cursor under ynh run, not shown live

- **What:** under `ynh run -v cursor`, ynh writes the harness into the run directory in Cursor's project layout, but whether its instructions, skills, agents, rules, commands, MCP servers, profiles, includes, sensors and delegates then reach the model on Cursor is unverified. The live check is deferred to a later release.
- **Owner:** unknown until verified.
- **Evidence:** checked: the files ynh writes, by running `ynh run` and `ynd preview` ([Project Instructions](artifacts.md#project-instructions)): instructions in `.cursorrules`, rules as `.mdc` files ([Rules](artifacts.md#rules)), MCP servers in `.cursor/mcp.json` ([MCP Servers](mcp.md#config-file-locations)), and the other artifacts linked under `.cursor/` ([Symlink Installation](vendors.md#symlink-installation-codex-cursor)). Cursor's docs name `.cursor/mcp.json` as the project file it reads MCP servers from ([MCP](https://cursor.com/docs/context/mcp)) and `.mdc` as its rule format ([Rules](https://cursor.com/docs/context/rules)); that Rules page does not mention `.cursorrules` ([ynh #742](https://github.com/eyelock/ynh/pull/742)). Shown live through `ynh run -v cursor`: hooks, session context, resume and focuses (the runs are named under **Checked on** above). Deferred: a live `ynh run -v cursor` for each of the rest. The record shows none, and the live runs on 2026-11-05 made no Cursor run under `ynh run`.
- **Tracking:** [ynh #751](https://github.com/eyelock/ynh/issues/751), v0.18.0: the live check is deferred to a later release. [ynh #716](https://github.com/eyelock/ynh/issues/716) records the v0.17.0 verification.
- **Workaround:** none verified.
- **Detail:** [Symlink Installation](vendors.md#symlink-installation-codex-cursor).

### L36: The Cursor backend, not shown live

- **What:** `ynh agent run --backend cursor` runs the worker with Cursor's CLI, `agent`. Whether it runs turns, and whether hooks, session context, `--auto-approve all` and ynh's own telemetry work on it, is unverified. The live check is deferred to a later release.
- **Owner:** unknown until verified.
- **Evidence:** checked, by code and tests rather than a live run: the worker takes its binary, `agent`, from the Cursor adapter ([ynh #536](https://github.com/eyelock/ynh/pull/536)); it reads the model from the init event of Cursor's documented stream-json ([ynh #444](https://github.com/eyelock/ynh/pull/444)); and the `--auto-approve` mapping was checked against Cursor's CLI parameters page ([ynh #426](https://github.com/eyelock/ynh/pull/426)). Deferred: a live `ynh agent run --backend cursor`. The record shows none.
- **Tracking:** [ynh #751](https://github.com/eyelock/ynh/issues/751), v0.18.0: the live check is deferred to a later release. [ynh #716](https://github.com/eyelock/ynh/issues/716) records the v0.17.0 verification.
- **Workaround:** none verified.
- **Detail:** [The Agent Loop](agent.md).

### L37: Copilot package MCP tools need approval

- **What:** the MCP servers of a `ynd export -v copilot` package (`.github/mcp.json` at the plugin root) start both under `--plugin-dir` and from a plugin installed from a marketplace. In an interactive session Copilot asks before the first call, and once the call is approved the tool runs, both under `--plugin-dir` and from an installed plugin. A one-shot run (`copilot -p`) refuses the call ("no one to approve") unless an approval for that folder is already stored in the Copilot home.
- **Owner:** the vendor: a one-shot Copilot run does not approve a plugin's MCP tool.
- **Evidence:** live on Copilot CLI 1.0.105, 2026-11-05, at `release/v0.17.0` [3916dd5](https://github.com/eyelock/ynh/commit/3916dd5) (the release verification, in the body of [ynh #753](https://github.com/eyelock/ynh/pull/753)): one-shot under `--plugin-dir`, the server started (its log shows `initialize` and `tools/list`) and the call was refused: `Permission denied because no interactive user response was available` ("Denied, no one to approve"). One-shot from an installed plugin, the server started (`tools/list`) and the call was refused. Interactive, live on 2026-11-06 with the same package. The run under `--plugin-dir` was on Copilot CLI 1.0.105, and the installed run on 1.0.106, after Copilot updated itself between the two. In each, the session's `events.jsonl` records `permission.requested` (`permissionMode` `manual`), then `permission.completed` with `decisionSource` `human_response`, then the tool's result, `MCP codeword: CORAL-927`, a codeword set fresh for this run. The canary MCP server's log has the `tools/call` line. The installed run's approval was given for the location (`approved-for-location`), and a later one-shot run in the same Copilot home and folder ran the call with no prompt. A one-shot under `--plugin-dir` with no stored approval was still refused on 1.0.106. Interactive, installed: live on Copilot CLI 1.0.105, 2026-11-05, with `ynd` from `develop` [1c68ebe](https://github.com/eyelock/ynh/commit/1c68ebe) ([L28](#l28-copilot-marketplace-index)). Earlier, an installed merged package loaded `.github/mcp.json` and not `mcp/claude.json` (Copilot CLI 1.0.105, 2026-10-30, written up in [ynh #621](https://github.com/eyelock/ynh/pull/621), which fixed [ynh #499](https://github.com/eyelock/ynh/issues/499)).
- **Tracking:** none.
- **Workaround:** call the tool from an interactive session. An approval given there for the location carries over to later one-shot runs in the same Copilot home and folder (shown on Copilot CLI 1.0.106). Under `ynh run`, a harness's MCP servers reach Copilot with `--additional-mcp-config` instead ([Copilot Format](mcp.md#copilot-format)).
- **Detail:** [Config File Locations](mcp.md#config-file-locations).
