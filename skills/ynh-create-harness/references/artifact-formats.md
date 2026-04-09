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

## Project instructions (AGENTS.md)

Optional file at harness root. Most vendors read `AGENTS.md` natively. For Claude, ynh generates a `CLAUDE.md` with an `@AGENTS.md` import.

| Vendor | Native support | ynh shim |
|--------|---------------|----------|
| Claude | No | `CLAUDE.md` with `@AGENTS.md` import |
| Codex  | Yes | — |
| Cursor | Yes | — |

Last source wins. Harness's own `AGENTS.md` takes priority over included repos.
