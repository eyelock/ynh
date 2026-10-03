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
  `AGENTS.md` directory, the `.harness.json` migrator below) are written to
  `.agents/harness/`.
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

Most migration is transparent. This doc describes what happens automatically,
what requires a one-time manual step, and what differs in the new format.

## The migration chain

All 0.1 → 0.2 conversions are handled by a filter chain in
`internal/migration/`. Loaders call the chain before reading; callers never
branch on old formats themselves. You rarely need to run migration
commands directly — ynh handles it on first access.

Three migrators:

| Migrator | Triggered by | Converts |
|---|---|---|
| `harness_format` | Any harness load or install | `.harness.json` → `.agents/harness/plugin.json` + `.agents/harness/installed.json` |
| `registry_format` | Any registry fetch | `registry.json` → `.agents/harness/marketplace.json` |
| `harness_storage` | Explicit (install, relocate) | Flat `~/.ynh/harnesses/<name>/` → namespaced `<org>--<repo>/<name>/` |

Format migrations run transparently. Storage relocation is triggered
explicitly on install to avoid surprising callers that hold the flat path.

## For harness authors (source repos)

If you author harnesses, convert your source trees:

```bash
cd my-harnesses
ynd migrate .
```

`ynd migrate` runs the full migration chain against every matching directory.
It handles any registered migrator — adding more migrators in future
releases does not require a new command.

Idempotent: safe to run twice. No-op if the target already uses the new format.

### What changes

| Before | After |
|---|---|
| `my-harness/.harness.json` | `my-harness/.agents/harness/plugin.json` |
| `installed_from` field inside manifest | separate `.agents/harness/installed.json` |

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

You do not need to do anything. On first use of any 0.1-installed harness,
ynh runs the format migration transparently:

```bash
ynh run david    # format migration runs once, then loads normally
```

### Storage relocation (optional)

Installed harnesses remain in the flat layout until you reinstall or
explicitly relocate them. To move them to the namespaced layout:

```bash
ynh install david@eyelock/assistants   # reinstall under the namespace
```

You can also keep them flat — ynh loads both layouts. Flat installs get a
synthetic `local/unknown` namespace if provenance is missing.

## Backward compatibility timeline

- **0.2.x** — `.harness.json` and `registry.json` continue to work via the
  migration chain. Old files are converted transparently on first read.
- **0.3.x** — Legacy migrators are removed. `.harness.json` and
  `registry.json` are no longer recognized. Run `ynd migrate` before
  upgrading to 0.3.

Dropping support in 0.3 means removing the migrator files and unregistering
them from `DefaultChain()`. No other code changes — the pattern is designed
for surgical removal.
