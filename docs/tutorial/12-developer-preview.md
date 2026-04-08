# Tutorial 12: Developer Preview

Use `ynd preview` and `ynd diff` to inspect assembled harness output without installing. These tools let you iterate on harness design and verify vendor-specific behavior before shipping.

## Prerequisites

```bash
# Clean up from any previous run
rm -rf /tmp/ynh-tutorial
```

Create a harness with multiple artifact types, hooks, and MCP servers:

```bash
mkdir -p /tmp/ynh-tutorial/preview-harness/skills/deploy
mkdir -p /tmp/ynh-tutorial/preview-harness/rules

cat > /tmp/ynh-tutorial/preview-harness/skills/deploy/SKILL.md << 'EOF'
---
name: deploy
description: Deploy application to staging or production
---

## Deploy Workflow

1. Run tests
2. Build artifacts
3. Push to registry
4. Deploy to target environment
EOF

cat > /tmp/ynh-tutorial/preview-harness/rules/safety.md << 'EOF'
---
name: safety
description: Production safety rules
---

Never deploy to production without running the test suite first.
Always create a rollback plan before deploying.
EOF

cat > /tmp/ynh-tutorial/preview-harness/harness.json << 'EOF'
{
  "name": "preview-demo",
  "version": "0.1.0",
  "default_vendor": "claude",
  "hooks": {
    "before_tool": [
      {
        "matcher": "Bash",
        "command": "/usr/local/bin/check-deploy.sh"
      }
    ],
    "after_tool": [
      {
        "command": "/usr/local/bin/validate-output.sh"
      }
    ]
  },
  "mcp_servers": {
    "sqlite": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-sqlite", "/tmp/status.db"]
    }
  }
}
EOF

cat > /tmp/ynh-tutorial/preview-harness/instructions.md << 'EOF'
You are a DevOps assistant. Use the deployment skill for releases and query the database for status checks.
EOF
```

## T12.1: Preview a harness for Claude

Preview shows the fully assembled vendor-native output as a tree with file contents:

```bash
ynd preview /tmp/ynh-tutorial/preview-harness -v claude
```

Expected output structure:

```
.claude/
  .claude-plugin/
    plugin.json
  rules/
    safety.md
  skills/
    deploy/
      SKILL.md
  settings.json
.mcp.json
CLAUDE.md
```

Key things to verify:
- `CLAUDE.md` contains the harness instructions
- `.claude/skills/deploy/SKILL.md` has the skill content
- `.claude/rules/safety.md` has the rule content
- `.claude/settings.json` has hooks in Claude's three-level format
- `.mcp.json` has the MCP server config

## T12.2: Preview the same harness for Cursor

```bash
ynd preview /tmp/ynh-tutorial/preview-harness -v cursor
```

Expected output structure:

```
.cursor/
  .cursor-plugin/
    plugin.json
  hooks.json
  mcp.json
  rules/
    safety.md
  skills/
    deploy/
      SKILL.md
.cursorrules
```

Note the differences from Claude:
- Instructions go to `.cursorrules` instead of `CLAUDE.md`
- Hooks go to `.cursor/hooks.json` instead of `.claude/settings.json`
- MCP config goes to `.cursor/mcp.json` instead of `.mcp.json`
- Artifacts are under `.cursor/` instead of `.claude/`

## T12.3: Compare Claude vs Cursor output

```bash
ynd diff /tmp/ynh-tutorial/preview-harness claude cursor
```

Expected output:

```
=== claude vs cursor ===
Only in claude:
  .claude/settings.json
  .mcp.json
  CLAUDE.md
Only in cursor:
  .cursor/hooks.json
  .cursor/mcp.json
  .cursorrules
Different content:
  ...
Identical:
  ...
```

The diff shows which files are vendor-specific and which content is shared. Artifacts like skills and rules may appear as identical content under different directory prefixes.

## T12.4: Preview a harness with hooks

Write the preview to a directory for closer inspection:

```bash
ynd preview /tmp/ynh-tutorial/preview-harness -v claude -o /tmp/ynh-tutorial/claude-output
```

Inspect the hook config:

```bash
cat /tmp/ynh-tutorial/claude-output/.claude/settings.json
```

Expected:

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "hooks": [
          { "type": "command", "command": "/usr/local/bin/validate-output.sh" }
        ]
      }
    ],
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          { "type": "command", "command": "/usr/local/bin/check-deploy.sh" }
        ]
      }
    ]
  }
}
```

Now compare with Cursor's hook format:

```bash
ynd preview /tmp/ynh-tutorial/preview-harness -v cursor -o /tmp/ynh-tutorial/cursor-output
cat /tmp/ynh-tutorial/cursor-output/.cursor/hooks.json
```

Expected:

```json
{
  "version": 1,
  "hooks": {
    "afterFileEdit": [
      { "command": "/usr/local/bin/validate-output.sh" }
    ],
    "beforeShellExecution": [
      { "command": "/usr/local/bin/check-deploy.sh" }
    ]
  }
}
```

## T12.5: Preview a harness with MCP servers

Inspect MCP config for each vendor:

```bash
cat /tmp/ynh-tutorial/claude-output/.mcp.json
```

Expected:

```json
{
  "mcpServers": {
    "sqlite": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-sqlite", "/tmp/status.db"]
    }
  }
}
```

```bash
cat /tmp/ynh-tutorial/cursor-output/.cursor/mcp.json
```

Expected: same JSON structure as Claude, different file location.

Preview Codex to see its MCP format:

```bash
ynd preview /tmp/ynh-tutorial/preview-harness -v codex -o /tmp/ynh-tutorial/codex-output
cat /tmp/ynh-tutorial/codex-output/.mcp.json
```

Expected (JSON, same format as Claude — at plugin root):

```json
{
  "mcpServers": {
    "sqlite": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-sqlite", "/tmp/status.db"]
    }
  }
}
```

## Clean up

```bash
rm -rf /tmp/ynh-tutorial
```

## What You Learned

- `ynd preview` assembles a harness for a specific vendor and shows the output without installing
- `ynd preview -o <dir>` writes the output to a directory for closer inspection
- `ynd diff` compares assembled output across two or more vendors
- Each vendor places hooks, MCP config, instructions, and artifacts in different locations and formats
- Preview and diff are the primary tools for iterating on harness design before publishing

## Next

[Tutorial 9: Delegation](tutorial/04-delegation.md) — chain harnesses together as subagents.
