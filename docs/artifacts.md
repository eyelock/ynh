# Artifacts Guide

Artifacts are the building blocks of a harness: skills, agents, rules, and commands. ynh doesn't define their format - it uses whatever format the vendor expects and passes content through unchanged.

## Skills

Skills follow the open [Agent Skills](https://agentskills.io) specification - a portable format adopted by Claude Code, Codex, Cursor, Gemini CLI, and many other agent products.

A skill is a directory containing a `SKILL.md` file with YAML frontmatter and Markdown instructions:

```
skills/
└── review/
    └── SKILL.md
```

**SKILL.md format** ([full spec](https://agentskills.io/specification)):

```markdown
---
name: review
description: Review code for security, performance, and maintainability issues.
---

## When to use

Use when reviewing pull requests or code changes.

## Steps

1. Check for OWASP top 10 vulnerabilities
2. Look for performance bottlenecks
3. Assess test coverage gaps

Provide specific, actionable feedback with file paths and line numbers.
```

**Required frontmatter:**

| Field | Description |
|-------|-------------|
| `name` | Lowercase, hyphens only. Must match the directory name. Max 64 chars. |
| `description` | What the skill does and when to use it. Max 1024 chars. |

**Optional frontmatter:** `license`, `compatibility`, `metadata`, `allowed-tools`. See the [spec](https://agentskills.io/specification) for details.

**Optional directories** within a skill:

| Directory | Purpose |
|-----------|---------|
| `scripts/` | Executable code the agent can run |
| `references/` | Additional documentation loaded on demand |
| `assets/` | Templates, schemas, static resources |

Skills use **progressive disclosure**: agents load only the `name` and `description` at startup, then read the full `SKILL.md` when the skill is activated. Keep `SKILL.md` under 500 lines; move detailed content to `references/`.

Skills from community sources like [skills.sh](https://skills.sh) and the [Anthropic examples](https://github.com/anthropics/skills) follow this same format and work directly with ynh.

For authoring guidance, see the [Agent Skills best practices](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/best-practices).

## Agents

An agent is a specialist that the AI can delegate to during a session. Agent format is vendor-specific - the following example uses the Claude Code convention:

```
agents/
└── code-reviewer.md
```

**Example:**

```markdown
---
name: code-reviewer
description: Review code for quality and security. Use proactively after modifications.
tools: Read, Grep, Glob
---

You are a code review specialist. When delegated to, review the provided code for:

- Security vulnerabilities
- Performance issues
- Readability and maintainability
- Test coverage gaps

Provide actionable feedback, not just observations.
```

Refer to your vendor's documentation for agent format details:
- [Claude Code sub-agents](https://docs.anthropic.com/en/docs/claude-code/sub-agents)
- [Codex agents](https://github.com/openai/codex)

## Rules

A rule provides persistent context loaded into every session. Rules are vendor-specific markdown files.

```
rules/
└── always-test.md
```

**Example:**

```markdown
Always write tests for new code. Prefer test-driven development.
Run tests before committing. Aim for high coverage on business logic.
```

## Commands

A command defines a reusable action. Commands are vendor-specific markdown files.

```
commands/
└── check.md
```

**Example:**

```markdown
Run project checks: format, lint, and test. Fix any issues found.

\```bash
make format && make lint && make test
\```

If any step fails, fix the issue and re-run until all checks pass.
```

## Project Instructions

A harness can include an `instructions.md` file with project-level instructions. At runtime, this is copied to the vendor-specific instructions file at the project root:

| Vendor | Target filename |
|--------|----------------|
| Claude | `CLAUDE.md` |
| Codex | `codex.md` |
| Cursor | `.cursorrules` |

```
my-harness/
├── harness.json
└── instructions.md       <- becomes CLAUDE.md (or vendor equivalent)
```

**Example instructions.md:**

```markdown
You are a senior developer working on a Go microservices codebase.

- Always write tests for new code
- Use table-driven tests
- Follow the existing patterns in the codebase
- Run `make check` before committing
```

If multiple sources provide `instructions.md` (e.g., an included repo and the harness itself), the last source wins. Since the harness's own content is processed last, its `instructions.md` takes priority over included repos.

## Where Artifacts Come From

Artifacts can come from three places:

### 1. Embedded in the harness

Files in the harness's own directory:

```
my-harness/
├── harness.json
├── skills/review/SKILL.md     <- embedded
└── rules/concise.md           <- embedded
```

### 2. External Git repos

Referenced in `harness.json`:

```json
{
  "includes": [
    {"git": "github.com/user/skills-repo", "pick": ["skills/commit"]}
  ]
}
```

### 3. Monorepo subdirectories

```json
{
  "includes": [
    {"git": "github.com/company/monorepo", "path": "packages/ai-config"}
  ]
}
```

All three sources are merged into the same vendor config at runtime. If two sources provide an artifact with the same name, the last one wins.

## Exporting Artifacts

Artifacts can be exported from ynh's harness format into vendor-native plugin layouts using `ynd export`. This resolves all remote includes, flattens the result, and writes distributable output per vendor. See the [export command reference](ynd.md#export) and [Tutorial 10](tutorial/05-export.md) for details.
