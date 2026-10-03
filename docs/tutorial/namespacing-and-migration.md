# Namespacing & Migration

Install same-named harnesses from different sources without collision using
canonical ids, and convert legacy `.harness.json` / `registry.json` files to
the 0.2 format with `ynd migrate`, the only command that reads them.

## Prerequisites

```bash
# Clean up from any previous run
rm -rf /tmp/ynh-ns-tutorial
mkdir -p /tmp/ynh-ns-tutorial
ynh registry remove /tmp/ynh-ns-tutorial/reg-a 2>/dev/null
ynh registry remove /tmp/ynh-ns-tutorial/reg-b 2>/dev/null
ynh uninstall github.com/eyelock/assistants/david 2>/dev/null
ynh uninstall github.com/acme/tools/david 2>/dev/null
ynh uninstall local/david 2>/dev/null
```

## Canonical ids — the new identity model

Every installed harness has a **canonical id**: a host-prefixed identifier
that uniquely names it across all sources. Two forms exist:

| Source                          | Canonical id form                       | Example                                    |
|---------------------------------|-----------------------------------------|--------------------------------------------|
| Remote registry / Git URL       | `<host>/<org>/<repo>/<name>`            | `github.com/eyelock/assistants/david`      |
| Local path / local-only source  | `local/<name>`                          | `local/my-harness`                         |

The canonical id is the only identifier accepted by harness-targeting commands
(`info`, `uninstall`, `run`, `update`). Bare names like `david` are rejected
with an error pointing you at `ynh ls`. The on-disk install directory is the
canonical id with `/` replaced by `--` (e.g.
`~/.ynh/harnesses/github.com--eyelock--assistants--david`).

Two `david` harnesses from different remote sources can coexist because their
canonical ids differ:

| Source                              | Canonical id                          |
|-------------------------------------|---------------------------------------|
| `github.com/eyelock/assistants`     | `github.com/eyelock/assistants/david` |
| `github.com/acme/tools`             | `github.com/acme/tools/david`         |

## Demo — two registries, two `david` harnesses

> **Network required for the install step.** The two demo registries below
> are local file:// paths (so the registry index works offline), but they
> both *point at* real `github.com` repos so each install resolves to a
> distinct canonical id. If you are offline, read [Search returns both entries](#search-returns-both-entries) through [Inspect by canonical id](#inspect-by-canonical-id) as a
> walkthrough rather than running the commands.

```bash
# Registry A — points at github.com/eyelock/assistants
mkdir -p /tmp/ynh-ns-tutorial/reg-a/.agents/harness
cat > /tmp/ynh-ns-tutorial/reg-a/.agents/harness/marketplace.json << 'EOF'
{
  "$schema": "https://eyelock.github.io/ynh/schema/marketplace.schema.json",
  "name": "eyelock-registry",
  "owner": {"name": "eyelock-registry"},
  "harnesses": [
    {
      "name": "david",
      "description": "Eyelock's development harness",
      "version": "0.1.0",
      "source": {
        "type": "github",
        "repo": "github.com/eyelock/assistants",
        "path": "ynh/david"
      }
    }
  ]
}
EOF
(cd /tmp/ynh-ns-tutorial/reg-a && git init -q && git add . && git commit -q -m init)

# Registry B — points at a hypothetical github.com/acme/tools
# (For this tutorial we re-use eyelock/assistants. The point is that the
# canonical id is derived from the source repo, not the registry that listed
# it.)
mkdir -p /tmp/ynh-ns-tutorial/reg-b/.agents/harness
cat > /tmp/ynh-ns-tutorial/reg-b/.agents/harness/marketplace.json << 'EOF'
{
  "$schema": "https://eyelock.github.io/ynh/schema/marketplace.schema.json",
  "name": "acme-registry",
  "owner": {"name": "acme-registry"},
  "harnesses": [
    {
      "name": "david",
      "description": "A different David harness",
      "version": "0.1.0",
      "source": {
        "type": "github",
        "repo": "github.com/eyelock/assistants",
        "path": "ynh/david"
      }
    }
  ]
}
EOF
(cd /tmp/ynh-ns-tutorial/reg-b && git init -q && git add . && git commit -q -m init)

ynh registry add /tmp/ynh-ns-tutorial/reg-a
ynh registry add /tmp/ynh-ns-tutorial/reg-b
```

> **Offline-only registries collide.** If both registries pointed at *local*
> paths instead of remote repos, both `david` installs would share the
> canonical id `local/david` and the second install would silently overwrite
> the first. Canonical-id namespacing only protects against name collisions
> between remote sources. Use distinct repos (real GitHub orgs are easiest)
> when you need two same-named harnesses to coexist.

## Search returns both entries

```bash
ynh search david
```

Expected: two rows, one per registry. The `FROM` column distinguishes them.

## Disambiguate install with `@<registry>`

A bare `ynh install david` is ambiguous — both registries match:

```bash
ynh install david 2>&1 | head -3
```

Expected: an error listing both candidates.

The `name@<registry>` form picks one registry's entry. The chosen entry's
*source repo* (not the registry) determines the canonical id:

```bash
ynh install david@eyelock-registry
```

Expected:
```
Installed harness "david"
  Location: /Users/<you>/.ynh/harnesses/github.com--eyelock--assistants--david
  Launcher: /Users/<you>/.ynh/bin/david
  Vendor:   claude
```

The canonical id is `github.com/eyelock/assistants/david`. The `@<registry>`
syntax exists only for `ynh install` to resolve the registry lookup; once
installed, you address the harness via its canonical id.

## Inspect by canonical id

```bash
ynh ls --format json | jq -r '.harnesses[].id'
```

Expected: `github.com/eyelock/assistants/david`.

```bash
ynh info github.com/eyelock/assistants/david --format json | jq -r '.harness.path'
```

Expected: a path containing `github.com--eyelock--assistants--david`.

Bare names are rejected:

```bash
ynh info david 2>&1
# Expected: Error: "david" is not a valid harness id. Use a canonical id ...
```

## Uninstall by canonical id

```bash
ynh uninstall github.com/eyelock/assistants/david
```

A short launcher (`~/.ynh/bin/david`) is created when the short name is
unambiguous; if you install a second `david` from another source, the short
launcher is removed and you invoke the harness with `ynh run <canonical-id>`.

## Migrate a legacy harness with `ynd migrate`

Create a harness in the legacy 0.1 format:

```bash
mkdir -p /tmp/ynh-ns-tutorial/legacy
cat > /tmp/ynh-ns-tutorial/legacy/.harness.json << 'EOF'
{
  "name": "legacy-demo",
  "version": "0.1.0",
  "description": "Legacy format harness"
}
EOF
```

ynh no longer reads `.harness.json`. A command that reads a harness refuses
this tree, names the fix, and leaves the tree exactly as it was:

```bash
ynd validate /tmp/ynh-ns-tutorial/legacy
```

Expected: the harness is reported `INVALID` (exit 1) with this issue:
```
  - /tmp/ynh-ns-tutorial/legacy uses the legacy .harness.json manifest, which ynh no longer reads; convert it with: ynd migrate /tmp/ynh-ns-tutorial/legacy
```

`ynd preview`, `ynd export` and `ynh install` refuse it the same way, and so
does `ynh run` started from inside the directory:

```bash
ynh install /tmp/ynh-ns-tutorial/legacy
```

Expected (exit 1):
```
Error: /tmp/ynh-ns-tutorial/legacy uses the legacy .harness.json manifest, which ynh no longer reads; convert it with: ynd migrate /tmp/ynh-ns-tutorial/legacy
```

```bash
find /tmp/ynh-ns-tutorial/legacy -type f | sort
```

Expected, unchanged:
```
/tmp/ynh-ns-tutorial/legacy/.harness.json
```

Convert it with `ynd migrate`. It deletes `.harness.json`, so it lists what it
will touch and asks first; pass `-y` to skip the prompt (also implied by
`$YNH_YES` or CI). Without it, a scripted run declines and exits non-zero. Use
`--dry-run` to list what would be migrated and change nothing.

```bash
ynd migrate -y /tmp/ynh-ns-tutorial/legacy
```

Expected:
```
1 director(ies) would be migrated under /tmp/ynh-ns-tutorial/legacy:
  /tmp/ynh-ns-tutorial/legacy
    harness format: .harness.json → .agents/harness/plugin.json
Migrated /tmp/ynh-ns-tutorial/legacy
  harness format: .harness.json → .agents/harness/plugin.json
Migrated 1 director(ies).
```

Verify the result:

```bash
find /tmp/ynh-ns-tutorial/legacy -type f | sort
```

Expected:
```
/tmp/ynh-ns-tutorial/legacy/.agents/harness/plugin.json
```

The harness now validates and loads, so the install that was refused
succeeds. The converted `plugin.json` also gains the `$schema` a 0.1 manifest
could leave out:

```bash
ynd validate /tmp/ynh-ns-tutorial/legacy
ynh install /tmp/ynh-ns-tutorial/legacy
```

```
Installed harness "legacy-demo"
  Location: /tmp/ynh-ns-tutorial/legacy
  Launcher: /Users/<you>/.ynh/bin/legacy-demo
```

`ynd migrate` runs the migration filter chain, so it handles any registered
migrator. Adding a new format migrator in future releases does not require a
new command.

## Recursive migration

Create multiple legacy harnesses at once, then migrate the whole tree:

```bash
mkdir -p /tmp/ynh-ns-tutorial/bulk/h1 /tmp/ynh-ns-tutorial/bulk/h2
cat > /tmp/ynh-ns-tutorial/bulk/h1/.harness.json << 'EOF'
{"name":"h1","version":"0.1.0"}
EOF
cat > /tmp/ynh-ns-tutorial/bulk/h2/.harness.json << 'EOF'
{"name":"h2","version":"0.1.0"}
EOF

ynd migrate -y /tmp/ynh-ns-tutorial/bulk
```

Expected:
```
2 director(ies) would be migrated under /tmp/ynh-ns-tutorial/bulk:
  /tmp/ynh-ns-tutorial/bulk/h1
    harness format: .harness.json → .agents/harness/plugin.json
  /tmp/ynh-ns-tutorial/bulk/h2
    harness format: .harness.json → .agents/harness/plugin.json
Migrated /tmp/ynh-ns-tutorial/bulk/h1
  harness format: .harness.json → .agents/harness/plugin.json
Migrated /tmp/ynh-ns-tutorial/bulk/h2
  harness format: .harness.json → .agents/harness/plugin.json
Migrated 2 director(ies).
```

## Transparent migration on use

There is none any more. Earlier releases converted a legacy tree silently
whenever a command read it, so `ynd validate` or `ynh install` left a working
copy with a deleted `.harness.json` and a new `.agents/harness/` nobody asked
for. Now only `ynd migrate`, which asks first, rewrites a tree you own.

A remote harness or registry still on a legacy manifest is refused too. ynh
does not convert its cached copy, because the next fetch would undo it; the
error says the manifest is the upstream's and asks its maintainer to run
`ynd migrate` and publish the result.

The one exception is ynh's own install directory: a copy that a very old ynh
installed under `~/.ynh/harnesses/` is converted the next time it is loaded,
since nobody else's files change.

## `ynh migrate` — upgrade `~/.ynh` schema

`ynd migrate` is for harness *source trees*. The companion command
`ynh migrate` upgrades the on-disk layout of `~/.ynh` itself (the home
directory schema). Run it after upgrading ynh across a major version:

```bash
ynh migrate
```

Expected on a current installation:
```
ynh home is already at schema version 3 — nothing to migrate.
```

When an upgrade is needed, the command rewrites the harness directory layout
(adding canonical-id namespaces under `~/.ynh/harnesses/`) and updates
`config.json` in place. It is idempotent — re-running it on a current home
does nothing.

## `ynh quarantine` — recover from broken installs

If a harness install fails partway through, or a manifest is missing required
fields, ynh moves the directory into a quarantine area instead of leaving a
broken install on disk. Inspect the quarantine:

```bash
ynh quarantine list
```

Expected: `No quarantined entries.` when nothing has been quarantined. Otherwise one row per entry:
```
NAME                  ORIGINAL PATH                                 REASON
broken-thing          /Users/<you>/.ynh/harnesses/broken-thing      plugin manifest has no name
```

Subcommands:

| Command | Purpose |
|---------|---------|
| `ynh quarantine list` | Show all quarantined entries with their reason |
| `ynh quarantine restore <name>` | Move an entry back into `~/.ynh/harnesses/` (you will likely need to fix the manifest first) |
| `ynh quarantine drop <name>` | Permanently delete a quarantined entry |

Quarantine is a safety net — a broken harness is preserved off to the side
while everything else keeps working. You decide whether to fix it (`restore`)
or delete it (`drop`).

## Clean up

```bash
ynh uninstall github.com/eyelock/assistants/david 2>/dev/null
ynh uninstall local/legacy-demo 2>/dev/null
ynh uninstall local/h1 2>/dev/null
ynh uninstall local/h2 2>/dev/null
ynh registry remove /tmp/ynh-ns-tutorial/reg-a 2>/dev/null
ynh registry remove /tmp/ynh-ns-tutorial/reg-b 2>/dev/null
rm -rf /tmp/ynh-ns-tutorial
```

## What You Learned

- The canonical id (`<host>/<org>/<repo>/<name>` or `local/<name>`) is the
  single identity form for harnesses; bare names are rejected by
  harness-targeting commands.
- Canonical-id namespacing prevents collisions between same-named harnesses
  from different remote sources; two local-path installs both land at
  `local/<name>` and will collide.
- `name@<registry>` is an `ynh install`-only disambiguator; once installed,
  use the canonical id.
- Short launchers (`~/.ynh/bin/<name>`) are created opportunistically when
  the short name is unambiguous; otherwise invoke the harness via
  `ynh run <canonical-id>`.
- `ynd migrate` is the only command that converts a 0.1 harness-source
  directory to the 0.2 format (`.harness.json` → `.agents/harness/plugin.json`,
  `registry.json` → `.agents/harness/marketplace.json`). Every other command
  refuses a legacy tree with that fix and leaves it untouched.
- `ynh migrate` upgrades the `~/.ynh` home directory schema after a major
  ynh upgrade.
- `ynh quarantine list/restore/drop` manages harnesses set aside because
  their install was broken or their manifest was invalid.

See [docs/namespacing.md](../namespacing.md) for the full canonical-id
reference and [docs/migration.md](../migration.md) for a complete 0.1 → 0.2
migration guide.
