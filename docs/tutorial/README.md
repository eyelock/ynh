# ynh Tutorial Series

Progressive tutorials from first steps to advanced configurations. Each tutorial builds on the previous, but can be run independently.

## Tutorials

### Build Your Harness

| # | Tutorial | What you'll learn |
|---|----------|-------------------|
| 1 | [First Harness](tutorial/01-first-harness.md) | Create, install, and run a harness with all artifact types |
| 2 | [Vendors & Symlinks](tutorial/02-vendors-and-symlinks.md) | Switch between Claude/Codex/Cursor, manage symlinks |
| 3 | [Composition](tutorial/03-composition.md) | Pull skills from Git repos with pick, path, and ref |
| 4 | [Hooks](tutorial/10-hooks.md) | Declare vendor-agnostic lifecycle hooks |
| 5 | [MCP Servers](tutorial/11-mcp-servers.md) | Declare MCP server dependencies per harness |
| 6 | [Profiles](tutorial/13-profiles.md) | Environment-specific overrides with profiles |

### Refine

| # | Tutorial | What you'll learn |
|---|----------|-------------------|
| 7 | [Developer Tools](tutorial/08-developer-tools.md) | Scaffold, lint, validate, format, compress, inspect with ynd |
| 8 | [Developer Preview](tutorial/12-developer-preview.md) | Preview and diff assembled output across vendors |
| 9 | [Include Editing](tutorial/17-include-editing.md) | Add, remove, and update includes in an installed harness |

### Automate

| # | Tutorial | What you'll learn |
|---|----------|-------------------|
| 9 | [Structured Output](tutorial/16-structured-output.md) | Use `--format json` for scripts, CI, and tool integration |

### Share & Scale

| # | Tutorial | What you'll learn |
|---|----------|-------------------|
| 10 | [Delegation](tutorial/04-delegation.md) | Chain harnesses together as subagents |
| 11 | [Export](tutorial/05-export.md) | Produce vendor-native distributable plugins |
| 12 | [Marketplace](tutorial/06-marketplace.md) | Generate marketplace indexes for team distribution |
| 13 | [Registry & Discovery](tutorial/07-registry-and-discovery.md) | Search and install harnesses from curated registries |
| 14 | [Docker Images](tutorial/09-docker-image.md) | Build harness appliance images for CI/CD |

## Manual Test Plan

The [Manual Test Plan](tutorial/manual-test-plan.md) covers every feature across both binaries. Use it to verify a release or validate your development build.

## Install

<!-- tabs:start -->

#### **Homebrew (recommended)**

```bash
brew tap eyelock/tap
brew install ynh
```

This installs both `ynh` (harness template manager) and `ynd` (developer tools).

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

Claude Code is used in most tutorial examples. Codex and Cursor are needed for Tutorial 2 (Vendors & Symlinks) and Tutorial 10 (Export).
