# Tutorial 10: Hooks

Declare vendor-agnostic hooks in a harness and preview the assembled output for each vendor. Hooks bridge the guide layer to feedback sensors like linters and validators.

## Prerequisites

```bash
# Clean up from any previous run
rm -rf /tmp/ynh-tutorial
```

## T10.1: Add hooks to a harness

Create a harness with hook declarations in `.harness.json`:

```bash
mkdir -p /tmp/ynh-tutorial/hook-harness/rules

cat > /tmp/ynh-tutorial/hook-harness/.harness.json << 'EOF'
{
  "name": "hook-demo",
  "version": "0.1.0",
  "default_vendor": "claude",
  "hooks": {
    "before_tool": [
      {
        "matcher": "Bash",
        "command": "/usr/local/bin/check-commands.sh"
      }
    ],
    "after_tool": [
      {
        "command": "/usr/local/bin/run-linter.sh"
      }
    ],
    "on_stop": [
      {
        "command": "/usr/local/bin/session-report.sh"
      }
    ]
  }
}
EOF

cat > /tmp/ynh-tutorial/hook-harness/instructions.md << 'EOF'
You are a careful coding assistant. All changes are validated by hooks.
EOF

cat > /tmp/ynh-tutorial/hook-harness/rules/safety.md << 'EOF'
---
name: safety
description: Safety-first coding
---

Never run destructive commands without confirmation.
EOF
```

Verify the structure:

```bash
ls -R /tmp/ynh-tutorial/hook-harness/
# Expected: .harness.json, instructions.md, rules/safety.md
```

## T10.2: Preview for Claude

```bash
ynd preview /tmp/ynh-tutorial/hook-harness -v claude
```

Expected output includes `.claude/hooks/hooks.json` with Claude's three-level hook structure:

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "hooks": [
          { "type": "command", "command": "/usr/local/bin/run-linter.sh" }
        ]
      }
    ],
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          { "type": "command", "command": "/usr/local/bin/check-commands.sh" }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          { "type": "command", "command": "/usr/local/bin/session-report.sh" }
        ]
      }
    ]
  }
}
```

Note: Claude groups hooks under matcher objects and wraps each command in `{"type": "command", ...}`.

## T10.3: Preview for Cursor

```bash
ynd preview /tmp/ynh-tutorial/hook-harness -v cursor
```

Expected output includes `.cursor/hooks.json` with Cursor's format:

```json
{
  "hooks": {
    "afterFileEdit": [
      { "command": "/usr/local/bin/run-linter.sh" }
    ],
    "beforeShellExecution": [
      { "command": "/usr/local/bin/check-commands.sh" }
    ],
    "stop": [
      { "command": "/usr/local/bin/session-report.sh" }
    ]
  },
  "version": 1
}
```

Note: Cursor uses different event names (`beforeShellExecution` / `afterFileEdit` vs `PreToolUse` / `PostToolUse`), includes a `version: 1` key, and a flat structure without matchers or type wrappers.

## T10.4: Preview for Codex

```bash
ynd preview /tmp/ynh-tutorial/hook-harness -v codex
```

Expected output includes `.codex/hooks.json` with Codex's three-level format (same as Claude):

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          { "type": "command", "command": "/usr/local/bin/check-commands.sh" }
        ]
      }
    ],
    "PostToolUse": [
      {
        "hooks": [
          { "type": "command", "command": "/usr/local/bin/run-linter.sh" }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          { "type": "command", "command": "/usr/local/bin/session-report.sh" }
        ]
      }
    ]
  }
}
```

Note: Codex uses the same event names and three-level nesting as Claude — matcher objects wrapping hook arrays with `{"type": "command", ...}` entries.

## T10.5: Write a blocking hook example

Create a hook script that blocks destructive git operations with agent-legible remediation:

```bash
cat > /tmp/ynh-tutorial/check-commands.sh << 'SCRIPT'
#!/bin/bash
# Block destructive git operations
# Exit code 2 = block the action across all vendors
if echo "$@" | grep -qE 'git (push --force|reset --hard|clean -fd)'; then
  cat << 'MSG'
BLOCKED: Destructive git operation detected.
Remediation: Use safer alternatives:
  - Instead of "git push --force", use "git push --force-with-lease"
  - Instead of "git reset --hard", use "git stash" to preserve changes
  - Instead of "git clean -fd", review files first with "git clean -fdn"
MSG
  exit 2
fi
exit 0
SCRIPT
chmod +x /tmp/ynh-tutorial/check-commands.sh
```

Test the script:

```bash
/tmp/ynh-tutorial/check-commands.sh "git push --force origin main"
echo "Exit code: $?"
# Expected: BLOCKED message, Exit code: 2

/tmp/ynh-tutorial/check-commands.sh "git push origin main"
echo "Exit code: $?"
# Expected: no output, Exit code: 0
```

## T10.6: Compare hook config across vendors

Use `ynd diff` to see how the same hooks translate differently:

```bash
ynd diff /tmp/ynh-tutorial/hook-harness claude cursor codex
```

Expected output shows:
- `.claude/hooks/hooks.json` is only in Claude
- `.cursor/hooks.json` is only in Cursor
- `.codex/hooks.json` is only in Codex
- Shared files (like `CLAUDE.md`, `.cursorrules`, `codex.md`) may be listed as identical or different depending on instructions content

The key difference: the same three hooks declared once in `.harness.json` produce three structurally different config files, each native to the vendor.

## Clean up

```bash
rm -rf /tmp/ynh-tutorial
```

## What You Learned

- Hooks are declared in `.harness.json` under `hooks` using canonical event names
- `ynd preview` shows the assembled vendor-native output without installing
- Claude, Cursor, and Codex each use different event names and nesting structures
- Hook scripts should exit with code 2 to block actions and include remediation instructions
- `ynd diff` compares the assembled output across vendors side by side

## Next

[Tutorial 5: MCP Servers](tutorial/11-mcp-servers.md) — declare tool dependencies that vendors load automatically.
