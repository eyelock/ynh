# Delegate sessions

When you ask your parent harness to hand a piece of work to another harness, the other harness, its
**delegate**, does not become a part of your parent's session. It runs as its own ynh session, with its
own vendor, instructions, MCP servers, hooks and workspace folders. Your parent asks, the delegate works
on its own, and you see its answer, through your parent. If the delegate has a question, it goes back to
your parent, which answers it or hands it to you.

This page explains why delegates work this way and how the pieces fit. It is explanation: it says why,
and does not list every field, tool and message. For those, see the [delegates reference](delegates-reference.md);
the manifest fields themselves are in [delegates_to](harnesses.md#delegates-to-optional) and
[Transport](harnesses.md#transport). For tasks, see the how-tos: [add a delegate](how-to/add-a-delegate.md),
[allow a headless parent](how-to/allow-a-headless-parent.md),
[reach delegates with the a2a CLI](how-to/reach-delegates-with-the-a2a-cli.md),
[make a delegate native](how-to/make-a-delegate-native.md) and
[find a delegation after a restart](how-to/find-a-delegation-after-a-restart.md).

Status, and what was and was not shown live, is on the [parity page](vendor-parity.md), which lists each
known gap in what a vendor receives as a numbered **limit** (L12, L39, L40) with its evidence. Two apply
here: [what is not shown live for A2A delegates](vendor-parity.md#l40-a2a-delegates-cursor-opencode-workspace-folders-and-a-person-answering-not-shown-live)
and [who answers a delegate's question](vendor-parity.md#l39-a-headless-parent-s-model-answers-a-delegate-s-question-not-a-person).
This page claims only what those limits show.

## Why

**A subagent is only part of a harness.** A `native` delegate is a vendor subagent definition inside the
parent's session. On every vendor it carries its instructions; only on Claude Code its MCP servers; on
no vendor its hooks or workspace folders ([known limit L12, delegates carry only part of a harness](vendor-parity.md#l12-delegates-carry-only-part-of-a-harness)).
It also ends with the parent's session. A delegate that is its own ynh session is assembled like any
other run, and it can outlive whoever started it.

**No vendor CLI speaks A2A, so ynh bridges it.** The five vendor CLIs are not A2A clients (when this
was designed, each had at most an open request, for example
[anthropics/claude-code #104852](https://github.com/anthropics/claude-code/issues/104852)). So a parent
reaches a delegate through a bridge, and ynh gives it two, both reaching the same endpoints: ynh's own
MCP server, which every vendor CLI can call, and the [official a2a CLI](https://github.com/a2aproject/a2a-cli)
as a shell route.

**A2A on the wire, not MCP alone.** [A2A](https://a2a-protocol.org/latest/specification/) (Agent2Agent)
is built for one agent handing a task to another: a durable task id, a cancel, and a state,
`input-required`, for a question back to the sender, answered by a new message on the same task. MCP has
these only through its Tasks extension, which only Claude Code's interactive mode offered when the
design was made. A2A is also what other agent tools speak. The cost is local plumbing: one HTTP endpoint,
one port and one token per delegate.

**The token, not the bridge, keeps a parent to its own delegates.** The `a2a` CLI can message any URL, so
a rule like "a parent starts only the delegates it names" cannot live in the bridge. It lives on the
delegate's side: each endpoint has its own bearer token, and only the parent's run holds it.

**Why `native` stays.** Native is a setting, per entry or for the whole harness, for a parent that wants
its delegate inside its own session and accepts what that costs: the delegate keeps its instructions but
loses its hooks and workspace folders, and its MCP servers on every vendor but Claude Code ([known limit L12, delegates carry only part of a harness](vendor-parity.md#l12-delegates-carry-only-part-of-a-harness)).

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
  endpoint's card URL and token in the parent's environment and in the `ynh-delegates` MCP server's entry.
- The delegate's own session starts on its endpoint's first message, never before.
- The parent never holds the delegate's tools. It sees the delegate's state, its questions and its result.
- Everything about a delegation is written to a record on disk, so either route, or a parent that
  restarted, reads the same state.

## Which harness a delegate runs as

A `delegates_to` entry is a source, not an installed harness. For an `a2a` entry, ynh finds the installed
harness whose recorded source matches and runs the delegate as that harness. So the delegate must be
installed (`ynh install <source>`) and **trusted** first: `ynh trust accept <id>` records that you have read
what the harness will run (its commands, hooks and MCP servers), which is explained under
[What a command sensor can reach](sensors.md#what-a-command-sensor-can-reach). A delegate that runs
nothing needs no accepting.

> **Watch out.** A delegate that is not installed, not trusted, changed since it was trusted, or unreadable
> refuses the parent's whole run, before the vendor starts, with the command that fixes it. A parent must
> not start believing it has a delegate it cannot reach, and ynh never installs or trusts anything on a
> parent's behalf. The messages are in the
> [reference](delegates-reference.md#refusals-when-a-delegate-cannot-be-matched); the steps to add one are
> in [Add a delegate](how-to/add-a-delegate.md).

**Being verified.** That a delegate installed from a remote git URL or from a registry entry, then trusted,
works as a delegate is the designed path. It has not been shown live. The live runs on the parity page
([what is not shown live for A2A delegates](vendor-parity.md#l40-a2a-delegates-cursor-opencode-workspace-folders-and-a-person-answering-not-shown-live)) used delegates a person installed and trusted by
hand, and the record does not say how each was installed. May, the verifier, has not yet run the remote and
registry cases.

## Trust

- A parent can start only its own `a2a` delegates: each endpoint refuses any request without its token,
  and only the parent's run holds it.
- An `a2a` delegate runs under its own trust grant: its MCP servers, hooks and focus prompt are reviewed
  there, not in the parent's digest. A `native` delegate is part of the parent's session, so its servers
  and focus prompt are in the parent's digest.
- The parent's digest covers every entry as written (source, profile or focus, and its settled
  transport), so repointing an entry or flipping it between `a2a` and `native` asks for review again.
- A delegate's own `delegates_to` applies only inside its own session.

## Reaching a delegate

`ynh run` opens the endpoints and closes them when the run ends; a delegation already running is a
detached session and goes on. An endpoint's address and token reach the parent through its environment
and the MCP server's entry, never through a command. The variables and the task states are in the
[reference](delegates-reference.md#the-a2a-endpoints).

## Reaching a delegate over MCP

`ynh run` adds one MCP server, ynh itself, to the parent's MCP config on every vendor, under the name
`ynh-delegates`. It has one tool per delegate and five more for every parent, and it returns structured
results with a declared schema, because in a spike an OpenCode parent given plain text tried to read the
task id off a string and started a stray task. Calls return at once and the parent polls, because the
vendors' own MCP tool-call timeouts must never be held open by a long delegate. The server keeps no
state of its own: it reads the delegation record by handle. The tools, the wait limit, the allow rules
and OpenCode's Code Mode are in the [reference](delegates-reference.md#the-ynh-delegates-mcp-server).

## The a2a CLI route

The other route is the official `a2a` CLI with its skill. `ynh run` writes two skills into the parent's
vendor skill folder: the official `a2a-cli`, vendored unchanged, and `ynh-a2a-delegates`, ynh's own short
companion that lists each delegate's card address and how to use the CLI. A command carries no variable
and no credential: Claude Code refuses a command with a variable under the rule `Bash(a2a:*)`
([ynh #849](https://github.com/eyelock/ynh/issues/849)), so the address is written into the skill and the
token comes from a small credential helper that ynh writes for the run. ynh does not install the `a2a`
binary. Which route the parent uses is its choice: ynh states no preference. The commands, the doctor's
checks and the Codex sandbox cost are in the [reference](delegates-reference.md#the-a2a-cli-route).

## Questions from a delegate

A delegate session gets one extra tool, `ask_parent`, from an MCP server named `ynh-parent`. A question
travels as an A2A task in state `input-required`, and the parent sees it by whichever route its client
offers: as an elicitation or a task where the client takes one, otherwise in the tool result, where the
parent's model answers it with a reply on the same task. In a headless parent that is the model
answering, not a person ([parity page: who answers a delegate's question](vendor-parity.md#l39-a-headless-parent-s-model-answers-a-delegate-s-question-not-a-person)).

A question outlives the delegate's wait: if no answer comes, the delegate is told `unanswered`, the
question stays open in the record, and the delegate chooses whether to keep waiting, withdraw it or carry
on. That keeps the delegate's tool call inside its vendor's timeout without losing the question. Each
answer belongs to one question. The routes, results and limits are in the
[reference](delegates-reference.md#questions-from-a-delegate).

## Outliving the parent

A delegate session must survive its parent going away (a restart, a handoff or a crash) and coming back.
So the delegate runs **detached**, in its own process group, and every delegation has a **record on
disk** under `$YNH_HOME/delegations/`, keyed by the parent harness and its working folder. The server
keeps no state, so a returning parent's new server answers for old handles from the record, and the
record says what happened while the parent was away. If the session process is killed outright, the
next status or cancel notices and marks the delegation `failed`. When ynh cannot verify that a process is
the recorded one, it signals nothing, because an id that merely exists may belong to something unrelated.
The record's contents, and how liveness is decided, are in the
[reference](delegates-reference.md#the-delegation-record).

## Native subagents

With `transport: native`, ynh assembles the delegate as a subagent definition in the parent's run dir,
and the parent's vendor runs it inside the parent's session. No endpoint is opened and no installed
harness is matched. It carries only part of its harness ([known limit L12, delegates carry only part of a harness](vendor-parity.md#l12-delegates-carry-only-part-of-a-harness)), which is the price of
choosing native. `ynd export` exports native delegates and leaves `a2a` ones out with one warning,
since they need ynh at runtime.

## Why a run dir is a snapshot

Several live sessions of one harness are the normal case once delegates are sessions: two reviews by the
same delegate, say. A later `ynh run` must not empty and rebuild the run dir under a live session
([ynh #765](https://github.com/eyelock/ynh/issues/765)). So a run dir is a content-addressed snapshot
behind a stable link: each run assembles into a staging folder, reuses an identical snapshot or adds a
new one, swaps the link atomically, and launches the vendor on the snapshot's real path, so the session
reads its own snapshot to the end. A snapshot nobody holds is deleted by the next run. The steps and the
Codex and Cursor limit are in the [reference](delegates-reference.md#run-dir-snapshots).

## Upgrading from v0.17.0

v0.17.0 turned every delegate into a vendor subagent. From v0.18.0 the default is `a2a`, and `native` is
the old behaviour, chosen with `"transport": "native"` on an entry or as the harness default.

- A parent accepted before v0.18.0 that has delegates reads `changed` once: review it with
  `ynh trust show <id>` and run `ynh trust accept <id>`.
- Each `a2a` delegate must be installed and trusted, or the parent's run is refused; `ynh install <source>`
  then `ynh trust accept <id>`.
- `ynh info` shows each delegate's transport and where it came from.

## Status and evidence

The status of A2A delegates, what was not shown live, what is not built (remote delegates, continuing a
delegate's session after it completes, `ynh run`'s own record of every launch, [ynh #746](https://github.com/eyelock/ynh/issues/746)) and the
evidence runs are on the parity page, under
[A2A delegates, not shown live](vendor-parity.md#l40-a2a-delegates-cursor-opencode-workspace-folders-and-a-person-answering-not-shown-live) and
[who answers a delegate's question](vendor-parity.md#l39-a-headless-parent-s-model-answers-a-delegate-s-question-not-a-person).

* **Why**
  * [Harness Engineering](/harness-engineering.md)
  * [The Factory Pattern](/factory-pattern.md)
