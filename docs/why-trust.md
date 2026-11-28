<!-- Explanation. Checked against a build of ynh v0.18.0 (develop) in a scratch YNH_HOME, 2026-11-27. -->

# Why ynh Asks You to Trust a Harness

A harness looks like a folder of words: instructions, skills, rules. Some of what it carries is not
words. Hooks, MCP servers and sensors are commands, and they run on your machine, as you, with your
environment and your credentials. A hook runs before or after the assistant's tools; an MCP server is a
program the assistant talks to; a sensor is a command `ynh check` and `ynh agent run` execute.

That is the point of them: a harness can bring a linter, a test runner or a guard along with it. It is
also why ynh asks you to look before they run.

## What the trust gate protects

Most of the time the code a harness runs is far from where you are looking. A harness installed from a
Git URL lives in ynh's home, `~/.ynh/harnesses/`, while you work in a project somewhere else. An
`ynh update` can bring new commands, or new versions of the scripts behind them, and nothing in your
project changes to show it.

So ynh keeps a record of what you agreed to. `ynh trust show` lists every command the harness can get
run, and `ynh trust accept` records a fingerprint of that list, the **digest**. Before `ynh run`,
`ynh agent run`, `ynh check`, `ynh sensors run`, `ynh baseline --explain` or `ynh image` starts anything, ynh works out the digest again and
compares. If you never accepted the harness, or what it runs has changed since you did, nothing starts,
and ynh tells you what it would have run and how to review it.

The gate is about *noticing*. It does not judge whether a command is safe; you do that, once, when you
read `ynh trust show`. What ynh adds is that your judgement stays attached to exactly what you read, and
lapses the moment that changes.

## What the review covers

The digest covers everything `ynh trust show` prints, for the harness and for every profile it has:

- each hook, with its event and matcher, including hooks an included harness brings when you have
  switched them on for that include;
- each MCP server: its command, arguments, URL and working folder, and the values of its environment
  entries and headers (fingerprinted, never printed);
- each sensor command;
- the **scripts** those commands run, when they live in the harness: the whole folder a script sits
  in, every file in it. That is why, in the tutorial, changing the body of `guard.sh` was noticed;
- the workspace folders the harness asks for, its launch focus prompt, and the delegates it hands
  work to, with the focus prompts it picks for them.

The commands that included harnesses bring are covered as well, because they run in the same session.
A harness that brings none of these needs no review, and runs at once.

What the review does not cover is the harness's words: its instructions, skills, agents, rules and
commands. They steer the assistant, but they run nothing by themselves. For a harness installed from
Git, though, your acceptance is also of the commit you reviewed, so an update to a new commit asks
again whatever it changed.

## What it leaves to you

Some things the digest cannot vouch for, and `ynh trust show` says so where it meets them:

- **Your project's own code.** A hook that runs `bash scripts/wake.sh`, or a sensor that runs
  `./lint.sh` in the project, runs the project's file, which you review as part of the project, like
  `make check`. `ynh trust show` marks such a script "not covered: project code".
- **Tools found on your `PATH`.** For a command such as `gofmt -l .` or `npx -y some-server`, the
  command text is covered, not the program it starts or the package it downloads.
- **What a covered script reaches outside its own folder**, such as a file it sources from elsewhere.

And the gate is not a sandbox. It decides whether a run starts, on what you reviewed. It does not limit
what an accepted command can do once it runs.

## Why the record lives outside the harness

The record is kept in `$YNH_HOME/trust.json`, in your ynh home, never inside the harness. A harness
that carried its own record could vouch for itself: whoever wrote the harness could ship an acceptance
with it. Keeping the record with you means only you, on your machine, can say "I have read this".

The same idea explains the other routes to acceptance:

- **`--trust`** accepts in the same step as an install, an image build or a folder run, without a
  separate review. In return it asks for everything to be pinned to a full 40-character commit SHA, a
  commit that cannot move, so what you accept today is exactly what runs tomorrow. That is how CI,
  which has nobody to read `ynh trust show`, accepts a harness.
- **An image** carries the acceptance made on the machine that built it. That host reviewed the
  harness; the image holds its record, and the gate inside the image still checks it at every start.
- **A delegate** that runs as its own session has its own record: you accept the delegate itself, not
  the parent that hands it work.

## Runs from a folder

There is one case where ynh does not check a harness's own commands: a run named by a path, such as
`ynh run ./my-harness`. A folder you name on the command line is yours, and you are looking at it, so
ynh says once that its own commands are not trust-checked and runs it. The commands its remote
includes bring are checked, because they come from somewhere else.

Because nothing vouches for a folder run, ynh never gives it the extras an accepted harness earns, such
as pre-approved MCP tools: see [What an accepted harness gets, by vendor](trust-by-vendor.md). To get
those, install the folder and accept it.

## Related

- [Your first trusted harness](tutorial/first-trusted-harness.md): the review and the gate, step by step.
- [What invalidates trust](what-invalidates-trust.md) and the [Trust reference](trust.md): the facts.
