---
name: ynh-adopt
description: Bring an existing AI setup — a .claude folder, .cursorrules, a grown CLAUDE.md — under ynh without losing it. Use when someone already has assistant config and wants to adopt ynh, rather than starting a harness from scratch.
---

# Adopt an existing setup

Almost nobody starts from nothing. They arrive with a `.claude/` folder someone
added six months ago, a `.cursorrules` from before that, and a `CLAUDE.md` that
has grown by accretion.

The question is never "how do I build a harness". It is **"what happens to what
I already have?"** Answer that first, or nothing else lands.

If they genuinely have nothing, use `ynh-create-harness` instead.

## Before you start

- `references/what-moves-where.md` — the decision table, the traps, and the two
  adoption strategies in full

## Step 1 — Inventory, out loud

Look, and tell them what you found. Do not guess.

```bash
ls -a
ls .claude .cursor .github 2>/dev/null
find . -maxdepth 2 \( -name 'CLAUDE.md' -o -name 'AGENTS.md' -o -name '.cursorrules' \
  -o -name 'copilot-instructions.md' -o -name '.harness.json' \) 2>/dev/null
```

What tends to be there:

| Found | What it is |
|---|---|
| `.claude/skills/`, `.claude/agents/`, `.claude/commands/` | already in the artifact format ynh uses |
| `.claude/settings.json` | permissions and hooks — **not** an artifact, stays put |
| `CLAUDE.md` | project instructions, Claude-specific filename |
| `.cursorrules` | project instructions, Cursor's legacy filename |
| `.cursor/rules/*.mdc` | Cursor rules with frontmatter |
| `.github/copilot-instructions.md` | Copilot instructions |
| `AGENTS.md` | already the cross-vendor form — the easy case |
| `.harness.json` | a legacy ynh manifest — run `ynd migrate` |

Then ask the question that decides everything else:

> Do you want this config to stay in this repo, or become something you can
> reuse across projects and share with your team?

Their answer picks the strategy in step 3.

## Step 2 — Deal with the instructions file first

**This is the trap that loses work silently.**

A harness root `CLAUDE.md` is **not** read as instructions. Verified:

| Harness root has | `ynd preview -v claude` emits |
|---|---|
| `CLAUDE.md` | *nothing* — silently ignored |
| `AGENTS.md` | `CLAUDE.md` with the content |
| `instructions.md` | `CLAUDE.md` with the content |

So a project whose instructions live in `CLAUDE.md` will adopt into a harness
that appears to work and has no instructions at all.

**Rename it to `AGENTS.md`.** That is the cross-vendor form; ynh adapts it per
vendor — `CLAUDE.md` for Claude, `codex.md` for Codex, `.cursorrules` for
Cursor, and a projected instructions file for Copilot.

If they have *several* instruction files — `CLAUDE.md` and `.cursorrules` and
`copilot-instructions.md` — that is usually the same content drifting apart.
Read all of them, merge into one `AGENTS.md`, show them the diff. This is often
the moment the value of ynh becomes obvious, so do not rush past it.

## Step 3 — Pick a strategy

Two, and the choice follows from step 1's question.

### A. Include in place — config stays in this repo

Leave everything where it is; point a manifest at it.

```json
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "my-project",
  "version": "0.1.0",
  "description": "...",
  "default_vendor": "claude",
  "includes": [ { "local": ".claude" } ]
}
```

Nothing moves, nothing is copied, `git blame` survives. Verified: a `local`
include pulls `.claude/skills/` and `.claude/agents/` into the assembled output.

Best when the config is genuinely project-specific and only this repo needs it.
The immediate payoff is vendor portability — the same artifacts now assemble for
Codex, Cursor and Copilot with `-v`.

### B. Move into a harness — config becomes reusable

Move the artifacts to the harness layout (`skills/`, `agents/`, `rules/`,
`commands/`) so the harness can be installed anywhere and shared.

Best when they want it across repos or across a team. Costs a `git mv` and the
history follows.

**There is no CLI for adding a `local` include** — `ynh include add` handles Git
includes only. Edit `.ynh-plugin/plugin.json` by hand for strategy A.

## Step 4 — Legacy manifest

If the inventory found `.harness.json`:

```bash
ynd migrate .
```

That runs the whole migration chain and converts it to
`.ynh-plugin/plugin.json` in place. `ynd validate` also migrates transparently,
so a harness can appear to work while still carrying the old file — run
`migrate` explicitly so the change is a reviewable diff rather than a surprise.

## Step 5 — Prove nothing was lost

Do this before installing, and show them the output. Adoption is only credible
if they can see their own artifacts come out the other side.

```bash
ynd validate .
ynd preview . -v claude
```

Check with them, by name, that every skill and agent from step 1's inventory is
in the preview. **A missing instructions file is the usual casualty** — if the
preview has no `CLAUDE.md`, step 2 was skipped.

Then show the thing they could not do before:

```bash
ynd preview . -v codex
ynd preview . -v cursor
ynd preview . -v copilot
```

Same artifacts, three more vendors, no extra work.

## Step 6 — Install and run

```bash
ynh install .
<name>
```

## Step 7 — What stays behind

Say this explicitly, because people expect ynh to swallow everything:

- **`.claude/settings.json`** stays. Permissions and hook wiring are
  project-local vendor config, not harness artifacts. A harness can declare its
  own hooks in the manifest; that is a separate decision, not part of adoption.
- **Anything genuinely specific to this repo** can stay project-local. Adopting
  everything is not the goal — a harness people keep is better than a complete
  one they abandon.

## Where to go next

- New skills for what they do repeatedly → `ynh-create-harness`
- What the project should observe and gate on → `ynh-sensors`
- Sharing across the team → `ynh-team-setup`
