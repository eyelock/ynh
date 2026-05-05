# Tutorial 17: Include Editing

Add, remove, and update includes in an existing harness from the CLI — no manual JSON editing required.

## Prerequisites

```bash
# Clean up from any previous run
rm -rf /tmp/ynh-tutorial-includes
mkdir -p /tmp/ynh-tutorial-includes
```

## T17.1: Add an include

Start with a bare harness and add a Git include to it:

```bash
mkdir -p /tmp/ynh-tutorial-includes/my-harness

mkdir -p /tmp/ynh-tutorial-includes/my-harness/.ynh-plugin
cat > /tmp/ynh-tutorial-includes/my-harness/.ynh-plugin/plugin.json << 'EOF'
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "my-harness",
  "version": "0.1.0",
  "default_vendor": "claude"
}
EOF

ynh include add /tmp/ynh-tutorial-includes/my-harness github.com/anthropics/skills
```

Expected:
```
Added include "github.com/anthropics/skills"
```

The include is written to `.ynh-plugin/plugin.json` immediately:

```bash
cat /tmp/ynh-tutorial-includes/my-harness/.ynh-plugin/plugin.json
```

Expected:
```json
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "my-harness",
  "version": "0.1.0",
  "default_vendor": "claude",
  "includes": [
    {
      "git": "github.com/anthropics/skills"
    }
  ]
}
```

## T17.2: Add with flags

Add a second include scoped to a subdirectory with specific picks and a pinned ref:

```bash
ynh include add /tmp/ynh-tutorial-includes/my-harness github.com/eyelock/assistants \
  --path skills/dev \
  --pick skills/dev-project,skills/dev-quality \
  --ref main
```

Expected:
```
Added include "github.com/eyelock/assistants" (path: "skills/dev")
```

```bash
cat /tmp/ynh-tutorial-includes/my-harness/.ynh-plugin/plugin.json
```

Expected:
```json
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "my-harness",
  "version": "0.1.0",
  "default_vendor": "claude",
  "includes": [
    {
      "git": "github.com/anthropics/skills"
    },
    {
      "git": "github.com/eyelock/assistants",
      "ref": "main",
      "path": "skills/dev",
      "pick": [
        "skills/dev-project",
        "skills/dev-quality"
      ]
    }
  ]
}
```

**Flag reference:**

| Flag | Purpose |
|------|---------|
| `--path <subdir>` | Scope into a subdirectory of the repo |
| `--pick <items>` | Comma-separated artifact paths in `type/name` form: `skills/<name>`, `agents/<name>.md`, `rules/<name>.md`, `commands/<name>.md`. All others excluded. |
| `--ref <ref>` | Pin to a branch, tag, or commit SHA |

## T17.3: Duplicate add → error

Adding the same URL + path combination a second time is an error:

```bash
ynh include add /tmp/ynh-tutorial-includes/my-harness github.com/anthropics/skills 2>&1
```

Expected:
```
Error: include "github.com/anthropics/skills" already present in harness "my-harness".
Use 'ynh include update' to change its options, or pass --replace to overwrite
```

## T17.4: Replace an existing include

Use `--replace` to overwrite an existing include rather than erroring:

```bash
ynh include add /tmp/ynh-tutorial-includes/my-harness github.com/anthropics/skills \
  --pick skills/frontend-design \
  --replace
```

Expected:
```
Replaced include "github.com/anthropics/skills"
```

```bash
cat /tmp/ynh-tutorial-includes/my-harness/.ynh-plugin/plugin.json
```

Expected:
```json
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "my-harness",
  "version": "0.1.0",
  "default_vendor": "claude",
  "includes": [
    {
      "git": "github.com/anthropics/skills",
      "pick": [
        "skills/frontend-design"
      ]
    },
    {
      "git": "github.com/eyelock/assistants",
      "ref": "main",
      "path": "skills/dev",
      "pick": [
        "skills/dev-project",
        "skills/dev-quality"
      ]
    }
  ]
}
```

## T17.5: Update an include

Change an include's options without removing and re-adding it. Only the flags you supply are updated; others are left unchanged.

Update the ref for the assistants include:

```bash
ynh include update /tmp/ynh-tutorial-includes/my-harness github.com/eyelock/assistants \
  --ref v1.0.0
```

Expected:
```
Updated include "github.com/eyelock/assistants"
```

The path and pick are unchanged:

```bash
cat /tmp/ynh-tutorial-includes/my-harness/.ynh-plugin/plugin.json
```

Expected:
```json
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "my-harness",
  "version": "0.1.0",
  "default_vendor": "claude",
  "includes": [
    {
      "git": "github.com/anthropics/skills",
      "pick": [
        "skills/frontend-design"
      ]
    },
    {
      "git": "github.com/eyelock/assistants",
      "ref": "v1.0.0",
      "path": "skills/dev",
      "pick": [
        "skills/dev-project",
        "skills/dev-quality"
      ]
    }
  ]
}
```

## T17.6: Update a path value

`--path` on `update` changes the path field — it does not control which include is targeted (that's `--from-path`):

```bash
ynh include update /tmp/ynh-tutorial-includes/my-harness github.com/eyelock/assistants \
  --path skills/tech
```

Expected:
```
Updated include "github.com/eyelock/assistants"
```

```bash
cat /tmp/ynh-tutorial-includes/my-harness/.ynh-plugin/plugin.json
```

Expected:
```json
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "my-harness",
  "version": "0.1.0",
  "default_vendor": "claude",
  "includes": [
    {
      "git": "github.com/anthropics/skills",
      "pick": [
        "skills/frontend-design"
      ]
    },
    {
      "git": "github.com/eyelock/assistants",
      "ref": "v1.0.0",
      "path": "skills/tech",
      "pick": [
        "skills/dev-project",
        "skills/dev-quality"
      ]
    }
  ]
}
```

## T17.7: Remove an include

```bash
ynh include remove /tmp/ynh-tutorial-includes/my-harness github.com/anthropics/skills
```

Expected:
```
Removed include "github.com/anthropics/skills"
```

```bash
cat /tmp/ynh-tutorial-includes/my-harness/.ynh-plugin/plugin.json
```

Expected:
```json
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "my-harness",
  "version": "0.1.0",
  "default_vendor": "claude",
  "includes": [
    {
      "git": "github.com/eyelock/assistants",
      "ref": "v1.0.0",
      "path": "skills/tech",
      "pick": [
        "skills/dev-project",
        "skills/dev-quality"
      ]
    }
  ]
}
```

## T17.8: Disambiguating a monorepo

A harness can include the same repo at two different paths. When a URL matches multiple includes, `--path` (for `remove`) or `--from-path` (for `update`) is required to disambiguate.

Set up two includes from the same repo at different paths:

```bash
mkdir -p /tmp/ynh-tutorial-includes/my-harness/.ynh-plugin
cat > /tmp/ynh-tutorial-includes/my-harness/.ynh-plugin/plugin.json << 'EOF'
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "my-harness",
  "version": "0.1.0",
  "default_vendor": "claude",
  "includes": [
    {
      "git": "github.com/eyelock/assistants",
      "path": "skills/dev"
    },
    {
      "git": "github.com/eyelock/assistants",
      "path": "skills/tech"
    }
  ]
}
EOF
```

Removing without `--path` fails:

```bash
ynh include remove /tmp/ynh-tutorial-includes/my-harness github.com/eyelock/assistants 2>&1
```

Expected:
```
Error: include "github.com/eyelock/assistants" matches multiple entries:
  skills/dev
  skills/tech
Use --path (remove) or --from-path (update) to disambiguate
```

Specify `--path` to target one:

```bash
ynh include remove /tmp/ynh-tutorial-includes/my-harness github.com/eyelock/assistants \
  --path skills/dev
```

Expected:
```
Removed include "github.com/eyelock/assistants" (path: "skills/dev")
```

Similarly for `update`, use `--from-path` to select which include to change:

```bash
ynh include add /tmp/ynh-tutorial-includes/my-harness github.com/eyelock/assistants \
  --path skills/dev
```

Expected:
```
Added include "github.com/eyelock/assistants" (path: "skills/dev")
```

```bash
ynh include update /tmp/ynh-tutorial-includes/my-harness github.com/eyelock/assistants \
  --from-path skills/dev \
  --ref v2.0.0
```

Expected:
```
Updated include "github.com/eyelock/assistants"
```

Only the `skills/dev` include is changed; `skills/tech` is untouched:

```bash
cat /tmp/ynh-tutorial-includes/my-harness/.ynh-plugin/plugin.json
```

Expected:
```json
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "my-harness",
  "version": "0.1.0",
  "default_vendor": "claude",
  "includes": [
    {
      "git": "github.com/eyelock/assistants",
      "path": "skills/tech"
    },
    {
      "git": "github.com/eyelock/assistants",
      "ref": "v2.0.0",
      "path": "skills/dev"
    }
  ]
}
```

## T17.9: Installed harnesses — name-based targeting (network required)

> **Skip in evals** — requires network access for the pre-fetch.

For installed harnesses, use the harness name instead of a path:

```bash
# Install a harness first
mkdir -p /tmp/ynh-tutorial-includes/base
mkdir -p /tmp/ynh-tutorial-includes/base/.ynh-plugin
cat > /tmp/ynh-tutorial-includes/base/.ynh-plugin/plugin.json << 'EOF'
{
  "name": "base",
  "version": "0.1.0",
  "default_vendor": "claude"
}
EOF

ynh install /tmp/ynh-tutorial-includes/base

# Now edit it by name — ynh resolves to ~/.ynh/harnesses/base
ynh include add base github.com/anthropics/skills
```

Expected:
```
Added include "github.com/anthropics/skills"
```

When targeting an installed harness by name, ynh pre-fetches the new include immediately so the harness is ready to run without a separate `ynh update`.

```bash
# Verify via ynh info — note the canonical id (local/<name> for local-path installs)
ynh info local/base
```

The installed harness's `.ynh-plugin/plugin.json` at `~/.ynh/harnesses/local--base/.ynh-plugin/plugin.json` now contains the added include.

`ynh include`'s `<harness>` argument follows the same rule as every other ref-accepting command: a canonical id (`local/<name>` or `<host>/<org>/<repo>/<name>`) for an installed harness, or an explicit filesystem path (`./<dir>`, `/abs/<dir>`) for a directory containing a `.ynh-plugin/plugin.json`. Bare names are rejected with a hint.

```bash
# Clean up
ynh uninstall local/base 2>/dev/null
```

## T17.10: Path resolution — id vs path

The `<harness>` argument is classified lexically:

- Starts with `./`, `../`, `/`, `~/`, or a Windows drive letter → filesystem path
- Slash-bearing without a path prefix → canonical id (e.g. `local/<name>`, `github.com/<org>/<repo>/<name>`)
- Anything else (bare names, `name@org/repo`) → rejected with a hint

```bash
# Filesystem path (must contain .ynh-plugin/plugin.json)
ynh include add ./my-harness github.com/acme/tools
ynh include add /tmp/ynh-tutorial-includes/my-harness github.com/acme/tools

# Canonical id (must be installed)
ynh include add local/my-harness github.com/acme/tools

# Bare name — rejected
ynh include add my-harness github.com/acme/tools
# Error: "my-harness" is not a valid harness id. Use a canonical id...
```

There is no ambiguity to resolve: the leading character of the ref decides the kind. Same rule across `info`, `uninstall`, `run`, `update`, `include`, `delegate`.

## Clean up

```bash
rm -rf /tmp/ynh-tutorial-includes
```

## What you learned

- `ynh include add <harness> <url>` — adds a Git include; accepts `--path`, `--pick`, `--ref`, `--replace`
- `ynh include remove <harness> <url>` — removes an include; uses `--path` to disambiguate monorepo entries
- `ynh include update <harness> <url>` — updates specific fields; only supplied flags change; uses `--from-path` to target one of multiple entries from the same URL; `--path` changes the path value
- `<harness>` is a **name** (installed harness) or a **path** (local directory); paths take a `/` or `.` prefix
- Installed harnesses are pre-fetched after `add` or `update` so `ynh run` works immediately — no separate `ynh update` needed
- `--pick` values must use the canonical `type/name` form (`skills/<name>`, `agents/<name>.md`, `rules/<name>.md`, `commands/<name>.md`); validated against the fetched repo before the manifest is touched
- If a bare basename or mistyped pick resolves to existing canonical entries, the error leads with a "Did you mean …?" hint (`--pick foo` → `did you mean skills/foo or agents/foo.md?`); otherwise the full available list is shown
- The `type/` prefix disambiguates a skill and a flat artifact that share a basename — both can be picked independently
- Mutations never happen if validation fails — the `.ynh-plugin/plugin.json` is only written after all checks pass

## Next

[Tutorial 3: Composition](tutorial/03-composition.md) — the `includes` field in detail, pick filtering, and allow-lists.
