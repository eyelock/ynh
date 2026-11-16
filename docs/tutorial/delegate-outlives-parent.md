# A Delegate That Outlives Its Parent

In this tutorial we will start a review, let the parent that started it end, and see that the
delegate is still running. Then we will start a new parent in the same folder and have it find the
delegate, answer the question the delegate was waiting on, and collect its result.

It continues [A First Delegate Session](first-delegate-session.md), and uses the folder and the
harnesses that tutorial builds. The commands are the Claude Code ones; for another parent vendor,
use the flags in that tutorial's [Which vendor?](first-delegate-session.md#which-vendor) table.

> **Note.** This is about `a2a` delegates, the default, which run as their own session. A `native`
> delegate is the vendor's own subagent and ends with the parent's session, so none of this applies to
> it ([Native subagents](../delegate-sessions.md#native-subagents)).

> **Note.** Last run end to end on 2026-11-16 on macOS, with ynh at `develop` `09e9f50`, on Claude Code
> as the parent. The same steps have not been run on Codex, Copilot CLI or OpenCode.

## Before you start

Do [A First Delegate Session](first-delegate-session.md) as far as the end of
[Trust the reviewer](first-delegate-session.md#trust-the-reviewer), and stop there: do not run its
Clean up. You should have ynh 0.18.0 or newer and, in `/tmp/ynh-tutorial`, the installed and trusted
`reviewer`, the installed `team-lead` and a `project` folder.

In the terminal you use here, point at the tutorial's ynh home again (the lines last only for the
shell you ran them in), and go to the project:

```sh
export YNH_HOME=/tmp/ynh-tutorial/home
export PATH=/tmp/ynh-tutorial/home/bin:$PATH
cd /tmp/ynh-tutorial/project
echo $YNH_HOME
```

```
/tmp/ynh-tutorial/home
```

## Start a review and let the parent end

Ask the team lead to start the review and not to wait for it. The reviewer's instructions make it ask
its parent a question first, which nobody will answer yet:

```bash
team-lead \
  --allowedTools=mcp__plugin_team-lead_ynh-delegates \
  "Start the reviewer on README.md. Do NOT wait for its result and do not answer any question from it: just tell me the task id you got and stop."
```

```
Opened 1 A2A delegate endpoint(s): reviewer
Launching claude...
The reviewer is now working on README.md. Its task id is `dlg-df601ae4cce0ee1a`.
```

Your task id will differ; it starts with `dlg-`. The command has returned: this parent session has
ended.

## See that the delegate is still there

The delegate runs in its own process, detached from the parent, so it did not end with it. Look for
it:

```bash
pgrep -fl 'ynh run local/reviewer'
```

You should see one line naming `ynh run local/reviewer`, with a process id; it also shows the review
prompt, and on macOS the project path begins `/private/tmp`. The delegate is waiting for its parent's
answer to "Which codeword should I sign with?".

ynh also keeps a record of each delegation on disk, under the ynh home and keyed by the parent
harness and the folder it ran in:

```bash
ls "$YNH_HOME"/delegations/local--team-lead/*/
```

The folder under `local--team-lead/` is one ynh names for the project folder. In it you should see a
`dlg-….json` file with the task id you were given, a `.log` beside it, and a `.json.lock` file you
can leave alone. The `.json` record is what a returning parent reads. If you also ran the later sections of the first
tutorial, the folder holds a record for each earlier delegation too; look for the task id you were
given.

## Start a new parent and collect the result

Now start the team lead again, in the same folder, and ask it to look for its delegations:

```bash
team-lead \
  --allowedTools=mcp__plugin_team-lead_ynh-delegates \
  "List your delegations and their states. If one is waiting on a question, answer it as your instructions say, then wait for its result and report it."
```

```
Opened 1 A2A delegate endpoint(s): reviewer
Launching claude...
```

After a minute or so, it reports. Your wording will differ:

```
I had one delegation, to the reviewer (dlg-df601ae4cce0ee1a). It was waiting on the question
"Which codeword should I sign with?". I answered PINEAPPLE, and the task has now finished.

The reviewer's result:
- The README is accurate but minimal. [...]
- It signed off with REVIEWED-PINEAPPLE.
```

If you ran the first tutorial's cancel and list steps, the reply may also mention those older
delegations. Look for the same task id you were given before (the reply's wording varies, so match on `dlg-`),
and `PINEAPPLE`. This is a new session of the team
lead: it did not remember the first one. It found the delegate because it asked for its delegations,
and ynh answered from the record.

The delegate has now finished and ended. Run the `pgrep` line again: it prints nothing. The record
stays on disk.

## What happened, and what did not

The delegate outlived its parent because it ran as its own process, and the new parent reattached
because the record on disk said what the delegate was doing. Two things did not happen: nothing told
the new parent that a delegate was waiting, so we asked it to list its delegations; and the first
parent's conversation was not resumed, only the delegate's state and result came back. Both are by
design: see [Outliving the parent](../delegate-sessions.md#outliving-the-parent).

> **Intended, not run.** A parent that is killed outright while its delegate runs, so that no record of
> how the run ended is written, is described in the same section (the next status or cancel marks it
> `failed`) and was not run here. Neither was a restarted parent on the `a2a` CLI route.

## Clean up

```bash
ynh uninstall local/team-lead
ynh uninstall local/reviewer
cd /tmp
rm -rf /tmp/ynh-tutorial
```

Each `ynh uninstall` of a local harness prints `Source tree left in place`: it leaves your folder,
and the `rm -rf` above removes it. Then close this terminal, or open a new one, to leave the tutorial's `YNH_HOME` and `PATH` behind.

## Learn more

- [Delegate Sessions: Outliving the parent](../delegate-sessions.md#outliving-the-parent): the design,
  including what a killed session leaves behind.
- [A First Delegate Session](first-delegate-session.md): the harnesses used here, and the other
  vendors.

## Next

[Native Delegation](delegation.md) — run a delegate as the vendor's own subagent instead.
