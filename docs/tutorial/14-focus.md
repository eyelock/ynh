# Tutorial 14: Focus

Define named focus entries that combine a profile with a prompt for repeatable, non-interactive AI execution. Focus entries are the bridge between harness configuration and CI automation.

## Prerequisites

```bash
# Clean up from any previous run
rm -rf /tmp/ynh-tutorial
ynh uninstall focus-demo 2>/dev/null

mkdir -p /tmp/ynh-tutorial
```

## T14.1: Create a harness with focus entries

Create a harness with profiles and focus entries that reference them:

```bash
mkdir -p /tmp/ynh-tutorial/focus-harness/skills/deploy

mkdir -p /tmp/ynh-tutorial/focus-harness/.ynh-plugin
cat > /tmp/ynh-tutorial/focus-harness/.ynh-plugin/plugin.json << 'EOF'
{
  "name": "focus-demo",
  "version": "0.1.0",
  "default_vendor": "claude",
  "hooks": {
    "after_tool": [
      { "command": "/usr/local/bin/format-check.sh" }
    ]
  },
  "mcp_servers": {
    "github": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": { "GITHUB_TOKEN": "${GITHUB_TOKEN}" }
    }
  },
  "profiles": {
    "ci": {
      "hooks": {
        "before_tool": [
          { "matcher": "Bash", "command": "/usr/local/bin/ci-guard.sh" }
        ]
      },
      "mcp_servers": {
        "github": null
      }
    }
  },
  "focus": {
    "review": {
      "profile": "ci",
      "prompt": "Review staged changes for quality and correctness"
    },
    "docs": {
      "prompt": "Generate API documentation for all public interfaces"
    }
  }
}
EOF

cat > /tmp/ynh-tutorial/focus-harness/instructions.md << 'EOF'
You are a development assistant. Follow team coding standards.
EOF

cat > /tmp/ynh-tutorial/focus-harness/skills/deploy/SKILL.md << 'EOF'
---
name: deploy
description: Deploy to staging or production
---

Run the deployment pipeline for the target environment.
EOF
```

Key points:
- `focus` is a top-level field alongside `profiles`
- Each focus has a `prompt` (required) and optional `profile`
- The `review` focus activates the `ci` profile and sends a review prompt
- The `docs` focus has no profile — it uses the base configuration
- The `ci` profile uses `null` to remove the inherited `github` MCP server

## T14.2: Validate focus entries

```bash
ynd validate /tmp/ynh-tutorial/focus-harness
```

Expected:
```
/tmp/ynh-tutorial/focus-harness: valid
```

The validator checks that each focus entry has a non-empty `prompt` and that any referenced profile exists.

## T14.3: Preview with --focus review

```bash
ynd preview /tmp/ynh-tutorial/focus-harness -v claude --focus review
```

Expected output includes:
- `.claude/hooks/hooks.json` with the `ci` profile's `before_tool` hook (PreToolUse) **and** the inherited base `after_tool` hook (PostToolUse) — profiles use merge semantics
- No `.claude/.mcp.json` — the `ci` profile removes the `github` MCP server via `null`

Compare with `--focus docs` (no profile, uses base config):

```bash
ynd preview /tmp/ynh-tutorial/focus-harness -v claude --focus docs
```

Expected: `.claude/hooks/hooks.json` has only the base `after_tool` hook (PostToolUse). `.claude/.mcp.json` has the `github` MCP server (inherited from base).

## T14.4: Focus and profile are mutually exclusive

```bash
ynd preview /tmp/ynh-tutorial/focus-harness -v claude --focus review --profile ci
```

Expected error:
```
Error: cannot use --focus and --profile together
```

A focus already includes a profile — specifying both is ambiguous.

## T14.5: Unknown focus is an error

```bash
ynd preview /tmp/ynh-tutorial/focus-harness -v claude --focus nonexistent
```

Expected error:
```
Error: focus "nonexistent" not defined in harness
```

## T14.6: Use YNH_FOCUS env var

The `YNH_FOCUS` environment variable activates a focus without the flag:

```bash
YNH_FOCUS=review ynd preview /tmp/ynh-tutorial/focus-harness -v claude
```

Expected: same output as `--focus review` — the `ci` profile's hooks merged with base, no MCP servers.

## T14.7: Install and view focus in ynh info

```bash
ynh install /tmp/ynh-tutorial/focus-harness
ynh info focus-demo
```

Expected output includes a `Focus:` section:
```
Focus:
  docs    (default)    "Generate API documentation for all public interfaces"
  review    profile=ci    "Review staged changes for quality and correctness"
```

And a `Profiles:` section:
```
Profiles:
  ci    hooks: before_tool    mcp_servers: github
```

## Clean up

```bash
ynh uninstall focus-demo 2>/dev/null
rm -rf /tmp/ynh-tutorial
```

## What You Learned

- Focus entries combine a profile + prompt for repeatable AI execution
- Each focus requires a `prompt`; the `profile` field is optional
- `--focus <name>` activates a focus on `ynh run`, `ynd preview`, `ynd diff`, and `ynd export`
- `YNH_FOCUS` env var activates a focus (flag takes precedence)
- `--focus` and `--profile` are mutually exclusive — focus already includes a profile
- Focus entries that reference a non-existent profile are caught by `ynd validate`
- `ynh info` displays focus entries and profiles

## Next

[Tutorial 15: Project-Local Config](15-project-local-config.md) — use `.ynh-plugin/plugin.json` in your project root for zero-install AI configuration.
