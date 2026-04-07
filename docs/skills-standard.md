# Agent Skills Standard

ynh harnesses use the [Agent Skills](https://agentskills.io) open standard for packaging skills and agents. This page covers the standard, cross-platform compatibility, and practical guidance for harness authors.

## The Standard

Agent Skills is an open specification originally developed by Anthropic for Claude Code, now adopted by 30+ tools including Cursor, OpenAI Codex, GitHub Copilot, VS Code, Gemini CLI, JetBrains Junie, and others.

- **Spec:** [agentskills.io/specification](https://agentskills.io/specification)
- **GitHub:** [github.com/agentskills/agentskills](https://github.com/agentskills/agentskills)

## Skill Structure

Every skill follows the same layout across all platforms:

```
skill-name/
├── SKILL.md          # Required: YAML frontmatter + markdown instructions
├── scripts/          # Optional: executable code (any language)
├── references/       # Optional: additional documentation
└── assets/           # Optional: templates, images, data files
```

## SKILL.md Frontmatter

### Required Fields

| Field | Constraints | Notes |
|-------|-------------|-------|
| `name` | 1–64 chars, lowercase `a-z`, `0-9`, hyphens | Must match parent directory name |
| `description` | 1–1024 chars | Used by the agent to decide when to invoke the skill |

### Optional Fields (Agent Skills 1.0 Spec)

| Field | In Spec | Claude Code Docs | Notes |
|-------|---------|-----------------|-------|
| `license` | Yes | No | License name or reference to bundled file |
| `compatibility` | Yes | No | Environment requirements (OS, packages, network). Max 500 chars. **See known issues below.** |
| `metadata` | Yes | No | Key-value map — commonly `author`, `version` |
| `allowed-tools` | Yes (experimental) | Yes | Space-delimited list of tools the agent can use without prompting |

Note that Claude Code's documentation omits `license`, `compatibility`, and `metadata` despite these being spec-standard fields. See [Known Issues](#known-issues) for practical implications.

### Claude Code Extensions

Claude Code adds these optional fields (not part of the Agent Skills spec):

| Field | Default | Effect |
|-------|---------|--------|
| `disable-model-invocation` | `false` | When `true`, only user can invoke via `/name` — removed from agent catalog |
| `user-invocable` | `true` | When `false`, hidden from `/` menu — only the agent auto-invokes |
| `model` | inherited | Override which model executes this skill |
| `context` | inline | Set to `fork` to run in an isolated subagent |
| `agent` | `general-purpose` | Subagent type when `context: fork` (e.g., `Explore`, `Plan`) |
| `argument-hint` | — | Autocomplete hint, e.g. `[issue-number]` |

**Reference:** [Claude Code skills docs](https://code.claude.com/docs/en/skills#frontmatter-reference)

### OpenAI Codex Extensions

Codex uses a separate `agents/openai.yaml` file (not frontmatter) for UI metadata, branding, and dependency declarations.

**Reference:** [Codex skill creation](https://developers.openai.com/codex/skills/create-skill)

## Body Content Guidelines

| Guideline | Recommendation | Source |
|-----------|---------------|--------|
| Max lines | 500 | All platforms |
| Max tokens | 5,000 | Agent Skills spec |
| Format | Markdown, imperative steps | All platforms |
| Reference material | Move to `references/` subdirectory | All platforms |

Write clear, actionable steps. Reference supporting files from SKILL.md so the agent knows they exist. Use `ynd compress` to reduce token usage on verbose skills.

## Progressive Disclosure

All three major platforms implement three-tier loading:

1. **Catalog** — skill name + description only (~50–100 tokens per skill). Always in context.
2. **Instructions** — full SKILL.md body (< 5,000 tokens recommended). Loaded when the agent decides the skill is relevant or the user invokes `/skill-name`.
3. **Resources** — `scripts/`, `references/`, `assets/`. Loaded on demand when instructions reference them.

This means your `description` field is critical — it's the only thing the agent sees when deciding whether to use a skill.

## Catalog Budget

### Claude Code

Claude Code allocates **2% of the context window** for the skill catalog.

| Context Size | Approximate Budget | Skills Supported |
|-------------|-------------------|-----------------|
| 200K tokens | ~4,000 tokens | ~53 skills |
| 1M tokens | ~20,000 tokens | ~260 skills |

When the budget is exhausted, remaining skills are silently excluded. The agent cannot discover or auto-invoke excluded skills, though `/name` invocation still works.

**Practical guidance:**
- Keep descriptions under 130 characters for large skill collections (60+)
- Use `disable-model-invocation: true` on manual-only skills to save catalog budget
- Monitor with `/context` (shows skills token count) and `/skills` (shows per-skill counts)

**Override:** Set `SLASH_COMMAND_TOOL_CHAR_BUDGET=N` to override the budget.

### Cursor / Codex

No documented catalog budget limits.

## Discovery Locations

Where each platform looks for skills:

### Claude Code

| Scope | Path |
|-------|------|
| Enterprise | Managed settings (admin-deployed) |
| Harnessl | `~/.claude/skills/<name>/SKILL.md` |
| Project | `.claude/skills/<name>/SKILL.md` |
| Plugin | `<plugin>/skills/<name>/SKILL.md` (namespaced as `plugin:skill`) |
| Nested | `<subdir>/.claude/skills/` (monorepo support) |

### Cursor

| Scope | Path |
|-------|------|
| Project | `.agents/skills/`, `.cursor/skills/` |
| User | `~/.cursor/skills/` |
| Compat | `.claude/skills/`, `.codex/skills/`, `~/.claude/skills/`, `~/.codex/skills/` |

### OpenAI Codex

| Scope | Path |
|-------|------|
| Repo (CWD upward) | `.agents/skills/` in current and parent directories |
| User | `$HOME/.agents/skills/` |
| Admin | `/etc/codex/skills/` |

## Invocation Control

| Frontmatter | User can invoke | Agent can invoke | In catalog |
|-------------|-----------------|------------------|-----------|
| (default) | Yes | Yes | Yes |
| `disable-model-invocation: true` | Yes | No | No |
| `user-invocable: false` | No | Yes | Yes |

Use `disable-model-invocation: true` for actions with side effects (deploy, push, commit) — saves catalog budget and prevents accidental auto-invocation.

Use `user-invocable: false` for background knowledge the agent should auto-apply without cluttering the `/` menu.

## Best Practices for Description Writing

- **Do:** State what the skill does AND when to use it
- **Do:** Include keywords matching how users naturally describe the task
- **Don't:** Explain how the skill works internally (that belongs in the body)
- **Don't:** Pad with synonyms hoping to match more queries
- **Target:** Under 130 chars for large collections, under 200 for small ones

## ynh and the Standard

ynh harnesses are fully compatible with the Agent Skills standard:

- Skills use the standard `skills/<name>/SKILL.md` layout with `scripts/`, `references/`, and `assets/` subdirectories
- Agents use markdown files with YAML frontmatter (`name`, `description`, `tools`)
- `ynd lint` validates required fields without rejecting optional spec or platform-specific fields
- `ynd inspect` generates skills and agents that follow the standard
- `ynd compress` helps keep skill bodies within the recommended 5,000 token limit

The vendor adapter in ynh handles placing artifacts in the correct discovery location for each platform (`.claude/`, `.cursor/`, `.codex/`).

## Diagnostics

| Platform | Command | Purpose |
|----------|---------|---------|
| Claude Code | `/skills` | List all discovered skills with per-skill token counts |
| Claude Code | `/context` | Show total skills budget usage |
| Claude Code | `/reload-plugins` | Reload plugin skills without restarting |
| Cursor | Settings (Cmd+Shift+J) → Rules | Shows discovered skills |
| Codex | `list skills` | List available skills |

## Known Issues

### Claude Code plugin skills demoted by spec-standard fields

Claude Code's plugin skill loader does not correctly handle some Agent Skills 1.0 spec fields. When a plugin skill includes the `compatibility` field (and possibly `license` or `metadata`), the skill is demoted: it receives minimal token allocation (~10 tokens), is namespaced differently, and is excluded from the agent's active context.

**Affected:** Plugin skills only (loaded via `.claude-plugin/`). Standalone skills in `.claude/skills/` appear unaffected.

**Workaround:** Do not use `compatibility`, `license`, or `metadata` frontmatter fields in skills distributed as ynh harnesses (which use the plugin format). Stick to `name`, `description`, and Claude Code extension fields.

**Impact on ynh:** Since ynh harnesses are loaded as Claude Code plugins via `--plugin-dir`, this affects all ynh-distributed skills. The `ynd create skill` and `ynd inspect` commands already generate skills with only `name` and `description`, so scaffolded skills are safe. If you add spec-standard optional fields manually, they may cause the skill to be demoted.

| Field | Spec Status | Safe in Plugins? |
|-------|-------------|-----------------|
| `name` | Required | Yes |
| `description` | Required | Yes |
| `license` | Optional | Avoid — may cause demotion |
| `compatibility` | Optional | Avoid — confirmed to cause demotion |
| `metadata` | Optional | Avoid — may cause demotion |
| `allowed-tools` | Experimental | Yes (documented by Claude Code) |
| Claude extensions (`disable-model-invocation`, etc.) | N/A | Yes |

This is a bug in Claude Code — it authored the spec but its plugin loader doesn't handle spec-defined fields correctly. Skills with these fields work fine in Cursor and Codex.

## References

- Agent Skills spec: [agentskills.io](https://agentskills.io)
- Claude Code skills: [code.claude.com/docs/en/skills](https://code.claude.com/docs/en/skills)
- Claude Code plugins: [code.claude.com/docs/en/plugins](https://code.claude.com/docs/en/plugins)
- Cursor skills: [cursor.com/docs/skills](https://cursor.com/docs/skills)
- Codex skills: [developers.openai.com/codex/skills](https://developers.openai.com/codex/skills/)
- Catalog budget research: [GitHub issue #13100](https://github.com/anthropics/claude-code/issues/13100)
