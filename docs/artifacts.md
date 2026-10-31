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
| `evals/` | Authoring-time test cases and fixtures. Not used at runtime, so ynh leaves it out when assembling and exporting |

Only an `evals/` directly inside a skill (`skills/<name>/evals/`) is skipped; every other file of the skill is copied, and an `evals/` nested deeper is kept.

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

Under Codex, ynh writes each agent as a TOML custom agent, `.codex/agents/<name>.toml`, because that is the only form Codex reads. Three fields always survive: `name` and `description` from the frontmatter, and the body as `developer_instructions`. An `effort` of `low`, `medium` or `high` becomes `model_reasoning_effort` with the same level, as [`ynh agent run --effort`](agent.md#effort) maps it for Codex; Claude Code's `xhigh` and `max` are dropped, since ynh maps only those three. Every other field is dropped, including `tools`, `disallowedTools` and `model`, since Codex has no tool allowlist and Claude Code's model names are not Codex models. A Codex custom agent runs with the session's permissions, so the read-only reviewer above is not read-only there: it can do whatever the session allows. See [Vendors: Symlink Installation](vendors.md#symlink-installation-codex-cursor) for the details.

Refer to your vendor's documentation for agent format details:
- [Claude Code sub-agents](https://docs.anthropic.com/en/docs/claude-code/sub-agents)
- [Codex subagents](https://learn.chatgpt.com/docs/agent-configuration/subagents)

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

A rule may open with YAML frontmatter. Claude Code reads `paths` to scope a
rule to matching files. Cursor ignores plain `.md` files in `.cursor/rules/`, so
for Cursor ynh renders each rule as `<name>.mdc` with a single frontmatter
block holding the three fields [Cursor reads](https://cursor.com/docs/context/rules),
merged from the rule's own:

| Cursor field | Taken from |
|---|---|
| `description` | the rule's `description`, else its file name (`always-test` becomes `Always Test`) |
| `globs` | the rule's `globs`, else its `paths`, comma-joined; omitted when it has neither |
| `alwaysApply` | the rule's `alwaysApply` when it is `true` or `false`; otherwise `false` when there are globs (Cursor ignores globs on an always-on rule) and `true` when there are none |

Any other field, such as `name`, is dropped from the `.mdc`, because Cursor does
not read it. A file whose opening `---` is never closed has no frontmatter and
is kept whole as the rule's body.

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

A harness can include an `AGENTS.md` file with project-level instructions.

**The two paths differ, and the difference matters if you go looking for the
file.** `ynd export` produces a redistributable plugin, so it emits `AGENTS.md`
alongside a vendor instructions file that imports it. `ynh run` and
`ynd preview` assemble a session, so they copy the instructions straight into
the vendor's own file — there is no `AGENTS.md` to import, and no import.

| Vendor | Reads `AGENTS.md` natively | `ynh run` / `ynd preview` | `ynd export` |
|---|---|---|---|
| Claude | No | instructions inlined into `CLAUDE.md` | `AGENTS.md` + `CLAUDE.md` containing `@AGENTS.md` |
| Codex | Yes | `codex.md` | `AGENTS.md` |
| Cursor | Yes | `.cursorrules` | `AGENTS.md` |
| Copilot | Yes | `AGENTS.md` | `AGENTS.md` |

Verified by running both commands per vendor, not read from the adapters.

So a reader who follows the export description and then inspects a *previewed*
harness will look for an import that is not there. Check which command produced
the tree before concluding something is wrong.

```
my-harness/
├── .agents/harness/plugin.json
└── AGENTS.md             <- read natively by Codex/Cursor; shimmed for Claude
```

**Example AGENTS.md:**

```markdown
You are a senior developer working on a Go microservices codebase.

- Always write tests for new code
- Use table-driven tests
- Follow the existing patterns in the codebase
- Run `make check` before committing
```

When several sources provide instructions, they are composed rather than overwritten. The harness's own instructions come first, then one section per include that has instructions, in include order, each headed `## Instructions from <source>` (the include's harness name when it has a manifest; otherwise `owner/repo` for a git include, and for a local one its path as written when relative or its directory name when absolute; `path` is appended when set; the heading never carries an absolute filesystem path). Content identical to a section already present appears once, so an include reached twice contributes one section. If the harness has no instructions of its own, the result is just the include sections.

An include that is only wanted for its skills can leave its instructions out with `"instructions": false` (see [Includes](harnesses.md#includes-optional)). Composition applies to `ynh run`, `ynd preview`, `ynd export` and `ynh image`.

> **Note:** `instructions.md` is also supported and takes precedence over `AGENTS.md` if both exist. New harnesses should use `AGENTS.md`.

## Where Artifacts Come From

Artifacts can come from three places:

### 1. Embedded in the harness

Files in the harness's own directory:

```
my-harness/
├── .agents/harness/plugin.json
├── skills/review/SKILL.md     <- embedded
└── rules/concise.md           <- embedded
```

### 2. External Git repos

Referenced in `.agents/harness/plugin.json`:

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

Artifacts can be exported from ynh's harness format into vendor-native plugin layouts using `ynd export`. This resolves all remote includes, flattens the result, and writes distributable output per vendor. See the [export command reference](ynd.md#export) and [Export](tutorial/export.md) for details.
