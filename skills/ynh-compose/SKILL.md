---
name: ynh-compose
description: Compose harnesses from other harnesses — includes, delegates, profiles, focuses and fork. Use when someone has one working harness and wants to reuse parts of it, share it, or vary it per situation rather than copying folders around.
---

# Compose a harness

Someone with one working harness is one step from the thing that makes ynh worth
using. **Composition is what separates a harness from a folder of markdown you
copy between repos**, and it is exactly what a new user will not discover
unaided.

Five mechanisms. Reach for them in this order, because that is the order of
increasing commitment.

| Mechanism | Question it answers |
|---|---|
| **`includes`** | can I use someone else's artifacts without copying them? |
| **`profiles`** | can this harness behave differently in different situations? |
| **`focuses`** | can I skip explaining what I want every time? |
| **`delegates_to`** | can this harness hand work to a specialist? |
| **`fork`** | I want someone's harness, but mine |

## Before you start

- `references/composition-patterns.md` — worked patterns, the resolution rules,
  and the traps

## Step 1 — Find out what they are copying

Ask directly:

> Is there anything in this harness you have copied from somewhere else, or that
> you would want in another project?

Copied artifacts are the signal. A skill duplicated across three repos wants to
be an include. A rule everyone on the team pastes wants to be a team harness.

## Step 2 — `includes` — use artifacts without owning them

An include pulls `skills/`, `agents/`, `rules/` and `commands/` from another
harness at assembly time. Nothing is copied into the repo.

```bash
ynh include add <harness> github.com/org/shared-skills
ynh include add <harness> github.com/org/monorepo --path harnesses/team --ref v1.2.0
```

Three things worth saying as you do it:

- **Pin third-party includes.** `--ref v1.2.0` or a commit. An include is
  markdown that becomes instructions inside your agent, so an unpinned branch is
  a supply-chain decision you did not make deliberately.
- **`pick` narrows it.** Take two skills from a repo of thirty rather than all
  thirty. Edit the manifest for this; there is no flag.
- **Local includes exist and have no CLI.** `{"local": "vendor-shared"}` in the
  manifest pulls from a directory in the same repo. `ynh include add` handles
  Git only, so write it by hand.

**A local include may not traverse above the harness directory.** `"../shared"`
is rejected at resolve time — and, today, `ynd validate` does not catch it, so
you find out at `ynd compose` or `ynd preview`. Keep included directories inside
the harness.

## Step 3 — `profiles` — one harness, several ways of working

A profile overlays config: extra `includes`, `hooks`, `mcp_servers` and
`env_passthrough`.

```bash
ynh profile add <harness> ci
ynh profile hook add <harness> ci on_stop "make check"
```

Propose one only where someone genuinely works differently. *An unused profile
is a maintenance cost with no reader* — do not create one per environment
because environments exist.

There is no `ynh profile ls`. Use `ynh info <name>`, which prints the resolved
profiles, focuses and sensors.

## Step 4 — `focuses` — a named way in

A focus binds a starting prompt to a profile. Cheapest thing here and the most
used, because it turns "what do I even ask it" into a command.

```bash
ynh focus add <harness> ship "Walk me through shipping this change." --profile ci
```

Ask what they did three times last week. That is a focus.

```bash
ynh run <harness> --focus ship
```

## Step 5 — `delegates_to` — hand work to a specialist

Delegation makes another harness available as a subagent. Use it when the other
harness is genuinely someone else's domain — a team harness delegating to
personal ones, or a generalist delegating to a security reviewer.

The `ynh-team-setup` skill covers this properly, including auth for private
repos. Send them there rather than half-explaining it.

**Includes vs delegates**, since this is the confusion:

- an **include** merges artifacts in — they become part of this harness
- a **delegate** stays separate and is called into — its context is its own

## Step 6 — `fork` — take it and make it yours

```bash
ynh fork <name> --to ./my-version
```

For "their harness is close, but I want to change it". Better than copying
because the provenance is recorded.

## Step 7 — Show them what composition produced

This is the step that makes it real, and it is worth running rather than
describing.

```bash
ynd compose <harness> --format text
```

```
Artifacts (2 total):
  TYPE   NAME    SOURCE
  skill  deploy  app
  skill  commit

Includes (1):
    [resolved]

Profiles: ci

Focuses:
  ship    profile=ci    "Walk me through shipping this change."
```

That answers "where did this artifact come from", which is the question
composition creates.

**Known gap:** artifacts from a *local* include come back with a blank SOURCE,
and the include itself shows no path. Git includes attribute correctly. If a
source is blank, it came from a local include.

Then confirm what a vendor actually receives:

```bash
ynd preview <harness> -v claude
ynd preview <harness> --profile ci
```

## Resolution order, when things surprise them

- **Vendor**: `-v` flag > harness `default_vendor` > global config
- **Instructions**: last source wins — the harness's own `AGENTS.md` beats an
  included one
- **Sensors**: root-only. An included harness's sensors are never read, by
  design, so "what observes this repository" stays in one committed file

## Where to go next

- Sharing with a team → `ynh-team-setup`
- What the composed harness should observe → `ynh-sensors`
