# Artifact Formats Reference

## Skills

A directory with a `SKILL.md` following the [Agent Skills](https://agentskills.io) spec.

```
skills/<name>/SKILL.md
```

Required frontmatter:

```yaml
---
name: review                # lowercase, hyphens. Must match directory name.
description: Review code for security and performance issues.
---
```

Body is markdown instructions. Optional subdirectories: `scripts/`, `references/`, `assets/`.

Keep SKILL.md under 500 lines. Move detailed content to `references/`.

## Agents

A markdown file with YAML frontmatter. Format is vendor-specific.

```
agents/<name>.md
```

```yaml
---
name: code-reviewer
description: Review code for quality and security. Use proactively after modifications.
tools: Read, Grep, Glob
---

System prompt content describing the agent's expertise.
```

## Rules

A plain markdown file loaded as persistent context every session.

```
rules/<name>.md
```

```markdown
Always write tests for new code. Prefer test-driven development.
Run tests before committing.
```

## Commands

A markdown file describing a reusable action.

```
commands/<name>.md
```

```markdown
Run project checks: format, lint, and test. Fix any issues found.

```bash
make format && make lint && make test
```
```

## Which artifact types survive which vendor

**Every vendor receives all four types when you `ynh run`.** The difference
appears when you *distribute* — `ynd export`, or publishing to a marketplace —
because the vendors' plugin formats differ in what they can carry.

| | Claude | Cursor | Codex | Copilot |
|---|---|---|---|---|
| skills | yes | yes | yes | yes |
| agents | yes | yes | **no** | yes |
| rules | yes | yes, renamed `.mdc` | **no** | **no** |
| commands | yes | yes | **no** | **no** |
| manifest | `.claude-plugin/` | `.cursor-plugin/` | `.codex-plugin/` | `.claude-plugin/` |

Measured with `ynd export` against a harness carrying all four, not read off a
table. Two details that surprise people:

- **Cursor renames rules** from `.md` to `.mdc` and adds frontmatter — the same
  content, a different file, which is what Cursor reads.
- **Copilot uses Claude's manifest directory**, `.claude-plugin/plugin.json`. A
  documented compatibility path, not a mistake.

### What this means when authoring

`ynd export` tells you what it dropped:

```console
$ ynd export . -v codex -o ./dist
Exported for codex → ./dist/codex (8 skills, 0 agents)
  warning: codex: skipping 3 agents, 1 rules, and 1 commands (not supported)
```

So nothing is lost silently. But it is worth knowing **before** you author, not
after:

- If the harness is only ever run through `ynh`, use whatever artifact types
  suit the work. All four reach every vendor.
- If you intend to publish it as a native plugin, put the load-bearing content
  in **skills** — the only type every vendor carries. An agent's instructions
  can usually live in a skill; a rule's often belongs in `AGENTS.md`, which
  every vendor receives.
- `ynh vendors` answers "is this CLI installed", **not** "what does this vendor
  support". This table is the second question.

## Project instructions (AGENTS.md)

Optional file at harness root. Write it once; ynh adapts it per vendor.

**The two paths differ, and it matters if you go looking for the output.**
`ynh run` / `ynd preview` assemble a session config; `ynd export` produces a
redistributable plugin directory.

| Vendor | `ynh run` / `ynd preview` | `ynd export` |
|--------|---------------------------|--------------|
| Claude | `CLAUDE.md`, instructions **inlined** — no `AGENTS.md` emitted | `AGENTS.md` + `CLAUDE.md` containing `@AGENTS.md` |
| Codex  | `codex.md` | `AGENTS.md` |
| Cursor | `.cursorrules` | `.cursorrules` + `AGENTS.md` |
| Copilot | `AGENTS.md` in the staging dir, which `ynh run` projects into the project as `.github/instructions/ynh-harness.instructions.md` (a plugin's bundled `AGENTS.md` is not read) | `AGENTS.md` |

The `@AGENTS.md` import is an export-path artifact only. If you preview a Claude
harness and look for it, you will not find it — the instructions are already in
`CLAUDE.md`.

Last source wins. Harness's own `AGENTS.md` takes priority over included repos.
