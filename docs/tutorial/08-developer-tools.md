# Tutorial 8: Developer Tools

Use ynd to scaffold, lint, validate, format, compress, and inspect harness artifacts.

## Prerequisites

```bash
# Clean up from any previous run
rm -rf /tmp/ynh-tutorial

mkdir -p /tmp/ynh-tutorial && cd /tmp/ynh-tutorial
```

## T8.1: Scaffold a harness

```bash
ynd create harness my-team
```

Expected: a `my-team/` directory with the full harness structure:

```bash
find my-team -type f | sort
# Expected:
#   my-team/.harness.json
#   my-team/AGENTS.md
```

Empty directories are also created: `skills/`, `agents/`, `rules/`, `commands/`.

### Error cases

```bash
ynd create harness my-team    # expect: "already exists"
ynd create harness ""         # expect: invalid name
```

## T8.2: Scaffold artifacts

```bash
cd my-team

ynd create skill code-review
ynd create agent security-reviewer
ynd create rule always-explain
ynd create command pre-commit
```

Verify each:

```bash
cat skills/code-review/SKILL.md    # frontmatter with name, description
cat agents/security-reviewer.md    # frontmatter with name, description, tools
cat rules/always-explain.md        # placeholder text
cat commands/pre-commit.md         # heading + bash block
```

## T8.3: Author content

Replace scaffolded files with real content:

```bash
cat > skills/code-review/SKILL.md << 'EOF'
---
name: code-review
description: Perform thorough code reviews with security and performance focus.
---

## When to use

Use when reviewing pull requests or code changes.

## Steps

1. Check for OWASP top 10 vulnerabilities
2. Look for performance bottlenecks
3. Verify error handling patterns
4. Check test coverage gaps

Provide specific, actionable feedback with file paths and line numbers.
EOF

cat > agents/security-reviewer.md << 'EOF'
---
name: security-reviewer
description: Specialized agent for security-focused code review.
tools: Read, Grep, Glob, Bash
---

You are a security specialist. When delegated to:

- Scan for hardcoded credentials and secrets
- Check authentication and authorization patterns
- Review input validation and sanitization
- Identify injection vulnerabilities

Report findings with severity levels and remediation steps.
EOF
```

## T8.4: Lint

```bash
ynd lint
# Expected: "Checked N file(s) — no issues found."
```

Introduce a problem:

```bash
printf "no trailing newline\ntrailing spaces   " > rules/dirty.md
ynd lint
# Expected: reports trailing whitespace and missing newline
rm rules/dirty.md
```

Target a specific file:

```bash
ynd lint skills/code-review/SKILL.md
# Expected: "Checked 1 file(s) — no issues found."
```

## T8.5: Validate

```bash
ynd validate
# Expected: ".: valid"
```

Break something:

```bash
mkdir -p skills/orphan
ynd validate
# Expected: INVALID, "skills/orphan/ missing SKILL.md"
rmdir skills/orphan
```

## T8.6: Format

```bash
printf '# Messy File   \n\nThis has trailing spaces.   \n\n\nMultiple blank lines above.\n' > rules/messy.md

ynd fmt
# Expected:
#   Formatted rules/messy.md
#   Formatted 1 of 6 file(s).

ynd fmt
# Expected: "Checked 6 file(s) — all formatted." (idempotent — no-op on second run)
```

## T8.7: Compress

Requires an LLM CLI on PATH (`claude`, `codex`, or `agent`). Uses LLM-powered SudoLang techniques to reduce prompt size while preserving semantics.

```bash
# Compress with auto-apply
ynd compress -y skills/code-review/SKILL.md
# Expected: shows char reduction, creates backup in ~/.ynd/backups/

# Verify frontmatter survives compression
ynd validate
# Expected: ".: valid"
```

### Backup management

```bash
# List backups
ynd compress --list-backups skills/code-review/SKILL.md
# Expected: numbered list with timestamps

# Restore from latest backup
ynd compress --restore skills/code-review/SKILL.md
# Expected: "Restored ... from backup <timestamp>"

# Compress again to create a second backup
ynd compress -y skills/code-review/SKILL.md

# List shows two backups
ynd compress --list-backups skills/code-review/SKILL.md
# Expected: 2 entries

# Restore a specific backup by number
ynd compress --restore --pick 2 skills/code-review/SKILL.md
# Expected: restores the older backup
```

## T8.8: Inspect

Requires an LLM CLI on PATH (`claude`, `codex`, or `agent`). Interactive codebase walkthrough that generates skills and agents from project analysis.

```bash
# Set up a project to inspect
cd /tmp/ynh-tutorial
git init test-project && cd test-project
echo "module example.com/test" > go.mod
echo "package main" > main.go
git add -A && git commit -m "init"

# Run inspect — artifacts go to .claude/ by default
ynd inspect -y
# Expected: analyzes project, proposes and generates skills/agents
ls -R .claude/skills/ .claude/agents/ 2>/dev/null
# Expected: generated SKILL.md and agent files

# Clean up for next test
rm -rf .claude/skills .claude/agents

# Override output directory
ynd inspect -y -o .
# Expected: artifacts in skills/ and agents/ at project root
rm -rf skills agents
```

### Vendor-specific output

```bash
ynd inspect -y -v cursor
ls -R .cursor/skills/ 2>/dev/null
# Expected: artifacts in .cursor/skills/
```

## Clean up

```bash
cd /
rm -rf /tmp/ynh-tutorial
```

## What you learned

- `ynd create` scaffolds harnesses and artifacts with correct structure
- `ynd lint` catches formatting issues (trailing whitespace, missing newlines)
- `ynd validate` checks structural correctness (SKILL.md exists in skill dirs, valid frontmatter)
- `ynd fmt` auto-formats markdown files (idempotent)
- `ynd compress` uses LLM to reduce prompt size, with backup/restore
- `ynd inspect` analyzes a project and generates skills/agents
- All ynd commands work on the current directory by default, or target specific files

## Complete

[Tutorial 8: Developer Preview](tutorial/12-developer-preview.md) — preview and diff assembled output across vendors.
