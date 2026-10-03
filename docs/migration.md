# Migration

## Manifest directory: `.ynh-plugin/` → `.agents/harness/`

The harness manifest directory moved from `.ynh-plugin/` to
`.agents/harness/`, beside the `.agents/skills/` and `.agents/plugins/`
directories other tools already read. Everything that lived in
`.ynh-plugin/` lives in `.agents/harness/` now: `plugin.json`,
`installed.json` and a registry's `marketplace.json`.

`.ynh-plugin/` is **deprecated**. It is still read, so nothing breaks today,
but the fallback will be removed in a later release. Move your harnesses with
`ynd migrate` before then.

- ynh reads `.agents/harness/` first and falls back to `.ynh-plugin/`. A
  harness or registry that has not moved keeps working, and reading one in
  your own tree never moves it: `ynh run`, `ynh check` and `ynd validate`
  leave `.ynh-plugin/` where it is.
- Every command that reads a manifest from `.ynh-plugin/` prints a
  deprecation warning on stderr, once per harness, naming the
  `ynd migrate` command that fixes it. Stdout, including `--format json`
  output, is unaffected.
- `ynd migrate <path>` renames `.ynh-plugin/` to `.agents/harness/` in every
  harness and registry under `<path>`, after confirming. `--dry-run` lists
  each move first. It is a single rename of the directory: nothing is copied
  or merged, and `installed.json` moves with the rest.
- `ynd migrate` leaves a tree alone, and says so, when `.ynh-plugin` is a
  symlink or holds one, or when `.agents/harness/` already exists. Merging
  two manifest directories means choosing between files, which is yours to
  do.
- Installs ynh copied under `~/.ynh/harnesses/` are ynh's own and are moved
  the next time ynh loads them, provenance included. A harness installed from
  a local path is not copied, so its source tree is yours to migrate. A
  cached checkout of a remote harness is the maintainer's: the warning asks
  them to run `ynd migrate` and publish the result.
- Editing commands (`ynh include`, `ynh hook`, `ynh profile`, `ynh focus`,
  `ynh mcp`, `ynh delegate`) rewrite the manifest where it already is.
- New manifests (`ynd create harness`, a synthesized manifest for a bare
  `AGENTS.md` directory, `ynd migrate` converting a `.harness.json`) are
  written to `.agents/harness/`.
- The directory that holds `plugin.json` is the harness's manifest
  directory, and `installed.json` and `marketplace.json` are read from and
  written to that same directory. A harness never reads half its manifest
  from each.
- If both directories hold a `plugin.json`, `.agents/harness/` wins.
  `ynd validate` reports the shadowed `.ynh-plugin/` copy so it cannot be
  edited by mistake, and any other manifest file split away from
  `plugin.json`, naming each one.

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

| Migrator | `ynd migrate` | Other commands, on a tree you own or a cached clone | Other commands, on an install under `~/.ynh/harnesses/` |
|---|---|---|---|
| `harness_format`: `.harness.json` → `.agents/harness/plugin.json` + `installed.json` | converts | refuses with the fix, changes nothing | converts on load |
| `registry_format`: `registry.json` → `.agents/harness/marketplace.json` | converts | refuses with the fix, changes nothing | converts on load |
| `manifest_dir`: `.ynh-plugin/` → `.agents/harness/` | renames | reads through the fallback, with a deprecation warning | renames on load |

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
  converted on load. `.ynh-plugin/` is still read, with a deprecation
  warning, until its fallback is removed in a later release.

Dropping a format entirely means deleting its migrator file and unregistering
it from the chains. No other code changes: the pattern is designed for
surgical removal.
