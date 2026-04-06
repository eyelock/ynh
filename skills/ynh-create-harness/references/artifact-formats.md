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

## Project instructions (instructions.md)

Optional file at harness root. Copied to vendor-specific filename at runtime:

| Vendor | Target |
|--------|--------|
| Claude | `CLAUDE.md` |
| Codex  | `codex.md` |
| Cursor | `.cursorrules` |

Last source wins. Harness's own `instructions.md` takes priority over included repos.
