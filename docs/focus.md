# Focus

A focus is a named binding of a prompt to an optional profile. It captures a recurring intent — "review staged changes", "audit dependencies", "summarise this release" — so the same harness can be run for that specific job in a single, repeatable invocation.

## Why Focus Matters

Profiles change *how* a harness runs (hooks, MCP servers, includes). Prompts express *what* the agent should do. The two are usually paired: a "security audit" prompt only makes sense when the audit profile's tools are loaded; a "format docs" prompt only matters when the formatter MCP is available. Focus encodes that pairing once so operators do not have to remember the right `--profile` + prompt combination each time.

A focus is the unit of automation. CI pipelines, schedulers, and loop drivers can drive a harness by name (`ynh run my-harness --focus security`) instead of by ad-hoc command construction.

## Manifest Format

Focuses live under the `focuses` key in `.ynh-plugin/plugin.json`. Each focus name maps to an object with a required `prompt` and an optional `profile`:

```json
{
  "name": "my-harness",
  "version": "0.1.0",
  "profiles": {
    "ci": { "hooks": { "before_tool": [{"command": "./scripts/strict-lint.sh"}] } },
    "security": { "mcp_servers": { "sast": {"command": "semgrep-mcp"} } }
  },
  "focuses": {
    "review":   { "prompt": "Review the staged changes and report findings." },
    "audit":    { "prompt": "Audit the codebase for vulnerabilities.", "profile": "security" },
    "ci-check": { "prompt": "Run the CI checklist and report failures.", "profile": "ci" }
  }
}
```

When `--focus audit` is used, the harness runs with the `security` profile loaded *and* the audit prompt as the initial instruction. A focus without a `profile` runs against the top-level (default) configuration.

## Selection

Focus is selected through a flag or environment variable:

| Method | Example | Precedence |
|--------|---------|------------|
| `--focus` flag | `ynh run my-harness --focus audit` | Highest |
| `YNH_FOCUS` env var | `YNH_FOCUS=audit ynh run my-harness` | Middle |
| _(none)_ | `ynh run my-harness` | Lowest — interactive, top-level config |

The `--focus` flag is supported on `ynh run`, `ynd preview`, `ynd diff`, and `ynd export`. When both the flag and the environment variable are set, the flag wins.

## Mutual Exclusivity

`--focus` and `--profile` are mutually exclusive. A focus already names a profile (or names "no profile" by omission); accepting `--profile` alongside it would create ambiguity over which profile wins.

Similarly, `--focus` is mutually exclusive with a trailing prompt on `ynh run` — the focus carries its own prompt and a second prompt would be silently dropped.

```
$ ynh run my-harness --focus audit --profile security
Error: cannot use --focus and --profile together (focus includes a profile)

$ ynh run my-harness --focus audit -- "and also format the docs"
Error: cannot use --focus and a trailing prompt together (focus includes a prompt)
```

## Non-Interactive Implication

Specifying `--focus` implies non-interactive mode by default — the focus prompt is fed to the vendor CLI as the initial instruction and execution proceeds without further user input. Pass `--interactive` alongside `--focus` to override and keep the session interactive after the focus prompt is delivered.

## Missing Focus Behavior

Selecting a focus that is not defined is a hard error:

```
Error: focus "release-notes" not defined in harness
```

This matches profile-not-found behaviour — a typo in a CI pipeline should fail loudly rather than silently fall back to defaults.

## Validation

`ynd validate` checks each focus's contents:

- `prompt` is required and must be a non-empty string.
- `profile`, if present, must reference a profile defined in the same manifest.
- Unknown fields inside a focus block are rejected.
- A focus referencing a missing profile fails validation with the focus name for context:

```
Error: focus "audit": profile "security" not defined in harness manifest
```

Profile removal is also focus-aware: `ynh profile remove` refuses to delete a profile while any focus still references it, listing the blocking focuses.

## CLI Editing

Focuses can be edited from the command line as well as authored directly in the manifest. Edits route through the same resolver as `ynh include` and `ynh profile`, so changes to a pointer-form local install land in your source tree.

```bash
# Add a focus (prompt required; profile optional)
ynh focus add <harness> <name> "<prompt>" [--profile <name>]

# Remove a focus
ynh focus remove <harness> <name>

# Update a focus — change prompt, change profile, or clear the profile binding
ynh focus update <harness> <name> [--prompt "<new>"] [--profile <name>] [--clear profile]
```

`<harness>` is a canonical id from `ynh ls` (e.g. `local/my-harness` or `github.com/<org>/<repo>/<name>`). For tree-form (registry/git) installs the edits land in the install copy; for pointer-form (local source) installs they land in your source tree.

See [reference.md](reference.md) for the complete flag matrix.

## Inline Focus in Sensors

A sensor's `focus` source can be either a string reference to a top-level focus or an inline focus object scoped to that sensor only. Inline focuses do not appear in the top-level `focuses` map and are not selectable via `--focus`. See [Sensors → focus source](sensors.md) for details.

## See Also

- [Profiles](profiles.md) — environment-specific runtime overrides
- [Hooks](hooks.md) — lifecycle commands a profile can declare
- [MCP Servers](mcp.md) — tool dependencies a profile can declare
- [Sensors](sensors.md) — observation surfaces that can carry an inline focus
- [Tutorial 7: Focus](tutorial/14-focus.md) — guided walkthrough
