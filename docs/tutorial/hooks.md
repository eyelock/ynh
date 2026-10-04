# Hooks

Declare vendor-agnostic hooks in a harness and preview the assembled output for each vendor. Hooks bridge the guide layer to feedback sensors like linters and validators.

## Prerequisites

```bash
# Clean up from any previous run
rm -rf /tmp/ynh-tutorial
```

## Add hooks to a harness

Create a harness with hook declarations in `.agents/harness/plugin.json`:

```bash
mkdir -p /tmp/ynh-tutorial/hook-harness/rules

mkdir -p /tmp/ynh-tutorial/hook-harness/.agents/harness
cat > /tmp/ynh-tutorial/hook-harness/.agents/harness/plugin.json << 'EOF'
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
ls -aR /tmp/ynh-tutorial/hook-harness/ | grep -v '^\.\{1,2\}$\|^\.:'
# Expected lines include: .agents, harness, instructions.md, rules, safety.md
```

## Preview for Claude

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

## Preview for Cursor

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

`.cursor/hooks.json` is the only hook file in the Cursor preview, because it is the one project path Cursor reads hooks from. A Cursor plugin reads its hooks from a different file, which `ynd export` writes instead (see [Export hooks in a plugin](#export-hooks-in-a-plugin)). Check it:

```bash
ynd preview /tmp/ynh-tutorial/hook-harness -v cursor -o /tmp/ynh-tutorial/hook-harness-cursor
find /tmp/ynh-tutorial/hook-harness-cursor -name hooks.json
```

Expected: exactly one line, ending in `.cursor/hooks.json`.

## Preview for Codex

```bash
ynd preview /tmp/ynh-tutorial/hook-harness -v codex
```

Expected output includes `.codex/hooks.json` with Codex's three-level format (same as Claude):

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

Note: Codex uses the same event names and three-level nesting as Claude — matcher objects wrapping hook arrays with `{"type": "command", ...}` entries.

## Export hooks in a plugin

A preview is a session layout. An exported plugin carries hooks where each vendor's plugin loader reads them: `hooks/<vendor>.json` at the plugin root, named by the `"hooks"` field of that vendor's manifest.

```bash
ynd export /tmp/ynh-tutorial/hook-harness -v claude,cursor,codex -o /tmp/ynh-tutorial/hook-export
find /tmp/ynh-tutorial/hook-export -path '*hooks*' -type f | sort
cat /tmp/ynh-tutorial/hook-export/claude/.claude-plugin/plugin.json
```

Expected: one `Exported for` line per vendor, plus `warning: codex: skipping 1 rules (not supported)` because a Codex plugin carries only skills. Then exactly three hook files, one per vendor, and no `hooks/hooks.json` or session path such as `.claude/hooks/hooks.json`:

```
/tmp/ynh-tutorial/hook-export/claude/hooks/claude.json
/tmp/ynh-tutorial/hook-export/codex/hooks/codex.json
/tmp/ynh-tutorial/hook-export/cursor/hooks/cursor.json
```

The Claude manifest names its file:

```json
{
  "name": "hook-demo",
  "version": "0.1.0",
  "hooks": "./hooks/claude.json"
}
```

A merged export (`--merged`, and every `ynd marketplace build` package) puts all three files in one plugin root, and each manifest still names only its own, so no vendor reads another's format. See [Hooks: Config File Locations](../hooks.md#config-file-locations) for why there is no shared `hooks/hooks.json`.

## Write a blocking hook example

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

## Compare hook config across vendors

Use `ynd diff` to see how the same hooks translate differently:

```bash
ynd diff /tmp/ynh-tutorial/hook-harness claude cursor codex
```

Expected output shows:
- `.claude/hooks/hooks.json` is only in Claude
- `.cursor/hooks.json` is only in Cursor
- `.codex/hooks.json` is only in Codex
- Shared files (like `CLAUDE.md`, `.cursorrules`, `codex.md`) may be listed as identical or different depending on instructions content

The key difference: the same three hooks declared once in `.agents/harness/plugin.json` produce three structurally different config files, each native to the vendor.

## Edit hooks from the command line

Hooks can also be added and removed from the CLI — useful for scripted setup and for GUI consumers. The CLI distinguishes harness-level (default) hooks from profile-level overrides:

```bash
# Top-level hooks. The new hook is appended after the existing Bash hook (index 0),
# so it lands at index 1, and that is the one to remove.
ynh hook add /tmp/ynh-tutorial/hook-harness before_tool "echo guard" --matcher Write
# Added hook (event before_tool)
ynh hook remove /tmp/ynh-tutorial/hook-harness before_tool 1
# Removed hook 1 (event before_tool)

# Profile-level hooks. The harness has no profile yet, so add one first.
ynh profile add /tmp/ynh-tutorial/hook-harness ci
# Added profile "ci"
ynh profile hook add /tmp/ynh-tutorial/hook-harness ci after_tool "echo done"
# Added hook to profile "ci" (event after_tool)
ynh profile hook remove /tmp/ynh-tutorial/hook-harness ci after_tool 0
# Removed hook 0 from profile "ci" (event after_tool)
ynh profile remove /tmp/ynh-tutorial/hook-harness ci
# Removed profile "ci"
```

The first positional argument accepts either a filesystem path (during authoring) or a canonical harness id (`local/<name>`, `github.com/<org>/<repo>/<name>`) once installed.

`<event>` is validated against the canonical set (`before_tool`, `after_tool`, `before_prompt`, `on_stop`, `on_session_start`). `<index>` is zero-based; removing the last entry for an event drops the event key entirely. See [hooks.md §"CLI Editing"](../hooks.md#cli-editing) and [reference.md](../reference.md) for the full flag matrix.

## Clean up

```bash
rm -rf /tmp/ynh-tutorial
```

## What You Learned

- Hooks are declared in `.agents/harness/plugin.json` under `hooks` using canonical event names
- `ynd preview` shows the assembled vendor-native output without installing
- Claude, Cursor, and Codex each use different event names and nesting structures
- Hook scripts should exit with code 2 to block actions and include remediation instructions
- `ynd diff` compares the assembled output across vendors side by side
- Hooks can be edited from the CLI with `ynh hook add/remove` (top-level) and `ynh profile hook add/remove` (profile-level) — the same surface a GUI consumer drives

Hooks often pair with sensors — a hook produces an artifact mid-session that a sensor declares a contract over. See [Sensors](sensors.md).

## Next

[MCP Servers](mcp-servers.md) — declare tool dependencies that vendors load automatically.
