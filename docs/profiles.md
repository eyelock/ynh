# Profiles

Profiles are named configuration variants that allow the same harness to serve different execution contexts — CI, developer, security audit — with different hooks and MCP servers. A single `harness.json` carries one set of top-level defaults plus any number of profiles that can be selected at run time.

## Why Profiles Matter

A harness that works well for interactive development may need different controls for CI (stricter linting, no interactive MCP servers) or for a security audit (audit logging, SAST tooling). Without profiles, you would need separate harnesses for each context or manual post-install editing. Profiles let the harness author declare all variants in one file, and the operator selects the right one at run time.

## harness.json Format

Profiles live under the `profiles` key in `harness.json`. Each profile name maps to an object containing `hooks` and/or `mcp_servers`:

```json
{
  "name": "my-harness",
  "version": "0.1.0",
  "hooks": {
    "before_tool": [{"command": "echo default"}]
  },
  "mcp_servers": {
    "github": {"command": "gh-mcp", "args": ["serve"]}
  },
  "profiles": {
    "ci": {
      "hooks": {
        "before_tool": [{"command": "./scripts/strict-lint.sh", "matcher": "Write"}]
      },
      "mcp_servers": {
        "github": {"command": "gh-mcp", "args": ["serve"]}
      }
    },
    "security-audit": {
      "hooks": {
        "before_tool": [{"command": "./scripts/audit-log.sh"}]
      },
      "mcp_servers": {
        "sast": {"command": "semgrep-mcp", "args": ["serve"]}
      }
    }
  }
}
```

The top-level `hooks` and `mcp_servers` are the defaults — used when no profile is selected. The `ci` and `security-audit` profiles each declare their own hooks and MCP servers.

## Replace Semantics

When a profile is selected, its `hooks` and `mcp_servers` **fully replace** the top-level values. There is no merge and no inheritance. If the `ci` profile defines `hooks` but omits `mcp_servers`, the assembled output will contain only the profile's hooks and no MCP servers.

This keeps behavior predictable: you see exactly what a profile produces by reading its block alone, without tracing merge logic across layers.

## Profile Selection

Profiles are selected through a flag or environment variable:

| Method | Example | Precedence |
|--------|---------|------------|
| `--profile` flag | `ynh run --profile ci` | Highest |
| `YNH_PROFILE` env var | `YNH_PROFILE=ci ynh run` | Middle |
| _(none)_ | `ynh run` | Lowest — uses top-level values |

The `--profile` flag is supported on `ynh run`, `ynd preview`, `ynd diff`, and `ynd export`.

When both the flag and the environment variable are set, the flag wins. When neither is set, the top-level `hooks` and `mcp_servers` are used as-is.

## Missing Profile Behavior

Selecting a profile that does not exist in `harness.json` is a hard error:

```
Error: profile "staging" not defined in harness.json
```

This is intentional — a typo in a CI pipeline should fail loudly rather than silently falling back to defaults.

## Scope

Profiles can override two fields:

| Field | Overridable by profile |
|-------|----------------------|
| `hooks` | Yes |
| `mcp_servers` | Yes |
| `name`, `version`, `description` | No — identity fields |
| `includes`, `delegates_to` | No — composition fields |
| `default_vendor` | No |

Restricting scope to hooks and MCP servers keeps profiles focused on runtime behavior. Identity and composition are fixed properties of the harness itself.

## Validation

`ynd validate` checks each profile's contents using the same rules as top-level fields:

- Hook entries must have a `command` field.
- MCP server entries must have either `command` or `url`, not both.
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
