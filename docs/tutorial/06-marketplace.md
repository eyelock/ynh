# Tutorial 6: Marketplace

Generate vendor-native marketplace indexes from a collection of harnesses and plugins. The output is a Git repo that Claude Code and Cursor can add as a custom marketplace.

## Prerequisites

```bash
# Clean up from any previous run
rm -rf /tmp/ynh-tutorial

mkdir -p /tmp/ynh-tutorial
```

## T6.1: Set up source material

Create a small marketplace with one harness and one standalone plugin:

### Standalone plugin (no harness.json)

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

### Harness (has harness.json with includes)

```bash
mkdir -p /tmp/ynh-tutorial/marketplace-src/harnesses/reviewer

cat > /tmp/ynh-tutorial/marketplace-src/harnesses/reviewer/harness.json << 'EOF'
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

## T6.2: Create the marketplace config

```bash
cat > /tmp/ynh-tutorial/marketplace-src/marketplace.json << 'EOF'
{
  "name": "tutorial-marketplace",
  "owner": {"name": "tutorial"},
  "description": "Sample marketplace for the ynh tutorial",
  "entries": [
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
- **`harness`** — has `harness.json` with includes. Fully exported (includes resolved, pick applied, delegates generated).

## T6.3: Build the marketplace

```bash
cd /tmp/ynh-tutorial/marketplace-src
ynd marketplace build -o /tmp/ynh-tutorial/marketplace-out
```

Expected output:
```
Marketplace built → /tmp/ynh-tutorial/marketplace-out (2 plugins)
```

## T6.4: Verify the output

### Directory structure

```bash
find /tmp/ynh-tutorial/marketplace-out -not -path '*/.git/*' -type f | sort
```

Expected (`.git/` excluded from listing — it's auto-created by the build):
```
.agents/plugins/marketplace.json
.claude-plugin/marketplace.json
.cursor-plugin/marketplace.json
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
  "owner": {"name": "tutorial"},
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

Same structure but in `.cursor-plugin/`. Both point to the same `./plugins/` relative paths.

## T6.5: Test with Claude Code

Claude Code requires local marketplaces to be Git repos (relative source paths like `./plugins/formatter` only resolve within a Git working tree):

```bash
cd /tmp/ynh-tutorial/marketplace-out
git init && git add . && git commit -m "init"
```

Now test in a Claude Code session:

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

> **Note:** This is a Claude Code requirement, not a ynh limitation. When distributing via GitHub (the normal path), the repo is already a Git repo. The `git init` step is only needed for local testing.

## T6.6: Build with --clean

Run from the directory containing `marketplace.json`:

```bash
cd /tmp/ynh-tutorial/marketplace-src
ynd marketplace build -o /tmp/ynh-tutorial/marketplace-out --clean
# Removes output dir before rebuilding
```

> **Important:** `ynd marketplace build` looks for `marketplace.json` in the current directory. Make sure you're in the directory that contains your marketplace config, not the output directory.

## T6.7: Build for specific vendors

```bash
cd /tmp/ynh-tutorial/marketplace-src
ynd marketplace build -o /tmp/ynh-tutorial/marketplace-claude -v claude
# Only generates .claude-plugin/marketplace.json
# Plugins still get .claude-plugin/plugin.json only
```

## Clean up

```bash
rm -rf /tmp/ynh-tutorial/marketplace-*
```

## What you learned

- `ynd marketplace build` generates vendor-native marketplace directories
- A marketplace config lists `plugin` entries (copy as-is) and `harness` entries (fully exported)
- Output includes `.claude-plugin/marketplace.json` and `.cursor-plugin/marketplace.json`
- Plugins get dual manifests so one physical directory serves both vendors
- Harnesses' remote includes are resolved and flattened during marketplace build
- Pick filtering carries through from harness metadata to the marketplace output
- Codex is excluded from marketplaces (no marketplace system)

## Next

[Tutorial 12: Registry & Discovery](tutorial/07-registry-and-discovery.md) — search and install from curated indexes.
