# Tutorial 18: Namespacing & Migration

Install same-named harnesses from different sources without collision using
canonical ids, and migrate legacy `.harness.json` / `registry.json` files to
the 0.2 format.

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

## T18.1: Canonical ids — the new identity model

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

## T18.2: Demo — two registries, two `david` harnesses

> **Network required for the install step.** The two demo registries below
> are local file:// paths (so the registry index works offline), but they
> both *point at* real `github.com` repos so each install resolves to a
> distinct canonical id. If you are offline, read T18.3 to T18.5 as a
> walkthrough rather than running the commands.

```bash
# Registry A — points at github.com/eyelock/assistants
mkdir -p /tmp/ynh-ns-tutorial/reg-a
cat > /tmp/ynh-ns-tutorial/reg-a/registry.json << 'EOF'
{
  "name": "eyelock-registry",
  "entries": [
    {
      "name": "david",
      "description": "Eyelock's development harness",
      "repo": "github.com/eyelock/assistants",
      "path": "ynh/david",
      "vendors": ["claude"],
      "version": "0.1.0"
    }
  ]
}
EOF
(cd /tmp/ynh-ns-tutorial/reg-a && git init -q && git add . && git commit -q -m init)

# Registry B — points at a hypothetical github.com/acme/tools
# (For this tutorial we re-use eyelock/assistants. The point is that the
# canonical id is derived from the source repo, not the registry that listed
# it.)
mkdir -p /tmp/ynh-ns-tutorial/reg-b
cat > /tmp/ynh-ns-tutorial/reg-b/registry.json << 'EOF'
{
  "name": "acme-registry",
  "entries": [
    {
      "name": "david",
      "description": "A different David harness",
      "repo": "github.com/eyelock/assistants",
      "path": "ynh/david",
      "vendors": ["claude"],
      "version": "0.1.0"
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

## T18.3: Search returns both entries

```bash
ynh search david
```

Expected: two rows, one per registry. The `FROM` column distinguishes them.

## T18.4: Disambiguate install with `@<registry>`

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

## T18.5: Inspect by canonical id

```bash
ynh ls --format json | jq -r '.harnesses[].id'
```

Expected: `github.com/eyelock/assistants/david`.

```bash
ynh info github.com/eyelock/assistants/david --format json | jq -r '.path'
```

Expected: a path containing `github.com--eyelock--assistants--david`.

Bare names are rejected:

```bash
ynh info david 2>&1
# Expected: Error: "david" is not a valid harness id. Use a canonical id ...
```

## T18.6: Uninstall by canonical id

```bash
ynh uninstall github.com/eyelock/assistants/david
```

A short launcher (`~/.ynh/bin/david`) is created when the short name is
unambiguous; if you install a second `david` from another source, the short
launcher is removed and you invoke the harness with `ynh run <canonical-id>`.

## T18.7: Migrate a legacy harness with `ynd migrate-manifest`

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

Migrate it in place:

```bash
ynd migrate-manifest /tmp/ynh-ns-tutorial/legacy
```

Expected:
```
Migrated /tmp/ynh-ns-tutorial/legacy
  harness format: .harness.json → .ynh-plugin/plugin.json
Migrated 1 director(ies).
```

Verify the result:

```bash
find /tmp/ynh-ns-tutorial/legacy -type f | sort
```

Expected:
```
/tmp/ynh-ns-tutorial/legacy/.ynh-plugin/plugin.json
```

`ynd migrate-manifest` runs the migration filter chain — it handles any registered
migrator. Adding a new format migrator in future releases does not require a
new command.

## T18.8: Recursive migration

Create multiple legacy harnesses at once, then migrate the whole tree:

```bash
mkdir -p /tmp/ynh-ns-tutorial/bulk/h1 /tmp/ynh-ns-tutorial/bulk/h2
cat > /tmp/ynh-ns-tutorial/bulk/h1/.harness.json << 'EOF'
{"name":"h1","version":"0.1.0"}
EOF
cat > /tmp/ynh-ns-tutorial/bulk/h2/.harness.json << 'EOF'
{"name":"h2","version":"0.1.0"}
EOF

ynd migrate-manifest /tmp/ynh-ns-tutorial/bulk
```

Expected:
```
Migrated /tmp/ynh-ns-tutorial/bulk/h1
  harness format: .harness.json → .ynh-plugin/plugin.json
Migrated /tmp/ynh-ns-tutorial/bulk/h2
  harness format: .harness.json → .ynh-plugin/plugin.json
Migrated 2 director(ies).
```

## T18.9: Transparent migration on use

Legacy harnesses do not strictly require `ynd migrate-manifest`. ynh runs the migration
chain automatically whenever a harness is loaded or installed, so an
unmigrated 0.1 harness still works:

```bash
mkdir -p /tmp/ynh-ns-tutorial/transparent
cat > /tmp/ynh-ns-tutorial/transparent/.harness.json << 'EOF'
{"name":"transparent","version":"0.1.0"}
EOF

ynh install /tmp/ynh-ns-tutorial/transparent
```

The install succeeds and the installed copy uses the 0.2 format. The source
directory is also migrated in place (the chain runs before the copy).

`ynd migrate-manifest` is still useful when you want to convert a whole tree
intentionally — for example when cleaning up a source repo before publishing.

## T18.10: `ynh migrate` — upgrade `~/.ynh` schema

`ynd migrate-manifest` is for harness *source trees*. The companion command
`ynh migrate` upgrades the on-disk layout of `~/.ynh` itself (the home
directory schema). Run it after upgrading ynh across a major version:

```bash
ynh migrate
```

Expected on a current installation:
```
ynh home is already at schema version 2 — nothing to migrate.
```

When an upgrade is needed, the command rewrites the harness directory layout
(adding canonical-id namespaces under `~/.ynh/harnesses/`) and updates
`config.json` in place. It is idempotent — re-running it on a current home
does nothing.

## T18.11: `ynh quarantine` — recover from broken installs

If a harness install fails partway through, or a manifest is missing required
fields, ynh moves the directory into a quarantine area instead of leaving a
broken install on disk. Inspect the quarantine:

```bash
ynh quarantine list
```

Expected (one row per quarantined entry, or an empty table):
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
ynh uninstall local/transparent 2>/dev/null
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
- `ynd migrate-manifest` converts harness-source directories from 0.1 to 0.2 format
  (disambiguated from `ynh migrate`, which upgrades the `~/.ynh` home schema)
  (`.harness.json` → `.ynh-plugin/plugin.json`, `registry.json` →
  `.ynh-plugin/marketplace.json`).
- `ynh migrate` upgrades the `~/.ynh` home directory schema after a major
  ynh upgrade.
- `ynh quarantine list/restore/drop` manages harnesses set aside because
  their install was broken or their manifest was invalid.

## Next

[Tutorial 18: Sensors](tutorial/19-sensors.md) — declare observation surfaces a loop driver consumes.

See [docs/namespacing.md](../namespacing.md) for the full canonical-id
reference and [docs/migration.md](../migration.md) for a complete 0.1 → 0.2
migration guide.
