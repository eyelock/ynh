# Profiles

Profiles are named configuration variants that allow the same harness to serve different execution contexts — CI, developer, security audit — with different hooks, MCP servers, and additional artifact sources. A single `.ynh-plugin/plugin.json` carries one set of top-level defaults plus any number of profiles that can be selected at run time.

## Why Profiles Matter

A harness that works well for interactive development may need different controls for CI (stricter linting, no interactive MCP servers) or for a security audit (audit logging, SAST tooling). Without profiles, you would need separate harnesses for each context or manual post-install editing. Profiles let the harness author declare all variants in one file, and the operator selects the right one at run time.

## Manifest Format

Profiles live under the `profiles` key in `.ynh-plugin/plugin.json`. Each profile name maps to an object containing any of `hooks`, `mcp_servers`, and `includes`:

```json
{
  "name": "my-harness",
  "version": "0.1.0",
  "hooks": {
    "before_tool": [{"command": "echo default"}],
    "after_tool": [{"command": "echo log"}]
  },
  "mcp_servers": {
    "github": {"command": "gh-mcp", "args": ["serve"]},
    "postgres": {"command": "pg-mcp"}
  },
  "profiles": {
    "ci": {
      "hooks": {
        "before_tool": [{"command": "./scripts/strict-lint.sh", "matcher": "Write"}]
      },
      "mcp_servers": {
        "postgres": null,
        "ci-metrics": {"command": "metrics-mcp"}
      }
    },
    "security-audit": {
      "mcp_servers": {
        "sast": {"command": "semgrep-mcp", "args": ["serve"]}
      }
    }
  }
}
```

The top-level `hooks` and `mcp_servers` are the defaults — used when no profile is selected.

## Merge Semantics

Profiles declare only what they change. Absent fields inherit from the top-level defaults.

**MCP servers** use deep merge — profile keys win on collision, absent keys are inherited. Server `env` maps are also deep-merged. Set a server to `null` to remove an inherited entry.

**Hooks** use per-event replace — if a profile declares `before_tool`, it replaces the default `before_tool`. Other events (like `after_tool`) are inherited.

**Includes** are appended to the base harness's `includes` when the profile is active. A profile cannot remove a base include; it only adds additional artifact sources. See [Profile-level Includes](#profile-level-includes) below.

Using the example above, selecting the `ci` profile produces:
- **hooks**: `before_tool` replaced with the CI lint hook; `after_tool` inherited from defaults
- **mcp_servers**: `github` inherited, `postgres` removed (null), `ci-metrics` added

Selecting `security-audit` produces:
- **hooks**: all inherited (profile declares none)
- **mcp_servers**: `github` and `postgres` inherited, `sast` added

## Profile-level Includes

A profile can declare its own `includes` array that is appended to the base harness's `includes` when the profile is active. This lets one harness carry multiple artifact sets and swap them based on context — the obvious use case is a "user view" under the default profile and a "contributor view" under a dev profile.

```json
{
  "name": "ynh-guide",
  "version": "0.1.0",
  "profiles": {
    "ynh-dev": {
      "includes": [
        {"local": ".claude"}
      ]
    }
  }
}
```

With no profile selected, the assembled output uses only the artifacts at the harness root (`skills/`, `agents/`, `rules/`, `commands/`). With `--profile ynh-dev`, the artifacts under `.claude/skills/`, `.claude/agents/`, `.claude/rules/`, and `.claude/commands/` are merged into the output on top of the base set.

Profile-level includes use the same shape as top-level includes — either `git` (remote) or `local` (path), with optional `path`, `ref`, and `pick`. See [Harness Manifest → includes](harnesses.md#includes-optional) for the full include schema.

Artifact-collision behaviour: profile includes are appended after base includes, so a later artifact with the same name takes precedence over an earlier one. This lets a profile shadow a base artifact with an alternative implementation while keeping the rest of the base intact.

## Profile Selection

Profiles are selected through a flag or environment variable:

| Method | Example | Precedence |
|--------|---------|------------|
| `--profile` flag | `ynh run --profile ci` | Highest |
| `YNH_PROFILE` env var | `YNH_PROFILE=ci ynh run` | Middle |
| _(none)_ | `ynh run` | Lowest — uses top-level values |

The `--profile` flag is supported on `ynh run`, `ynd preview`, `ynd diff`, and `ynd export`. It can be combined with the `--harness` flag for explicit harness source selection.

When both the flag and the environment variable are set, the flag wins. When neither is set, the top-level `hooks` and `mcp_servers` are used as-is.

## Missing Profile Behavior

Selecting a profile that does not exist in `.ynh-plugin/plugin.json` is a hard error:

```
Error: profile "staging" not defined in harness manifest
```

This is intentional — a typo in a CI pipeline should fail loudly rather than silently falling back to defaults.

## Scope

Profiles can override or extend three fields:

| Field | Profile behaviour |
|-------|----------------------|
| `hooks` | Per-event replace |
| `mcp_servers` | Deep merge (null removes) |
| `includes` | Append (profile entries added after base entries) |
| `name`, `version`, `description` | Fixed — identity fields |
| `delegates_to` | Fixed — composition field |
| `default_vendor` | Fixed |

Keeping identity and delegation fixed keeps a harness's surface stable across profiles — what varies is the runtime behaviour (hooks, MCP servers) and the artifact set.

## Validation

`ynd validate` checks each profile's contents using the same rules as top-level fields:

- Hook entries must have a `command` field.
- MCP server entries must have either `command` or `url`, not both.
- Include entries must have exactly one of `git` or `local`.
- Profile names must be non-empty strings.
- Unknown fields inside a profile block are rejected.

Validation runs across all profiles in one pass, reporting errors with the profile name for context:

```
Error: profile "ci": hooks.before_tool[0]: missing "command" field
```

## See Also

- [Hooks](hooks.md) — hook format and vendor translation
- [MCP Servers](mcp.md) — MCP server format and vendor translation
- [Vendor Support](vendors.md) — vendor capabilities and differences
