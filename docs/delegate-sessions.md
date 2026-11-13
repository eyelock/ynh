# Delegate sessions

> **Status:** this is the design for `delegates_to` in v0.18.0, tracked in [ynh #729](https://github.com/eyelock/ynh/issues/729). It is not built yet: v0.17.0 and `develop` at [e6d1cdf](https://github.com/eyelock/ynh/commit/e6d1cdf) still turn a delegate into a vendor subagent. For how delegates work in the release you have, see [delegates_to](harnesses.md#delegates-to-optional), [MCP Servers of Delegates](mcp.md#mcp-servers-of-delegates) and [limit L12](vendor-parity.md#l12-delegates-carry-only-part-of-a-harness). Page checked against the design decided on 2026-11-03 and settled on 2026-11-06.

How `delegates_to` works from v0.18.0: every delegate is its own ynh session, and the parent reaches
it only through ynh's MCP server. This is a rewrite in place, with no opt-in flag, compatibility
shim or migration. The Claude Code native-subagent path is removed.

## Why

Before v0.18.0 a delegate was turned into a vendor subagent definition inside the parent's session.
A subagent carries only part of a harness: its instructions everywhere, its MCP servers only on
Claude Code, and its hooks and workspace folders nowhere (limit
[L12](vendor-parity.md#l12-delegates-carry-only-part-of-a-harness) on the parity page, [ynh #729](https://github.com/eyelock/ynh/issues/729)). On
OpenCode a delegate has run at all only since [ynh #762](https://github.com/eyelock/ynh/pull/762),
and then only as a subagent.
A subagent also dies with the parent's session. A delegate that is its own session carries its whole
harness on every vendor, and can outlive whoever started it.

## The shape

```
parent vendor CLI ──MCP (stdio)──> ynh delegate server ──starts──> ynh session of the delegate
   (any of five)                    (one per parent session)          (detached, own run dir)
                                           │                                   │
                                           └──── delegation record on disk ────┘
```

- `ynh run` of a harness with `delegates_to` adds one MCP server, ynh itself, to the parent's MCP
  config. The server exposes **one tool per delegate** the parent names, and nothing else.
- Calling a delegate's tool starts that delegate as its own `ynh run`: its own run dir, vendor,
  hooks, MCP servers, workspace grant, focus or profile (from the entry), and the parent's working
  folder. The tool's input is the task and an optional model.
- The parent never holds the delegate's tools. It sees only the delegate's result.

## Which harness a delegate runs as

A `delegates_to` entry is a source (`git`, `path`, `ref`), not an installed harness. ynh matches it
to the **installed harness whose source matches it** (the Git URL and path, or the local folder) and
runs the delegate as that harness. The delegate then uses that harness's own trust, workspace and
hook grants. The schema is unchanged.

If no installed harness matches, or the match is not trusted, the tool call fails and names the
`ynh install` or `ynh trust accept` to run. ynh never installs or trusts anything on a parent's
behalf. A parent's own trust digest no longer covers its delegates' commands, because each delegate
has its own grant.

## Trust

- A parent can start only the delegates in its own `delegates_to`. The server has no tool that takes
  a harness name, so there is nothing to refuse beyond an unknown tool.
- Each delegate runs under its own grant, as above. A delegate's own `delegates_to` applies only
  inside its own session.

## Talking to the parent: Tasks, blocking and handles

The protocol is MCP, because it is the only one every vendor CLI can call as a parent. The Go MCP SDK
(v1.8.0) does not implement the Tasks extension, so ynh writes the server's JSON-RPC itself, as the
[round-trip spike](#evidence) did in about 300 lines.

The server reads each request's client capabilities: for 2026-07-28 clients, from
`params._meta["io.modelcontextprotocol/clientCapabilities"]`; for older ones, from `initialize`.

| Parent offers | What a delegate call returns |
|---|---|
| Tasks (Claude Code interactive, 2.1.304+) | A task at once. The parent polls `tasks/get`, and the call does not block its turn. |
| No Tasks (Codex, Cursor, Copilot CLI, OpenCode, `claude -p`) | The call blocks until the delegate finishes **or a bounded wait runs out**. Then it returns the delegate's handle and says it is still running. |

The bounded wait is shorter than the vendor's own tool-call timeout, so a long delegate never times
out the call. It is a setting, with a default below the shortest timeout measured per vendor. Every
parent also gets five handle tools: `delegate_list`, `delegate_status`, `delegate_result`,
`delegate_cancel` and `delegate_reply` ([Reaching a delegate over MCP](#reaching-a-delegate-over-mcp)). A blocking parent uses them to wait in turns, and any parent uses them to come
back to its delegates.

## Questions from a delegate

A delegate session gets one extra tool from ynh for asking its parent a question. ynh passes the
question up the way the parent can take it:

- **Tasks:** the task moves to `input_required` with an `elicitation/create`, and the answer comes
  back through `tasks/update`. This works in Claude Code interactive.
- **Blocking:** a server-initiated `elicitation/create` during the call. This works in the
  interactive UIs of Codex, Copilot CLI and OpenCode. Cursor's CLI declines it without asking.
- **Headless parent** (`claude -p`, `codex exec`, `copilot -p`, `opencode run`): there is no one to
  ask. The question is declined, and the delegate's tool says so, so the delegate goes on with its
  own default or fails plainly. A headless parent cannot answer a delegate's question, on any
  vendor, and the parity page will list that as a limit.

A question asked while the parent is away waits in the delegation record until a parent comes back
and collects it.

## Outliving the parent

A delegate session must survive its parent going away (a restart, a handoff or a crash) and coming
back.

- The delegate runs **detached** from the parent's process and its MCP server: a ynh process in its
  own process group, with the vendor CLI as its child, so ynh sees the vendor's exit.
- Each delegation has a **record on disk** under `$YNH_HOME/delegations/`, keyed by the parent
  harness and its working folder. The record holds the handle, the delegate, the task, its status
  and each change with a timestamp, the questions asked and their answers, the exit code and the
  result.
- The server keeps no state of its own. A returning parent's new server answers for old handles
  from the record (`tasks/get`, or the handle tools), so the parent can reattach to a running
  delegate or collect a finished one. The record is also what says what happened while the parent
  was away.
- If the session process is killed outright, nothing is left to record how the run ended. The next
  status or cancel notices that the recorded session process (its id and start time) is gone,
  marks the delegation `failed` with an error saying so, and stops the delegate's run if it is
  still the recorded process. Cancel signals the run's process group only while its leader is
  still the process the record names, since a group id can be reused by an unrelated process.
- The start time is read from `/proc/<pid>/stat` where there is a `/proc` (Linux, including
  BusyBox images such as ynh's own, which have no `ps -o lstart`), and from `ps -o lstart=` on
  macOS. When it cannot be read (now, or when the record was written), ynh does not take that as
  a dead process: a session process that still exists is treated as alive, so status leaves the
  delegation running. But nothing is then verified, so nothing is signalled: cancel signals the
  run's group only when the run, or its parent session process, is verifiably the recorded
  process (same start time). Otherwise cancel still marks the delegation `cancelled`, records in
  its `error` that the run was not signalled and may still be running, and leaves the process
  alone, because an id that merely exists may belong to something unrelated. The same rule holds
  for the orphan stop when a session is found dead. Only a process that is gone, or verifiably
  another one, counts as dead.
- The handle tools take a **handle**, never a path: the handle is checked against `dlg-` and 16 hex
  digits and looked up under `$YNH_HOME/delegations/`.

## Reaching a delegate: the A2A endpoints

`ynh run` of a harness opens one localhost A2A endpoint for each `delegates_to` entry whose
transport is `a2a` (the default), and closes them when the run ends. A delegation already running
is a detached session and goes on.

- Each endpoint listens on `127.0.0.1` on a free port, serves JSON-RPC with
  [a2a-go](https://github.com/a2aproject/a2a-go) v2, and has its own random bearer token. Every
  request but the agent card needs `Authorization: Bearer <token>`; a wrong or missing token is
  HTTP 401. The token is what keeps a parent to its own delegates.
- The delegate's session starts on the endpoint's **first message**, never on a card fetch or a
  refused call. The task maps onto the delegation record: `working` while it runs,
  `input-required` while the delegate has asked a question (answered by a message on the same
  task), `completed` with the result as an artifact, `canceled` by cancel (which stops the run's
  process group), `failed` with the error, the exit code and the last 40 lines of the run's log,
  so a delegate that wrote nothing to its output still says why. Every result carries the task and
  context ids. A non-blocking send returns at once and the task goes on.
- The parent's environment names each endpoint: `YNH_DELEGATES` (the delegates' names,
  comma-separated) and, per delegate, `YNH_DELEGATE_<NAME>_CARD` (the agent card URL) and
  `YNH_DELEGATE_<NAME>_AUTH` (`Bearer <token>`). `<NAME>` is the delegate's harness name
  upper-cased, every character outside A-Z and 0-9 turned into `_`. Two entries that map to the
  same `<NAME>` are refused at run, naming both. With exactly one A2A delegate, `A2ACLI_AGENT_CARD`
  and `A2ACLI_AUTH` carry the same values, which the `a2a` CLI reads by itself; with several, they
  are not set, so the CLI never silently picks one.
- A delegate session never inherits its parent's `YNH_DELEGATE*` or `A2ACLI_*` values; a delegate
  with delegates of its own gets its own when it runs.
- A delegate that is not installed or not trusted refuses the run, with the command to fix it.

## Reaching a delegate over MCP

A parent whose delegates run over A2A reaches them through one MCP server, ynh itself. `ynh run`
adds it to the parent's MCP config on every vendor, under the name `ynh-delegates`, as
`ynh delegate-mcp` over stdio. A harness that declares an MCP server of that name is refused at
run. The server's entry carries the endpoints (the same `YNH_DELEGATE*` values as above, with
`YNH_HOME`, since Codex hands its MCP servers a trimmed environment), so it lives for the
session: the endpoints open before the config is written, and a run of such a harness keeps its
own snapshot, since the token in it is fresh. A run from a baked image has no such server: its
config was written at build time, so use the `a2a` CLI there ([The a2a CLI route](#the-a2a-cli-route)).

**Both routes, on a live run.** A live `ynh run` of a parent with A2A delegates gives it both
routes to the same endpoints: these tools, and the `a2a-cli` and `ynh-a2a-delegates` skills with
the `a2a` CLI. A baked image's run gets the skills only. Which route the parent uses is its own
choice: ynh states no preference, and the companion skill does not either. They differ in what
they need and how they speak:

| | MCP tools (this section) | `a2a` CLI ([The a2a CLI route](#the-a2a-cli-route)) |
|---|---|---|
| Needs | the vendor's allow rule for `ynh-delegates` (none on OpenCode; Codex is approved by ynh) | the `a2a` binary (0.4.0 or newer) and a shell allow rule; on Codex, a sandbox that reaches localhost |
| Results | structured, with a declared schema | the CLI's own output |
| Handle | `task_id` is the delegation handle (`dlg-` and 16 hex digits) | the A2A task id the CLI prints |
| Waiting | a bounded `wait` on the call, or poll | `send --async` and `task get` |

Both reach the one delegation record, so `delegate_list` shows a task started with the CLI, but its
id there is the handle, not the A2A task id the CLI used; use each route's own id with its own
tools. A task started with the CLI can be answered with `delegate_reply` (the answer goes to the
record), and one started with a tool can be inspected with `delegate_status`.

**One tool per delegate, and five for every parent.** All of them return `structuredContent`
with a declared `outputSchema`, and the same JSON as text. The fields are `task_id`, `context_id`,
`state` (`working`, `input-required`, `completed`, `failed` or `canceled`), `message`, and
`result` once completed or `log_tail` once failed. `task_id` is the delegation's handle: the one
thing to pass back.

| Tool | Does |
|---|---|
| `delegate_to_<name>` (`task`, optional `wait`) | Starts the delegate through its A2A endpoint, with its token, and returns its handle at once with state `working`. A wrong or missing token's 401 comes back as a tool error. |
| `delegate_list` | The session's delegations and their states. |
| `delegate_status` (`task_id`, optional `wait`) | The state. When `input-required`, `message` is the delegate's question. |
| `delegate_result` (`task_id`, optional `wait`) | The result once completed; the state for one still going. |
| `delegate_cancel` (`task_id`) | Cancels and stops the run's process group. |
| `delegate_reply` (`task_id`, `message`, optional `wait`) | A message on the same task: the answer to a question, which is how a parent answers `input-required` without elicitation. |

`<name>` is the delegate's harness name, lower-cased, each character outside a-z and 0-9 turned
into `_`. The server has no tool that takes a harness name or a path: a handle is checked against
`dlg-` and 16 hex digits, and must belong to this parent.

**Start and poll.** A call returns at once. `wait` (seconds) holds it until the delegate finishes
or asks a question, and past the wait it returns the handle with state `working` and says so.
The longest wait is a setting, `YNH_DELEGATE_MAX_WAIT` in seconds (30 when unset), and a larger `wait` is cut to
it. 30 sits under the shortest tool-call timeout known to apply by default, Codex's 60 seconds
(`tool_timeout_sec`). A parent that sets its own timeout lower than a wait it asks for loses the
call, not the delegate (see the evidence below): the handle is in the record, and `delegate_list`
finds it again.

**MCP Tasks.** A client that offers the Tasks extension (`io.modelcontextprotocol/tasks`; from
`params._meta["io.modelcontextprotocol/clientCapabilities"]` for 2026-07-28 clients, from
`initialize` for older ones) gets a task at once from a delegate's tool, and `tasks/get`,
`tasks/update` (an elicitation response, the answer) and `tasks/cancel` map onto the delegation.
The task id is the handle.

**No state of its own.** The handle tools read the delegation record by handle, so a parent that
restarted, with a new server and new endpoints, answers for the old handles. `delegate_reply`
goes through the A2A endpoint when this server started the task, and straight to the record when
it did not, or when the endpoint is still following the question's own message.

### Allowing the tools

A headless vendor denies MCP tools until they are allowed, and the delegates' tools are no
exception:

| Parent | Allow rule |
|---|---|
| Claude Code | `--allowedTools mcp__plugin_<harness>_ynh-delegates` (so `mcp__plugin_boss_ynh-delegates__delegate_to_reviewer` for the harness `boss`), or an `allow` entry in the settings |
| Codex | none: ynh sets `mcp_servers.ynh-delegates.default_tools_approval_mode="approve"` for a one-shot run, as it does for the harness's other servers, unless your config or `-c` sets it |
| Copilot CLI | `--allow-tool=ynh-delegates` |
| OpenCode | none |
| Cursor | not run (no licence), so not known |

The MCP server runs outside Codex's sandbox, so this route needs no sandbox network.

`ynh doctor` checks these rules for a harness with A2A delegates (none for a harness with only
`transport: native` delegates), as warnings that never fail a run. What it reads is the vendor's
settings on disk, so it **cannot confirm** a rule given on a command line (`--allowedTools`,
`--allow-tool`, `-c`) and warns anyway; a parent that passes the rule that way can ignore it.

| Finding | Fix |
|---|---|
| Claude Code: no `mcp__plugin_<harness>_ynh-delegates` rule in `~/.claude/settings.json`, `.claude/settings.json` or `.claude/settings.local.json` | add it to `permissions.allow`, or pass `--allowedTools` |
| Copilot CLI: no `ynh-delegates` entry in `~/.copilot/config.json`, `~/.copilot/settings.json` or `.copilot/settings.json` | add it to the allowed tools, or pass `--allow-tool=ynh-delegates` |
| Codex: `~/.codex/config.toml` sets `mcp_servers.ynh-delegates.default_tools_approval_mode` to something other than `approve`, or the harness sets `mcp_manual_approval` and the config does not approve it | remove the line or set it to `"approve"`; drop `mcp_manual_approval` |
| Cursor | not checked, and said so: the route has not been run there |
| OpenCode | nothing to check |

### Evidence

Live, 2026-11-13, a parent `boss` with one delegate `reviewer` (a Claude Code session answering
`REVIEWED-OK`), on a build of this change run by path, with `wait` 25: a delegate started, held
and finished in one call on Claude Code (`claude -p`), Codex (`codex exec`), Copilot CLI
and OpenCode (which calls it through `tools["ynh-delegates"].delegate_to_reviewer(...)` and read
the fields off the structured result). On Claude Code, start with no wait, `delegate_status`,
`delegate_cancel` and `delegate_list` each worked. Timeouts driven past: Claude Code with
`MCP_TOOL_TIMEOUT=5000` and a `wait` of 25 failed with `tool "delegate_to_reviewer" timed out
after 5s`; Codex with `tool_timeout_sec=5` failed with `timed out awaiting tools/call after
5s`. Neither parent got the handle, and the delegate kept running. **Not shown live:** Cursor (usage limit), the timeouts of OpenCode and Copilot CLI, MCP Tasks on a
real client, and `delegate_reply` answering a live `input-required` (a delegate's own question
tool is not built yet, see [Questions from a delegate](#questions-from-a-delegate)). Tasks and
`delegate_reply` are covered by tests only.

## The a2a CLI route

A parent reaches its A2A delegates by shell with the official
[a2a CLI](https://github.com/a2aproject/a2a-cli) (v0.4.0) and its skill. When a harness has an A2A
delegate, `ynh run` writes two skills into the parent's vendor skill folder (a live run also gives
the parent the same delegates as MCP tools, `delegate_to_<name>` and the handle tools; see [Reaching a
delegate over MCP](#reaching-a-delegate-over-mcp) for when to use which, and for a baked image, which
gets these skills and no tools):

- `a2a-cli`, the official skill, vendored **unchanged** from a2a-cli commit
  `5c95123d1c8d50a1cd6dfc2f28a14f5533a383ae` (skill version 2026.09.22) with its Apache-2.0
  licence. The official installs take no commit, so ynh keeps its own copy in
  `internal/a2acli/vendored/a2a-cli/`, with the commit recorded beside it. Updating it is a
  deliberate change.
- `ynh-a2a-delegates`, ynh's own short companion: which environment variables name each
  delegate's card and credential, and how to prefix a command with them
  (`A2ACLI_AGENT_CARD="$YNH_DELEGATE_<NAME>_CARD" A2ACLI_AUTH="$YNH_DELEGATE_<NAME>_AUTH" a2a send --async ...`);
  to start with `send --async` and poll with `task get`; never to invent a task id when a command is
  refused or fails; to pass a task as a plain prompt, not a long inline script; and to answer an
  `input-required` question with `send --task-id <id>` on the same task. It tells the parent never to
  print the credential.

ynh does not install the `a2a` binary. `ynh doctor` warns (it never fails) for a harness with A2A
delegates when the CLI route would not work. These checks are about the CLI route only: a live
run's MCP route needs none of them (its own allow rule has its own check, under
[Allowing the tools](#allowing-the-tools)). A parent that uses the
tools can ignore these warnings:

| Finding | Fix |
|---|---|
| `a2a` is not on PATH, or older than 0.4.0 | `curl -fsSL https://raw.githubusercontent.com/a2aproject/a2a-cli/main/install.sh \| A2A_VERSION=0.4.0 sh`, or `brew tap a2aproject/a2a-cli https://github.com/a2aproject/a2a-cli && brew install a2a` |
| the vendored skill is missing from the run dir's skill folder or differs from the pin | `ynh run <harness>` writes the pinned copy |
| Codex: the sandbox is not `workspace-write` with `network_access = true` | in `~/.codex/config.toml`, `sandbox_mode = "workspace-write"` and `[sandbox_workspace_write]` `network_access = true` |
| Claude Code: no `Bash(a2a:*)` allow rule | add it to `permissions.allow` in a settings file, or pass `--allowedTools 'Bash(a2a:*)'` |
| Copilot CLI: no `shell(a2a:*)` allow rule | add it to the allowed tools, or pass `--allow-tool 'shell(a2a:*)'` |

Two of these have a cost worth knowing. With Codex's default sandbox the `a2a` CLI cannot reach
`127.0.0.1` (`connect: operation not permitted`); turning `network_access` on means the shell is no
longer sandboxed from localhost generally. And without an allow rule a headless run denies every
`a2a` call. OpenCode needs no allow rule. Cursor's shell route has not been tested.

## One run dir, many live sessions

Several live sessions of one harness are now the normal case: two reviews by the same delegate, say.
In v0.17.0 a later `ynh run` empties and rebuilds the harness's one run dir under a live session
([ynh #765](https://github.com/eyelock/ynh/issues/765)). The [ynh #764](https://github.com/eyelock/ynh/pull/764)
lock only stops two assemblies interleaving. A run dir's contents also differ per launch:
`--instructions`, Codex's compose of the project's `AGENTS.md`, and OpenCode's launch-time
`AGENTS.md` and session context.

So a run dir becomes a **content-addressed snapshot** behind a stable link:

1. Under the [ynh #764](https://github.com/eyelock/ynh/pull/764) assembly lock, a run assembles into a staging folder, including every per-launch
   file, and hashes the tree (paths, modes, contents).
2. If a snapshot with that hash exists, the run uses it and discards the staging folder: an
   unchanged rebuild is skipped. If not, the staging folder is renamed to
   `$YNH_HOME/run/.snapshots/<name>/<hash>`.
3. The stable path `$YNH_HOME/run/<name>` is a symlink, swapped to the snapshot atomically (a new
   link renamed over the old one). Existing project symlinks and the legacy bare-name alias keep
   resolving.
4. Still under the assembly lock, the run takes a **shared lock** on its snapshot's lock file
   (outside the snapshot, under `cache/run-locks/`). `ynh run` stays alive as the parent of every
   vendor CLI ([ynh #745](https://github.com/eyelock/ynh/issues/745)), so it holds that lock until
   the vendor exits.
5. The run launches the vendor on the snapshot's **real path**, never the stable link, so the
   session reads its own snapshot to the end.
6. Still under the assembly lock, the run deletes each snapshot that is neither current nor
   locked (it can take the lock exclusively without waiting).

A baked image's pre-assembled run dir is used as it is, as in v0.17.0.

**Limit:** Codex and Cursor project symlinks point through the stable link, so they show the newest
snapshot, not the session's. A session ynh launches reads its own snapshot through its working
folder, Codex and Cursor included. Anything reading the project's links reads the newest.

**Resume and trust on Codex and Cursor.** Both key state by the folder a session starts in, and that is
a new snapshot whenever the harness changes. ynh carries it instead of giving them a stable folder:
`--resume` finds the newest session that started in any snapshot of the harness and resumes it by id in
the newest snapshot, and Cursor gets `--trust` for an accepted harness (see
[vendors](vendors.md#vendor-notes)). Two runs of one harness do not share a folder unless their inputs
are equal, when they share one sealed snapshot.

## What is removed

- Delegates as vendor agent files, `.ynh-delegate-agents.json`, Claude's `--agents`, per-delegate
  MCP carried into a subagent, and OpenCode delegate subagents
  ([ynh #762](https://github.com/eyelock/ynh/pull/762)).
- The parent trust digest's coverage of delegate commands.
- `ynd export` leaves delegates out with a warning: they need ynh at runtime.

## Later

- **A2A** ([v1.0](https://a2a-protocol.org/latest/specification/)) as a binding for remote
  delegates. Its task states map onto MCP Tasks, but it is HTTP-only, and no vendor CLI is known to
  be an A2A client.
- **The layer-1 record** (v0.19.0, [ynh #746](https://github.com/eyelock/ynh/issues/746)): `ynh run`'s
  own vendor-neutral record of every launch, written to the ynr spool. It builds on the delegation
  record, so each delegate session leaves its own record like any other ynh session.
- **Continuing a delegate's session** with a follow-up message, where the vendor can resume with a
  prompt.

## Evidence

- **Parity audit**, 2026-11-03, live runs on all five vendors at ynh 8fdfb1c (results on the
  parity page, limit [L12](vendor-parity.md#l12-delegates-carry-only-part-of-a-harness)). Off Claude Code, a delegate carried only its
  instructions. On Claude Code it also carried its MCP servers, but its hooks did not fire. No
  vendor carried its workspace folders, and on OpenCode a delegate did not run until
  [ynh #762](https://github.com/eyelock/ynh/pull/762).
- **Round-trip spike**, 2026-11-03: a throwaway MCP server exposing one delegate as a tool, with
  Claude Code 2.1.317, Codex 0.166.0, Cursor agent 2026.10.22, Copilot CLI 1.0.105 and OpenCode
  2.2.3 as parent. Only Claude Code interactive offered Tasks. It was verified end to end: handle,
  polling, `input_required` dialog, `tasks/update`, result. Blocking elicitation was answered in the
  Codex, Copilot CLI and OpenCode interactive UIs. Cursor's CLI declined it in 6 ms without asking.
  Every headless mode declined it or never answered. The Go MCP SDK v1.8.0 has no Tasks
  ([go-sdk #626](https://github.com/modelcontextprotocol/go-sdk/issues/626)). Vendor tool-call
  timeouts were not measured; the build measures them to set the bounded wait.
