# Focus

Define named focus entries that combine a profile with a prompt for repeatable, non-interactive AI execution. Focus entries are the bridge between harness configuration and CI automation. They can also serve as the source of an agent-driven [sensor](../sensors.md#focus) — referenced by name or inlined inside the sensor.

## Prerequisites

```bash
# Clean up from any previous run
rm -rf /tmp/ynh-tutorial
ynh uninstall local/focus-demo 2>/dev/null
ynh uninstall local/wake-demo 2>/dev/null

mkdir -p /tmp/ynh-tutorial
```

## Create a harness with focus entries

Create a harness with profiles and focus entries that reference them:

```bash
mkdir -p /tmp/ynh-tutorial/focus-harness/skills/deploy

mkdir -p /tmp/ynh-tutorial/focus-harness/.agents/harness
cat > /tmp/ynh-tutorial/focus-harness/.agents/harness/plugin.json << 'EOF'
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "focus-demo",
  "version": "0.1.0",
  "default_vendor": "claude",
  "env_passthrough": ["GITHUB_TOKEN"],
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
  "focuses": {
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

The `github` server reads its token from `${GITHUB_TOKEN}`. Preview refuses a
reference to a variable that is not both listed in `env_passthrough` and set. If you have no token
in your shell, give it a demo value (preview never sends it anywhere); a real one is kept:

```bash
export GITHUB_TOKEN="${GITHUB_TOKEN:-ghp-demo-token}"
```

Key points:
- `focus` is a top-level field alongside `profiles`
- Each focus has a `prompt` (required) and optional `profile`
- The `review` focus activates the `ci` profile and sends a review prompt
- The `docs` focus has no profile — it uses the base configuration
- `env_passthrough` allowlists `GITHUB_TOKEN` for the MCP server's `${GITHUB_TOKEN}` reference
- The `ci` profile uses `null` to remove the inherited `github` MCP server

## Validate focus entries

```bash
ynd validate /tmp/ynh-tutorial/focus-harness
```

Expected:
```
/tmp/ynh-tutorial/focus-harness: valid
  checked:
    manifest     /tmp/ynh-tutorial/focus-harness/.agents/harness/plugin.json against https://eyelock.github.io/ynh/schema/plugin.schema.json
    includes     none
    mcp_servers  github runs `npx -y @modelcontextprotocol/server-github`
    hooks        after_tool runs `/usr/local/bin/format-check.sh`
    profiles     ci
    focuses      docs
                 review
    sensors      none
    delegates_to none
    skills       deploy
    agents       none
    rules        none
    commands     none
    instructions instructions.md
```

The validator checks that each focus entry has a non-empty `prompt` and that any referenced profile exists.

## Preview with --focus review

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

## Focus and profile are mutually exclusive

```bash
ynd preview /tmp/ynh-tutorial/focus-harness -v claude --focus review --profile ci
```

Expected error:
```
Error: cannot use --focus and --profile together
```

A focus already includes a profile — specifying both is ambiguous.

## Unknown focus is an error

```bash
ynd preview /tmp/ynh-tutorial/focus-harness -v claude --focus nonexistent
```

Expected error:
```
Error: focus "nonexistent" not defined in harness
```

## Use YNH_FOCUS env var

The `YNH_FOCUS` environment variable activates a focus without the flag:

```bash
YNH_FOCUS=review ynd preview /tmp/ynh-tutorial/focus-harness -v claude
```

Expected: same output as `--focus review` — the `ci` profile's hooks merged with base, no MCP servers.

## Install and view focus in ynh info

```bash
ynh install /tmp/ynh-tutorial/focus-harness
ynh info local/focus-demo
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

## Edit focuses from the command line

Hand-editing the manifest is fine for an author. For an interactive consumer — a UI over the manifest, a script, an ad-hoc tweak — the CLI provides the same surface:

```bash
# Add a focus
ynh focus add /tmp/ynh-tutorial/focus-harness security "audit this PR for security issues" --profile ci

# Update prompt or profile binding
ynh focus update /tmp/ynh-tutorial/focus-harness security --prompt "audit thoroughly for security issues"
ynh focus update /tmp/ynh-tutorial/focus-harness security --clear-profile

# Remove a focus
ynh focus remove /tmp/ynh-tutorial/focus-harness security
```

`--profile` and `--clear-profile` are mutually exclusive on `update`. Adding a focus that references a profile name not defined in the manifest is rejected with an error. Removing a profile referenced by any focus is also rejected (the focus has to be updated or removed first) — see [Profiles §"Edit profiles from the command line"](profiles.md#edit-profiles-from-the-command-line).

The first positional argument is either a filesystem path (as above, while authoring) or a canonical id (`local/focus-demo`, `github.com/<org>/<repo>/<name>`) once the harness is installed.

## Launch focus: a harness that wakes with a job

A harness can name the focus it always wakes with. Add `launch_focus` beside the focus it names:

```bash
mkdir -p /tmp/ynh-tutorial/wake-harness/.agents/harness
cat > /tmp/ynh-tutorial/wake-harness/.agents/harness/plugin.json << 'EOF'
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "wake-demo",
  "version": "0.1.0",
  "default_vendor": "claude",
  "profiles": {
    "live": { "hooks": { "on_stop": [{ "command": "echo live-profile-marker" }] } }
  },
  "focuses": {
    "wake": { "prompt": "Begin: act on your wake block", "profile": "live" },
    "next": { "prompt": "Take the next task" }
  },
  "launch_focus": "wake"
}
EOF
ynd validate /tmp/ynh-tutorial/wake-harness | head -1
ynh install /tmp/ynh-tutorial/wake-harness
```

Expected first line:
```
/tmp/ynh-tutorial/wake-harness: valid
```

A bare launch, with no `--focus` and no prompt, runs `--focus wake --interactive`: the session stays open, the focus prompt is its first message, and the focus's profile is applied. ynh says so on stderr.

*This launches:* `claude Begin: act on your wake block --plugin-dir ~/.ynh/run/local--wake-demo/.claude --add-dir ~/.ynh/run/local--wake-demo`
```bash
ynh run local/wake-demo
```

Expected on stderr:
```
Using launch_focus "wake" (pass --focus or a prompt to run something else)
Launching claude...
```

The profile's hook is in the assembled session:

```bash
grep -c live-profile-marker ~/.ynh/run/local--wake-demo/.claude/hooks/hooks.json
```

Expected:
```
1
```

An explicit `--focus` is unchanged: it runs that focus one-shot, and the launch focus (its prompt, its profile, its interactive session) is not involved. A scheduler that runs `worker --focus next` still gets a one-shot run that exits.

*This launches:* `claude --plugin-dir ~/.ynh/run/local--wake-demo/.claude --add-dir ~/.ynh/run/local--wake-demo -p Take the next task`
```bash
ynh run local/wake-demo --focus next
```

The `live` profile is not applied to it:

```bash
grep -c live-profile-marker ~/.ynh/run/local--wake-demo/.claude/hooks/hooks.json 2>/dev/null || echo 0
```

Expected:
```
0
```

An explicit prompt replaces the wake prompt but keeps the launch focus's profile. Whether the session is interactive is up to you, as it always was: one-shot unless you pass `--interactive`.

*This launches:* `claude --plugin-dir ~/.ynh/run/local--wake-demo/.claude --add-dir ~/.ynh/run/local--wake-demo -p summarise the repository`
```bash
ynh run local/wake-demo -- "summarise the repository"
```

```bash
grep -c live-profile-marker ~/.ynh/run/local--wake-demo/.claude/hooks/hooks.json
```

Expected:
```
1
```

There is no switch to turn the launch focus off: a prompt is how you get a session that does not wake with it. `ynd validate` rejects a `launch_focus` that names a focus that is not there:

```bash
mkdir -p /tmp/ynh-tutorial/bad-wake/.agents/harness
sed 's/"launch_focus": "wake"/"launch_focus": "sleep"/' /tmp/ynh-tutorial/wake-harness/.agents/harness/plugin.json > /tmp/ynh-tutorial/bad-wake/.agents/harness/plugin.json
ynd validate /tmp/ynh-tutorial/bad-wake 2>&1 | grep launch_focus
```

Expected:
```
  - launch_focus references undefined focus "sleep"
```

Because the launch focus's prompt steers every session, `ynh trust show local/wake-demo` lists it, and the trust digest covers it. See [Focus: Launch focus](../focus.md#launch-focus).

## Clean up

```bash
ynh uninstall local/focus-demo 2>/dev/null
ynh uninstall local/wake-demo 2>/dev/null
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
- `launch_focus` names the focus a bare `ynh run` wakes with: interactive, its prompt first, its profile applied; an explicit `--focus` or prompt overrides it
- Focuses can be edited with `ynh focus add/remove/update` — the same surface a GUI consumer drives

## Next

[Project-Local Config](project-local-config.md) — use `.agents/harness/plugin.json` in your project root for zero-install configuration.
