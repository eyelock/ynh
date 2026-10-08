# Profiles

Profiles are named configuration variants that allow the same harness to serve different execution contexts — CI, developer, security audit — with different hooks, MCP servers, and additional artifact sources. A single `.agents/harness/plugin.json` carries one set of top-level defaults plus any number of profiles that can be selected at run time.

## Why Profiles Matter

A harness that works well for interactive development may need different controls for CI (stricter linting, no interactive MCP servers) or for a security audit (audit logging, SAST tooling). Without profiles, you would need separate harnesses for each context or manual post-install editing. Profiles let the harness author declare all variants in one file, and the operator selects the right one at run time.

## Manifest Format

Profiles live under the `profiles` key in `.agents/harness/plugin.json`. Each profile name maps to an object containing any of `hooks`, `mcp_servers`, and `includes`:

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

**MCP servers** use deep merge: profile keys win on collision, absent keys are inherited. Server `env` maps are also deep-merged. Within a server that exists in both, `type`, `command`, `args`, `cwd`, `url` and `headers` are replaced when the profile sets them, and only `env` is merged key by key. Set a server to `null` to remove an inherited entry, including one inherited from an [included harness](mcp.md#servers-from-included-harnesses).

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

Pass `--profile` more than once to choose a profile for the root and for included harnesses in the same run: `--profile work --profile github:ci`. At most one value may be unqualified (the root's) and at most one per namespace; a repeat is an error. See [Profiles and focuses of included harnesses](#profiles-and-focuses-of-included-harnesses). `YNH_PROFILE` stays a single value, which may itself be namespaced.

## Profiles and focuses of included harnesses

An include that resolves to a harness (see [Servers from Included Harnesses](mcp.md#servers-from-included-harnesses)) brings its profiles and focuses into scope, under a namespace. The namespace is the included harness's `name`, or the `as` alias on the include entry:

```json
{
  "includes": [
    { "git": "github.com/eyelock/assistants", "path": "ynh/github" },
    { "git": "github.com/eyelock/assistants", "path": "ynh/github", "ref": "v2", "as": "gh-v2" }
  ]
}
```

A value of the form `namespace:name` selects from that harness:

```bash
ynh run my-harness --profile github:ci                  # github's "ci" profile, for github only
ynh run my-harness --profile work --profile github:ci   # the root's "work" plus github's "ci"
ynh run my-harness --focus github:triage                # github's focus: its prompt, and its profile
```

An unqualified value is the root's, exactly as before, so nothing that exists changes. The separator is `:` because harness names may contain dots but never colons, so a value with one colon is unambiguous.

What the selection does:

- **Only that include changes.** The profile is applied to the included harness while the graph is resolved, before its own `includes` are followed. Its `mcp_servers` (including `null` removals), `includes` and `env_passthrough` take effect for that harness; the profile's includes are resolved like any other. The root and the other includes are untouched. An included profile's `mcp_isolation` is ignored: isolation is a launch decision of the root. Its `hooks` are not carried (see [hooks](hooks.md#root-harness-only-rule)).
- **A focus brings its profile.** `--focus github:triage` uses the focus's prompt as the run's prompt, and applies the focus's `profile`, if it has one, to `github`. `--focus` and `--profile` still exclude each other, qualified or not.
- **Repeat `--profile`, once per harness.** At most one unqualified value and one per namespace; `--profile github:a --profile github:b` is an error.
- **Transitive harnesses have namespaces too.** A harness reached through another include is addressed by its own name or alias, wherever it sits in the graph.

Errors:

```
Error: no included harness has namespace "x" (available: github, db)
Error: profile "nope" not defined in included harness "github" (available: [ci local])
Error: focus "nope" not defined in included harness "github" (available: [triage])
Error: namespace "github" is ambiguous: eyelock/a//ynh/github and eyelock/b//github; give one include an "as" alias
```

Two distinct harnesses may share a namespace; that is an error only when a `namespace:name` value uses it. Give one of the includes an `as` alias to tell them apart.

The same commands accept namespaced values: `ynh run`, `ynh agent run`, `ynd preview`, `ynd export`, `ynd diff` and `ynd compose` (profiles only, compose has no `--focus`). `ynd preview` lists what an include offers under "Focuses from included harnesses:" and "Profiles from included harnesses:". `ynh focus ls` and `ynh profile ls` list only the harness's own entries, which are the ones you can edit.

## Missing Profile Behavior

Selecting a profile that does not exist in `.agents/harness/plugin.json` is a hard error:

```
Error: profile "staging" not defined in harness manifest (available: [ci local])
```

The error names the profiles the harness does declare, sorted by name. A harness
with no profiles says `(the harness declares no profiles)` instead.

This is intentional — a typo in a CI pipeline should fail loudly rather than silently falling back to defaults.

## Scope

Profiles can override or extend three fields:

| Field | Profile behaviour |
|-------|----------------------|
| `hooks` | Per-event replace |
| `mcp_servers` | Deep merge (null removes) |
| `mcp_isolation` | Replace when set, either way; inherited when absent |
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

A profile cannot override or add sensors — sensors are observation declarations, not runtime context, and live only at the top level. An [inline focus inside a sensor](sensors.md#focus) can name a profile, but the sensor itself is profile-independent.

## CLI Editing

Profiles can be edited from the command line as well as authored directly in the manifest. The CLI mirrors `ynh include` / `ynh delegate` and routes edits through the same resolver, so changes to a pointer-form local install land in your source tree.

```bash
# Create / remove a profile (errors if any focus still references it)
ynh profile add <harness> <name>
ynh profile remove <harness> <name>

# Hooks inside a profile
ynh profile hook add <harness> <profile> <event> "<command>" [--matcher <pattern>]
ynh profile hook remove <harness> <profile> <event> <index>

# MCP servers inside a profile
ynh profile mcp add <harness> <profile> <name> --command <cmd> [--arg <v>...] [--env K=V...]
ynh profile mcp add <harness> <profile> <name> --url <url> [--header K=V...]
ynh profile mcp add <harness> <profile> <name> --null         # suppress an inherited server
ynh profile mcp update <harness> <profile> <name> [flags] [--clear-args|--clear-env|--clear-headers]
ynh profile mcp remove <harness> <profile> <name>

# Profile-level Git includes (no --pick; whole include is merged in)
ynh profile include add <harness> <profile> <url> [--path <subdir>] [--ref <ref>] [--replace]
ynh profile include remove <harness> <profile> <url> [--path <subdir>]
ynh profile include update <harness> <profile> <url> [--from-path <subdir>] [--path <subdir>] [--ref <ref>]
```

`<harness>` is a canonical id from `ynh ls` (e.g. `local/my-harness` or `github.com/<org>/<repo>/<name>`). For tree-form (registry/git) installs the edits land in the install copy; for pointer-form (local source) installs they land in your source tree.

`profile remove` refuses if any focus still references the profile and lists the blocking focuses — fix the focuses first, then retry.

See [reference.md](reference.md) for the complete flag matrix.

## See Also

- [Focus](focus.md) — bind a prompt to a profile for repeatable, non-interactive runs
- [Hooks](hooks.md) — hook format and vendor translation
- [MCP Servers](mcp.md) — MCP server format and vendor translation
- [Sensors](sensors.md) — observation surfaces declared by the harness
- [Vendor Support](vendors.md) — vendor capabilities and differences
