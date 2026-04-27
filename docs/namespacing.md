# Namespacing

> **Version:** 0.2+. For pre-0.2 single-name installs, see `docs/migration.md`.

ynh 0.2 introduces namespaced harness storage and an `@` syntax for unambiguous
references across registries.

## Why namespaces

A registry publishes harnesses by name. Two registries can each publish a
harness called `david` — they are different harnesses with different authors,
different content, different update streams. Before 0.2, `ynh install david`
would silently pick the first match. After 0.2, `ynh install david@eyelock/assistants`
resolves the exact one.

## Namespace format

A namespace is derived from the Git URL of the registry that publishes a
harness. It is always `<owner>/<repo>`:

| Registry URL | Namespace |
|---|---|
| `https://github.com/eyelock/assistants` | `eyelock/assistants` |
| `git@github.com:eyelock/assistants.git` | `eyelock/assistants` |
| `https://gitlab.com/myorg/tools` | `myorg/tools` |
| Local path `/Users/david/harnesses` | `david/harnesses` (last two segments) |

Derivation is URL-based (not registry-name-based) so the namespace is stable
across machines and across registry renames.

## `@` syntax

Every command that accepts a harness name also accepts `name@org/repo`:

```bash
ynh install david@eyelock/assistants
ynh run david@eyelock/assistants
ynh run david@eyelock/assistants --profile staging
ynh info david@eyelock/assistants
ynh uninstall david@eyelock/assistants
ynh update david@eyelock/assistants
```

### Disambiguation rules (install)

Applied in order:

1. Local filesystem path (`./foo` or `/abs/path`) → install from local dir
2. Git SSH URL (`git@...`) → install directly
3. Git HTTPS URL (`https://...`) → install directly
4. `name@org/repo` → look up `name` in the registry with matching namespace
5. Git shorthand (`github.com/owner/repo`) → install as Git URL
6. Plain `name` → search registries; error if ambiguous

### Unambiguous short names

If only one installed harness has a given name across all namespaces, the
short name still works for `run`, `info`, `uninstall`, and `update`:

```bash
ynh run david                     # works when david is unambiguous
ynh run david@eyelock/assistants  # always unambiguous
```

If two registries both have `david` installed, the short form errors with a
list of candidates.

## Storage layout

Installed harnesses live at `~/.ynh/harnesses/<org>--<repo>/<name>/`:

```
~/.ynh/harnesses/
  eyelock--assistants/
    david/
      .ynh-plugin/
        plugin.json
        installed.json
      skills/
    researcher/
      .ynh-plugin/
        plugin.json
  myorg--tools/
    formatter/
      .ynh-plugin/
        plugin.json
```

The `--` separator is the filesystem-safe encoding of the `/` in the
namespace. Namespace plus name is a globally unique install key.

### Launchers

`~/.ynh/bin/<org>--<repo>--<name>` is always created for a namespaced install.
`~/.ynh/bin/<name>` (the short launcher) is created only when `<name>` is
unambiguous across all installed harnesses. Installing a second harness with
the same short name from a different namespace removes the short launcher.
