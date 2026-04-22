# Tutorial 18: Namespacing & Migration

Install the same-named harness from multiple registries using `@` syntax, and
migrate legacy `.harness.json` / `registry.json` files to the 0.2 format.

## Prerequisites

```bash
# Clean up from any previous run
rm -rf /tmp/ynh-ns-tutorial
mkdir -p /tmp/ynh-ns-tutorial
ynh registry remove /tmp/ynh-ns-tutorial/reg-a 2>/dev/null
ynh registry remove /tmp/ynh-ns-tutorial/reg-b 2>/dev/null
ynh uninstall david 2>/dev/null
ynh uninstall david@acme/tools 2>/dev/null
ynh uninstall david@eyelock/assistants 2>/dev/null
```

## T18.1: Create two registries with overlapping names

Two different authors both publish a harness called `david`. In 0.1 this would
silently conflict. In 0.2 each harness lives under its own namespace.

```bash
# Registry A — eyelock/assistants
mkdir -p /tmp/ynh-ns-tutorial/reg-a/harnesses/david/.ynh-plugin
cat > /tmp/ynh-ns-tutorial/reg-a/harnesses/david/.ynh-plugin/plugin.json << 'EOF'
{
  "name": "david",
  "version": "0.1.0",
  "description": "Eyelock's development harness"
}
EOF

mkdir -p /tmp/ynh-ns-tutorial/reg-a/.ynh-plugin
cat > /tmp/ynh-ns-tutorial/reg-a/.ynh-plugin/marketplace.json << 'EOF'
{
  "name": "eyelock-assistants",
  "owner": {"name": "eyelock"},
  "harnesses": [
    {"name": "david", "source": "./harnesses/david"}
  ]
}
EOF

(cd /tmp/ynh-ns-tutorial/reg-a && git init -q && git add . && git commit -q -m init)

# Registry B — acme/tools
mkdir -p /tmp/ynh-ns-tutorial/reg-b/harnesses/david/.ynh-plugin
cat > /tmp/ynh-ns-tutorial/reg-b/harnesses/david/.ynh-plugin/plugin.json << 'EOF'
{
  "name": "david",
  "version": "2.0.0",
  "description": "Acme's David harness (different author)"
}
EOF

mkdir -p /tmp/ynh-ns-tutorial/reg-b/.ynh-plugin
cat > /tmp/ynh-ns-tutorial/reg-b/.ynh-plugin/marketplace.json << 'EOF'
{
  "name": "acme-tools",
  "owner": {"name": "acme"},
  "harnesses": [
    {"name": "david", "source": "./harnesses/david"}
  ]
}
EOF

(cd /tmp/ynh-ns-tutorial/reg-b && git init -q && git add . && git commit -q -m init)
```

## T18.2: Register both

```bash
ynh registry add /tmp/ynh-ns-tutorial/reg-a
ynh registry add /tmp/ynh-ns-tutorial/reg-b
```

Expected:
```
Added registry: /tmp/ynh-ns-tutorial/reg-a
Added registry: /tmp/ynh-ns-tutorial/reg-b
```

## T18.3: Understand namespaces

The namespace comes from the registry URL, not the user-chosen name. For local
paths like these, it's the last two path segments of the source URL. Real GitHub
registries would derive namespaces like `eyelock/assistants` and `acme/tools`.

Run a search to see both `david` entries, each from its own registry:

```bash
ynh search david
```

Expected (two rows): one from each registry.

## T18.4: Disambiguate install with @ syntax

Attempting plain install is ambiguous:

```bash
ynh install david 2>&1 | head -3
```

Expected: an error listing both candidates.

Install from a specific registry using `name@org/repo`:

```bash
ynh install "david@ynh-ns-tutorial/reg-a"
ynh install "david@ynh-ns-tutorial/reg-b"
```

> **Note:** For GitHub-hosted registries, the @ syntax is `name@owner/repo`
> (for example `david@eyelock/assistants`). Local-path registries use the last
> two path segments as the namespace.

## T18.5: List both — namespaces shown

```bash
ynh ls
```

Both harnesses appear, each under its namespace. The `ynh/bin/david` short
launcher is only created when the name is unambiguous — installing a second
`david` removes the short launcher and requires the namespaced launcher
(`ynh-ns-tutorial--reg-a--david`) or `ynh run david@<namespace>`.

## T18.6: Migrate a legacy harness with ynd migrate

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
ynd migrate /tmp/ynh-ns-tutorial/legacy
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

`ynd migrate` runs the migration filter chain — it handles any registered
migrator. Adding a new format migrator in future releases does not require a
new command.

## T18.7: Recursive migration

Create multiple legacy harnesses at once, then migrate the whole tree:

```bash
mkdir -p /tmp/ynh-ns-tutorial/bulk/h1 /tmp/ynh-ns-tutorial/bulk/h2
cat > /tmp/ynh-ns-tutorial/bulk/h1/.harness.json << 'EOF'
{"name":"h1","version":"0.1.0"}
EOF
cat > /tmp/ynh-ns-tutorial/bulk/h2/.harness.json << 'EOF'
{"name":"h2","version":"0.1.0"}
EOF

ynd migrate -r /tmp/ynh-ns-tutorial/bulk
```

Expected:
```
Migrated /tmp/ynh-ns-tutorial/bulk/h1
  harness format: .harness.json → .ynh-plugin/plugin.json
Migrated /tmp/ynh-ns-tutorial/bulk/h2
  harness format: .harness.json → .ynh-plugin/plugin.json
Migrated 2 director(ies).
```

## T18.8: Transparent migration on use

Legacy harnesses do not strictly require `ynd migrate`. ynh runs the migration
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

`ynd migrate` is still useful when you want to convert a whole tree
intentionally — for example when cleaning up a source repo before publishing.

## Clean up

```bash
ynh uninstall david@ynh-ns-tutorial/reg-a 2>/dev/null
ynh uninstall david@ynh-ns-tutorial/reg-b 2>/dev/null
ynh uninstall legacy-demo transparent h1 h2 2>/dev/null
ynh registry remove /tmp/ynh-ns-tutorial/reg-a 2>/dev/null
ynh registry remove /tmp/ynh-ns-tutorial/reg-b 2>/dev/null
rm -rf /tmp/ynh-ns-tutorial
```

## What You Learned

- Namespaces are derived from the registry URL and are stable across machines
- `name@org/repo` syntax disambiguates between same-named harnesses
- `ynh ls` shows namespace information alongside each installed harness
- Short launchers (`~/.ynh/bin/<name>`) exist only when the short name is
  unambiguous; namespaced launchers always exist
- `ynd migrate` converts `.harness.json` → `.ynh-plugin/plugin.json` and
  `registry.json` → `.ynh-plugin/marketplace.json` in place
- Migration runs transparently on install and load; `ynd migrate` is the
  explicit tool for bulk conversion and source-repo cleanup

## Next

See [docs/namespacing.md](../namespacing.md) for the full @ syntax reference
and [docs/migration.md](../migration.md) for a complete 0.1 → 0.2 migration
guide.
