# A Delegate That Outlives Its Parent

In this tutorial we will start a review, let the parent that started it end, and see that the
delegate is still running. Then we will start a new parent in the same folder and have it find the
delegate, answer the question the delegate was waiting on, and collect its result.

It stands on its own: it builds the two small harnesses it needs. If delegates are new to you,
[A First Delegate Session](first-delegate-session.md) explains the words used here and why ynh asks
you to trust a delegate. The commands are the Claude Code ones; for another parent vendor, use the
flags in that tutorial's [Which vendor?](first-delegate-session.md#which-vendor) table.

> **Note.** This is about `a2a` delegates, the default, which run as their own session. A `native`
> delegate is the vendor's own subagent and ends with the parent's session, so none of this applies to
> it ([Native subagents](../delegate-sessions.md#native-subagents)).

> **Note.** Last run end to end on 2026-11-16 on macOS, with ynh at `develop` `09e9f50`, on Claude Code
> as the parent. The same steps have not been run on Codex, Copilot CLI or OpenCode.

## Before you start

You need:

- `git`, with `user.name` and `user.email` set (`git config user.name`; the reviewer is committed to
  a throwaway repository);
- ynh 0.18.0 or newer, installed; until v0.18.0 is released, Go 1.26 or newer (`go version`)
  instead, to build ynh from `develop` inside the tutorial folder, as shown below;
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

Until v0.18.0 is released, build ynh from `develop` into the tutorial's own home. If you already
have ynh 0.18.0 or newer installed, skip these two commands:

```sh
git clone --branch develop https://github.com/eyelock/ynh.git /tmp/ynh-tutorial/ynh-src
make -C /tmp/ynh-tutorial/ynh-src install INSTALL_DIR=/tmp/ynh-tutorial/home/bin
```

The build prints each command it runs, then ends with:

```
Installed ynh and ynd to /tmp/ynh-tutorial/home/bin
```

`INSTALL_DIR` puts the two binaries in `/tmp/ynh-tutorial/home/bin`, not in `~/.ynh/bin`, so the ynh
you normally use is not replaced. Check which ynh you have:

```sh
ynh version
```

It should say 0.18.0 or newer. The build from `develop` prints `dev-develop-` and a commit, such as
`dev-develop-3c75df5`, and counts. Delegates that run as their own session arrived in v0.18.0.

The `export` lines last only for the shell you ran them in. Run them again in every new terminal, and
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

## Install and trust

```bash
ynh install /tmp/ynh-tutorial/reviewer
ynh install /tmp/ynh-tutorial/team-lead
```

Each prints `Installed harness "..."` and its location, launcher and vendor; the team lead's install
also prints `Fetching 0 include(s) and 1 delegate(s)...` and a `Fetched` line for the reviewer. ynh will not start a
delegate until you have recorded that you reviewed it. The reviewer here runs no commands, so there is
little to read, but the record is still needed:

```bash
ynh trust accept local/reviewer
```

```
Recorded local/reviewer: 0 command(s), sha256:1282d82bafd3a002
```

Your digest after `sha256:` may differ.

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
is there with an `asked_at` and no `lapsed_at` while it is open. If you also ran the later sections of the first
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
