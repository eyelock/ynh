# Migrating from 0.1 to 0.2

ynh 0.2 is a breaking release that changes three things:

1. **Manifest format** — `.harness.json` → `.ynh-plugin/plugin.json`
2. **Registry format** — `registry.json` → `.ynh-plugin/marketplace.json`
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
| `harness_format` | Any harness load or install | `.harness.json` → `.ynh-plugin/plugin.json` + `.ynh-plugin/installed.json` |
| `registry_format` | Any registry fetch | `registry.json` → `.ynh-plugin/marketplace.json` |
| `harness_storage` | Explicit (install, relocate) | Flat `~/.ynh/harnesses/<name>/` → namespaced `<org>--<repo>/<name>/` |

Format migrations run transparently. Storage relocation is triggered
explicitly on install to avoid surprising callers that hold the flat path.

## For harness authors (source repos)

If you author harnesses, convert your source trees:

```bash
cd my-harnesses
ynd migrate -r .
```

`ynd migrate` runs the full migration chain against every matching directory.
It handles any registered migrator — adding more migrators in future
releases does not require a new command.

Idempotent: safe to run twice. No-op if the target already uses the new format.

### What changes

| Before | After |
|---|---|
| `my-harness/.harness.json` | `my-harness/.ynh-plugin/plugin.json` |
| `installed_from` field inside manifest | separate `.ynh-plugin/installed.json` |

The `installed_from` field no longer lives in the author-controlled manifest.
It moves to `.ynh-plugin/installed.json`, written by `ynh install` at install
time. Authors never write `installed.json`; add `.ynh-plugin/installed.json`
to `.gitignore` if you install your own harness locally for testing.

## For registry maintainers

Convert `registry.json` in place:

```bash
cd my-registry
ynd migrate .
```

### What changes

| Before (`registry.json`) | After (`.ynh-plugin/marketplace.json`) |
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
  `registry.json` are no longer recognized. Run `ynd migrate -r` before
  upgrading to 0.3.

Dropping support in 0.3 means removing the migrator files and unregistering
them from `DefaultChain()`. No other code changes — the pattern is designed
for surgical removal.
