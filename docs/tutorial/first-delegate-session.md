# A First Delegate Session

In this tutorial we will make one harness hand a piece of work to another. We will build a
**reviewer** harness, then a **team-lead** harness that names the reviewer as its delegate. We will
ask the team lead for a review and watch the reviewer do it in its own session, ask the team lead a
question, and sign its answer. Then we will cancel a delegation, list what happened, and add a second
delegate fetched from GitHub.

Everything happens in `/tmp/ynh-tutorial`, with a ynh built for this tutorial and a ynh home of its
own, so your usual ynh setup, its installed harnesses and its trust records are not touched.

The examples use Claude Code. [Other vendors](#other-vendors) says what changes for the rest.

## Before you start

You need:

- `git`, and Go 1.26 or newer (`go version`);
- Claude Code (`claude`), logged in as you normally use it.

If you have run this tutorial before, start clean:

```bash
rm -rf /tmp/ynh-tutorial
```

## Build ynh into the tutorial folder

Make the folder and fetch ynh's source:

```sh
mkdir -p /tmp/ynh-tutorial
git clone --branch develop \
  https://github.com/eyelock/ynh.git \
  /tmp/ynh-tutorial/ynh-src
```

Build it:

```sh
make -C /tmp/ynh-tutorial/ynh-src build
```

The build ends with lines naming Go packages, such as:

```
github.com/eyelock/ynh/cmd/ynd
```

Now point this terminal at the new build and at a ynh home of its own. Run these two lines exactly:

```sh
export YNH_HOME=/tmp/ynh-tutorial/home
export PATH=/tmp/ynh-tutorial/ynh-src/bin:/tmp/ynh-tutorial/home/bin:$PATH
```

They last only for this terminal. If you open a new one during the tutorial, run the two lines
again there.

Check that the terminal uses the new build:

```sh
which ynh
```

```
/tmp/ynh-tutorial/ynh-src/bin/ynh
```

```sh
ynh version
```

```
dev-develop-6848839
```

Yours ends with a different commit, but it starts with `dev-develop-`.

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

Ask the team lead for a review now, before we have reviewed the reviewer's hook:

```bash
team-lead \
  --allowedTools=mcp__plugin_team-lead_ynh-delegates \
  "Ask the reviewer to review README.md"
```

ynh refuses, and nothing starts:

```
Error: delegate /private/tmp/ynh-tutorial/reviewer of team-lead is installed as local/reviewer but not trusted
  Review with 'ynh trust show local/reviewer', then run: ynh trust accept local/reviewer
```

Trust is ynh's record that you have read what a harness will run, before ynh runs it. We do that next.

> **Note.** On macOS the path begins `/private/tmp`, because `/tmp` is a link to it.

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
  digest:   sha256:3d35eeebcbc2283a
```

and lists the hook:

```
Hooks, executed by the vendor CLI with your environment:

  on_session_start
    echo Reviewer session started.
```

A list of environment variables follows; yours will differ. It ends:

```
Not yet reviewed. 'ynh trust accept local/reviewer' records that you have read the above.
```

We have read it. Record that:

```bash
ynh trust accept local/reviewer
```

```
Recorded local/reviewer: 1 command(s), sha256:3d35eeebcbc2283a
```

The `sha256:` value is the one `ynh trust show` printed as `digest:`. This trust is recorded in `/tmp/ynh-tutorial/home`, not in your usual ynh home.

## Get a review

Ask again:

*This launches:* `claude --plugin-dir .../run/.snapshots/local--team-lead/.../.claude --add-dir .../run/.snapshots/local--team-lead/... --append-system-prompt You are a team lead. ... -p Ask the reviewer to review README.md --allowedTools=mcp__plugin_team-lead_ynh-delegates`
*Your output will differ: it shows what the model did.*
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

After a minute or so, the team lead reports:

```
The reviewer finished its review of README.md. I handed the whole review to them
and didn't read the file myself.

- Verdict: The README is accurate but too thin. It says the project prints hello,
  and it doesn't say how to install or run it.
- Sign-off: The reviewer asked which codeword to sign with. I answered PINEAPPLE,
  and they signed off with REVIEWED-PINEAPPLE.
```

Look for `REVIEWED-PINEAPPLE`. The reviewer could only know `PINEAPPLE` by asking the team lead,
and the team lead's model answered it, so the question went from the delegate to its parent and
back.

## Cancel a delegation

Start a review and stop it at once:

*This launches:* `claude --plugin-dir .../run/.snapshots/local--team-lead/.../.claude --add-dir .../run/.snapshots/local--team-lead/... --append-system-prompt You are a team lead. ... -p Start the reviewer on README.md, cancel it at once, and say its state. --allowedTools=mcp__plugin_team-lead_ynh-delegates`
*Your output will differ: it shows what the model did.*
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

*This launches:* `claude --plugin-dir .../run/.snapshots/local--team-lead/.../.claude --add-dir .../run/.snapshots/local--team-lead/... --append-system-prompt You are a team lead. ... -p List your delegations and their states. --allowedTools=mcp__plugin_team-lead_ynh-delegates`
*Your output will differ: it shows what the model did.*
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

A delegate can come from a Git URL. Install the `researcher` harness from eyelock/assistants:

```bash
ynh install github.com/eyelock/assistants --path ynh/researcher
```

```
Fetching 1 include(s) and 0 delegate(s)...
  Fetched eyelock/assistants
Installed harness "researcher"
```

followed by its location, launcher and vendor. Check whether it needs trusting:

```bash
ynh trust show github.com/eyelock/assistants/researcher
```

Among its first lines:

```
  state:    no-commands
```

It runs no commands of its own, so there is nothing to accept. Now name it in the team lead,
writing the whole manifest again:

```bash
cat > /tmp/ynh-tutorial/team-lead/.agents/harness/plugin.json <<'EOF'
{
  "name": "team-lead",
  "version": "0.1.0",
  "description": "Hands reviews to a reviewer",
  "default_vendor": "claude",
  "delegates_to": [
    {"git": "/tmp/ynh-tutorial/reviewer"},
    {"git": "github.com/eyelock/assistants",
     "path": "ynh/researcher"}
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

and then the same lines as before. Ask the new delegate something:

*This launches:* `claude --plugin-dir .../run/.snapshots/local--team-lead/.../.claude --add-dir .../run/.snapshots/local--team-lead/... --append-system-prompt You are a team lead. ... -p Ask the researcher delegate, in one sentence, what kind of harness it is. --allowedTools=mcp__plugin_team-lead_ynh-delegates`
*Your output will differ: it shows what the model did.*
```bash
team-lead \
  --allowedTools=mcp__plugin_team-lead_ynh-delegates \
  "Ask the researcher delegate, in one sentence, what kind of harness it is."
```

```
Opened 2 A2A delegate endpoint(s): researcher,reviewer
Launching claude...
```

```
The researcher delegate said: "I'm Claude Code, Anthropic's agent harness: a
terminal-based CLI that lets me read and edit files, run commands, and call tools
or sub-agents on your behalf."
```

> **Intended, not run.** A delegate from a remote Git URL or a registry entry whose harness runs commands,
> so that it needs `ynh trust accept` as the reviewer did, is the designed path and has not been shown
> live. The researcher here runs no commands, so it needs no accepting.

## What we did

We made a delegate and a parent, installed both, trusted the delegate's hook, and had the parent hand
it a review. The reviewer ran as its own session, asked its parent a question and signed its answer.
We cancelled a delegation, listed both, and added a second delegate from GitHub.

## Other vendors

The same harnesses work with another vendor as the parent, chosen with `-v`. What changes:

- **Codex:** use `-v codex` in place of `--allowedTools=...`: ynh
  approves the delegate tools itself. The first run in a project asks to make two links in it; say
  yes.
- **Copilot CLI:** add `-v copilot --allow-tool=ynh-delegates`, and end the prompt with "and wait
  for its result". Without "wait for its result", the model may stop once the review has started.
- **OpenCode:** use `-v opencode` in place of `--allowedTools=...`. No allow rule is needed.

The reviewer itself still runs on Claude Code, its own `default_vendor`.

> **Intended, not run.** Cursor as the parent has not been run on the delegate routes for v0.18.0.

## Clean up

```bash
ynh uninstall local/team-lead
ynh uninstall local/reviewer
ynh uninstall github.com/eyelock/assistants/researcher
cd /tmp
rm -rf /tmp/ynh-tutorial
```

Then close this terminal, or open a new one, to leave the tutorial's `YNH_HOME` and `PATH` behind.

## Learn more

- [Delegate Sessions](../delegate-sessions.md): why a delegate is its own session, and how it reaches
  its parent.
- [delegates_to](../harnesses.md#delegates-to-optional) and [Transport](../harnesses.md#transport):
  the manifest fields.
- [Delegation](delegation.md): the same idea with `transport: native`, as the vendor's own
  subagent.

## Next

[Delegation](delegation.md) — run a delegate as the vendor's own subagent instead.
