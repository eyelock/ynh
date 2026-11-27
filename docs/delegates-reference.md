# Delegates reference

Facts about how a delegate runs under `ynh run`, as of v0.18.0. For why it is built this way, see
[Delegate sessions](delegate-sessions.md). The manifest fields are in
[delegates_to](harnesses.md#delegates-to-optional) and [Transport](harnesses.md#transport). What was shown
live, and what was not, is under limit
[A2A delegates, not shown live](vendor-parity.md#l40-a2a-delegates-a-person-answering-a-baked-image-and-nesting-not-shown-live)
on the [parity page](vendor-parity.md).

This page is reference: facts only. Steps are in the how-tos and reasons are in
[Delegate sessions](delegate-sessions.md); a fact that needs a reason or a step goes there, not here. A
"limit" is a numbered known gap on the parity page, linked by what it says.

## Transport

| | `a2a` (default) | `native` |
|---|---|---|
| What the delegate is | its own detached ynh session, started on the endpoint's first message | a vendor subagent inside the parent's session |
| Endpoint, `ynh-delegates` server | yes | no |
| Installed harness matched | yes, by source | no: built from the entry's source |
| Carries its own MCP servers, hooks, workspace folders | yes, as its own run | instructions on every vendor, MCP servers only on Claude Code, hooks and workspace nowhere ([delegates carry only part of a harness](vendor-parity.md#l12-delegates-carry-only-part-of-a-harness)) |
| Ends with the parent's session | no | yes |
| `ynd export` | left out, with one warning | exported |

`transport` takes `a2a` or `native`, on a `delegates_to` entry or as a harness-level default. An entry's
value wins; `a2a` applies when neither says. `ynh info` prints each delegate's resolved transport and its
source: `transport=a2a (default)`, `transport=native (entry)` or `(harness)`.

A `native` delegate on a vendor with no native subagents is refused by `ynh install`, `ynh run` and
`ynd validate`, with no fallback to `a2a`:

```
<harness>: delegates_to[0] (<entry>) has transport native, and cursor has no native subagents
  Fix: set transport: a2a on this delegate, or run on a vendor with subagents
```

Only Cursor declares none today (its subagents are unverified,
[Cursor under `ynh run`, not shown live](vendor-parity.md#l35-cursor-under-ynh-run-not-shown-live)). Claude Code, Codex, Copilot CLI and
OpenCode declare native subagents.

### Allow rules for a native delegate's MCP servers

A `native` delegate on Claude Code carries its MCP servers into the parent's session as the subagent's
own servers. A headless (one-shot) Claude Code parent calls their tools only with an allow rule for each
of them, named after the server as the delegate's manifest declares it: `mcp__<server>`. Without one the
server starts and no tool is called. ynh adds no such rule.

| Delegate's server | Allow rule |
|---|---|
| `probe` | `--allowedTools=mcp__probe`, or `mcp__probe` in `permissions.allow` |
| `probe` and `tickets` | `--allowedTools=mcp__probe,mcp__tickets`, or both in `permissions.allow` |

The rule is not the `mcp__plugin_<harness>_<server>` form of a harness's own servers, and not the
`ynh-delegates` rule in [Allow rules for the tools](#allow-rules-for-the-tools). A `native` delegate on
Codex, Copilot CLI or OpenCode carries no MCP servers, so there is no rule to add. `ynh doctor` does not
check this rule.

Shown live with `--allowedTools` on Claude Code, in the v0.18.0 acceptance, 2026-11-26
([delegates carry only part of a harness](vendor-parity.md#l12-delegates-carry-only-part-of-a-harness)).
The `permissions.allow` form is Claude Code's own equivalent of the flag and was not run for this.

## Refusals when a delegate cannot be matched

For an `a2a` entry, `ynh run` of the parent matches the entry to the installed harness whose source
matches (the Git URL and path, or the local folder), before it opens the endpoints. An unmatched delegate
refuses the parent's whole run, before the vendor starts. A delegate is *trusted* once `ynh trust accept <id>`
has recorded that you read what it will run ([what that covers](sensors.md#what-a-command-sensor-can-reach)):

```
delegate <source> of <parent> is not installed
  Run: ynh install <source>, then ynh trust accept <the installed id>
delegate <source> of <parent> is installed as <id> but not trusted
  Review with 'ynh trust show <id>', then run: ynh trust accept <id>
delegate <source> of <parent> is installed as <id> but its commands changed since you accepted them
  Review with 'ynh trust show <id>', then run: ynh trust accept <id>
delegate <source> of <parent> is installed as <id> but cannot be read in full: <what is missing>
  Run: ynh update <id>
```

A delegate that executes nothing (no sensors, hooks or MCP servers) needs no `ynh trust accept` to be
matched. When several installs match one entry, the trusted match with the smallest id wins.

> **Intended, not run.** A delegate installed from a remote git URL or from a registry entry, with trust accepted,
> then started by its parent. It is the designed path; the record does not yet show it run
> ([A2A delegates, not shown live](vendor-parity.md#l40-a2a-delegates-a-person-answering-a-baked-image-and-nesting-not-shown-live)).

## The A2A endpoints

`ynh run` opens one endpoint for each `delegates_to` entry whose transport is `a2a`, and closes them when
the run ends. A delegation already running is a detached session and goes on.

- Listens on `127.0.0.1`, a free port; JSON-RPC served with [a2a-go](https://github.com/a2aproject/a2a-go)
  v2 (v2.8.0); its own random bearer token.
- Every request but the agent card needs `Authorization: Bearer <token>`. A wrong or missing token is HTTP
  401 and starts nothing. The card is served without a token.
- The delegate's session starts on the first message, never on a card fetch or a refused call.

| A2A task state | Delegation |
|---|---|
| `working` | running |
| `input-required` | the delegate has asked a question; answered by a message on the same task |
| `completed` | finished; the result is an artifact |
| `canceled` | cancelled; the run's process group is stopped |
| `failed` | the error, the exit code and the last 40 lines of the run's log |

Every result carries the task and context ids. A non-blocking send returns at once and the task goes on.

### Environment of the parent

| Variable | Value |
|---|---|
| `YNH_DELEGATES` | the A2A delegates' names, comma-separated |
| `YNH_DELEGATE_<NAME>_CARD` | the agent card URL |
| `YNH_DELEGATE_<NAME>_AUTH` | `Bearer <token>` |
| `A2ACLI_AGENT_CARD`, `A2ACLI_AUTH` | the same values, set only with exactly one A2A delegate |
| `A2ACLI_CREDENTIAL_HELPER` | a helper that answers with each endpoint's own token; always set |

`<NAME>` is the delegate's harness name upper-cased, every character outside A-Z and 0-9 turned into `_`.
Two entries that map to the same `<NAME>` are refused at run, naming both. A delegate session does not
inherit its parent's `YNH_DELEGATE*` or `A2ACLI_*`; it gets `YNH_DELEGATION_RECORD`, which marks it as a
delegate's session. `YNH_DELEGATE_MAX_WAIT` and `YNH_PARENT_ASK_MAX_WAIT` are below. Also in the
[CLI reference](reference.md).

## The `ynh-delegates` MCP server

`ynh run` adds `ynh delegate-mcp` over stdio to the parent's MCP config on every vendor, under the name
`ynh-delegates`. A harness that declares an MCP server of that name is refused at run. The entry carries the
endpoints (the `YNH_DELEGATE*` values, and `YNH_HOME`, since Codex hands its MCP servers a trimmed
environment). A run from a baked image has no such server: its config was written at build time; use the
[a2a CLI route](#the-a2a-cli-route) there.

A live `ynh run` of a parent with A2A delegates gives both routes to the same endpoints. A baked image's
run gets the skills only.

| | MCP tools | `a2a` CLI |
|---|---|---|
| Needs | the vendor's allow rule for `ynh-delegates` (none on OpenCode; Codex is approved by ynh) | the `a2a` binary (0.4.0 or newer) and a shell allow rule; on Codex, a sandbox that reaches localhost |
| Results | structured, with a declared schema | the CLI's own output |
| Handle | `task_id`, `dlg-` and 16 hex digits | the A2A task id the CLI prints |
| Waiting | a bounded `wait` on the call, or poll | `send --async` and `task get` |

Both reach the one delegation record, so `delegate_list` shows a task started with the CLI, but its id
there is the handle, not the A2A task id the CLI used. A task started with the CLI can be answered with
`delegate_reply`; one started with a tool can be inspected with `delegate_status`.

### Tools

Every tool returns `structuredContent` with a declared `outputSchema`, and the same JSON as text. Fields:
`task_id`, `context_id`, `state` (`working`, `input-required`, `completed`, `failed` or `canceled`),
`message`, and `result` once completed or `log_tail` once failed.

| Tool | Does |
|---|---|
| `delegate_to_<name>` (`task`, optional `wait`) | Starts the delegate through its A2A endpoint, with its token, and returns its handle with state `working`: at once with no `wait`, or after up to `wait` seconds (at most the wait limit) when the delegate finishes or asks something first. A client that offers MCP Tasks (Claude Code interactive) is the exception: it gets a task at once and `wait` is not used, so a parent there holds with `delegate_status` and a `wait` ([MCP Tasks, below](#tools), [ynh #902](https://github.com/eyelock/ynh/issues/902)). A wrong or missing token's 401 comes back as a tool error. |
| `delegate_list` | Every delegation of this parent harness in this working folder, with its state, including those from earlier sessions and the `a2a` CLI. |
| `delegate_status` (`task_id`, optional `wait`) | The state. When `input-required`, `message` is the delegate's question. |
| `delegate_result` (`task_id`, optional `wait`) | The result once completed; the state for one still going. |
| `delegate_cancel` (`task_id`) | Cancels and stops the run's process group. |
| `delegate_reply` (`task_id`, `message`, optional `wait`) | A message on the same task: the answer to a question. |

`<name>` is the delegate's harness name, lower-cased, each character outside a-z and 0-9 turned into `_`.
The server has no tool that takes a harness name or a path: a handle is checked against `dlg-` and 16 hex
digits, and must belong to this parent.

`wait` (seconds) holds a call until the delegate finishes or asks a question; past it the call returns the
handle with state `working`. `YNH_DELEGATE_MAX_WAIT` (seconds, 30 when unset) is the longest wait; a larger
`wait` is cut to it. 30 sits under the shortest known default tool-call timeout, Codex's 60 seconds
(`tool_timeout_sec`). A parent that sets its own timeout lower than a wait it asks for loses the call, not
the delegate: the handle is in the record, and `delegate_list` finds it again (shown live, 2026-11-13, on a
build of [ynh #832](https://github.com/eyelock/ynh/pull/832): Claude Code with `MCP_TOOL_TIMEOUT=5000`, Codex
with `tool_timeout_sec=5`).

**MCP Tasks.** A client that offers the Tasks extension (`io.modelcontextprotocol/tasks`) gets a task at
once from a delegate's tool, and `tasks/get`, `tasks/update` and `tasks/cancel` map onto the delegation; the
task id is the handle. Of the five vendors, only Claude Code's interactive mode offered Tasks in the spike of
2026-11-03. The Go MCP SDK has no Tasks extension ([go-sdk #626](https://github.com/modelcontextprotocol/go-sdk/issues/626)),
so ynh writes this server's JSON-RPC itself.

> **Known issue.** OpenCode (2.2.3) exposes MCP tools through Code Mode: the model sees one `execute` tool and calls
> `tools["ynh-delegates"].delegate_to_<name>({...})` inside it. In a check of 2026-11-13 a parent did not find
> the tools until its prompt named Code Mode.

### Allow rules for the tools

A headless vendor denies MCP tools until they are allowed. These rules are for an `a2a` delegate's
`ynh-delegates` server; a `native` delegate's own servers need the rules in
[Allow rules for a native delegate's MCP servers](#allow-rules-for-a-native-delegate-s-mcp-servers).

| Parent | Allow rule |
|---|---|
| Claude Code | `--allowedTools mcp__plugin_<harness>_ynh-delegates` (so `mcp__plugin_boss_ynh-delegates__delegate_to_reviewer` for the harness `boss`), or an `allow` entry in the settings |
| Codex | none: ynh sets `mcp_servers.ynh-delegates.default_tools_approval_mode="approve"` for a one-shot run, unless your config or `-c` sets it |
| Copilot CLI | `--allow-tool=ynh-delegates` |
| OpenCode | none |
| Cursor | not known: the route has not been run on Cursor |

The MCP server runs outside Codex's sandbox, so this route needs no sandbox network.

`ynh doctor` checks these for a harness with A2A delegates (none for one with only `native` delegates), as
warnings that never fail a run. It reads the vendor's settings on disk, so it cannot confirm a rule given on
a command line (`--allowedTools`, `--allow-tool`, `-c`) and warns anyway. For Claude Code it reads the three
settings files as JSON, `permissions.allow` and `permissions.deny` apart, and warns when a deny rule names
the server or one of its tools even if an allow rule is also there. For Copilot CLI it only looks for the
allow entry's text; a denied entry is not seen.

| Finding | Fix |
|---|---|
| Claude Code: a `permissions.deny` rule for `mcp__plugin_<harness>_ynh-delegates` (or one of its tools), or no allow rule for it in `~/.claude/settings.json`, `.claude/settings.json` or `.claude/settings.local.json` | remove the deny rule; add the allow rule to `permissions.allow`, or pass `--allowedTools` |
| Copilot CLI: no `ynh-delegates` entry in `~/.copilot/config.json`, `~/.copilot/settings.json` or `.copilot/settings.json` | add it to the allowed tools, or pass `--allow-tool=ynh-delegates` |
| Codex: `~/.codex/config.toml` sets `mcp_servers.ynh-delegates.default_tools_approval_mode` to something other than `approve`, or the harness sets `mcp_manual_approval` and the config does not approve it | remove the line or set it to `"approve"`; drop `mcp_manual_approval` |
| Cursor | not checked, and said so |
| OpenCode | nothing to check |

## The a2a CLI route

When a harness has an A2A delegate, `ynh run` writes two skills into the parent's vendor skill folder:

- `a2a-cli`, the official skill, vendored unchanged from a2a-cli commit
  `5c95123d1c8d50a1cd6dfc2f28a14f5533a383ae` (skill version 2026.09.22), Apache-2.0, in
  `internal/a2acli/vendored/a2a-cli/`.
- `ynh-a2a-delegates`, ynh's companion (`internal/a2acli/companion.md`), written again for each launch with
  each delegate's card address, which a command passes with `-a`.

Commands: `a2a send -a <card address> --async "<task>"`, then `a2a task get -a <address> <id>` until
`input-required` (answer with `a2a send --task-id <id> "<answer>"`) or `completed`; `a2a task cancel <id>`
takes it to `canceled`. A command carries no variable: Claude Code refuses one under `Bash(a2a:*)`
([ynh #849](https://github.com/eyelock/ynh/issues/849)). The token comes from the credential helper: for every
run with A2A delegates ynh writes a private script (mode 0700, in its own temporary folder), scoped to
`127.0.0.1`, and sets `A2ACLI_CREDENTIAL_HELPER` to it. The folder is removed when the session ends, normally
or on SIGINT, SIGTERM or SIGHUP once the vendor is running; it is left behind on SIGKILL, and the next launch
with A2A delegates removes any such folder older than seven days. A baked image's skill is written before any
endpoint exists, so it teaches the variable form
(`A2ACLI_AGENT_CARD="$YNH_DELEGATE_<NAME>_CARD" A2ACLI_AUTH="$YNH_DELEGATE_<NAME>_AUTH" a2a send --async ...`).

ynh does not install the `a2a` binary. `ynh doctor` warns (never fails) when this route would not work:

| Finding | Fix |
|---|---|
| `a2a` is not on PATH, or older than 0.4.0 | `curl -fsSL https://raw.githubusercontent.com/a2aproject/a2a-cli/main/install.sh \| A2A_VERSION=0.4.0 sh`, or `brew tap a2aproject/a2a-cli https://github.com/a2aproject/a2a-cli && brew install a2a` |
| the vendored skill is missing from the run dir's skill folder or differs from the pin | `ynh run <harness>` writes the pinned copy |
| Codex: the sandbox is not `workspace-write` with `network_access = true` | in `~/.codex/config.toml`, `sandbox_mode = "workspace-write"` and `[sandbox_workspace_write]` `network_access = true` |
| Claude Code: no `Bash(a2a:*)` allow rule | add it to `permissions.allow`, or pass `--allowedTools 'Bash(a2a:*)'`. One rule covers every command, for any number of delegates |
| Copilot CLI: no `shell(a2a:*)` allow rule | add it to the allowed tools, or pass `--allow-tool 'shell(a2a:*)'` |

> **Watch out.** With Codex's default sandbox the CLI cannot reach `127.0.0.1` (`connect: operation not permitted`);
> turning `network_access` on fixes it and means the shell is no longer sandboxed from localhost generally. A
> headless run denies every `a2a` call without an allow rule; OpenCode needs none. Cursor's shell route has not
> been run.

## Questions from a delegate

A delegate's session gets `ask_parent` from an MCP server named `ynh-parent` (`ynh parent-mcp`), added by
`ynh run` on every vendor, only when `YNH_DELEGATION_RECORD` is set. A harness that declares an MCP server of
that name is refused at run.

`ask_parent` (`question`, optional `wait`) writes the question to the delegation record and waits, at most 45
seconds (`YNH_PARENT_ASK_MAX_WAIT`, in seconds). The record puts the delegate's A2A task into
`input-required`, with the question as its message. The parent sees it by its route:

| Route | What the parent sees | How it answers |
|---|---|---|
| MCP, with Tasks (Claude Code interactive) | the task moves to `input_required` with an `elicitation/create` in `inputRequests` | `tasks/update`; a declined elicitation is recorded as `(the parent declined to answer)` and the delegate's tool says `declined` |
| MCP, elicitation but no Tasks (interactive Codex, Copilot CLI, OpenCode) | during a call that waits, a server-initiated `elicitation/create`, once per question | the elicitation, or `delegate_reply` (then the elicitation's answer is dropped). A client that refuses, declines or cancels has not answered: Cursor's CLI refuses without asking, Copilot CLI's `-p` declines at once |
| MCP, headless (`claude -p`, `codex exec`, `copilot -p`, `opencode run`) | state `input-required`, the question as `message` | `delegate_reply` on the same task: the model answers, not a person ([a headless parent's model answers a delegate's question](vendor-parity.md#l39-a-headless-parent-s-model-answers-a-delegate-s-question-not-a-person)) |
| `a2a` CLI | `a2a task get` shows `input-required` with the question as the status message | `a2a send --task-id <id> "<answer>"` |

`ask_parent` returns `answered`, `declined`, `unanswered`, `withdrawn` or `ended` (the delegation was
cancelled while it waited), with `question_id`, the `answer` when answered, and a `message` on what to do
next. When the wait runs out it returns `unanswered` and the question stays open; the delegate may call
`ask_parent` again with `question_id` to keep waiting, with `question_id` and `withdraw` to close it, or
carry on. A delegation that has ended shows its final state, whatever it left open. A task on the CLI route
left `input-required` stays so until it is next spoken to. Each answer is recorded against the question it was
given for; a late elicited answer for a question already answered is dropped.

**Allowing `ask_parent`.** Codex's `ynh run` approves it with the harness's own servers; Copilot CLI's
`--allow-all-tools` and OpenCode need nothing. For Claude Code, `ynh run` adds
`--allowedTools=mcp__plugin_<delegate>_ynh-parent` to a delegate's one-shot session, and the same kind of
rule for the delegate's own declared MCP servers, only on a delegate's one-shot Claude Code session whose
delegation record names that harness: not on a harness run directly, not on a harness a delegate's model runs
from its shell, not an interactive session, and not with `mcp_manual_approval`
([MCP servers](mcp.md#claude-code-format), [ynh #858](https://github.com/eyelock/ynh/issues/858)).

## The delegation record

One record per delegation under `$YNH_HOME/delegations/`, keyed by the parent harness and its working folder.
It holds the handle, the delegate, the task, its status and each change with a timestamp, the questions and
answers, the exit code and the result. The delegate runs detached: a ynh process in its own process group, with
the vendor CLI as its child.

- If the session process is killed outright, the next status or cancel notices that the recorded process (its
  id and start time) is gone, marks the delegation `failed` with an error saying so, and stops the run if it is
  still the recorded process. Cancel signals the process group only while its leader is the process the record
  names.
- The start time is read from `/proc/<pid>/stat` where there is a `/proc` (Linux, including BusyBox images such
  as ynh's own) and from `ps -o lstart=` on macOS. When it cannot be read, a process that still exists is treated
  as alive, and nothing is signalled unless the run, or its parent session process, is verifiably the recorded
  process. Otherwise cancel marks the delegation `cancelled`, records in `error` that the run was not signalled
  and may still be running, and leaves the process alone.
- The handle tools take a handle, never a path.

## Run dir snapshots

1. Under the assembly lock, a run assembles into a staging folder, including every per-launch file, and hashes
   the tree (paths, modes, contents).
2. If a snapshot with that hash exists, the run uses it and discards the staging folder; otherwise the folder is
   renamed to `$YNH_HOME/run/.snapshots/<name>/<hash>`.
3. `$YNH_HOME/run/<name>` is a symlink, swapped to the snapshot atomically. Existing project symlinks and the
   legacy bare-name alias keep resolving.
4. The run takes a shared lock on its snapshot's lock file (under `cache/run-locks/`), and holds it until the
   vendor exits ([ynh #745](https://github.com/eyelock/ynh/issues/745)).
5. The run launches the vendor on the snapshot's real path, never the stable link.
6. The run deletes each snapshot that is neither current nor locked.

A baked image's pre-assembled run dir is used as it is. Limit: Codex and Cursor project symlinks point through
the stable link, so they show the newest snapshot, not the session's. Both key state by the folder a session
starts in, so `--resume` finds the newest session that started in any snapshot of the harness and resumes it by
id in the newest snapshot, and Cursor gets `--trust` for an accepted harness ([Vendors](vendors.md#vendor-notes),
[Cursor resume across snapshots, not shown live](vendor-parity.md#l38-cursor-resume-and-interactive-trust-across-snapshots-not-shown-live)).
