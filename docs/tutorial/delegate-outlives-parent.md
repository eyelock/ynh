<!-- Tutorial. Updated 2026-11-28 for ynh v0.18.2 (ynh #1058: accepting the parent accepts its a2a delegates; a delegate opens at a permission level), with its install, accept and launch output re-run on a build of develop at 960ecb6f (P0 to P4 of ynh #1058 merged) plus ynh #1063, with a stand-in claude. Lines marked TODO(live) need a live Claude Code session; they are checked in the release's live check. -->

# A Delegate That Outlives Its Parent

In this tutorial we will start a review, let the parent that started it end, and see that the
delegate is still running. Then we will start a new parent in the same folder and have it find the
delegate, answer the question the delegate was waiting on, and collect its result.

It stands on its own: it builds the two small harnesses it needs. If delegates are new to you,
[A First Delegate Session](first-delegate-session.md) explains the words used here and why ynh asks
you to accept a delegate. [Your First Delegation](first-delegation.md) shows accepting a parent and its
delegate in one step, and the permission level a delegate opens at. The commands are the Claude Code ones; for another parent vendor, use the
flags in that tutorial's [Which vendor?](first-delegate-session.md#which-vendor) table.

> **Note.** This is about `a2a` delegates, the default, which run as their own session. A `native`
> delegate is the vendor's own subagent and ends with the parent's session, so none of this applies to
> it ([Native subagents](../delegate-sessions.md#native-subagents)).

<!-- TODO(live): re-run the whole page end to end on Claude Code on a build of what ships for v0.18.2, and update the note below with that build and date. -->

> **Note.** Last run end to end on 2026-11-16 on macOS, with ynh at `develop` `09e9f50`, on Claude Code
> as the parent; its install and accept steps were re-run for v0.18.2 on 2026-11-28. The same steps have
> not been run on Codex, Copilot CLI or OpenCode.

## Before you start

You need:

- `git`, with `user.name` and `user.email` set (`git config user.name`; the reviewer is committed to
  a throwaway repository);
- ynh 0.18.2 or newer, installed. Check with `ynh version` (a build of `develop` prints
  `dev-develop-` and a commit, and counts). Delegates that run as their own session arrived in
  v0.18.0; accepting a parent with its delegates, and the level a delegate opens at, in v0.18.2. To
  install or upgrade, see the [install instructions](README.md);
- Claude Code (`claude`), logged in as you normally use it.

If you have run this tutorial before, start clean:

```bash
rm -rf /tmp/ynh-tutorial
```

## Set up the tutorial folder and ynh home

Make the tutorial folder, and give this terminal a ynh home of its own inside it, so your usual ynh
setup and its trust records are not touched:

```bash
mkdir -p /tmp/ynh-tutorial
export YNH_HOME=/tmp/ynh-tutorial/home
export PATH=/tmp/ynh-tutorial/home/bin:$PATH
```

They last only for the shell you ran them in. Run the `export` lines again in every new terminal, and
in every separate shell your tools start for you.

## Make the reviewer, the team lead and a project

The reviewer asks its parent a question before it reviews, which is what the delegate will be waiting
on. Its instructions also tell it to keep waiting. Each wait lasts at most 45 seconds
([Questions from a delegate](../delegate-sessions.md#questions-from-a-delegate)), and a delegate that
is not told to keep asking may give up after two waits, about 90 seconds, and sign with a placeholder:

```bash
mkdir -p /tmp/ynh-tutorial/reviewer/.agents/harness
```

```bash
cat > /tmp/ynh-tutorial/reviewer/.agents/harness/plugin.json <<'EOF'
{
  "name": "reviewer",
  "version": "0.1.0",
  "description": "Reviews a file and reports one finding",
  "default_vendor": "claude"
}
EOF
```

```bash
cat > /tmp/ynh-tutorial/reviewer/instructions.md <<'EOF'
You are a reviewer. You are given one file to review.

Before you start, ask your parent one question with
the ask_parent tool: "Which codeword should I sign with?"

If ask_parent returns "unanswered", call it again with
the same question_id and keep waiting. Never withdraw
the question and never guess a codeword.

Then read the file and reply with one short finding,
signed on its own last line as REVIEWED-<codeword>.
EOF
```

```bash
git -C /tmp/ynh-tutorial/reviewer init -q
git -C /tmp/ynh-tutorial/reviewer add .
git -C /tmp/ynh-tutorial/reviewer commit -q -m init
```

The team lead names the reviewer in `delegates_to`, and answers its question:

```bash
mkdir -p /tmp/ynh-tutorial/team-lead/.agents/harness
```

```bash
cat > /tmp/ynh-tutorial/team-lead/.agents/harness/plugin.json <<'EOF'
{
  "name": "team-lead",
  "version": "0.1.0",
  "description": "Hands reviews to a reviewer",
  "default_vendor": "claude",
  "delegates_to": [
    {"git": "/tmp/ynh-tutorial/reviewer"}
  ]
}
EOF
```

```bash
cat > /tmp/ynh-tutorial/team-lead/instructions.md <<'EOF'
You are a team lead. Hand every review to your
reviewer delegate; do not review files yourself.

If the reviewer asks which codeword to sign with,
answer: PINEAPPLE.
EOF
```

A project for the reviewer to review:

```bash
mkdir -p /tmp/ynh-tutorial/project
```

```bash
cat > /tmp/ynh-tutorial/project/README.md <<'EOF'
# Demo

This project prints hello.
EOF
```

## Install and accept

Install the team lead. It fetches the reviewer it names:

```bash
ynh install /tmp/ynh-tutorial/team-lead
```

```
Fetching 0 include(s) and 1 delegate(s)...
  Fetched /tmp/ynh-tutorial/reviewer
Installed harness "team-lead"
  Location: /tmp/ynh-tutorial/team-lead
  Launcher: /tmp/ynh-tutorial/home/bin/team-lead
  Vendor:   claude
```

ynh will not start the team lead, or its delegate, until you have recorded that you reviewed them.
Neither runs commands, so there is little to read, but the record is still needed: the team lead runs
here with a prompt and nobody to approve, and the reviewer runs as its own session, and each opens at
a permission level. Accept the team lead, which installs the reviewer and accepts it too:

```bash
ynh trust accept local/team-lead
```

```
A2A delegate /tmp/ynh-tutorial/reviewer of team-lead, opens at all when run unattended (default):
Installed harness "reviewer"
  Location: /tmp/ynh-tutorial/reviewer
  Launcher: /tmp/ynh-tutorial/home/bin/reviewer
  Vendor:   claude
Accepted local/reviewer (accept, with local/team-lead): 0 command(s), sha256:..., opens at all when run unattended
Accepted local/team-lead (accept): 0 command(s), sha256:..., opens at all when run unattended
  opens at all when run unattended (default)
```

Neither declares a level, so both open at `all`, the default for an accepted harness
([Permission levels](../permission-levels.md)). The team lead says so when it starts, on its first
line: `auto-approve: all (default)`.

## Start a review and let the parent end

Go to the project, in the terminal you will use from here. Every `team-lead` command below must run
from this folder, because a returning parent finds its delegates by the folder it runs in. Check that
the home took, too:

```bash
cd /tmp/ynh-tutorial/project
echo $YNH_HOME
```

```
/tmp/ynh-tutorial/home
```

Ask the team lead to start the review and not to wait for it. The reviewer's instructions make it ask
its parent a question first, which nobody will answer yet:

```bash
team-lead \
  --allowedTools=mcp__plugin_team-lead_ynh-delegates \
  "Start the reviewer on README.md. Do NOT wait for its result and do not answer any question from it: just tell me the task id you got and stop."
```


```
auto-approve: all (default)
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
answer to "Which codeword should I sign with?". The reviewer was still waiting, and still
unlapsed in its record, when a new parent answered it three minutes later; but it keeps asking only
as long as the model follows its instructions, so do not leave it for long.

ynh also keeps a record of each delegation on disk, under the ynh home and keyed by the parent
harness and the folder it ran in:

```bash
ls "$YNH_HOME"/delegations/local--team-lead/*/
```

The folder under `local--team-lead/` is one ynh names for the project folder. In it you should see a
`dlg-….json` file with the task id you were given, a `.log` beside it, and a `.json.lock` file you
can leave alone. The `.json` record is what a returning parent reads, and it shows the waiting: print it with
`cat "$YNH_HOME"/delegations/local--team-lead/*/dlg-*.json` and look at `"questions"`: your question
is there with an `asked_at` and no `lapsed_at` while it is open.

The record also carries `"auto_approve": "all"` and `"auto_approve_from": "default"`: the level the
reviewer opened at, and that it came from the default for an accepted harness.

<!-- TODO(live): check these two fields in a live record. -->

If you also ran the later sections of the first
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
auto-approve: all (default)
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

<!-- TODO(live): delegate_status and the result carry auto_approve and auto_approve_from (internal/delegatemcp/server.go at develop 960ecb6f); check them in a live run. -->

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
