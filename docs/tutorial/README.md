# ynh Tutorial Series

Progressive tutorials from first steps to advanced configurations. Each tutorial builds on the previous, but can be run independently.

## Tutorials

| # | Tutorial | What you'll learn |
|---|----------|-------------------|
| 1 | [First Persona](tutorial/01-first-persona.md) | Create, install, and run a persona with all artifact types |
| 2 | [Vendors & Symlinks](tutorial/02-vendors-and-symlinks.md) | Switch between Claude/Codex/Cursor, manage symlinks |
| 3 | [Composition](tutorial/03-composition.md) | Pull skills from Git repos with pick, path, and ref |
| 4 | [Delegation](tutorial/04-delegation.md) | Chain personas together as subagents |
| 5 | [Export](tutorial/05-export.md) | Produce vendor-native distributable plugins |
| 6 | [Marketplace](tutorial/06-marketplace.md) | Generate marketplace indexes for team distribution |
| 7 | [Registry & Discovery](tutorial/07-registry-and-discovery.md) | Search and install personas from curated registries |
| 8 | [Developer Tools](tutorial/08-developer-tools.md) | Scaffold, lint, validate, format, compress, inspect with ynd |
| 9 | [Docker Images](tutorial/09-docker-image.md) | Build persona appliance images for CI/CD |

## Manual Test Plan

The [Manual Test Plan](tutorial/manual-test-plan.md) covers every feature across both binaries. Use it to verify a release or validate your development build.

## Install

<!-- tabs:start -->

#### **Homebrew (recommended)**

```bash
brew tap eyelock/tap
brew install ynh
```

This installs both `ynh` (persona manager) and `ynd` (developer tools).

#### **Build from Source**

Requires Go 1.25+.

```bash
git clone https://github.com/eyelock/ynh.git
cd ynh
make deps      # installs Go, linter, formatter
make install   # builds and installs to ~/.ynh/bin/
```

After `make install`, verify you're running your local build:

```bash
ynh version
# Expected: dev-<branch>-<sha> (not a release tag like v0.0.9)
```

If `ynh version` shows a release tag or stale version, ensure `~/.ynh/bin` is on your PATH and re-run `make install` after any code change you want to test.

<!-- tabs:end -->

```bash
ynh version
ynd version
```

You also need at least one AI coding assistant CLI installed:

| Vendor | CLI | Install |
|--------|-----|---------|
| Claude Code | `claude` | `npm install -g @anthropic-ai/claude-code` |
| OpenAI Codex | `codex` | `npm install -g @openai/codex` |
| Cursor | `agent` | Bundled with [Cursor](https://cursor.com) |

Claude Code is used in most tutorial examples. Codex and Cursor are needed for Tutorial 2 (Vendors & Symlinks) and Tutorial 5 (Export).
