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
    "postToolUse": [
      { "command": "/usr/local/bin/run-linter.sh" }
    ],
    "preToolUse": [
      { "command": "/usr/local/bin/check-commands.sh", "matcher": "Bash" }
    ],
    "stop": [
      { "command": "/usr/local/bin/session-report.sh" }
    ]
  },
  "version": 1
}
```

Note: Cursor uses different event names (`preToolUse` / `postToolUse` vs `PreToolUse` / `PostToolUse`), includes a `version: 1` key, and a flat structure without matcher groups or type wrappers. Cursor matches `preToolUse` and `postToolUse` against its own tool types (`Shell`, `Read`, `Write`, ...), not Claude's tool names, so the preview prints a warning that the `Bash` matcher is written as-is and will not match; use `Shell` for Cursor.

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

## Run something once per session, with a timeout

`on_stop` fires at the end of **every turn**, so a ten-turn session runs it ten times. For work that belongs at the end of the **session**, such as flushing a log, use `on_session_end`. It is best-effort: vendors give it a short time budget, it cannot block, and it may not fire at all if the process is killed, so keep the script fast and idempotent. See [Hooks: on_session_end semantics](../hooks.md#on-session-end-semantics).

Create a second harness with a per-turn report on `on_stop` and a `Bash` guard on `before_tool`:

```bash
mkdir -p /tmp/ynh-tutorial/end-harness/.agents/harness
cat > /tmp/ynh-tutorial/end-harness/.agents/harness/plugin.json << 'EOF'
{
  "name": "end-demo",
  "version": "0.1.0",
  "default_vendor": "claude",
  "hooks": {
    "before_tool": [
      { "matcher": "Bash", "command": "/usr/local/bin/check-commands.sh" }
    ],
    "on_stop": [
      { "command": "/usr/local/bin/turn-report.sh" }
    ]
  }
}
EOF
echo "You are a careful assistant." > /tmp/ynh-tutorial/end-harness/instructions.md
```

Add the once-per-session hook from the command line, with a timeout of 10 seconds:

```bash
ynh hook add /tmp/ynh-tutorial/end-harness on_session_end /usr/local/bin/flush-log.sh --timeout 10
```

Expected: `Added hook (event on_session_end)`. The manifest now holds `{ "command": "/usr/local/bin/flush-log.sh", "timeout": 10 }` under `on_session_end`. `timeout` is a whole number of seconds from 1 to 3600, and ynd writes it to each vendor's own field.

### Claude: `SessionEnd` with `timeout`

```bash
ynd preview /tmp/ynh-tutorial/end-harness -v claude -o /tmp/ynh-tutorial/end-claude
cat /tmp/ynh-tutorial/end-claude/.claude/hooks/hooks.json
```

Expected: the hook file has `PreToolUse`, `SessionEnd` and `Stop` (`on_stop`). The `SessionEnd` entry carries the timeout:

```json
    "SessionEnd": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "/usr/local/bin/flush-log.sh",
            "timeout": 10
          }
        ]
      }
    ],
```

Claude Code gives `SessionEnd` hooks a shared budget of 1.5 seconds and ignores a plugin hook's `timeout` when setting it. `ynh run` therefore also sets `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS` (here `10000`) in the Claude process it launches. A plugin you export cannot do that, as the export step below shows.

### Copilot: `.copilot/hooks/hooks.json` with `timeoutSec`

```bash
ynd preview /tmp/ynh-tutorial/end-harness -v copilot -o /tmp/ynh-tutorial/end-copilot
cat /tmp/ynh-tutorial/end-copilot/.copilot/hooks/hooks.json
```

Expected: Copilot's flat format with PascalCase events, `"version": 1`, and the timeout in Copilot's field name:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "type": "command",
        "command": "/usr/local/bin/check-commands.sh",
        "matcher": "Bash"
      }
    ],
    "SessionEnd": [
      {
        "type": "command",
        "command": "/usr/local/bin/flush-log.sh",
        "timeoutSec": 10
      }
    ],
    "Stop": [
      {
        "type": "command",
        "command": "/usr/local/bin/turn-report.sh"
      }
    ]
  },
  "version": 1
}
```

### Codex: a cap, with a warning

Codex lets a `SessionEnd` hook run for at most 3 seconds. A larger `timeout` is clamped to that cap, and the preview says so instead of changing it silently:

```bash
ynd preview /tmp/ynh-tutorial/end-harness -v codex -o /tmp/ynh-tutorial/end-codex 2>&1
grep -A8 SessionEnd /tmp/ynh-tutorial/end-codex/.codex/hooks.json
```

Expected: the warning, then the `Preview written to` line, then the `SessionEnd` entry with `"timeout": 3`:

```
  warning: hook timeout 10s on on_session_end exceeds the 3s codex allows for it, so 3s is written
Preview written to /tmp/ynh-tutorial/end-codex
```

### Cursor: a matcher that will not match

Cursor matches `preToolUse` against its own tool types (`Shell`, `Read`, `Write`), not Claude's `Bash`. ynd writes the matcher as declared and warns:

```bash
ynd preview /tmp/ynh-tutorial/end-harness -v cursor -o /tmp/ynh-tutorial/end-cursor 2>&1
```

Expected output begins with the warning, then the `Preview written to` line:

```
  warning: hook matcher "Bash" on before_tool is written to Cursor as-is, but Cursor matches tool types such as Shell, Read and Write, so a Claude tool name like Bash will not match
```

Change the matcher to `Shell` for Cursor and the warning goes away.

### Export: Claude cannot raise its own budget

```bash
ynd export /tmp/ynh-tutorial/end-harness -v claude -o /tmp/ynh-tutorial/end-export 2>&1
```

Expected: after the `Exported for claude` line, a warning that names the variable to set when you launch Claude Code with the exported plugin:

```
  warning: claude ignores a plugin hook's timeout for SessionEnd, so on_session_end will be cancelled after about 1.5 s; set CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS=10000 in the environment that launches Claude Code
```

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
ynh hook add /tmp/ynh-tutorial/hook-harness before_tool "echo guard" --matcher Write --timeout 5
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

`<event>` is validated against the canonical set (`before_tool`, `after_tool`, `before_prompt`, `on_stop`, `on_session_start`, `on_session_end`). `<index>` is zero-based; removing the last entry for an event drops the event key entirely. See [hooks.md §"CLI Editing"](../hooks.md#cli-editing) and [reference.md](../reference.md) for the full flag matrix.

## Clean up

```bash
rm -rf /tmp/ynh-tutorial
```

## What You Learned

- Hooks are declared in `.agents/harness/plugin.json` under `hooks` using canonical event names
- `ynd preview` shows the assembled vendor-native output without installing
- Claude, Cursor, and Codex each use different event names and nesting structures; Copilot has its own flat format
- `on_stop` fires at the end of every turn, `on_session_end` once per session (best-effort), and `timeout` is written to each vendor's own field, with a warning wherever a vendor caps or ignores it
- Hook scripts should exit with code 2 to block actions and include remediation instructions
- `ynd diff` compares the assembled output across vendors side by side
- Hooks can be edited from the CLI with `ynh hook add/remove` (top-level) and `ynh profile hook add/remove` (profile-level) — the same surface a GUI consumer drives

Hooks often pair with sensors — a hook produces an artifact mid-session that a sensor declares a contract over. See [Sensors](sensors.md).

## Next

[MCP Servers](mcp-servers.md) — declare tool dependencies that vendors load automatically.
