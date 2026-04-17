# Tutorial 7: Registry & Discovery

Search for harnesses from curated registries and install them by name. A registry is just a Git repo with a `registry.json` index.

## Prerequisites

```bash
# Clean up from any previous run
rm -rf /tmp/ynh-tutorial
ynh uninstall david planner tester 2>/dev/null
ynh registry remove /tmp/ynh-tutorial/my-registry 2>/dev/null

mkdir -p /tmp/ynh-tutorial
```

## T7.1: Create a local registry

A registry is a Git repo containing `registry.json`:

```bash
mkdir -p /tmp/ynh-tutorial/my-registry
cd /tmp/ynh-tutorial/my-registry

cat > registry.json << 'EOF'
{
  "name": "tutorial-registry",
  "description": "Sample registry for the ynh tutorial",
  "entries": [
    {
      "name": "david",
      "description": "Full-stack development harness with Go expertise",
      "keywords": ["go", "development", "full-stack", "testing"],
      "repo": "github.com/eyelock/assistants",
      "path": "ynh/david",
      "vendors": ["claude", "codex", "cursor"],
      "version": "0.1.0"
    },
    {
      "name": "planner",
      "description": "Project planning and architecture harness",
      "keywords": ["planning", "architecture", "design"],
      "repo": "github.com/eyelock/assistants",
      "path": "ynh/planner",
      "vendors": ["claude"],
      "version": "0.1.0"
    },
    {
      "name": "media-management",
      "description": "Music library processing and Apple Music import",
      "keywords": ["media", "music", "mp3", "apple-music", "ffmpeg"],
      "repo": "github.com/eyelock/assistants",
      "path": "plugins/media-management",
      "vendors": ["claude"],
      "version": "0.1.0"
    }
  ]
}
EOF

git init && git add . && git commit -m "init registry"
```

## T7.2: Add the registry

```bash
ynh registry add /tmp/ynh-tutorial/my-registry
```

Expected:
```
Added registry: /tmp/ynh-tutorial/my-registry
```

## T7.3: List registries

```bash
ynh registry list
```

Expected:
```
  /tmp/ynh-tutorial/my-registry
```

## T7.4: Search

Search matches against name, description, and keywords (case-insensitive):

```bash
ynh search "go"
```

Expected:
```
NAME   DESCRIPTION                                       REPO                                       VENDORS              FROM
david  Full-stack development harness with Go expertise  github.com/eyelock/assistants (ynh/david)  claude,codex,cursor  tutorial-registry (registry)
```

```bash
ynh search "planning"
```

Expected:
```
NAME     DESCRIPTION                                REPO                                         VENDORS  FROM
planner  Project planning and architecture harness  github.com/eyelock/assistants (ynh/planner)  claude   tutorial-registry (registry)
```

```bash
ynh search "music"
```

Expected:
```
NAME              DESCRIPTION                                      REPO                                                      VENDORS  FROM
media-management  Music library processing and Apple Music import  github.com/eyelock/assistants (plugins/media-management)  claude   tutorial-registry (registry)
```

```bash
ynh search "nonexistent"
# Expected: No results for "nonexistent"
```

## T7.5: Install — by exact name

```bash
ynh install david
```

Expected: resolves `david` from the registry to `github.com/eyelock/assistants` with `--path ynh/david`, then installs normally.

```
Installed harness "david"
  Location: /Users/<you>/.ynh/harnesses/david
  Launcher: /Users/<you>/.ynh/bin/david
  Vendor:   claude
```

## T7.6: Install — with registry qualifier

If you have multiple registries and names collide:

```bash
ynh install planner@tutorial-registry
```

The `name@registry` format bypasses ambiguity.

## T7.7: Install — direct URL still works

```bash
ynh install github.com/eyelock/assistants --path ynh/tester
```

Git URLs (containing `/`) take precedence over registry search.

## T7.8: Install — partial match suggests results

If the name doesn't match exactly but is similar to registry entries:

```bash
ynh install development
# Expected:
#   Error: "development" not found. Similar results:
#     david - Full-stack development harness with Go expertise (from tutorial-registry)
```

ynh tries an exact name match first. If that fails, it searches descriptions and keywords and shows similar results — but doesn't install automatically. Use the exact name:

```bash
ynh install david
```

## T7.9: Install — no match error

```bash
ynh install nonexistent-thing
# Expected:
#   Error: "nonexistent-thing" not found in any registry.
#     Did you mean a Git URL? Try: ynh install github.com/user/nonexistent-thing
```

## T7.10: Update registries

```bash
ynh registry update
```

Expected:
```
  tutorial-registry (up to date, 3 entries)
```

This fetches the latest `registry.json` from each configured registry.

## T7.11: Remove a registry

```bash
ynh registry remove /tmp/ynh-tutorial/my-registry
ynh registry list
# Expected: no registries
```

## Disambiguation rules

ynh resolves the install argument in this order:

| Pattern | Example | Interpretation |
|---|---|---|
| Starts with `.` or `/` | `./my-harness`, `/tmp/foo` | Local path |
| Starts with `git@` | `git@github.com:user/repo.git` | Git SSH URL |
| Starts with `https://` | `https://github.com/user/repo` | Git HTTPS URL |
| Contains `@` | `david@my-registry` | Registry qualified name |
| Contains `/` | `github.com/user/repo` | Git URL shorthand |
| Plain word | `david` | Registry search |

Rules are applied in order. Earlier rules take precedence.

## Config storage

Registries are stored in `~/.ynh/config.json`:

```json
{
  "default_vendor": "claude",
  "registries": [
    {"url": "/tmp/ynh-tutorial/my-registry"}
  ]
}
```

Remote registries use Git URLs:
```json
{
  "registries": [
    {"url": "github.com/eyelock/ynh-registry"},
    {"url": "github.com/your-org/internal-registry", "ref": "main"}
  ]
}
```

## Clean up

```bash
ynh uninstall david 2>/dev/null
ynh uninstall planner 2>/dev/null
ynh uninstall tester 2>/dev/null
```

## What you learned

- A registry is a Git repo with `registry.json` listing available harnesses
- `ynh registry add/list/remove/update` manages registry sources
- `ynh search` does text matching on name, description, and keywords
- `ynh install <name>` resolves from registries (exact match installs, multiple matches prompt)
- `name@registry` disambiguates across registries
- Git URLs and local paths still work as before (higher precedence)
- Registries are cached locally and updated on demand

## Next

[Tutorial 13: Docker Images](tutorial/09-docker-image.md) — build harness appliance images for CI/CD.
