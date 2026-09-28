# Marketplace

Generate vendor-native marketplace indexes from a collection of harnesses and plugins. The output is a Git repo that Claude Code and Cursor can add as a custom marketplace.

## Prerequisites

```bash
# Clean up from any previous run
rm -rf /tmp/ynh-tutorial

mkdir -p /tmp/ynh-tutorial
```

## Set up source material

Create a small marketplace with one harness and one standalone plugin:

### Standalone plugin (vendor-native format)

```bash
mkdir -p /tmp/ynh-tutorial/marketplace-src/plugins/formatter/.claude-plugin
mkdir -p /tmp/ynh-tutorial/marketplace-src/plugins/formatter/skills/auto-format

cat > /tmp/ynh-tutorial/marketplace-src/plugins/formatter/.claude-plugin/plugin.json << 'EOF'
{
  "name": "formatter",
  "version": "1.0.0",
  "description": "Auto-format code on save"
}
EOF

cat > /tmp/ynh-tutorial/marketplace-src/plugins/formatter/skills/auto-format/SKILL.md << 'EOF'
---
name: auto-format
description: Format code using project conventions.
---

When invoked, format the specified files using the project's
configured formatter (prettier, gofmt, black, etc.).
EOF
```

### Harness (has .agents/harness/plugin.json with includes)

```bash
mkdir -p /tmp/ynh-tutorial/marketplace-src/harnesses/reviewer

mkdir -p /tmp/ynh-tutorial/marketplace-src/harnesses/reviewer/.agents/harness
cat > /tmp/ynh-tutorial/marketplace-src/harnesses/reviewer/.agents/harness/plugin.json << 'EOF'
{
  "name": "reviewer",
  "version": "1.0.0",
  "description": "Code review harness with external skills",
  "includes": [
    {
      "git": "github.com/eyelock/assistants",
      "path": "skills/dev",
      "pick": ["skills/dev-review", "skills/dev-quality"]
    }
  ]
}
EOF

cat > /tmp/ynh-tutorial/marketplace-src/harnesses/reviewer/instructions.md << 'EOF'
You are a code reviewer. Be thorough but constructive.
EOF
```

## Create the marketplace config

```bash
cat > /tmp/ynh-tutorial/marketplace-src/marketplace.json << 'EOF'
{
  "$schema": "https://eyelock.github.io/ynh/schema/marketplace.schema.json",
  "name": "tutorial-marketplace",
  "owner": {"name": "tutorial"},
  "description": "Sample marketplace for the ynh tutorial",
  "harnesses": [
    {
      "type": "plugin",
      "source": "./plugins/formatter"
    },
    {
      "type": "harness",
      "source": "./harnesses/reviewer",
      "description": "Code review with dev-quality and dev-review skills"
    }
  ]
}
EOF
```

Two entry types:
- **`plugin`** — already a valid plugin directory. Copied as-is, missing vendor manifests generated.
- **`harness`** — has `.agents/harness/plugin.json` with includes. Fully exported (includes resolved, pick applied, delegates generated).

## Build the marketplace

```bash
cd /tmp/ynh-tutorial/marketplace-src
ynd marketplace build -o /tmp/ynh-tutorial/marketplace-out
```

Expected output:
```
Marketplace built → /tmp/ynh-tutorial/marketplace-out (2 plugins)
```

## Verify the output

### Directory structure

```bash
find /tmp/ynh-tutorial/marketplace-out -not -path '*/.git/*' -type f | sort
```

Expected (`.git/` excluded from listing — it's auto-created by the build):
```
.agents/plugins/marketplace.json
.claude-plugin/marketplace.json
.cursor-plugin/marketplace.json
.github/plugin/marketplace.json
.ynd-marketplace
plugins/formatter/.claude-plugin/plugin.json
plugins/formatter/.codex-plugin/plugin.json
plugins/formatter/.cursor-plugin/plugin.json
plugins/formatter/skills/auto-format/SKILL.md
plugins/reviewer/.claude-plugin/plugin.json
plugins/reviewer/.codex-plugin/plugin.json
plugins/reviewer/.cursor-plugin/plugin.json
plugins/reviewer/.cursorrules
plugins/reviewer/AGENTS.md
plugins/reviewer/CLAUDE.md
plugins/reviewer/skills/dev-quality/SKILL.md
plugins/reviewer/skills/dev-review/SKILL.md
README.md
```

`.ynd-marketplace` marks the directory as one ynd built. It is what lets a later
`--clean` rebuild into it while keeping the `.git` history.

### Git repo

The output directory is automatically initialized as a Git repo so Claude Code can resolve relative plugin source paths:

```bash
git -C /tmp/ynh-tutorial/marketplace-out log --oneline
```

Expected: a single commit with message `ynd marketplace build`.

Key points:
- Each plugin has **both** `.claude-plugin/` and `.cursor-plugin/` manifests
- The reviewer harness's remote includes are resolved and flattened (dev-review, dev-quality appear as local skills)
- Pick filtering was applied (only the 2 picked skills, not all 7 dev skills)

### Claude marketplace.json

```bash
cat /tmp/ynh-tutorial/marketplace-out/.claude-plugin/marketplace.json
```

Expected (formatted):
```json
{
  "name": "tutorial-marketplace",
  "owner": {
    "name": "tutorial"
  },
  "description": "Sample marketplace for the ynh tutorial",
  "plugins": [
    {
      "name": "formatter",
      "description": "Auto-format code on save",
      "version": "1.0.0",
      "source": "./plugins/formatter"
    },
    {
      "name": "reviewer",
      "description": "Code review with dev-quality and dev-review skills",
      "version": "1.0.0",
      "source": "./plugins/reviewer"
    }
  ]
}
```

### Cursor marketplace.json

```bash
cat /tmp/ynh-tutorial/marketplace-out/.cursor-plugin/marketplace.json
```

**Not the same structure.** Cursor's documented format differs from Claude's in
two ways, and `ynd marketplace build` now emits each vendor's own shape:

```json
{
  "name": "tutorial-marketplace",
  "owner": {
    "name": "tutorial"
  },
  "metadata": {
    "description": "Sample marketplace for the ynh tutorial"
  },
  "plugins": [
    {
      "name": "formatter",
      "source": "./plugins/formatter",
      "description": "Auto-format code on save"
    },
    {
      "name": "reviewer",
      "source": "./plugins/reviewer",
      "description": "Code review with dev-quality and dev-review skills"
    }
  ]
}
```

| | Claude | Cursor |
|---|---|---|
| description | top-level `description` | nested `metadata.description` |
| plugin version | `"version": "1.0.0"` | absent |

`source` is the same in both — `./plugins/<name>`, where `build` puts each
plugin. Codex and Copilot differ again; see
[Marketplace](../marketplace.md) for the full comparison.

## Test with Claude Code

Claude Code requires local marketplaces to be Git repos (relative source paths like `./plugins/formatter` only resolve within a Git working tree). There is nothing to do here: `ynd marketplace build` already made `marketplace-out` a Git repo with a commit (see [Git repo](#git-repo) above).

Test it in a Claude Code session:

```bash
# Add the marketplace
# /plugin marketplace add /tmp/ynh-tutorial/marketplace-out

# Install plugins
# /plugin install formatter@tutorial-marketplace
# /plugin install reviewer@tutorial-marketplace

# Reload to activate
# /reload-plugins

# Verify — ask Claude about available skills
# What skills do I have from the formatter and reviewer plugins?
```

> **Note:** This is a Claude Code requirement, not a ynh limitation. When distributing via GitHub (the normal path), the repo is already a Git repo. Locally, the build's own `git init` and commit cover it.

## Build with --clean

Run from the directory containing `marketplace.json`:

`--clean` removes the output directory before rebuilding. Use it on a directory that
holds stale output and is not a Git working copy — here, a scratch directory with a
leftover file:

```bash
mkdir -p /tmp/ynh-tutorial/marketplace-stale
echo old > /tmp/ynh-tutorial/marketplace-stale/leftover.txt
cd /tmp/ynh-tutorial/marketplace-src
ynd marketplace build -o /tmp/ynh-tutorial/marketplace-stale --clean
```

It asks before deleting:

```
--clean will permanently delete /tmp/ynh-tutorial/marketplace-stale and its 1 entry.
Delete it? [y/N]
```

Anything but `y` leaves the directory alone and exits 1. Pass `-y` (also implied by
`$YNH_YES` or CI) to skip the prompt:

```bash
ynd marketplace build -o /tmp/ynh-tutorial/marketplace-stale --clean -y
```

```
Marketplace built → /tmp/ynh-tutorial/marketplace-stale (2 plugins)
```

`--clean` refuses outright to delete the filesystem root, `$HOME`, the current
directory, any ancestor of it, or a Git working copy, and `-y` does not override
that. There is one exception: the repository `ynd marketplace build` created
itself. The build output is initialised as a Git repo (see above) and marked with
`.ynd-marketplace`, so `--clean` on `marketplace-out` empties it but keeps `.git`
and `.ynd-marketplace`, rebuilds, and commits on top:

```bash
echo stale > /tmp/ynh-tutorial/marketplace-out/stale.txt
ynd marketplace build -o /tmp/ynh-tutorial/marketplace-out --clean -y
```

```
Marketplace built → /tmp/ynh-tutorial/marketplace-out (2 plugins)
```

`stale.txt` is gone and the history is intact. When the rebuilt content matches
the last commit, as it does here, there is nothing new to commit, so `git log`
still shows the one `ynd marketplace build` commit; change the config and rebuild
and a second one appears on top.

A Git repo that ynd did not create is still refused, `-y` or not:

```bash
mkdir -p /tmp/ynh-tutorial/marketplace-foreign
git -C /tmp/ynh-tutorial/marketplace-foreign init -q
ynd marketplace build -o /tmp/ynh-tutorial/marketplace-foreign --clean -y
```

```
Error: --clean refuses to delete /tmp/ynh-tutorial/marketplace-foreign: it is a git working copy (not created by ynd)
```

> **Important:** `ynd marketplace build` looks for `marketplace.json` in the current directory. Make sure you're in the directory that contains your marketplace config, not the output directory.

## Build for specific vendors

```bash
cd /tmp/ynh-tutorial/marketplace-src
ynd marketplace build -o /tmp/ynh-tutorial/marketplace-claude -v claude
# Only generates .claude-plugin/marketplace.json
# Plugins still get .claude-plugin/plugin.json only
```

## Build as Agent Plugins

`--format agent-plugin` builds each `harness` entry as one portable
[Agent Plugins](https://agent-plugins.org) package, the format Codex, Copilot,
VS Code and Cursor load directly. The indexes are written for every vendor
as before; the clients that load the format detect it from each package's
root manifest, and Claude Code finds its own manifest inside:

```bash
cd /tmp/ynh-tutorial/marketplace-src
ynd marketplace build -o /tmp/ynh-tutorial/marketplace-portable --format agent-plugin
find /tmp/ynh-tutorial/marketplace-portable -not -path '*/.git/*' -type f | LC_ALL=C sort
```

Expected (`.git/` excluded; `LC_ALL=C` pins the order):
```
/tmp/ynh-tutorial/marketplace-portable/.agents/plugins/marketplace.json
/tmp/ynh-tutorial/marketplace-portable/.claude-plugin/marketplace.json
/tmp/ynh-tutorial/marketplace-portable/.cursor-plugin/marketplace.json
/tmp/ynh-tutorial/marketplace-portable/.github/plugin/marketplace.json
/tmp/ynh-tutorial/marketplace-portable/.ynd-marketplace
/tmp/ynh-tutorial/marketplace-portable/README.md
/tmp/ynh-tutorial/marketplace-portable/plugins/formatter/.claude-plugin/plugin.json
/tmp/ynh-tutorial/marketplace-portable/plugins/formatter/.codex-plugin/plugin.json
/tmp/ynh-tutorial/marketplace-portable/plugins/formatter/.cursor-plugin/plugin.json
/tmp/ynh-tutorial/marketplace-portable/plugins/formatter/skills/auto-format/SKILL.md
/tmp/ynh-tutorial/marketplace-portable/plugins/reviewer/.claude-plugin/plugin.json
/tmp/ynh-tutorial/marketplace-portable/plugins/reviewer/AGENTS.md
/tmp/ynh-tutorial/marketplace-portable/plugins/reviewer/CLAUDE.md
/tmp/ynh-tutorial/marketplace-portable/plugins/reviewer/plugin.json
/tmp/ynh-tutorial/marketplace-portable/plugins/reviewer/skills/dev-quality/SKILL.md
/tmp/ynh-tutorial/marketplace-portable/plugins/reviewer/skills/dev-review/SKILL.md
```

The `reviewer` harness became a package: `plugin.json` and `skills/` are the
portable core, `.claude-plugin/plugin.json` and `CLAUDE.md` are Claude Code's
compatibility layer. The `formatter` entry is a Claude Code plugin, not an
Agent Plugin, so it is copied as-is with vendor manifests generated, exactly
as in the vendor-format build.

```bash
ynd validate /tmp/ynh-tutorial/marketplace-portable/plugins/reviewer
# Expected: /tmp/ynh-tutorial/marketplace-portable/plugins/reviewer: valid (Agent Plugin 1.0.0)
```

### Test with GitHub Copilot

Copilot CLI registers a local marketplace directory and loads Agent Plugins
from it natively. Unlike the Claude Code test above, no `git init` is needed
(the build already did it, and Copilot does not require it). These commands
change your Copilot configuration; the last two undo it.

```bash
copilot plugin marketplace add /tmp/ynh-tutorial/marketplace-portable
copilot plugin marketplace browse tutorial-marketplace
```

Expected:
```
Marketplace "tutorial-marketplace" added successfully.
Plugins in "tutorial-marketplace":
  • formatter - Auto-format code on save
  • reviewer - Code review with dev-quality and dev-review skills

Install with: copilot plugin install <plugin-name>@tutorial-marketplace
```

```bash
copilot plugin install reviewer@tutorial-marketplace
```

Expected output begins:
```
Plugin "reviewer" installed successfully. Installed 2 skills.
```

Copilot loads the package live from `/tmp/ynh-tutorial/marketplace-portable/plugins/reviewer` rather than copying it, so an edit to the built output takes effect on its next session.

Both skills the harness pulled in from its remote include are now Copilot
skills:

```bash
copilot skill list
```

Expected output includes a `Plugin skills:` section naming both (the project
and built-in sections vary by machine):
```
Plugin skills:
  dev-quality - ...
  dev-review - ...
```

Undo the registration:

```bash
copilot plugin uninstall reviewer
copilot plugin marketplace remove tutorial-marketplace
# Expected: Marketplace "tutorial-marketplace" removed successfully.
```

## Clean up

```bash
rm -rf /tmp/ynh-tutorial
```

## What you learned

- `ynd marketplace build` generates vendor-native marketplace directories
- A marketplace config lists `plugin` entries (copy as-is) and `harness` entries (fully exported)
- Output includes an index per vendor: `.claude-plugin/marketplace.json`, `.cursor-plugin/marketplace.json`, `.agents/plugins/marketplace.json` (Codex) and `.github/plugin/marketplace.json` (Copilot)
- Plugins get every vendor's manifest so one physical directory serves them all
- Harnesses' remote includes are resolved and flattened during marketplace build
- Pick filtering carries through from harness metadata to the marketplace output
- Codex is included: each plugin gets a `.codex-plugin/plugin.json` that points only at its skills
- `--format agent-plugin` builds each harness entry as one portable Agent Plugins package, with every vendor's index still written
- Copilot CLI registers the built directory as a marketplace and installs a package from it with its skills, which is the end-to-end proof that a real client reads what ynh wrote

## Next

[Registry & Discovery](registry-and-discovery.md) — search and install harnesses from curated registries.
