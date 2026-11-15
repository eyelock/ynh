# Find a delegation after the parent restarted

Your parent session ended (a restart, a handoff or a crash) while a delegate was working. After these steps
you have its state and, if it finished, its result.

A delegate runs detached and every delegation has a record on disk, so it outlives its parent. Why is in
[Outliving the parent](../delegate-sessions.md#outliving-the-parent); the record is described in the
[reference](../delegates-reference.md#the-delegation-record).

## Before you start

Start the parent again from the same working folder with `ynh run <parent>`. Records are keyed by the
parent harness and its working folder, so another folder will not find them.

## Steps

1. Ask the parent to list its delegations. The `delegate_list` tool returns every delegation of this parent
   in this folder, with its state, including those from earlier sessions and the `a2a` CLI. The handle is
   `dlg-` and 16 hex digits.

2. Get the state of one: `delegate_status` with the handle. When it is `input-required`, `message` is the
   delegate's question; answer it with `delegate_reply` on the same handle.

3. Collect a finished one: `delegate_result` with the handle. A `failed` delegation carries the error, the
   exit code and the last 40 lines of its log (`log_tail`).

4. Stop one you no longer want: `delegate_cancel` with the handle.

5. If the record itself is wanted, it is under `$YNH_HOME/delegations/`.

Notes:

- A delegation started with the `a2a` CLI shows in `delegate_list`, but its id there is the handle, not the
  A2A task id the CLI used.
- If the session process was killed outright, the next status or cancel notices and marks the delegation
  `failed` with an error saying so.
- When ynh cannot verify that a recorded process is the one it started, it signals nothing, and a cancel
  records that the run was not signalled and may still be running.
- Continuing a delegate's session after it completed is not built
  ([A2A delegates, not shown live](../vendor-parity.md#l40-a2a-delegates-cursor-opencode-workspace-folders-and-a-person-answering-not-shown-live)).

## Checked against

[The delegation record](../delegates-reference.md#the-delegation-record) and the tools table, v0.18.0
(develop `4c05c4b5`), 2026-11-15. Shown live: a parent that lost a call to a short tool timeout found its
handle again with `delegate_list` (2026-11-13, [ynh #832](https://github.com/eyelock/ynh/pull/832)). The
record does not show a restarted parent doing the same; that is the designed behaviour, not a shown one.
