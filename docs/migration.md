# Migration

## Manifest directory: `.ynh-plugin/` → `.agents/harness/`

The harness manifest directory moved from `.ynh-plugin/` to
`.agents/harness/`, beside the `.agents/skills/` and `.agents/plugins/`
directories other tools already read. Everything that lived in
`.ynh-plugin/` lives in `.agents/harness/` now: `plugin.json`,
`installed.json` and a registry's `marketplace.json`.

ynh **no longer reads `.ynh-plugin/`** in a tree you own (#404). Run
`ynd migrate` on any harness or registry that still has one.

- A harness or registry in your own tree that keeps its manifest only in
  `.ynh-plugin/` is an error. `ynh install`, `ynh run`, `ynh check`,
  `ynd validate`, `ynd preview` and `ynd export` all stop with a message that
  names the directory and the fix: `ynd migrate <path>`. Reading a tree never
  moves it, so the tree is exactly as it was.
- `ynd migrate <path>` renames `.ynh-plugin/` to `.agents/harness/` in every
  harness and registry under `<path>`, after confirming. `--dry-run` lists
  each move first. It is a single rename of the directory: nothing is copied
  or merged, and `installed.json` moves with the rest. It is the one command
  that still understands the old layout.
- A tree that has **both** directories is an error too, and the message names
  both. `ynd migrate` leaves it alone, and so does everything else: merging
  two manifest directories means choosing between files, which is yours to
  do. The same goes for a `.ynh-plugin` that is a symlink or holds one.
- A remote source that has not moved still works. The copies ynh makes are
  ynh's own, so it renames the manifest directory inside them the first time
  it loads one: the clone under `~/.ynh/cache/` (installs, includes,
  delegates, registries) and the install under `~/.ynh/harnesses/`,
  provenance included. The remote's own repository, and your working copy of
  it, are never touched.
- A harness installed from a local path is not copied, so its source tree is
  yours to migrate.
- Editing commands (`ynh include`, `ynh hook`, `ynh profile`, `ynh focus`,
  `ynh mcp`, `ynh delegate`) work on `.agents/harness/`.
- New manifests (`ynd create harness`, a synthesized manifest for a bare
  `AGENTS.md` directory, `ynd migrate` converting a `.harness.json`) are
  written to `.agents/harness/`.

To move a harness onto the documented layout:

```bash
ynd migrate --dry-run .
ynd migrate .
ynd validate .
```

Pass `.` explicitly: `ynd migrate` refuses a git working copy when no path
is given. Commit the rename afterwards; git records it as a move.

# Migrating from 0.1 to 0.2

ynh 0.2 is a breaking release that changes three things:

1. **Manifest format** — `.harness.json` → `.agents/harness/plugin.json`
2. **Registry format** — `registry.json` → `.agents/harness/marketplace.json`
3. **Storage layout** — flat `~/.ynh/harnesses/<name>/` → namespaced `~/.ynh/harnesses/<org>--<repo>/<name>/`

`.harness.json` and `registry.json` are converted only by `ynd migrate`,
which lists what it will change and asks first. ynh no longer reads either
format anywhere else, and no command converts one as a side effect of
reading it. This doc describes that one-time step and what differs in the new
format.

## The migration chain

All 0.1 → 0.2 conversions are handled by a filter chain in
`internal/migration/`. Loaders call the chain before reading; callers never
branch on old formats themselves. There are two chains, and they differ in
which trees they may rewrite:

- `MigrateChain()` is run only by `ynd migrate`, behind its confirmation. It
  is the only chain that rewrites a tree ynh did not install: your source
  trees and registries.
- `FormatChain()` is run by every command that reads a harness or registry
  (`ynh install`, `ynh run`, `ynh check`, `ynd validate`, `ynd preview`,
  `ynd export`, `ynd marketplace build`, registry fetches and the rest). It
  rewrites only installs under `~/.ynh/harnesses/`, which are ynh's own
  copies.

| Migrator | `ynd migrate` | Other commands, on a tree you own | Other commands, on a copy ynh owns: an install under `~/.ynh/harnesses/` or a clone under `~/.ynh/cache/` |
|---|---|---|---|
| `harness_format`: `.harness.json` → `.agents/harness/plugin.json` + `installed.json` | converts | refuses with the fix, changes nothing | converts on load |
| `registry_format`: `registry.json` → `.agents/harness/marketplace.json` | converts | refuses with the fix, changes nothing | converts on load |
| `manifest_dir`: `.ynh-plugin/` → `.agents/harness/` | renames | refuses with the fix, changes nothing | renames on load |

A command that meets a tree whose only manifest is `.harness.json` fails
with an error naming the directory and the fix:

```
/path/to/tree uses the legacy .harness.json manifest, which ynh no longer reads; convert it with: ynd migrate /path/to/tree
```

A legacy `registry.json` gets the same error with `registry.json` in place of
the manifest name. `ynd validate` reports it as an issue and exits 1. For a
cached clone of a remote harness or registry under `~/.ynh/cache/`, the error
says the manifest is the upstream's and asks its maintainer to run
`ynd migrate` and publish the result: converting the clone would be undone by
the next fetch.

Storage relocation (flat `~/.ynh/harnesses/<name>/` → canonical-id layout) is
handled by `ynh migrate`, which upgrades the `~/.ynh` home directory.

## For harness authors (source repos)

If you author harnesses, convert your source trees:

```bash
cd my-harnesses
ynd migrate .
```

`ynd migrate` runs the full migration chain against every matching directory.
It handles any registered migrator, so adding more migrators in future
releases does not require a new command. Use `--dry-run` to list what would
change first.

Idempotent: safe to run twice. No-op if the target already uses the new format.

### What changes

| Before | After |
|---|---|
| `my-harness/.harness.json` | `my-harness/.agents/harness/plugin.json` |
| `installed_from` field inside manifest | separate `.agents/harness/installed.json` |
| `$schema` ending `harness.schema.json`, or none | `$schema` ending `plugin.schema.json` |

The `installed_from` field no longer lives in the author-controlled manifest.
It moves to `.agents/harness/installed.json`, written by `ynh install` at install
time. Authors never write `installed.json`; add `.agents/harness/installed.json`
to `.gitignore` if you install your own harness locally for testing.

## For registry maintainers

Convert `registry.json` in place:

```bash
cd my-registry
ynd migrate .
```

### What changes

| Before (`registry.json`) | After (`.agents/harness/marketplace.json`) |
|---|---|
| `entries: [...]` | `harnesses: [...]` |
| Entry fields: `name`, `repo`, `path`, `keywords`, `version` | Entry fields: `name`, `source`, `keywords`, `version`, `description`, `author`, `category`, `tags` |
| `repo: "owner/repo"` | `source: {type: "github", repo: "owner/repo"}` |

The new `source` field supports four shapes: relative path string, GitHub
object (`type: github`), generic Git URL (`type: url`), and sparse-clone
monorepo entry (`type: git-subdir`). See `docs/marketplace.md` for the full
spec.

## For end users (installed harnesses)

You do not need to do anything for a harness ynh copied into
`~/.ynh/harnesses/`. Those copies are ynh's own, so the first load of a
0.1-installed one converts it:

```bash
ynh run david    # format migration runs once, then loads normally
```

A harness installed from a local path is not copied: ynh reads your source
tree, and refuses it until you run `ynd migrate` on it.

### Storage relocation (optional)

Installed harnesses remain in the flat layout until you reinstall or
explicitly relocate them. To move them to the namespaced layout:

```bash
ynh install david@eyelock/assistants   # reinstall under the namespace
```

You can also keep them flat — ynh loads both layouts. Flat installs get a
synthetic `local/unknown` namespace if provenance is missing.

## Backward compatibility timeline

- **0.2.x**: `.harness.json` and `registry.json` were converted transparently,
  in place, by any command that read them.
- **0.3.x to 0.7.x**: the transparent conversion stayed, past the 0.3 removal
  this page once promised, and kept rewriting users' source trees on read
  (#406).
- **Now**: only `ynd migrate` converts `.harness.json` and `registry.json`.
  Every other command refuses a tree that still uses them, names the fix and
  leaves the tree untouched; installs under `~/.ynh/harnesses/` are still
  converted on load.
- **0.14**: the `.ynh-plugin/` read fallback is gone (#404). A tree you own
  that still has one is refused with `ynd migrate <path>`; a clone in
  `~/.ynh/cache/`, like an install, is renamed on load so a remote that has
  not moved keeps working.

Dropping a format entirely means deleting its migrator file and unregistering
it from the chains. No other code changes: the pattern is designed for
surgical removal.
