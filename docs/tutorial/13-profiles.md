# Tutorial 13: Profiles

Configure environment-specific overrides with profiles. A profile can add rules, hooks, MCP servers, and other settings that activate only when the profile is selected.

## Prerequisites

```bash
# Clean up from any previous run
rm -rf /tmp/ynh-tutorial
ynh uninstall profile-demo 2>/dev/null

mkdir -p /tmp/ynh-tutorial
```

## T13.1: Add profiles to the plugin manifest

Create a harness with a `ci` profile that adds stricter rules and a lint hook:

```bash
mkdir -p /tmp/ynh-tutorial/profile-harness/skills/deploy
mkdir -p /tmp/ynh-tutorial/profile-harness/rules

mkdir -p /tmp/ynh-tutorial/profile-harness/.ynh-plugin
cat > /tmp/ynh-tutorial/profile-harness/.ynh-plugin/plugin.json << 'EOF'
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "profile-demo",
  "version": "0.1.0",
  "default_vendor": "claude",
  "hooks": {
    "after_tool": [
      { "command": "/usr/local/bin/format-check.sh" }
    ]
  },
  "profiles": {
    "ci": {
      "hooks": {
        "before_tool": [
          {
            "matcher": "Bash",
            "command": "/usr/local/bin/ci-guard.sh"
          }
        ]
      },
      "mcp_servers": {
        "ci-db": {
          "command": "npx",
          "args": ["-y", "@modelcontextprotocol/server-sqlite", "/tmp/ci.db"]
        }
      }
    },
    "local": {
      "mcp_servers": {
        "dev-db": {
          "command": "npx",
          "args": ["-y", "@modelcontextprotocol/server-sqlite", "/tmp/dev.db"]
        }
      }
    }
  }
}
EOF

cat > /tmp/ynh-tutorial/profile-harness/instructions.md << 'EOF'
You are a deployment assistant. Follow safety procedures for all environments.
EOF

cat > /tmp/ynh-tutorial/profile-harness/skills/deploy/SKILL.md << 'EOF'
---
name: deploy
description: Deploy to staging or production
---

Run the deployment pipeline for the target environment.
EOF

cat > /tmp/ynh-tutorial/profile-harness/rules/safety.md << 'EOF'
Never deploy without running tests first.
EOF
```

Key points:
- `profiles` is a top-level field in `.ynh-plugin/plugin.json`
- Each profile can contain `hooks` and `mcp_servers`
- Profiles declare only what they change — absent fields inherit from top-level defaults
- MCP servers are deep-merged (profile keys win on collision); hooks use per-event replace
- Set an MCP server to `null` in a profile to remove an inherited server

## T13.2: Validate profiles

```bash
ynd validate /tmp/ynh-tutorial/profile-harness
```

Expected:
```
/tmp/ynh-tutorial/profile-harness: valid
```

The validator checks that profile names are valid and profile contents use the correct schema.

## T13.3: Preview with --profile ci

```bash
ynd preview /tmp/ynh-tutorial/profile-harness -v claude --profile ci
```

Expected output includes:
- `.claude/hooks/hooks.json` with the `ci` profile's `before_tool` hook **and** the inherited base `after_tool` hook (profiles use per-event merge — the `ci` profile's `before_tool` replaces the default, but `after_tool` is inherited)
- `.claude/.mcp.json` with the `ci-db` MCP server from the `ci` profile (no base MCP servers to inherit)

Compare with the base (no profile):

```bash
ynd preview /tmp/ynh-tutorial/profile-harness -v claude
```

Expected: `.claude/hooks/hooks.json` has only the base `after_tool` hook. No `.claude/.mcp.json` (no MCP servers in base config).

## T13.4: Run with --profile ci

Install the harness first:

```bash
ynh install /tmp/ynh-tutorial/profile-harness
```

Launch interactively with the `ci` profile:

```bash
profile-demo --profile ci
```

Inside the Claude session, enable the plugin and reload to activate hooks and MCP servers:

```
/plugin enable profile-demo
/reload-plugins
```

Expected reload output includes: `3 hooks · 1 plugin MCP server` (or similar counts). Then ask:

```
what hooks and MCP servers are configured?
```

The `ci` profile's `before_tool` hook replaces the base, and the `ci-db` MCP server is added. The base `after_tool` hook is inherited since the profile doesn't declare it.

> **Note:** Claude Code's `--plugin-dir` auto-activates skills and commands but not hooks or MCP servers. The `/plugin enable` + `/reload-plugins` step is needed to activate them. This is a Claude Code limitation — Codex and Cursor activate all plugin components automatically.

## T13.5: Try --profile nonexistent

```bash
ynd preview /tmp/ynh-tutorial/profile-harness -v claude --profile nonexistent
```

Expected error:
```
Error: profile "nonexistent" not defined in harness manifest
```

## T13.6: Use YNH_PROFILE env var

The `YNH_PROFILE` environment variable activates a profile without the flag:

```bash
YNH_PROFILE=ci ynd preview /tmp/ynh-tutorial/profile-harness -v claude
```

Expected: same output as `--profile ci` — the `ci` profile's settings are merged with the base values.

This is useful in CI/CD pipelines:

```yaml
# .github/workflows/deploy.yml
env:
  YNH_PROFILE: ci
steps:
  - run: profile-demo -- "run deployment checks"
```

## T13.7: Both flag and env var — flag wins

```bash
YNH_PROFILE=local ynd preview /tmp/ynh-tutorial/profile-harness -v claude --profile ci
```

Expected: the `ci` profile is active (not `local`). The `--profile` flag takes precedence over `YNH_PROFILE`.

Verify by checking the MCP output — you should see `ci-db` (from the `ci` profile), not `dev-db` (from the `local` profile).

## T13.8: Use ynd diff --profile ci

Compare base vs profile output across vendors:

```bash
ynd diff /tmp/ynh-tutorial/profile-harness claude cursor --profile ci
```

Expected output shows the vendor-specific differences with the `ci` profile applied to both:
- Claude: hooks in `.claude/hooks/hooks.json`, MCP in `.claude/.mcp.json`
- Cursor: hooks in `.cursor/hooks.json`, MCP in `.cursor/mcp.json`

Compare with no profile to see what the profile adds:

```bash
ynd diff /tmp/ynh-tutorial/profile-harness claude cursor
```

## T13.9: Profile-level includes — bundle extra artifacts per profile

Profiles can also declare their own `includes` array. When the profile is active, those artifact sources are **appended** to the base harness's includes. This is how a single harness can carry a "user view" by default and a "contributor view" under a dev profile.

The include source can be a local filesystem path (`local`) — ideal for ship-alongside-the-manifest directories — or a remote Git URL (`git`), same as a top-level include.

Create a bundle directory next to the harness:

```bash
mkdir -p /tmp/ynh-tutorial/profile-harness/dev-extras/skills/deep-debug

cat > /tmp/ynh-tutorial/profile-harness/dev-extras/skills/deep-debug/SKILL.md << 'EOF'
---
name: deep-debug
description: Systematic debugging workflow for production incidents.
---

When invoked, walk through: reproduce, isolate, bisect, hypothesize, verify.
EOF
```

Add a profile that pulls it in:

```bash
cat > /tmp/ynh-tutorial/profile-harness/.ynh-plugin/plugin.json << 'EOF'
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "profile-demo",
  "version": "0.1.0",
  "default_vendor": "claude",
  "profiles": {
    "ci": {
      "hooks": {
        "before_tool": [
          {
            "matcher": "Bash",
            "command": "/usr/local/bin/ci-guard.sh"
          }
        ]
      }
    },
    "dev": {
      "includes": [
        {"local": "dev-extras"}
      ]
    }
  }
}
EOF
```

Preview without profile — only the base artifacts appear:

```bash
ynd preview /tmp/ynh-tutorial/profile-harness -v claude | grep SKILL
# Expected: only skills declared at the harness root
```

Preview with `--profile dev` — `deep-debug` appears on top of the base set:

```bash
ynd preview /tmp/ynh-tutorial/profile-harness -v claude --profile dev | grep deep-debug
# Expected: skills/deep-debug/SKILL.md shows up
```

Paths in `local` are relative to the harness root (or absolute). The `pick` field filters which artifacts to include from the source. A profile cannot remove base includes — it only appends.

## Clean up

```bash
ynh uninstall profile-demo 2>/dev/null
rm -rf /tmp/ynh-tutorial
```

## What You Learned

- Profiles are declared in `.ynh-plugin/plugin.json` under `profiles` as named config objects
- Each profile can override `hooks`, `mcp_servers`, and add `includes`
- Profiles use merge semantics: MCP servers are deep-merged (profile keys win), hooks use per-event replace (absent events inherited), includes are appended (profile cannot remove a base include)
- Set an MCP server to `null` in a profile to remove an inherited server
- `--profile <name>` activates a profile on `ynh run`, `ynd preview`, and `ynd diff`
- `YNH_PROFILE` env var activates a profile (flag takes precedence)
- Invalid profile names produce helpful errors listing available profiles
- Profile-level `includes` support both remote (`git`) and local (`local`) sources — same shape as top-level includes
- `ynd validate` checks profile schema validity

## Next

[Tutorial 7: Developer Tools](tutorial/08-developer-tools.md) — scaffold, lint, validate, format, compress, inspect with ynd.
