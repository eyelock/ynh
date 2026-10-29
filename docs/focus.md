# Focus

A focus is a named binding of a prompt to an optional profile. It captures a recurring intent — "review staged changes", "audit dependencies", "summarise this release" — so the same harness can be run for that specific job in a single, repeatable invocation.

## Why Focus Matters

Profiles change *how* a harness runs (hooks, MCP servers, includes). Prompts express *what* the agent should do. The two are usually paired: a "security audit" prompt only makes sense when the audit profile's tools are loaded; a "format docs" prompt only matters when the formatter MCP is available. Focus encodes that pairing once so operators do not have to remember the right `--profile` + prompt combination each time.

A focus is the unit of automation. CI pipelines, schedulers, and loop drivers can drive a harness by name (`ynh run my-harness --focus security`) instead of by ad-hoc command construction.

## Manifest Format

Focuses live under the `focuses` key in `.agents/harness/plugin.json`. Each focus name maps to an object with a required `prompt` and an optional `profile`:

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

## Focuses of Included Harnesses

A focus of an included harness is selected as `namespace:name`, where the namespace is the included harness's name or the `as` alias on the include:

```bash
ynh run my-harness --focus github:triage
```

The focus's prompt is the run's prompt, and its `profile`, if any, is applied to that included harness only. The root's configuration is unchanged. The namespace, the `:` separator and the ambiguity rule are described in [Profiles and focuses of included harnesses](profiles.md#profiles-and-focuses-of-included-harnesses).

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

## Resuming a Session

`ynh run <harness> --resume` (or `--resume=<id>`) continues a previous session
instead of starting one. What reaches the resumed session:

- **A trailing prompt** (`-- "<prompt>"`) is delivered to it. With
  `--interactive` the session stays open with the prompt as its next message;
  without, it is a one-shot run, as on a cold launch.
- **A focus prompt is not delivered.** `--resume --focus <name>` still applies
  the focus's profile, but a focus prompt starts a task and a resume continues
  one, so a caller that re-sends a stored command with `--resume` does not
  repeat the task. If there is nothing to resume and ynh falls back to a cold
  launch, the focus prompt is used as normal, because that is a new session.

| Vendor | Resume with a prompt |
|--------|----------------------|
| Claude | Supported, interactive and one-shot |
| Codex | One-shot only (`codex exec resume`); refused with `--interactive` |
| Copilot | Refused: no documented form |
| Cursor | Refused: no documented form |

Where a vendor refuses, ynh stops before launching with an error naming it
(for example `codex cannot resume a session interactively with a prompt; drop
--resume or the prompt`). The prompt is never dropped silently.

### Binding a resume to a session name

Without `--session-name`, `--resume` continues the newest session in the
directory, which may be another caller's. For Claude, `--session-name <name>`
binds the resume to that name instead:

- A launch without `--resume` starts a new session under an id ynh chooses
  (`claude --session-id <uuid>`) and records it for the name, the harness and
  the directory in `$YNH_HOME/sessions/`, replacing any earlier record.
- `--resume` continues the recorded session. If nothing is recorded, or Claude
  no longer has that session, ynh says so on stderr and starts a new one. It
  never falls back to the newest session in the directory.
- `--resume=<id>` continues that session, and the name follows it if Claude
  has it.
- If `--session-id`, `--resume`, `-r`, `--continue` or `-c` is passed straight
  to Claude, ynh adds none of its own and binds nothing.

Two names in one directory keep separate sessions. Codex, Copilot and Cursor
are unchanged: `--session-name` is only a label there, and `--resume` still
continues the newest session in the directory.

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
  - focus.audit: references unknown profile "security"
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
ynh focus update <harness> <name> [--prompt "<new>"] [--profile <name>] [--clear-profile]
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
- [Focus](tutorial/focus.md) — guided walkthrough
