# A First Delegate Session

In this tutorial we will make one harness hand a piece of work to another. We will build a
**reviewer** harness, then a **team-lead** harness that names the reviewer as its delegate. We will
ask the team lead for a review and watch the reviewer do it in its own session, ask the team lead a
question, and sign its answer. Then we will cancel a delegation, list what happened, and add a second
delegate fetched from GitHub.

Everything happens in `/tmp/ynh-tutorial`, with a ynh home of its own, so your usual ynh setup, its
installed harnesses and its trust records are not touched.

## Who this is for

ynh launches AI coding tools (Claude Code, Codex and others) with a harness: a folder of instructions
and settings. A delegate lets one harness hand a piece of work to another, which does it in its own
session and reports back.

This tutorial is for anyone who wants to see delegation work once, from never having used ynh to using it daily. The
tutorial starts from nothing but `git`, ynh 0.18.0 or newer and a vendor CLI, and builds everything else it needs.

If you already install and trust harnesses, you can skip ahead: the parts that are about delegates
are [Make the team lead](#make-the-team-lead) (the `delegates_to` field),
[Ask before trusting](#ask-before-trusting) (the refusal) and [Get a review](#get-a-review) (the
reviewer asking its parent a question). Everything before them only builds the two harnesses. To skip
it you still need ynh 0.18.0 or newer with the `export` lines, the reviewer installed from a folder that
is a committed Git repository (`delegates_to` reads it with `git`), and the team lead installed. You
can paste the manifests and instructions in as they are.

## Words used here

- A **vendor** is an AI command-line tool that ynh launches, such as Claude Code (`claude`).
- A **harness** is a folder with a manifest (`.agents/harness/plugin.json`, the file that names it
  and sets its options) and instructions, which ynh launches inside a vendor. A **hook** is a command
  the manifest asks ynh to run at a set moment, such as when a session starts.
- A **delegate** is a harness that another harness (its **parent**) hands work to. It runs as its own
  session, not inside the parent's. The parent names its delegates in the manifest field
  `delegates_to`.
- **A2A** is the protocol ynh uses to carry that work and the delegate's answers between the two
  sessions. [Delegate Sessions](../delegate-sessions.md) explains it; you do not need it to follow
  this page.
- **Trust** is ynh's record that you have read what a harness will run. We meet it in
  [Ask before trusting](#ask-before-trusting).

## Which vendor?

The reviewer always runs on Claude Code, its own `default_vendor`, whichever parent you choose, so
you need `claude` installed and logged in in every case. The parent is your choice.

The commands below use Claude Code as the parent, so on Claude Code you change nothing. To use
another parent, replace `--allowedTools=mcp__plugin_team-lead_ynh-delegates` in every `team-lead`
command with the flags in the table, and read `claude` in ynh's `Launching claude...` line as your
vendor.

| Parent vendor | Flags in place of `--allowedTools=...` | Also |
|---|---|---|
| Claude Code | none: the commands are as written | |
| Codex | `-v codex` | See [Other vendors](#other-vendors) for the first-run prompt and a known issue. |
| Copilot CLI | `-v copilot --allow-tool=ynh-delegates` | End each prompt with "and wait for its result". |
| OpenCode | `-v opencode` | Its output is long tool-call tracing; look for `PINEAPPLE` as described under [Get a review](#get-a-review). |

`--allowedTools=...` is a Claude Code flag: ynh passes flags it does not recognise on to the vendor.

Model output differs on every run and every vendor. Where this page shows a reply, what matters is
named beside it.

## Before you start

You need:

- `git`, with `user.name` and `user.email` set (`git config user.name`; the reviewer is committed
  to a throwaway repository);
- ynh 0.18.0 or newer, installed;
- Claude Code (`claude`), logged in as you normally use it, and, if you chose another parent, that
  vendor's CLI (`codex`, `copilot` or `opencode`), installed and logged in.

If you have run this tutorial before, start clean:

```bash
rm -rf /tmp/ynh-tutorial
```

## Set up the tutorial folder and ynh home

Make the tutorial folder, and give this terminal a ynh home of its own inside it. Run these three
lines exactly:

```sh
mkdir -p /tmp/ynh-tutorial
export YNH_HOME=/tmp/ynh-tutorial/home
export PATH=/tmp/ynh-tutorial/home/bin:$PATH
```

Check it took:

```sh
echo $YNH_HOME
```

```
/tmp/ynh-tutorial/home
```

Check which ynh you have:

```sh
ynh version
```

It should say 0.18.0 or newer (a build of `develop` prints `dev-develop-` and a commit, and counts).
Delegates that run as their own session arrived in v0.18.0; with an
older ynh, a delegate is a vendor subagent and this tutorial will not behave as written. To install or
upgrade, see the [install instructions](README.md).

The `export` lines last only for the shell you ran them in. Run them again in every new terminal, and
in every separate shell your tools start for you (an editor's terminal, a script, an agent that runs
each command in a fresh shell). If `echo $YNH_HOME` prints nothing, the lines were lost and ynh would
use your usual home: run them again. The same goes for the `cd` later on: the working folder is also
per shell.

## Make the reviewer

The reviewer is an ordinary harness: a manifest and some instructions. Make its folder and manifest:

```bash
mkdir -p /tmp/ynh-tutorial/reviewer/.agents/harness
```

```bash
cat > /tmp/ynh-tutorial/reviewer/.agents/harness/plugin.json <<'EOF'
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
```

The hook is a command, so ynh will ask us to review it before the reviewer may run. Now its
instructions:

```bash
cat > /tmp/ynh-tutorial/reviewer/instructions.md <<'EOF'
You are a reviewer. You are given one file to review.

Before you start, ask your parent one question with
the ask_parent tool: "Which codeword should I sign with?"

Then read the file and reply with one short finding,
signed on its own last line as REVIEWED-<codeword>.
EOF
```

Make the folder a Git repository:

```bash
git -C /tmp/ynh-tutorial/reviewer init -q
git -C /tmp/ynh-tutorial/reviewer add .
git -C /tmp/ynh-tutorial/reviewer commit -q -m init
```

These print nothing.

## Install the reviewer

```bash
ynh install /tmp/ynh-tutorial/reviewer
```

```
Installed harness "reviewer"
  Location: /tmp/ynh-tutorial/reviewer
  Launcher: /tmp/ynh-tutorial/home/bin/reviewer
  Vendor:   claude
```

## Make the team lead

The team lead names the reviewer in `delegates_to`. Its instructions also say what to answer when
the reviewer asks for a codeword:

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

Install it:

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

Look at what ynh made of the delegate:

```bash
ynh info local/team-lead
```

Among the lines it prints:

```
Delegates to:
  /tmp/ynh-tutorial/reviewer  transport=a2a (default)
```

`transport=a2a (default)` means the reviewer will run as its own ynh session, which is what we want.

## Make a project to review

```bash
mkdir -p /tmp/ynh-tutorial/project
```

```bash
cat > /tmp/ynh-tutorial/project/README.md <<'EOF'
# Demo

This project prints hello.
EOF
```

From here on, run every command from the project:

```bash
cd /tmp/ynh-tutorial/project
```

## Ask before trusting

Ask the team lead for a review now, before we have reviewed the reviewer's hook (on another vendor,
use the flags from [Which vendor?](#which-vendor)):

```bash
team-lead \
  --allowedTools=mcp__plugin_team-lead_ynh-delegates \
  "Ask the reviewer to review README.md"
```

ynh refuses, and nothing starts. On macOS the path in the message begins `/private/tmp`, not `/tmp`,
because `/tmp` is a link to it:

```
Error: delegate /private/tmp/ynh-tutorial/reviewer of team-lead is installed as local/reviewer but not trusted
  Review with 'ynh trust show local/reviewer', then run: ynh trust accept local/reviewer
```

Trust is ynh's record that you have read what a harness will run, before ynh runs it. We do that next,
as in [Your First Trusted Harness](first-trusted-harness.md).

## Trust the reviewer

See what the reviewer would run:

```bash
ynh trust show local/reviewer
```

It begins:

```
local/reviewer
  source:   /tmp/ynh-tutorial/reviewer
  state:    unreviewed
  digest:   sha256:...
```

and lists the hook:

```
Hooks, executed by the vendor CLI with your environment:

  on_session_start
    echo Reviewer session started.
```

A list of environment variables follows; yours will differ. It is the environment the hook would
inherit, so it can include tokens and keys from your own shell; it shows names, not values, and can
run to about fifty names. That is why ynh shows it before you accept. You do not need to read each
one. It ends:

```
Not yet reviewed. 'ynh trust accept local/reviewer' records that you have read the above.
```

We have read it. Record that:

```bash
ynh trust accept local/reviewer
```

```
Accepted local/reviewer (accept): 1 command(s), sha256:...
```

The `sha256:` value is the one `ynh trust show` printed as `digest:`. It depends on your ynh's digest scheme as well as on the reviewer, so it is not shown here: a build of `develop` from 2026-11-26 prints `sha256:abab6f64cd7a3573`. This trust is recorded in `/tmp/ynh-tutorial/home`, not in your usual ynh home.

## Get a review

Ask again:

```bash
team-lead \
  --allowedTools=mcp__plugin_team-lead_ynh-delegates \
  "Ask the reviewer to review README.md"
```

`--allowedTools=...` lets this one-shot Claude Code run call the team lead's delegate tools without
stopping to ask you. ynh prints:

```
Opened 1 A2A delegate endpoint(s): reviewer
Launching claude...
```

`Launching claude...` names your parent vendor. In a terminal with no input attached you may also
see a `Warning: no stdin data received in 3s, proceeding without it.` line and a 3 second wait; the
run carries on.

After a minute or so, the team lead reports. Your wording will differ:

```
The reviewer has reviewed `/private/tmp/ynh-tutorial/project/README.md`. It signed off with the
codeword PINEAPPLE.

- **Accuracy:** The README is accurate.
- **Gaps:** It's minimal. It only says the project prints hello. It has no usage, install or run
  instructions, and no license or contact details.
- **Suggestion:** Add a short "Usage" section with the command to run the project.

I haven't changed the file.
```

Look for `PINEAPPLE` in the reply. The reviewer signs its review `REVIEWED-PINEAPPLE`; the team lead
may quote that line or only mention the codeword, and either shows it worked. The reviewer could only
know `PINEAPPLE` by asking the team lead, and the team lead's model answered it, so the question went
from the delegate to its parent and back.

On macOS the model's own text may also show `/private/tmp/...` paths, and a later step's reply may
quote `REVIEWED-PINEAPPLE` too. If a run has printed nothing for three minutes, press Ctrl-C and run
it again.

## Cancel a delegation

Start a review and stop it at once:

```bash
team-lead \
  --allowedTools=mcp__plugin_team-lead_ynh-delegates \
  "Start the reviewer on README.md, cancel it at once, and say its state."
```

```
The reviewer's state is canceled. I started it on README.md (task
dlg-4e9c69e50ee808b9) and cancelled it right away. It had only reached
"working", so it produced no review.
```

The task id is different every time. It always starts with `dlg-`.

## List what happened

Each delegation is recorded, so a later session of the team lead can see the earlier ones:

```bash
team-lead \
  --allowedTools=mcp__plugin_team-lead_ynh-delegates \
  "List your delegations and their states."
```

```
Two delegations, both to the reviewer:

| Task ID              | Delegate | State     |
| dlg-c11fe55296d14e92 | reviewer | completed |
| dlg-4e9c69e50ee808b9 | reviewer | canceled  |
```

## Add a delegate from GitHub

A delegate can come from a Git URL, and ynh asks you to trust it just as it did the reviewer. ynh's
own repository keeps a small harness for this tutorial, the `librarian`, in
[docs/tutorial/harnesses/librarian](https://github.com/eyelock/ynh/tree/develop/docs/tutorial/harnesses/librarian).
It answers one question and runs one hook. Install it:

```bash
ynh install github.com/eyelock/ynh --path docs/tutorial/harnesses/librarian
```

```
Installed harness "librarian"
```

followed by its location, launcher and vendor. Check whether the harness needs trusting:

```bash
ynh trust show github.com/eyelock/ynh/librarian
```

It begins:

```
github.com/eyelock/ynh/librarian
  source:   github.com/eyelock/ynh (develop)
  commit:   <the commit ynh fetched>
  state:    unreviewed
  digest:   sha256:...
```

`(develop)` is the branch ynh fetched, ynh's default. The `commit:` line names the commit it fetched;
yours will differ. So does the digest, which depends on your ynh's digest scheme: a build of `develop`
from 2026-11-26 prints `sha256:8ed89d1ee6cd8c32`. Then
it lists the hook:

```
Hooks, executed by the vendor CLI with your environment:

  on_session_start
    echo Librarian session started.
```

and, as for the reviewer, the names of the environment variables the hook would inherit, ending:

```
Not yet reviewed. 'ynh trust accept github.com/eyelock/ynh/librarian' records that you have read the above.
```

This hook came from someone else's repository, so reading it before accepting matters more than it
did for the reviewer, which you wrote yourself. It only prints a line. Record that you have read
it:

```bash
ynh trust accept github.com/eyelock/ynh/librarian
```

```
Accepted github.com/eyelock/ynh/librarian (accept): 1 command(s), sha256:...
```

Now name it in the team lead, writing the whole manifest again:

```bash
cat > /tmp/ynh-tutorial/team-lead/.agents/harness/plugin.json <<'EOF'
{
  "name": "team-lead",
  "version": "0.1.0",
  "description": "Hands reviews to a reviewer",
  "default_vendor": "claude",
  "delegates_to": [
    {"git": "/tmp/ynh-tutorial/reviewer"},
    {"git": "github.com/eyelock/ynh",
     "path": "docs/tutorial/harnesses/librarian"}
  ]
}
EOF
```

Install the team lead again, so it picks up the change:

```bash
ynh install /tmp/ynh-tutorial/team-lead
```

```
Fetching 0 include(s) and 2 delegate(s)...
```

and then a `Fetched` line for each delegate, followed by the same install lines as before. Ask the new delegate something:

```bash
team-lead \
  --allowedTools=mcp__plugin_team-lead_ynh-delegates \
  "Ask the librarian delegate what a harness is."
```

```
Opened 2 A2A delegate endpoint(s): librarian,reviewer
Launching claude...
```

After a minute or so the team lead reports the librarian's answer. Your wording will differ:

```
The librarian said: "A harness is a folder of instructions and settings that ynh
launches inside an AI coding tool. SHELVED"
```

Look for `SHELVED` in the reply. The librarian's instructions ask it to end its answer with that
word; the team lead may quote the answer or only mention the word, and either shows it worked. Only
the librarian, running as its own session, was told to say it.

## What we did

We made a delegate and a parent, installed both, trusted the delegate's hook, and had the parent hand
it a review. The reviewer ran as its own session, asked its parent a question and signed its answer.
We cancelled a delegation, listed both, and added a second delegate from GitHub, trusting its hook
as we did the reviewer's.

## Other vendors

The same harnesses work with another vendor as the parent, chosen as in [Which vendor?](#which-vendor).
What else changes:

- **Codex:** ynh approves the delegate tools itself. The first run in a project asks to make two
  links in it; say yes. Without a terminal to answer on, run
  `ynh run local/team-lead -v codex --install` once first.
- **Copilot CLI:** without "and wait for its result" at the end of the prompt, the model may stop once
  the review has started.
- **OpenCode:** no allow rule is needed. Its output is long tool-call tracing, nothing like the replies
  shown here; look for the codeword as before.

The reviewer itself still runs on Claude Code, its own `default_vendor`.

> **Known issue.** On Codex, the prompt "Ask the reviewer to review README.md" can make the model use the
> `a2a` shell route, which fails with `command not found: a2a` unless the `a2a` binary is installed (`ynh
> doctor` warns of this). Name the tool in the prompt instead: "Use your delegate_to_reviewer tool to ask the
> reviewer to review README.md, wait for its result, and report it."

> **Note.** Last run end to end on 2026-11-17 on macOS, with ynh at `develop` `3c75df5`, with Claude Code
> as the parent. That run fetched the librarian from this page's pull request branch, before it reached
> `develop`. An earlier run, on 2026-11-15 with ynh at `develop` `09e9f50`, also had Codex, Copilot CLI
> and OpenCode as the parent, with an earlier GitHub delegate that ran no commands; the librarian steps
> have not been run on those vendors.

> **Intended, not run.** Cursor as the parent has not been run on the delegate routes for v0.18.0.

## Clean up

```bash
ynh uninstall local/team-lead
ynh uninstall local/reviewer
ynh uninstall github.com/eyelock/ynh/librarian   # only if you added it
cd /tmp
rm -rf /tmp/ynh-tutorial
```

Each `ynh uninstall` of a local harness prints `Source tree left in place`: it leaves your folder,
and the `rm -rf` above removes it. Then close this terminal, or open a new one, to leave the tutorial's `YNH_HOME` and `PATH` behind.

## Learn more

- [Delegate Sessions](../delegate-sessions.md): why a delegate is its own session, and how it reaches
  its parent.
- [delegates_to](../harnesses.md#delegates-to-optional) and [Transport](../harnesses.md#transport):
  the manifest fields.
- [Native Delegation](delegation.md): the same idea with `transport: native`, as the vendor's own
  subagent.

## Next

[Native Delegation](delegation.md) — run a delegate as the vendor's own subagent instead.
