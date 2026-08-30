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
