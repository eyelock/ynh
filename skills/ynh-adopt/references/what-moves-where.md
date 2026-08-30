# What moves, what stays, and what silently vanishes

Every claim here was checked by running it against a real project, not read off
a schema.

## The decision table

| Found in the project | Becomes | Notes |
|---|---|---|
| `.claude/skills/<name>/SKILL.md` | `skills/<name>/SKILL.md` | already the right format — a move, not a rewrite |
| `.claude/agents/<name>.md` | `agents/<name>.md` | check `tools:` is present; ynh's linter requires it |
| `.claude/commands/<name>.md` | `commands/<name>.md` | |
| `.claude/rules/` or rule-ish markdown | `rules/<name>.md` | plain markdown, no frontmatter |
| `CLAUDE.md` | **`AGENTS.md`** | rename — see the trap below |
| `.cursorrules` | merge into `AGENTS.md` | Cursor's legacy form; ynh regenerates it per vendor |
| `.cursor/rules/*.mdc` | `rules/<name>.md` | ynh writes `.mdc` back out for Cursor |
| `.github/copilot-instructions.md` | merge into `AGENTS.md` | |
| `AGENTS.md` | stays `AGENTS.md` | already correct |
| `.harness.json` | `.ynh-plugin/plugin.json` | `ynd migrate .` |
| `.claude/settings.json` | **stays put** | vendor config, not a harness artifact |
| `.mcp.json` | manifest `mcp_servers` | a decision, not a move |

## The trap: a root `CLAUDE.md` is ignored

This is the one that loses work without an error.

The assembler copies the harness's instructions to the vendor's instructions
file. It looks for `instructions.md` and `AGENTS.md`. It does **not** look for
`CLAUDE.md` at the harness root — that is a file ynh *generates*, not one it
reads.

Verified by putting each name at a harness root in turn and running
`ynd preview . -v claude`:

| Harness root has | Preview emits |
|---|---|
| `CLAUDE.md` | *nothing* |
| `AGENTS.md` | `CLAUDE.md` containing the content |
| `instructions.md` | `CLAUDE.md` containing the content |

So a project whose instructions live in `CLAUDE.md` adopts into a harness that
validates, previews, installs and runs — with no instructions at all. Everything
looks fine.

**Always check the preview for an instructions file by name.** Its absence is
the signal.

Note this is only about the *harness root*. A `CLAUDE.md` in the user's project
directory is still read by Claude Code as normal; it is just not part of the
harness.

## Strategy A: include in place

```json
"includes": [ { "local": ".claude" } ]
```

Verified: a project with `.claude/skills/deploy/SKILL.md` and
`.claude/agents/reviewer.md` and this manifest produces

```
.claude-plugin/plugin.json
.claude/agents/reviewer.md
.claude/skills/deploy/SKILL.md
```

Nothing is copied, nothing is moved, history is untouched.

**There is no CLI for this.** `ynh include add` manages Git includes only, so
the `local` include is hand-written into `.ynh-plugin/plugin.json`.

When it is right:

- the config is genuinely project-specific
- the team is not ready to move files around
- you want the vendor-portability win today with the smallest possible diff

When it is not:

- they want to install this harness in another repo — a local include points at
  a path that will not exist there
- they want to share it — use Git includes or a team harness instead

## Strategy B: move into the harness layout

```bash
git mv .claude/skills skills
git mv .claude/agents agents
git mv CLAUDE.md AGENTS.md
```

`git mv` keeps history, and the diff reads as a rename rather than a
delete-plus-add.

When it is right: the artifacts are worth reusing, or the team should share
them. This is also the step before `ynh-team-setup`.

## Adopting from a vendor that is not Claude

The `.claude/` layout is the common case, but the same logic holds:

| From | Notes |
|---|---|
| **Cursor** | `.cursor/rules/*.mdc` → `rules/`. ynh writes `.mdc` back out with frontmatter, so nothing is lost round-tripping. `.cursorrules` is deprecated upstream; fold it into `AGENTS.md`. |
| **Codex** | Already reads `AGENTS.md` natively, so instructions usually need no change. Skills in `.agents/skills/` move to `skills/`. |
| **Copilot** | `.github/copilot-instructions.md` → `AGENTS.md`. Custom agents are `.agent.md` files under `.github/agents/`; the frontmatter is compatible, the extension is not — rename to `<name>.md` under `agents/`. |

## After adoption: the check that proves it

```bash
ynd validate .
ynd preview . -v claude | sort
```

Read the preview against the step 1 inventory, item by item. Anything in the
inventory and not in the preview did not adopt.

Then the payoff, which is worth demonstrating rather than describing:

```bash
for v in claude codex cursor copilot; do
  echo "== $v"; ynd preview . -v "$v" -o "/tmp/adopt-$v" >/dev/null && \
    find "/tmp/adopt-$v" -type f | sed "s|/tmp/adopt-$v/||" | sort
done
```

Same artifacts, four vendor layouts. That is the argument for adopting, and it
is more convincing run than explained.
