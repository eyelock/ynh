# Delegate sessions

> **Status:** built for v0.18.0 ([ynh #729](https://github.com/eyelock/ynh/issues/729)) and merged on `develop`, not yet released. v0.17.0, the release you may have, turns every delegate into a vendor subagent: for that, see [delegates_to](harnesses.md#delegates-to-optional) and [limit L12](vendor-parity.md#l12-delegates-carry-only-part-of-a-harness). Page checked on 2026-11-14 against `develop` at [adfcf55b](https://github.com/eyelock/ynh/commit/adfcf55b), the live v0.18.0 acceptance run of the same day at [ce97443](https://github.com/eyelock/ynh/commit/ce974435), and its re-verification at [c2690e2d](https://github.com/eyelock/ynh/commit/c2690e2d) ([Evidence](#evidence)). What those runs could not show is said where it applies, and gathered under [Not shown live](#not-shown-live).

A harness's `delegates_to` names other harnesses it may hand work to. From v0.18.0 each entry runs
one of two ways, chosen by its [`transport`](harnesses.md#transport):

- **`a2a`, the default.** The delegate is its own ynh session, started on demand and detached from
  the parent. The parent talks to it over [A2A](https://a2a-protocol.org/latest/specification/)
  (Agent2Agent, v1.0), through ynh's MCP server or the official `a2a` CLI.
- **`native`, an option.** The delegate is a subagent inside the parent's own vendor session, as in
  v0.17.0. It carries only part of its harness ([L12](vendor-parity.md#l12-delegates-carry-only-part-of-a-harness)).

This page explains why the default is A2A, how the pieces fit, and what each choice costs. The
fields themselves are in [delegates_to](harnesses.md#delegates-to-optional) and
[Transport](harnesses.md#transport); the environment variables are in the [reference](reference.md).

## Why

**A subagent is only part of a harness.** A native delegate is written as a vendor subagent
definition inside the parent's session. On every vendor it carries its instructions; only on Claude
Code its MCP servers; on no vendor its hooks or workspace folders (limit
[L12](vendor-parity.md#l12-delegates-carry-only-part-of-a-harness), [ynh #729](https://github.com/eyelock/ynh/issues/729)).
It also dies with the parent's session. A delegate that is its own ynh session is designed to be
assembled like any other run, with its own vendor, MCP servers, hooks, workspace grant, profile or
focus, and it can outlive whoever started it. That its own instructions, MCP servers and hooks take
effect inside its session was shown live on Claude Code, Codex, Copilot CLI and OpenCode, and its
workspace folders on all but OpenCode; Cursor was not run ([Not shown live](#not-shown-live)).

**No vendor CLI speaks A2A, so ynh bridges it.** When this was designed (2026-11-13), none of the
five vendor CLIs was an A2A client: each had only an open request
([anthropics/claude-code #104852](https://github.com/anthropics/claude-code/issues/104852),
[openai/codex #58146](https://github.com/openai/codex/issues/58146),
[anomalyco/opencode #62903](https://github.com/anomalyco/opencode/issues/62903)), Copilot CLI had
nothing found, and Cursor's agent protocol is ACP. So a parent always reaches A2A through a bridge.
ynh gives it two, both reaching the same endpoints: ynh's own MCP server, which every vendor CLI can
call, and the [official a2a CLI](https://github.com/a2aproject/a2a-cli) with its skill, as a shell
route. There is no `ynh delegate` command for the parent.

**Why A2A on the wire, and not MCP alone.** A2A is built for one agent handing a task to another: a
durable task id, a cancel, and a task state, `input-required`, for a question back to the sender,
answered by a new message on the same task. MCP has these only through its Tasks extension, which,
of the five, only Claude Code's interactive mode offered in the spike of 2026-11-03. A2A is also what
other agent tools speak, so the same endpoint serves ynh's MCP tools, the `a2a` CLI and any A2A
client. The cost is local plumbing: one HTTP endpoint, one port and one token per delegate.

**Why the token, not the bridge, keeps a parent to its own delegates.** The `a2a` CLI can message
any URL, so a rule like "a parent starts only the delegates it names" cannot live in the bridge. It
lives on the delegate's side: each endpoint has its own bearer token, and only the parent's run
holds it.

**Why native stays.** Native subagents stay as a setting, chosen per entry or for the whole harness,
for a parent that wants its delegate inside its own session and can live with L12. A2A is the
default.

## The shape

```
                      ┌── MCP (stdio) ──> ynh-delegates ──┐
parent vendor CLI ────┤                (ynh delegate-mcp) ├── A2A over HTTP, ──> localhost endpoint ──first message──> delegate's ynh session
 (ynh run of parent)  └── shell ──> a2a CLI ──────────────┘   bearer token        (one per a2a delegate,              (detached, own run dir,
                                                                                   opened by ynh run)                  own vendor and harness)
                                                     │                                                                         │
                                                     └──────────────────── delegation record on disk ─────────────────────────┘
```

- `ynh run` of a parent opens one localhost A2A endpoint for each `a2a` delegate, and puts each
  endpoint's card URL and token in the parent's environment and in the `ynh-delegates` MCP server's
  entry.
- The delegate's own session starts on its endpoint's first message, never before.
- The parent never holds the delegate's tools. It sees the delegate's state, its questions and its
  result.
- Everything about a delegation is written to a record on disk, so either route, or a parent that
  restarted, reads the same state.

## Choosing a transport

`transport` takes `a2a` or `native`, on a `delegates_to` entry or as a harness-level default; an
entry's value wins, and `a2a` applies when neither says. One harness may mix the two
([Transport](harnesses.md#transport)). `ynh info` shows each delegate's resolved transport and where
it came from, as `transport=a2a (default)`, `transport=native (entry)` or `(harness)`.

A `native` delegate on a vendor with no native subagents is refused by `ynh install`, `ynh run` and
`ynd validate`, with no fallback to `a2a`:

```
<harness>: delegates_to[0] (<entry>) has transport native, and cursor has no native subagents
  Fix: set transport: a2a on this delegate, or run on a vendor with subagents
```

Today that is only Cursor: its subagents are unverified ([L35](vendor-parity.md#l35-cursor-under-ynh-run-not-shown-live)),
so its adapter declares none. Claude Code, Codex, Copilot CLI and OpenCode declare native subagents.

## Which harness an A2A delegate runs as

A `delegates_to` entry is a source (`git`, `path`, `ref`), not an installed harness. For an `a2a`
entry, ynh matches it to the **installed harness whose source matches it** (the Git URL and path, or
the local folder) and runs the delegate as that harness, under that harness's own trust, workspace
and hook grants. The schema is unchanged.

The match is made when `ynh run` of the parent opens the endpoints, so a delegate that cannot be
reached **refuses the parent's whole run**, before the vendor starts, with the command to fix it. A
parent must not start believing it has a delegate it cannot reach. The refusal is one of:

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

A delegate that executes nothing (no sensors, hooks or MCP servers) needs no `ynh trust accept` to
be matched. ynh never installs or trusts anything on a parent's behalf. When several installs match one
entry, the trusted match with the smallest id wins, so the result never depends on listing order.

## Trust

- A parent can start only its own `a2a` delegates: each endpoint refuses any request without its
  token, and only the parent's run holds it.
- An `a2a` delegate runs under its own trust grant, as above: its MCP servers, hooks and focus
  prompt are reviewed there, not in the parent's digest. The review step was seen live (a delegate
  that declares commands was refused until `ynh trust accept`), and once accepted its servers and
  hooks took effect inside its session ([Evidence](#evidence)). A `native`
  delegate is part of the parent's
  session, so its MCP servers and focus prompt are in the parent's digest.
- The parent's digest covers every entry as written (source, profile or focus, and its settled
  transport), so repointing an entry or flipping it between `a2a` and `native` asks for review again.
  A parent accepted before v0.18.0 that has delegates reads `changed` once.
- A delegate's own `delegates_to` applies only inside its own session.

## Reaching a delegate: the A2A endpoints

`ynh run` of a harness opens one localhost A2A endpoint for each `delegates_to` entry whose
transport is `a2a` (the default), and closes them when the run ends. A delegation already running
is a detached session and goes on.

- Each endpoint listens on `127.0.0.1` on a free port, serves JSON-RPC with
  [a2a-go](https://github.com/a2aproject/a2a-go) v2 (v2.8.0), and has its own random bearer token.
  Every request but the agent card needs `Authorization: Bearer <token>`; a wrong or missing token is
  HTTP 401, and starts nothing. The card is served without a token, by design.
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
  are not set, so the CLI never silently picks one. Whatever the number, `A2ACLI_CREDENTIAL_HELPER`
  names a helper that answers with each endpoint's own token (see
  [The a2a CLI route](#the-a2a-cli-route)).
- A delegate session never inherits its parent's `YNH_DELEGATE*` or `A2ACLI_*` values; a delegate
  with delegates of its own gets its own when it runs. It does get `YNH_DELEGATION_RECORD`, which
  marks it as a delegate's session ([Questions from a delegate](#questions-from-a-delegate)).

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
thing to pass back. Structured results are not a nicety: in the spike, an OpenCode parent given a
plain-text result tried to read `task_id` off a string and started a stray task.

| Tool | Does |
|---|---|
| `delegate_to_<name>` (`task`, optional `wait`) | Starts the delegate through its A2A endpoint, with its token, and returns its handle at once with state `working`. A wrong or missing token's 401 comes back as a tool error. |
| `delegate_list` | Every delegation of this parent harness in this working folder, with its state: those started by earlier sessions and by the `a2a` CLI as well as this session's. |
| `delegate_status` (`task_id`, optional `wait`) | The state. When `input-required`, `message` is the delegate's question. |
| `delegate_result` (`task_id`, optional `wait`) | The result once completed; the state for one still going. |
| `delegate_cancel` (`task_id`) | Cancels and stops the run's process group. |
| `delegate_reply` (`task_id`, `message`, optional `wait`) | A message on the same task: the answer to a question, which is how a parent answers `input-required` without elicitation. |

`<name>` is the delegate's harness name, lower-cased, each character outside a-z and 0-9 turned
into `_`. The server has no tool that takes a harness name or a path: a handle is checked against
`dlg-` and 16 hex digits, and must belong to this parent.

**How OpenCode shows these tools.** OpenCode (2.2.3) exposes MCP tools through its Code Mode by
default: the model sees one `execute` tool and calls a server's tools as code inside it, as
`tools["ynh-delegates"].delegate_to_<name>({...})`, reading the fields off the structured result.
In a check of 2026-11-13 an OpenCode parent did not find the delegate tools until its prompt named
Code Mode; a prompt to an OpenCode parent should say that the delegates are reached through
`execute`, under `tools["ynh-delegates"]`.

**Start and poll.** A call returns at once. `wait` (seconds) holds it until the delegate finishes
or asks a question, and past the wait it returns the handle with state `working` and says so.
The longest wait is a setting, `YNH_DELEGATE_MAX_WAIT` in seconds (30 when unset), and a larger
`wait` is cut to it. The reason is the vendors' own MCP tool-call timeouts: a long delegate must
never hold a tool call open. 30 sits under the shortest timeout known to apply by default, Codex's
60 seconds (`tool_timeout_sec`). A parent that sets its own timeout lower than a wait it asks for
loses the call, not the delegate (see [Evidence](#evidence)): the handle is in the record, and
`delegate_list` finds it again.

**MCP Tasks.** A client that offers the Tasks extension (`io.modelcontextprotocol/tasks`; from
`params._meta["io.modelcontextprotocol/clientCapabilities"]` for 2026-07-28 clients, from
`initialize` for older ones) gets a task at once from a delegate's tool, and `tasks/get`,
`tasks/update` (an elicitation response, the answer) and `tasks/cancel` map onto the delegation.
The task id is the handle. Of the five vendors, only Claude Code's interactive mode offered Tasks in
the spike of 2026-11-03. The Go MCP SDK has no Tasks extension
([go-sdk #626](https://github.com/modelcontextprotocol/go-sdk/issues/626)), so ynh writes this
server's JSON-RPC itself.

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
| Cursor | not known: the route has not been run on Cursor ([Not shown live](#not-shown-live)) |

The MCP server runs outside Codex's sandbox, so this route needs no sandbox network.

`ynh doctor` checks these rules for a harness with A2A delegates (none for a harness with only
`transport: native` delegates), as warnings that never fail a run. What it reads is the vendor's
settings on disk, so it **cannot confirm** a rule given on a command line (`--allowedTools`,
`--allow-tool`, `-c`) and warns anyway; a parent that passes the rule that way can ignore it.
For Claude Code it reads the three settings files as JSON, `permissions.allow` and
`permissions.deny` apart, and warns when a deny rule names the server or one of its tools even if an
allow rule is also there, since deny wins. A file that is not valid JSON gets a note, and its rules
are not counted. For Copilot CLI it only looks for the allow entry's text: its settings schema is
unconfirmed, and no deny list is known, so a denied entry there is not seen.

| Finding | Fix |
|---|---|
| Claude Code: a `permissions.deny` rule for `mcp__plugin_<harness>_ynh-delegates` (or one of its tools), or no allow rule for it in `~/.claude/settings.json`, `.claude/settings.json` or `.claude/settings.local.json` | remove the deny rule; add the allow rule to `permissions.allow`, or pass `--allowedTools` |
| Copilot CLI: no `ynh-delegates` entry in `~/.copilot/config.json`, `~/.copilot/settings.json` or `.copilot/settings.json` | add it to the allowed tools, or pass `--allow-tool=ynh-delegates` |
| Codex: `~/.codex/config.toml` sets `mcp_servers.ynh-delegates.default_tools_approval_mode` to something other than `approve`, or the harness sets `mcp_manual_approval` and the config does not approve it | remove the line or set it to `"approve"`; drop `mcp_manual_approval` |
| Cursor | not checked, and said so: the route has not been run there |
| OpenCode | nothing to check |

## The a2a CLI route

A parent reaches its A2A delegates by shell with the official
[a2a CLI](https://github.com/a2aproject/a2a-cli) (v0.4.0) and its skill. When a harness has an A2A
delegate, `ynh run` writes two skills into the parent's vendor skill folder (a live run also gives
the parent the same delegates as MCP tools; see [Reaching a delegate over MCP](#reaching-a-delegate-over-mcp)):

- `a2a-cli`, the official skill, vendored **unchanged** from a2a-cli commit
  `5c95123d1c8d50a1cd6dfc2f28a14f5533a383ae` (skill version 2026.09.22) with its Apache-2.0
  licence. The official installs take no commit, so ynh keeps its own copy in
  `internal/a2acli/vendored/a2a-cli/`, with the commit recorded beside it. Updating it is a
  deliberate change.
- `ynh-a2a-delegates`, ynh's own short companion (`internal/a2acli/companion.md`): for each delegate,
  the address of its agent card, which a command gives the CLI with `-a`
  (`a2a send -a <card address> --async "<task>"`, and the same `-a` on every other command, `task get`
  and `task cancel` included); to start with `send --async` and poll with `task get`; never to invent
  a task id when a command is refused or fails; to pass a task as a plain prompt, not a long inline
  script; and to answer an `input-required` question with `send --task-id <id>` on the same task. It
  lists the addresses because they change on every run: ynh writes this skill again for each launch,
  once the endpoints are open. It tells the parent never to print a credential, and never to pass one.

A command names a delegate by its address and carries **no variable**, because Claude Code refuses a
command with one under the allow rule `Bash(a2a:*)` ("A variable in this command can't be checked
before it runs"), with `A2ACLI_AGENT_CARD="$YNH_DELEGATE_<NAME>_CARD" a2a ...` and `a2a -a "$VAR"` alike
([ynh #849](https://github.com/eyelock/ynh/issues/849)). The credential comes from the a2a CLI's own
credential helper instead: for every run with A2A delegates ynh writes a small private script (0700, in
its own temporary folder) and sets `A2ACLI_CREDENTIAL_HELPER` to it, scoped to `127.0.0.1`. The CLI
runs it for each request and it answers with the bearer token of the endpoint asked about, so several
delegates each get their own and nothing secret is in a command. This works under the one allow rule
`ynh doctor` recommends, with any number of delegates.

The helper folder is removed when the session ends, normally or on SIGINT, SIGTERM or SIGHUP once the
vendor is running. It is left behind if ynh is killed with SIGKILL, or signalled after the endpoints
open and before the vendor starts. The tokens in it are for endpoints that closed with that run, so
a leftover is not a live credential, and the next launch with A2A delegates removes any such folder
older than seven days. A baked image's skill is written before any endpoint exists, so it cannot list
addresses: it teaches the variable form
(`A2ACLI_AGENT_CARD="$YNH_DELEGATE_<NAME>_CARD" A2ACLI_AUTH="$YNH_DELEGATE_<NAME>_AUTH" a2a send --async ...`),
which a vendor that does not refuse a variable accepts.

A task on this route goes `send --async` (state `working`), then `task get` until it is
`input-required` (answered with `send --task-id <id> "<answer>"`) or `completed`, and
`task cancel <id>` takes it to `canceled`.

ynh does not install the `a2a` binary. `ynh doctor` warns (it never fails) for a harness with A2A
delegates when the CLI route would not work. These checks are about the CLI route only: a live
run's MCP route needs none of them (its own allow rule has its own check, under
[Allowing the tools](#allowing-the-tools)). A parent that uses the tools can ignore these warnings:

| Finding | Fix |
|---|---|
| `a2a` is not on PATH, or older than 0.4.0 | `curl -fsSL https://raw.githubusercontent.com/a2aproject/a2a-cli/main/install.sh \| A2A_VERSION=0.4.0 sh`, or `brew tap a2aproject/a2a-cli https://github.com/a2aproject/a2a-cli && brew install a2a` |
| the vendored skill is missing from the run dir's skill folder or differs from the pin | `ynh run <harness>` writes the pinned copy |
| Codex: the sandbox is not `workspace-write` with `network_access = true` | in `~/.codex/config.toml`, `sandbox_mode = "workspace-write"` and `[sandbox_workspace_write]` `network_access = true` |
| Claude Code: no `Bash(a2a:*)` allow rule | add it to `permissions.allow` in a settings file, or pass `--allowedTools 'Bash(a2a:*)'`. That one rule covers every command the skill teaches, for any number of delegates |
| Copilot CLI: no `shell(a2a:*)` allow rule | add it to the allowed tools, or pass `--allow-tool 'shell(a2a:*)'` |

Three things on this route have a cost worth knowing:

- **Codex's sandbox.** With Codex's default sandbox the `a2a` CLI cannot reach `127.0.0.1`
  (`connect: operation not permitted`). Turning `network_access` on fixes that, and means the shell
  is no longer sandboxed from localhost generally. The MCP route needs neither.
- **Claude Code with several delegates.** Under `Bash(a2a:*)` a command that carries a variable is
  refused ("A variable in this command can't be checked before it runs"), so the companion skill gives
  each delegate's card address (`a2a send -a <card address> ...`) and the CLI gets the token from the
  credential helper, with no variable in any command
  ([ynh #849](https://github.com/eyelock/ynh/issues/849), fixed for v0.18.0). It works with any number
  of delegates under the one allow rule: shown live with two delegates, each question answered with
  `send --task-id`. A baked image still teaches the variable form.
- **Allow rules.** Without one a headless run denies every `a2a` call. OpenCode needs none. Cursor's
  shell route has not been run.

## Questions from a delegate

A delegate session gets one extra tool from ynh for asking its parent a question: `ask_parent`, from
an MCP server named `ynh-parent` (`ynh parent-mcp`, which `ynh run` adds to the config of a delegate's
session, on every vendor, as it does `ynh-delegates` for a parent). Only a delegate's session has it:
a `ynh run` carrying `YNH_DELEGATION_RECORD`, which the delegate's launcher sets. A harness that
declares an MCP server of that name is refused at run, with a fix to rename it.

A question travels as an A2A task in state `input-required`:

1. `ask_parent` (`question`, optional `wait`) writes the question to the delegation record and waits
   for its answer. At most 45 seconds, `YNH_PARENT_ASK_MAX_WAIT` in seconds changes it: that sits
   under Codex's 60-second tool-call timeout.
2. The record puts the delegate's A2A task into `input-required`, with the question as the task's
   message. The parent sees it by its route:
   - **MCP route, with Tasks** (Claude Code interactive): the task moves to `input_required` with an
     `elicitation/create` in `inputRequests`, and the answer comes back through `tasks/update`. A
     declined elicitation is recorded as the answer `(the parent declined to answer)`, and the
     delegate's tool says `declined`.
   - **MCP route, elicitation but no Tasks** (the interactive UIs of Codex, Copilot CLI and
     OpenCode): during a call that waits (`wait` on the delegate's tool, `delegate_status`,
     `delegate_result` or `delegate_reply`) the server sends a server-initiated `elicitation/create`,
     once for a question, and records the answer against that question only. The client may
     take longer than the call is held: the answer is then recorded whenever it comes, unless the
     question was answered another way first (by `delegate_reply`, say), in which case it is dropped
     and never lands on a later question. While the elicitation is outstanding, the result's note
     says it is still waiting for its answer: wait, or answer with `delegate_reply`, and the
     elicitation's answer is dropped. A client that refuses the request, declines it or
     cancels it has not answered: Cursor's CLI refuses without asking, and Copilot CLI's `-p`
     declines at once for want of a person. The question then stays open in the result, with a note
     that it was put to the client, for the next route.
   - **MCP route, headless** (`claude -p`, `codex exec`, `copilot -p`, `opencode run`, and anything
     above that gave no answer): the question is in the structured result of the delegate's tool or
     `delegate_status`, with state `input-required` and the question as `message`. The parent's
     model answers it with `delegate_reply` on the same task. This is the model
     answering, not a person ([L39](vendor-parity.md#l39-a-headless-parent-s-model-answers-a-delegate-s-question-not-a-person)).
   - **a2a CLI route:** `a2a task get` shows state `input-required` with the question as the status
     message, and the parent's model answers with `a2a send --task-id <id> "<answer>"` on the same
     task, as the `ynh-a2a-delegates` skill teaches.
3. The answer goes back to the delegate's tool, which returns `answered` with the `answer`.

**A question outlives the delegate's wait.** A question nobody answers stays `input-required`, and a
parent that comes back later (a restart, a handoff) collects it: the record holds it, and
`delegate_status` or `delegate_list` shows it. When the delegate's wait runs out, `ask_parent`
returns `unanswered`, saying no answer came, and the question stays open. The delegate chooses: call
`ask_parent` again with the `question_id` to keep waiting, call it with `question_id` and `withdraw`
to close the question and go on, on its own default or by failing plainly, or just carry on. This
keeps the delegate's tool call inside its vendor's timeout without losing the question. A delegation
that has ended shows its final state, whatever it left open. A task on the a2a CLI route that was
left `input-required` stays so, as the A2A task store holds it, until it is next spoken to: a
message on a task whose question is closed follows the record to where the delegation is, instead of
answering.

**An answer belongs to one question.** Each answer is recorded against the question it was given
for. A late elicited answer for a question already answered (by `delegate_reply`, say) is dropped,
never recorded against a later question.

`ask_parent` returns `answered`, `declined`, `unanswered`, `withdrawn` or `ended` (the delegation was
cancelled while it waited), with the `question_id`, the `answer` when answered, and a `message` on
what to do next.

**Allowing the tool.** A delegate's one-shot session has no one to approve it either. Codex's
`ynh run` approves it with the harness's own servers, and Copilot CLI's `--allow-all-tools` and
OpenCode need nothing; for Claude Code, `ynh run` adds `--allowedTools=mcp__plugin_<delegate>_ynh-parent`
to a delegate's one-shot session. It adds the same kind of rule for the delegate's own declared MCP servers, on a delegate's
one-shot Claude Code session only, one whose delegation record names that harness (not on a harness run directly,
nor on another harness the delegate's model runs from its shell, nor an interactive session, nor with
`mcp_manual_approval`), so the delegate can call its own harness's tools headless with no allow rule in
the user's Claude settings ([MCP servers](mcp.md#claude-code-format),
[ynh #858](https://github.com/eyelock/ynh/issues/858)). A harness with `mcp_manual_approval` gets none, and so no question. The `ynh-parent` rule is added on the same condition, so a harness the delegate's model runs from its shell gets no rule for it either.

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

## Native subagents

With `transport: native`, a delegate is what it was in v0.17.0: ynh assembles it as a subagent
definition in the parent's run dir, and the parent's vendor runs it inside the parent's session. No
endpoint is opened, no `ynh-delegates` server is added for it, and no installed harness is matched:
the subagent is built from the entry's source.

It carries only part of its harness: its instructions on every vendor, its MCP servers only on
Claude Code, and its hooks and workspace folders nowhere. On OpenCode it is offered as a subagent the
task tool can call (written with `mode: all`, [ynh #762](https://github.com/eyelock/ynh/pull/762)).
This is limit [L12](vendor-parity.md#l12-delegates-carry-only-part-of-a-harness), and it is the
price of choosing native. A native delegate also ends with the parent's session.

`ynd preview` and `ynh image` assemble native delegates the same way; `ynd export` exports native
delegates and leaves `a2a` ones out with one warning, since they need ynh at runtime.

## One run dir, many live sessions

Several live sessions of one harness are the normal case once delegates are sessions: two reviews
by the same delegate, say. Before v0.18.0, a later `ynh run` emptied and rebuilt the harness's one
run dir under a live session ([ynh #765](https://github.com/eyelock/ynh/issues/765)); the
[ynh #764](https://github.com/eyelock/ynh/pull/764) lock only stopped two assemblies interleaving.
A run dir's contents also differ per launch: `--instructions`, Codex's compose of the project's
`AGENTS.md`, and OpenCode's launch-time `AGENTS.md` and session context.

So a run dir is a **content-addressed snapshot** behind a stable link:

1. Under the assembly lock, a run assembles into a staging folder, including every per-launch file,
   and hashes the tree (paths, modes, contents).
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

A baked image's pre-assembled run dir is used as it is.

**Limit:** Codex and Cursor project symlinks point through the stable link, so they show the newest
snapshot, not the session's. A session ynh launches reads its own snapshot through its working
folder, Codex and Cursor included. Anything reading the project's links reads the newest.

**Resume and trust on Codex and Cursor.** Both key state by the folder a session starts in, and that is
a new snapshot whenever the harness changes. ynh carries it instead of giving them a stable folder:
`--resume` finds the newest session that started in any snapshot of the harness and resumes it by id in
the newest snapshot, and Cursor gets `--trust` for an accepted harness (see
[vendors](vendors.md#vendor-notes)). Two runs of one harness do not share a folder unless their inputs
are equal, when they share one sealed snapshot.

## Not shown live

The v0.18.0 verification of 2026-11-14 (the acceptance run, then a re-run on `develop` at
[c2690e2d](https://github.com/eyelock/ynh/commit/c2690e2d); [Evidence](#evidence)) could not show
these. Each is open, not a known failure:

- **Cursor, on every route** (MCP tools, the `a2a` CLI, and as a delegate): its Workspace Trust
  stopped the runs, and the Cursor delegate was left unaccepted. Native on Cursor is refused by design
  ([Choosing a transport](#choosing-a-transport)).
- **A workspace grant's effect on an OpenCode delegate.** ynh grants the folder, but OpenCode, with no
  restricted agent, reads folders outside its own without any grant, so a test of the grant cannot
  fail there.
- **A person answering a question** through elicitation or MCP Tasks in a real client. Covered by
  tests and by a scripted MCP client only; every live question was answered by the parent's model.
- **The allow rule for a baked image's** declared MCP servers on a Claude Code delegate: unit-tested
  only.
- **A harness run from a Claude Code delegate's shell** (the nested case): shown inside a Copilot CLI
  delegate only ([Claude Code Format](mcp.md#claude-code-format)).
- **The tool-call timeouts of OpenCode and Copilot CLI**, which were not driven past.

## Not built

- **Remote delegates.** Endpoints listen on `127.0.0.1` only, for delegates on the same machine.
- **Continuing a delegate's session** with a follow-up message after it completes, where the vendor
  can resume with a prompt.
- **`ynh run`'s own record of every launch** (v0.19.0, [ynh #746](https://github.com/eyelock/ynh/issues/746)),
  written to the ynr spool, so each delegate session would leave its own record like any other ynh
  session.

## Evidence

- **Parity audit**, 2026-11-03, live runs on all five vendors at ynh 8fdfb1c (results on the
  parity page, limit [L12](vendor-parity.md#l12-delegates-carry-only-part-of-a-harness)). Off Claude
  Code, a delegate carried only its instructions. On Claude Code it also carried its MCP servers, but
  its hooks did not fire. No vendor carried its workspace folders, and on OpenCode a delegate did not
  run until [ynh #762](https://github.com/eyelock/ynh/pull/762).
- **MCP round-trip spike**, 2026-11-03: a throwaway MCP server exposing one delegate as a tool, with
  Claude Code 2.1.317, Codex 0.166.0, Cursor agent 2026.10.22, Copilot CLI 1.0.105 and OpenCode
  2.2.3 as parent. Only Claude Code interactive offered Tasks; it was verified end to end (handle,
  polling, `input_required` dialog, `tasks/update`, result). Blocking elicitation was answered in the
  Codex, Copilot CLI and OpenCode interactive UIs. Cursor's CLI declined it in 6 ms without asking.
  Every headless mode declined it or never answered.
- **Design review**, 2026-11-13: MCP against A2A for agent-to-agent, from web sources (the vendor
  requests linked under [Why](#why); the [A2A specification](https://a2a-protocol.org/latest/specification/)
  v1.0; [a2a-go](https://github.com/a2aproject/a2a-go); the [a2a CLI](https://github.com/a2aproject/a2a-cli)).
  It settled the four decisions this page describes: two bridges (ynh's MCP tools and the `a2a` CLI)
  and no ynh delegate command; endpoints opened lazily by `ynh run`, one token each; the `transport`
  setting; and native refused, with a Fix line, on a vendor without subagents.
- **A2A round-trip spike**, 2026-11-13, not merged: a2a-go v2.8.0 serving a stand-in delegate,
  reached through an MCP bridge and through the `a2a` CLI v0.4.0, with Claude Code 2.1.323, Codex
  0.166.0, Copilot CLI 1.0.109 and OpenCode 2.2.3 as parent, one headless run per vendor and route.
  Both routes went from start to an `input-required` round trip to cancel on all four, Codex's CLI
  route only with sandbox network on. A wrong or missing token was HTTP 401 on both routes and
  started nothing. Cursor was not run (usage limit). It is where start-and-poll, the per-delegate
  environment and structured results came from.
- **The build**, merged on `develop`: the detached session and its record
  ([ynh #817](https://github.com/eyelock/ynh/pull/817), [ynh #819](https://github.com/eyelock/ynh/pull/819)),
  the `transport` setting ([ynh #825](https://github.com/eyelock/ynh/pull/825),
  [ynh #826](https://github.com/eyelock/ynh/pull/826), [ynh #829](https://github.com/eyelock/ynh/pull/829)),
  the endpoints ([ynh #827](https://github.com/eyelock/ynh/pull/827)), the `a2a` CLI route
  ([ynh #830](https://github.com/eyelock/ynh/pull/830)), the MCP tools
  ([ynh #832](https://github.com/eyelock/ynh/pull/832), [ynh #841](https://github.com/eyelock/ynh/pull/841)),
  `ynh info`'s transport ([ynh #828](https://github.com/eyelock/ynh/pull/828)), questions
  ([ynh #844](https://github.com/eyelock/ynh/pull/844)) and snapshots
  ([ynh #776](https://github.com/eyelock/ynh/pull/776), [ynh #813](https://github.com/eyelock/ynh/pull/813),
  [ynh #815](https://github.com/eyelock/ynh/pull/815)).
- **MCP route timeouts**, live, 2026-11-13, on a build of [ynh #832](https://github.com/eyelock/ynh/pull/832)
  run by path: Claude Code with `MCP_TOOL_TIMEOUT=5000` and a `wait` of 25 failed with
  `tool "delegate_to_reviewer" timed out after 5s`; Codex with `tool_timeout_sec=5` failed with
  `timed out awaiting tools/call after 5s`. Neither parent got the handle, and the delegate kept
  running.
- **v0.18.0 acceptance**, live, 2026-11-14, `develop` at [ce97443](https://github.com/eyelock/ynh/commit/ce974435)
  (with [ynh #844](https://github.com/eyelock/ynh/pull/844), [ynh #847](https://github.com/eyelock/ynh/pull/847)
  and [ynh #848](https://github.com/eyelock/ynh/pull/848)), a scratch `YNH_HOME`, `a2a` 0.4.0 from
  its release, on Claude Code 2.1.325, codex-cli 0.166.0, Copilot CLI 1.0.110, OpenCode 2.2.3 and
  cursor-agent 2026.11.05-86047df. Shown:
  - **MCP route:** a delegate started, held and finished, and a question round trip (`input-required`,
    answered by the parent's model with `delegate_reply`, each with a codeword only the parent knew),
    with the parent as Claude Code, Codex, Copilot CLI and OpenCode
    ([L39](vendor-parity.md#l39-a-headless-parent-s-model-answers-a-delegate-s-question-not-a-person)).
    A delegate asked through `ask_parent` on each of the four. One parent with two, and with four,
    A2A delegates.
  - **Questions**, through a scripted MCP client with elicitation against a real Claude Code
    delegate: a question stayed `input-required` after the delegate's wait ran out; a late elicited
    answer sent after `delegate_reply`, while a second question was open, was dropped, never recorded
    against the second; a declined elicitation left the question open. A delegate given no answer got
    `unanswered` and went on.
  - **a2a CLI route**, on Claude Code (plain commands), Codex (sandbox network on; the default
    sandbox refused with `connect: operation not permitted`) and Copilot CLI: the card served with no
    token; a send with no or a wrong token was 401 and started no delegation; `send --async`,
    `input-required`, `send --task-id`, `completed`; `task cancel` to `canceled`. A failed task
    carried its log tail. The delegate's environment had `YNH_DELEGATION_RECORD` and no
    `YNH_DELEGATE*` or `A2ACLI_*`.
  - **Native** on Claude Code, Copilot CLI and OpenCode (the delegate's codeword came back), and on
    Codex in a stability check of 2026-11-13 on `develop` at [3be2fd6](https://github.com/eyelock/ynh/commit/3be2fd6f)
    and [8ea433e](https://github.com/eyelock/ynh/commit/8ea433ec);
    native on Cursor refused at `ynh install` and at `ynh run -v cursor` with the Fix line. `ynh info`
    showed each delegate's transport.
  - **Snapshots** ([ynh #765](https://github.com/eyelock/ynh/issues/765)), on a stub vendor: the same
    inputs reused the snapshot; changed inputs made a new one and swapped the link while the live
    session's snapshot stayed unchanged; after that session ended, the next run deleted it.
  - **Found:** the Claude Code prefix-form refusal under [The a2a CLI route](#the-a2a-cli-route)
    (since fixed in [ynh #849](https://github.com/eyelock/ynh/issues/849)), and
    that an untrusted delegate refuses the parent's whole run, as
    [Which harness an A2A delegate runs as](#which-harness-an-a2a-delegate-runs-as) now says.
- **A delegate's own harness**, live, 2026-11-14, `develop` at [ce97443](https://github.com/eyelock/ynh/commit/ce974435),
  after a person's `ynh trust accept` of one delegate per vendor: one Claude Code parent started four
  delegates (Claude Code, Codex, Copilot CLI, OpenCode) over the MCP route. Each delegate had a
  codeword in its instructions, a probe MCP server that logs every call, an `on_session_start` hook,
  and a workspace folder, granted in one run (`YNH_WORKSPACE_ROOTS`) and not in a control run.
  Instructions and hooks took effect on all four (each hook ran once, with its own
  `YNH_DELEGATION_RECORD`; on Codex under the accepted harness, [L7](vendor-parity.md#l7-codex-hooks-need-an-accepted-harness)).
  The MCP server's tool was called on Codex, Copilot CLI and OpenCode; on Claude Code it was refused,
  which [ynh #858](https://github.com/eyelock/ynh/issues/858) then fixed. Workspace: on Claude Code and
  Copilot CLI the granted folder was read and an ungranted one refused, and in the control both were
  refused; on Codex the folder was in the sandbox's writable roots only when granted (reading cannot
  show it, since Codex reads outside folders either way); on OpenCode both folders were read in both
  runs, so the effect is unverified there.
- **v0.18.0 re-verification**, live, 2026-11-14, a fresh build of `develop` at
  [c2690e2d](https://github.com/eyelock/ynh/commit/c2690e2d) (vendor versions not recorded for this
  run), with a full-harness Claude Code delegate re-run at [adfcf55b](https://github.com/eyelock/ynh/commit/adfcf55b):
  - **A delegate's own harness again**, on the same four: instructions and hooks on all four; MCP tools
    called on all four, Claude Code now included ([ynh #862](https://github.com/eyelock/ynh/pull/862));
    workspace as above.
  - **The allow rule's limits** ([ynh #858](https://github.com/eyelock/ynh/issues/858)): the same
    Claude Code harness run directly got no rule and its tool call was refused. A real nested run, a
    Copilot CLI delegate's model running `ynh run` of a second Claude Code harness from its shell, ran
    inside the delegate's session (its hook logged the enclosing record) and got no rule. Not run inside
    a Claude Code delegate.
  - **a2a CLI route with card addresses** ([ynh #849](https://github.com/eyelock/ynh/issues/849),
    [ynh #857](https://github.com/eyelock/ynh/pull/857)): a Claude Code parent with two A2A delegates
    (Claude Code and OpenCode), under only `Bash(a2a:*)`, reached both with `a2a send` and
    `a2a task get -a http://127.0.0.1:<port> ...`, no variable and no `--auth`, and answered both
    questions with `send --task-id`. The two tokens appeared in none of the transcript, the run's
    output, the process arguments, the skill files or the delegation logs; only in the snapshot's
    `.mcp.json`, for the MCP route. The helper was mode 0700 in a 0700 folder, removed on a normal end,
    SIGTERM, SIGHUP and SIGINT to the process group, and left on SIGKILL; a folder 8 days old was swept
    at the next launch, one 6 days old kept.
  - **a2a CLI route** with Codex (sandbox network on; the default sandbox refused), Copilot CLI and
    OpenCode as parent, each with a question round trip. MCP route and
    [L39](vendor-parity.md#l39-a-headless-parent-s-model-answers-a-delegate-s-question-not-a-person)
    with all four parents; the scripted elicitation client's checks; native on all four and refused
    on Cursor; snapshots, all as in the acceptance run.
