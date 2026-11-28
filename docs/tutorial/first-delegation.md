<!-- Tutorial. Written 2026-11-28 against a build of ynh develop at 960ecb6f (P0 to P4 of ynh #1058 merged) plus this pull request, in a scratch YNH_HOME with the harnesses served over file:// and a stand-in claude on PATH. Lines marked TODO(live) need a vendor session, which the stand-in cannot give; they are checked in the release's live check on all five vendors. -->

# Your First Delegation

In this tutorial we will install a harness called **team** whose job is to hand reviews to a second
harness, its delegate **reviewer**. We will accept team once, which accepts reviewer too, start team,
and watch reviewer do a piece of work in its own session. Then we will tie reviewer down so it may
edit files but not run commands, and see ynh ask us to accept the change before it runs again.

Everything happens in `/tmp/ynh-first-delegation`, with a ynh home of its own, so your usual ynh
setup is not touched.

## Words used here

- A **harness** is a folder with a manifest (`.agents/harness/plugin.json`) and instructions, which
  ynh launches inside an AI command-line tool, its **vendor** (here Claude Code, `claude`).
- A **delegate** is a harness that another harness, its **parent**, hands work to. It runs as its own
  session. The parent names it in the manifest field `delegates_to`.
- **Accepting** a harness is how you tell ynh you have read what it will run. Until you do, ynh
  refuses to run it.
- A **permission level** is what a session may do with nobody there to approve: `none`, `edits` or
  `all`. A delegate's session is such a session.

## Before you start

You need:

- `git`, with `user.name` and `user.email` set;
- ynh v0.18.2 or newer (`ynh version`);
- Claude Code (`claude`), installed and logged in.

If you have run this tutorial before, start clean:

```bash
rm -rf /tmp/ynh-first-delegation
```

## Set up a ynh home of its own

Run these three lines exactly:

```bash
mkdir -p /tmp/ynh-first-delegation
export YNH_HOME=/tmp/ynh-first-delegation/home
export PATH=/tmp/ynh-first-delegation/home/bin:$PATH
```

They last only for this shell. If you open another terminal, run them again there.

## Publish the two harnesses

On a real team, team and reviewer would be repositories on GitHub. Here we make them as two Git
repositories on disk, and ynh fetches them over `file://` exactly as it would from GitHub.

First the reviewer. It has one hook, a command it runs when its session starts, so it has something
for us to review:

```bash
mkdir -p /tmp/ynh-first-delegation/reviewer/.agents/harness
cat > /tmp/ynh-first-delegation/reviewer/.agents/harness/plugin.json <<'EOF'
{
  "name": "reviewer",
  "version": "0.1.0",
  "description": "Reviews a file and reports one finding",
  "default_vendor": "claude",
  "hooks": {
    "on_session_start": [
      {"command": "echo Reviewer session started."}
    ]
  }
}
EOF
cat > /tmp/ynh-first-delegation/reviewer/instructions.md <<'EOF'
You are a reviewer. You are given one file to review.
Read it and reply with one short finding.
EOF
git -C /tmp/ynh-first-delegation/reviewer init -q
git -C /tmp/ynh-first-delegation/reviewer add .
git -C /tmp/ynh-first-delegation/reviewer commit -q -m init
```

Now team. Its `delegates_to` entry pins reviewer to the commit we just made, so team always gets that
exact reviewer:

```bash
REVIEWER_SHA=$(git -C /tmp/ynh-first-delegation/reviewer rev-parse HEAD)
mkdir -p /tmp/ynh-first-delegation/team/.agents/harness
cat > /tmp/ynh-first-delegation/team/.agents/harness/plugin.json <<EOF
{
  "name": "team",
  "version": "0.1.0",
  "description": "Hands reviews to a reviewer",
  "default_vendor": "claude",
  "delegates_to": [
    {"git": "file:///tmp/ynh-first-delegation/reviewer", "ref": "$REVIEWER_SHA"}
  ]
}
EOF
cat > /tmp/ynh-first-delegation/team/instructions.md <<'EOF'
You lead a team. When asked to review a file, hand the file to your
delegate reviewer with the delegate_to_reviewer tool, wait for its
result, and report its finding.
EOF
git -C /tmp/ynh-first-delegation/team init -q
git -C /tmp/ynh-first-delegation/team add .
git -C /tmp/ynh-first-delegation/team commit -q -m init
```

These print nothing.

## Install team

```bash
ynh install file:///tmp/ynh-first-delegation/team
```

```
Fetching 0 include(s) and 1 delegate(s)...
  Fetched /tmp/ynh-first-delegation/reviewer
Installed harness "team"
  Location: /tmp/ynh-first-delegation/home/harnesses/local--team
  Launcher: /tmp/ynh-first-delegation/home/bin/team
  Vendor:   claude
```

Notice that ynh fetched reviewer as well. We installed one thing and got both.

## Try to run it

```bash
ynh run local/team
```

ynh refuses, before Claude Code starts:

```
Error: delegate file:///tmp/ynh-first-delegation/reviewer of team is not installed
  Run: ynh install file:///tmp/ynh-first-delegation/reviewer --ref ..., then ynh trust accept <the installed id>
  Accept the parent, which accepts all its a2a delegates with it: ynh trust accept local/team
```

Take the last line: you do not need to install reviewer by hand, because accepting team does it for
you. First, look at what you would be accepting.

## Read what team will run

```bash
ynh trust show local/team
```

```
local/team
  source:   file:///tmp/ynh-first-delegation/team (main)
  commit:   ...
  state:    no-commands
  digest:   sha256:...

This harness declares no commands.

Permission level when run unattended (a delegate session, a one-shot 'ynh run' or 'ynh agent run'):

  opens at all when run unattended (not declared; the default for an accepted harness)

A2A delegates, the whole tree, each its own harness with its own record. 'ynh trust accept local/team' accepts them all, or none:

  file:///tmp/ynh-first-delegation/reviewer: (not installed; accepting installs it) [not-installed], declared by team
    opens at all when run unattended (default)
    hook on_session_start: echo Reviewer session started.
```

Two things to notice. team itself runs no commands, but when it runs with nobody watching it opens at
`all`: it may do anything it asks to. And reviewer is listed under team, with what it runs and the
level it opens at, to be accepted with it.

## Accept team

```bash
ynh trust accept local/team
```

```
A2A delegate file:///tmp/ynh-first-delegation/reviewer of team, opens at all when run unattended (default):
It executes:
  hook on_session_start: echo Reviewer session started.
Installed harness "reviewer"
  Location: /tmp/ynh-first-delegation/home/harnesses/local--reviewer
  Launcher: /tmp/ynh-first-delegation/home/bin/reviewer
  Vendor:   claude
Accepted local/reviewer (accept, with local/team): 1 command(s), sha256:..., opens at all when run unattended
Accepted local/team (accept): 0 command(s), sha256:..., opens at all when run unattended
  opens at all when run unattended (default)
```

ynh showed what reviewer runs, installed it at its pinned commit, and accepted it with team, all or
nothing. Each has its own record:

```bash
ynh trust
```

```
HARNESS         STATE     VIA              COMMANDS  SOURCE
local/reviewer  accepted  with local/team  1         file:///tmp/ynh-first-delegation/reviewer
local/team      accepted  accept           0         file:///tmp/ynh-first-delegation/team
```

`with local/team` says reviewer was accepted through its parent.

## Start team

Make a file for the reviewer to read, then start team from its folder:

```bash
cd /tmp/ynh-first-delegation
printf 'def add(a, b):\n    return a - b\n' > calc.py
ynh run local/team --allowedTools=mcp__plugin_team_ynh-delegates
```

`--allowedTools=...` is a Claude Code flag that lets team call its delegate without asking you each
time; ynh passes flags it does not know on to the vendor. ynh prints, before Claude Code opens:

```
Opened 1 A2A delegate endpoint(s): reviewer
Launching claude...
```

The first line names the delegate team can reach. The level reviewer opens at shows once team hands it
work.

## Hand reviewer a task

In the Claude Code session, type:

```
Review calc.py.
```

team calls `delegate_to_reviewer`. reviewer starts in a session of its own, reads `calc.py`, and
replies; team reports the finding, something like "`add` subtracts instead of adding". The words differ
on every run: what matters is that the finding came from reviewer.

Now ask team how reviewer was run:

```
Call delegate_status for that delegation and show me auto_approve and auto_approve_from.
```

<!-- TODO(live): the two fields are delegate_status's (internal/delegatemcp/server.go at develop 960ecb6f); check them and their values in a live Claude Code session. -->

team shows, among the other fields:

```
auto_approve: all
auto_approve_from: default
```

This is the permission level we saw in `ynh trust show`: reviewer declared none, so as an accepted
harness it opened at `all`, the default. Leave the session with `/exit`.

## Tie reviewer down

reviewer only needs to read and edit files. We will tell team to start it at `edits`: file edits are
approved, commands are refused.

Where you set this depends on who owns the harness. You cannot change an installed harness in place:
`ynh update` overwrites it from its source. You can change a harness you author, or a fork of one.
Here we author team, so we set `auto_approve` on its `delegates_to` entry. That narrows reviewer when
team starts it; it never widens what reviewer itself declares.

```bash
cat > /tmp/ynh-first-delegation/team/.agents/harness/plugin.json <<EOF
{
  "name": "team",
  "version": "0.1.0",
  "description": "Hands reviews to a reviewer",
  "default_vendor": "claude",
  "delegates_to": [
    {"git": "file:///tmp/ynh-first-delegation/reviewer", "ref": "$REVIEWER_SHA", "auto_approve": "edits"}
  ]
}
EOF
git -C /tmp/ynh-first-delegation/team commit -q -am "start reviewer at edits"
ynh update local/team
```

```
Checking harness source file:///tmp/ynh-first-delegation/team...
  Updated.
Checking delegate file:///tmp/ynh-first-delegation/reviewer...
  Already up to date.
Checked 2 source(s) for harness "local/team", 1 updated.

local/team is no longer trusted: what it executes changed since you accepted it on ..., so ynh run, ynh agent run and ynh check refuse it.
  Review it: ynh trust show local/team
  Then accept: ynh trust accept local/team
```

(If `REVIEWER_SHA` is empty because you opened a new shell, set it again as in
[Publish the two harnesses](#publish-the-two-harnesses).)

The level is part of what you accepted, so changing it asks for acceptance again. Try to run team:

```bash
ynh run local/team
```

```
Error: refusing to run local/team: your acceptance on ... does not cover it: it was accepted at commit ..., and is now at ....
Review them: ynh trust show local/team
Then accept:  ynh trust accept local/team
```

Read the change:

```bash
ynh trust show local/team
```

The end of the output now reads:

```
Permission level when run unattended (a delegate session, a one-shot 'ynh run' or 'ynh agent run'):

  opens at all when run unattended (not declared; the default for an accepted harness)
  delegate file:///tmp/ynh-first-delegation/reviewer: edits

A2A delegates, the whole tree, each its own harness with its own record. 'ynh trust accept local/team' accepts them all, or none:

  file:///tmp/ynh-first-delegation/reviewer: local/reviewer [accepted], declared by team
    opens at edits when run unattended (entry)
    hook on_session_start: echo Reviewer session started.
```

team itself still opens at `all`; reviewer, started by team, opens at `edits`, and `(entry)` says the
level came from team's entry. reviewer's own record is untouched: only team changed. Accept it:

```bash
ynh trust accept local/team
```

```
A2A delegate file:///tmp/ynh-first-delegation/reviewer of team, opens at edits when run unattended (entry):
It executes:
  hook on_session_start: echo Reviewer session started.
Accepted local/reviewer (accept, with local/team): 1 command(s), sha256:..., opens at edits when run unattended
Accepted local/team (accept): 0 command(s), sha256:..., opens at all when run unattended
  opens at all when run unattended (default)
  delegate file:///tmp/ynh-first-delegation/reviewer: edits
```

And check the level and where it comes from:

```bash
ynh info local/team
```

The top and the delegate line read:

```
ID:           local/team
Name:         team
Vendor:       claude
Auto-approve: all (default)
...
Delegates to:
  file:///tmp/ynh-first-delegation/reviewer  ref=...  transport=a2a (default)  auto_approve=edits (entry)
```

Start team again and ask for the same review. This time `delegate_status` shows:

<!-- TODO(live): check these two lines in a live Claude Code session. -->

```
auto_approve: edits
auto_approve_from: entry
```

### `edits` on other vendors

`edits` is held exactly or refused, never widened. Codex and Cursor have no mode that approves edits
while refusing commands, so a reviewer started at `edits` on either is refused when it starts. On
Codex, use `none`, or run reviewer on another vendor.

Cursor refuses `none` too (print mode still writes files), so on Cursor use `all`, or another vendor.

## Narrow one run instead

To narrow without changing any manifest, pass `--auto-approve` to `ynh run`. On an interactive run,
the session itself is unchanged (you still approve its prompts) and every delegate it starts is capped
at the level you give:

```bash
ynh run local/team --auto-approve none --allowedTools=mcp__plugin_team_ynh-delegates
```

ynh prints the same two lines as before and passes Claude Code no permission flag. The cap shows when
reviewer runs: `delegate_status` says `auto_approve: none`, `auto_approve_from: caller_cap`.

<!-- TODO(live): check caller_cap in delegate_status in a live Claude Code session. -->

A one-shot run, with the task after `--`, opens team itself at the level, and says so first:

```bash
ynh run local/team --auto-approve edits -- "Review calc.py."
```

```
auto-approve: edits (flag)
Opened 1 A2A delegate endpoint(s): reviewer
Launching claude...
```

`--auto-approve` only narrows: asking for more than a harness's level is refused before anything starts,
for example `Error: --auto-approve: flag: auto-approve all is wider than the ceiling edits; a request may only narrow (all > edits > none)`.

## Clean up

```bash
rm -rf /tmp/ynh-first-delegation
```

Open a new terminal, or unset `YNH_HOME`, to go back to your usual ynh home.

## What we did

We installed a parent and got its delegate with it, accepted both in one step, saw the delegate open
at `all`, and narrowed it to `edits` with one field, which ynh made us accept again.

## Next

[A First Delegate Session](first-delegate-session.md): delegates asking their parent questions,
cancelling a delegation, and listing what happened.

See also [Permission levels](../permission-levels.md), every level on every vendor and where a level
comes from, and [Run a harness on CI](../how-to/run-a-harness-on-ci.md), the same set up by a script.
