# Hooks

Hooks are shell commands that vendors execute at specific lifecycle events during an agent session. They bridge the **guide layer** (what ynh manages) to the **sensor layer** (linters, tests, validators) by declaring *when* a command should run, without embedding the tool itself.

A harness declares hooks in `metadata.json`. At assembly time, ynh translates them into the vendor-native config format. The hook scripts themselves live outside the harness — they are regular shell commands or scripts on the host machine.

## Why Hooks Matter

Martin Fowler's harness engineering framework distinguishes feedforward controls (guides) from feedback controls (sensors). Hooks are the connection point: a harness declares "run this linter before every tool use" and the vendor runtime enforces it. The harness author defines the *intent*; the hook script provides the *mechanism*.

OpenAI's harness engineering guidance emphasizes that hook blocking messages should contain **agent-legible remediation instructions** — when a hook blocks an action, the error output should tell the agent *what to do differently*, not just *what went wrong*.

## Canonical Events

ynh defines four canonical hook events. Each vendor translates these to its native event names.

| Canonical Event | Description |
|----------------|-------------|
| `before_tool` | Runs before a tool/command is invoked. Can block execution. |
| `after_tool` | Runs after a tool/command completes. Can reject the result. |
| `before_prompt` | Runs before a user prompt is submitted to the model. |
| `on_stop` | Runs when the agent session ends. |

## metadata.json Format

Hooks are declared under `ynh.hooks` in `metadata.json`. Each event maps to an array of hook entries:

```json
{
  "ynh": {
    "hooks": {
      "before_tool": [
        {
          "matcher": "Bash",
          "command": "/usr/local/bin/check-dangerous-commands.sh"
        }
      ],
      "after_tool": [
        {
          "command": "/usr/local/bin/run-linter.sh"
        }
      ],
      "on_stop": [
        {
          "command": "/usr/local/bin/cleanup.sh"
        }
      ]
    }
  }
}
```

Each hook entry has:

| Field | Required | Description |
|-------|----------|-------------|
| `command` | Yes | Shell command to execute |
| `matcher` | No | Tool name pattern to scope the hook (only meaningful for `before_tool` and `after_tool`) |

## Vendor Translation

Each vendor uses different event names and config file formats:

### Event Name Mapping

| Canonical | Claude Code | Cursor | Codex |
|-----------|-------------|--------|-------|
| `before_tool` | `PreToolUse` | `beforeShellExecution` | `PreToolUse` |
| `after_tool` | `PostToolUse` | `afterShellExecution` | `PostToolUse` |
| `before_prompt` | `UserPromptSubmit` | `beforeSubmitPrompt` | `UserPromptSubmit` |
| `on_stop` | `Stop` | `stop` | `Stop` |

### Config File Locations

| Vendor | File | Format |
|--------|------|--------|
| Claude Code | `.claude/settings.json` | Three-level nesting: event > matcher group > hook array |
| Cursor | `.cursor/hooks.json` | Flat: event > hook array |
| Codex | `.codex/hooks.json` | Two-level: event > hook array with optional matcher field |

### Claude Code Format

Claude uses a three-level structure. Hook entries are grouped by matcher, and each group contains an array of inner hooks:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          { "type": "command", "command": "/usr/local/bin/check-dangerous-commands.sh" }
        ]
      }
    ]
  }
}
```

### Cursor Format

Cursor uses a flat structure. Matchers are not supported — all hooks for an event fire unconditionally:

```json
{
  "hooks": {
    "beforeShellExecution": [
      { "command": "/usr/local/bin/check-dangerous-commands.sh" }
    ]
  }
}
```

### Codex Format

Codex uses a two-level structure with optional matcher on each entry:

```json
{
  "hooks": {
    "PreToolUse": [
      { "matcher": "Bash", "command": "/usr/local/bin/check-dangerous-commands.sh" }
    ]
  }
}
```

## Blocking Hooks

A hook can block an action by using exit code 2. To provide the agent with context about why the action was blocked, the hook script should output a JSON object or a text message to stdout.

### Portable Hook Script Pattern

To write a hook that works across all vendors, output remediation instructions and exit with code 2:

```bash
#!/bin/bash
# check-dangerous-commands.sh — block destructive git operations
if echo "$@" | grep -qE 'git (push --force|reset --hard|clean -fd)'; then
  echo '{"error": "Destructive git operation blocked. Use --no-force or create a backup branch first."}' >&2
  exit 2
fi
exit 0
```

The blocking message should be **agent-legible**: tell the agent what to do instead, not just what failed. For example, "Use git push without --force" rather than "Force push not allowed."

## Root-Harness-Only Rule

Hooks declared in **included harnesses** (via `includes`) are dropped during assembly. Only the root harness's hooks are used. This prevents composed harnesses from silently injecting lifecycle behavior that the harness author didn't explicitly declare.

If an included harness needs hooks, copy its hook declarations into the root harness's `metadata.json`.

## Portable Hook Script Advice

When writing hook scripts for use across vendors:

1. **Output correct JSON for the event type** — Claude expects `{"type": "command"}` wrapper; Cursor and Codex do not. Your *script output* (blocking messages) should be plain text or simple JSON that any vendor can display.
2. **Use exit code 2 for blocking** — all three vendors recognize exit code 2 as "block this action."
3. **Include remediation instructions** — tell the agent how to fix the problem, not just that there is one.
4. **Keep scripts idempotent** — hooks may fire multiple times per session.
5. **Use absolute paths** — the working directory during hook execution varies by vendor.

## See Also

- [Tutorial 10: Hooks](tutorial/10-hooks.md) — step-by-step walkthrough
- [Harness Engineering](harness-engineering.md) — how hooks bridge guides to sensors
- [Vendor Support](vendors.md) — vendor capabilities and differences
