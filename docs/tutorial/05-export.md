# Tutorial 5: Export

Produce vendor-native distributable plugins from harnesses. The exported output passes strict vendor validation and can be loaded directly by Claude Code, Cursor, or Codex.

## Prerequisites

```bash
# Clean up from any previous run
rm -rf /tmp/ynh-tutorial

mkdir -p /tmp/ynh-tutorial
```

## T5.1: Create a harness to export

```bash
mkdir -p /tmp/ynh-tutorial/exportable/skills/review
mkdir -p /tmp/ynh-tutorial/exportable/agents

cat > /tmp/ynh-tutorial/exportable/skills/review/SKILL.md << 'EOF'
---
name: review
description: Code review with security and performance focus.
---

Review code for:
1. Security vulnerabilities (OWASP top 10)
2. Performance bottlenecks
3. Error handling gaps
EOF

cat > /tmp/ynh-tutorial/exportable/agents/checker.md << 'EOF'
---
name: checker
description: Automated checks subagent.
---

Run automated checks on the codebase and report results.
EOF

cat > /tmp/ynh-tutorial/exportable/instructions.md << 'EOF'
You are a code quality harness. Focus on correctness and security.
EOF

cat > /tmp/ynh-tutorial/exportable/.harness.json << 'EOF'
{
  "name": "exportable",
  "version": "1.0.0",
  "description": "A harness designed for cross-vendor export",
  "default_vendor": "claude",
  "includes": [
    {
      "git": "github.com/eyelock/assistants",
      "path": "skills/pause",
      "pick": ["skills/take-a-moment"]
    }
  ]
}
EOF
```

## T5.2: Export for all vendors

```bash
ynd export /tmp/ynh-tutorial/exportable -o /tmp/ynh-tutorial/export-output
```

Expected output:
```
Exported for claude → /tmp/ynh-tutorial/export-output/claude (2 skills, 1 agents)
Exported for codex → /tmp/ynh-tutorial/export-output/codex (2 skills, 0 agents)
  warning: codex: skipping 1 agents (not supported)
Exported for cursor → /tmp/ynh-tutorial/export-output/cursor (2 skills, 1 agents)
```

## T5.3: Verify Claude export

```bash
ls -Ra /tmp/ynh-tutorial/export-output/claude/
```

Expected:
```
.claude-plugin/               # plugin manifest directory
  plugin.json                 # generated from .harness.json
agents/
  checker.md                  # local agent
skills/
  review/SKILL.md             # local skill
  take-a-moment/SKILL.md      # picked from remote include
AGENTS.md                     # instructions (cross-vendor format)
CLAUDE.md                     # @AGENTS.md import (Claude reads this)
```

Key points:
- `AGENTS.md` contains the actual instructions (read by Codex, Cursor, Copilot, etc.)
- `CLAUDE.md` contains just `@AGENTS.md` — Claude Code reads this and imports the instructions. No duplication, no conflict with the project's own `CLAUDE.md` (the plugin lives in its own directory).
- No `.claude/` wrapper — artifacts are at the plugin root
- Remote includes are resolved and flattened

### Verify Claude reads the instructions

```bash
cat /tmp/ynh-tutorial/export-output/claude/CLAUDE.md
# Expected: @AGENTS.md

cat /tmp/ynh-tutorial/export-output/claude/AGENTS.md
# Expected: You are a code quality harness. Focus on correctness and security.
```

### Validate the Claude export

```bash
claude plugin validate /tmp/ynh-tutorial/export-output/claude
# Expected: validation passes
```

### Verify the @-import works

The exported `CLAUDE.md` contains `@AGENTS.md` — Claude's import syntax. Verify Claude reads the instructions by launching from the export directory:

```bash
cd /tmp/ynh-tutorial/export-output/claude
git init && git add -A && git commit -m "init"
claude -p "What should you focus on? Answer in one sentence."
```

Expected: Claude should respond mentioning **code quality**, **correctness**, and **security** — the instructions from `AGENTS.md` imported via `CLAUDE.md`'s `@AGENTS.md` reference.

Return to your previous directory before continuing:

```bash
cd -
```

## T5.4: Verify Cursor export

```bash
ls -Ra /tmp/ynh-tutorial/export-output/cursor/
```

Expected (note: `.cursor-plugin` and `.cursorrules` are hidden — `ls -Ra` shows them):
```
.cursor-plugin/
  plugin.json

agents/
  checker.md

skills/
  review/SKILL.md
  take-a-moment/SKILL.md

.cursorrules                  # Cursor-native instructions
AGENTS.md                     # universal instructions
```

Cursor gets both `.cursorrules` (Cursor-native) and `AGENTS.md` (universal). Without `-a`, you'd only see `agents/`, `AGENTS.md`, and `skills/`.

## T5.5: Verify Codex export

```bash
ls -Ra /tmp/ynh-tutorial/export-output/codex/
```

Expected:
```
.codex-plugin/
  plugin.json
skills/
  review/SKILL.md
  take-a-moment/SKILL.md
AGENTS.md
```

Key points:
- `.codex-plugin/plugin.json` — plugin manifest with path pointers for skills and MCP
- Skills go to `skills/` at the plugin root (same as Claude and Cursor)
- Agents, rules, and commands are **excluded** (Codex doesn't support them in plugins)
- `AGENTS.md` only (Codex natively consumes it)

## T5.6: Export for specific vendors

```bash
ynd export /tmp/ynh-tutorial/exportable -o /tmp/ynh-tutorial/export-claude -v claude
ls /tmp/ynh-tutorial/export-claude/
# Expected: only claude/ directory
```

## T5.7: Export in merged mode

Merged mode produces one directory with both Claude and Cursor manifests — useful for CI pipelines and marketplace-ready plugins:

```bash
ynd export /tmp/ynh-tutorial/exportable -o /tmp/ynh-tutorial/export-merged --merged -v claude,cursor
ls -Ra /tmp/ynh-tutorial/export-merged/
```

Expected (note: hidden directories and files shown with `-a`):
```
.claude-plugin/
  plugin.json

.cursor-plugin/
  plugin.json

agents/
  checker.md

skills/
  review/SKILL.md
  take-a-moment/SKILL.md

.cursorrules
AGENTS.md
CLAUDE.md
```

One physical directory with both vendor manifests — serves Claude and Cursor from the same files.

## T5.8: Export with --clean

```bash
# First export
ynd export /tmp/ynh-tutorial/exportable -o /tmp/ynh-tutorial/clean-test

# Second export adds a new vendor dir from a different run
ynd export /tmp/ynh-tutorial/exportable -o /tmp/ynh-tutorial/clean-test -v claude

# Old cursor/ and codex/ dirs still exist from first run
ls /tmp/ynh-tutorial/clean-test/
# Expected: claude/ cursor/ codex/

# --clean removes entire output first
ynd export /tmp/ynh-tutorial/exportable -o /tmp/ynh-tutorial/clean-test -v claude --clean
ls /tmp/ynh-tutorial/clean-test/
# Expected: claude/ only
```

## T5.9: Export from a Git URL

```bash
ynd export github.com/eyelock/assistants --path ynh/david -o /tmp/ynh-tutorial/remote-export -v claude
```

Clones the repo, applies `--path` scoping, exports. Same as exporting a local directory.

## T5.10: Export with no instructions

```bash
mkdir -p /tmp/ynh-tutorial/no-instructions
cat > /tmp/ynh-tutorial/no-instructions/.harness.json << 'EOF'
{"name": "no-instructions", "version": "0.1.0"}
EOF

ynd export /tmp/ynh-tutorial/no-instructions -o /tmp/ynh-tutorial/no-inst-out -v claude
# Expected: succeeds (no warning)

ls -a /tmp/ynh-tutorial/no-inst-out/claude/
# Expected: .claude-plugin/ only (generated from .harness.json, no AGENTS.md)
```

## Clean up

```bash
rm -rf /tmp/ynh-tutorial/export-*
rm -rf /tmp/ynh-tutorial/clean-test
rm -rf /tmp/ynh-tutorial/remote-export
rm -rf /tmp/ynh-tutorial/no-inst-out
```

## What you learned

- `ynd export` produces vendor-native distributable plugins
- Each vendor gets its own layout:
  - Claude: `.claude-plugin/plugin.json` + artifacts at root
  - Cursor: `.cursor-plugin/plugin.json` + `.cursorrules`
  - Codex: `.codex-plugin/plugin.json` + `skills/` (agents, rules, commands excluded)
- `--merged` produces a single dir with dual manifests (marketplace-ready)
- Remote includes are resolved and flattened into the export
- Pick filtering carries through to the export
- `AGENTS.md` is the universal instruction format (read by Codex, Cursor, Copilot, etc.)
- `CLAUDE.md` is generated with `@AGENTS.md` import — Claude reads this, no duplication

## Next

[Tutorial 11: Marketplace](tutorial/06-marketplace.md) — generate marketplace indexes for distribution.
