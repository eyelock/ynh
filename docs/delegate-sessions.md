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
parent also gets four handle tools: `delegate_list`, `delegate_status`, `delegate_result` and
`delegate_cancel`. A blocking parent uses them to wait in turns, and any parent uses them to come
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
- The handle tools take a **handle**, never a path: the handle is checked against `dlg-` and 16 hex
  digits and looked up under `$YNH_HOME/delegations/`.

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
