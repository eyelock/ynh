# ynh Tutorial Series

Progressive tutorials from first steps to advanced configurations. Each tutorial builds on the previous, but can be run independently.

## Tutorials

### Build Your Harness

| Tutorial | What you'll learn |
|---|---|
| [First Harness](tutorial/first-harness.md) | Create, install, and run a harness with all artifact types |
| [Vendors & Symlinks](tutorial/vendors-and-symlinks.md) | Switch between Claude/Codex/Cursor, manage symlinks |
| [Composition](tutorial/composition.md) | Pull skills from Git repos with pick, path, and ref |
| [Include Editing](tutorial/include-editing.md) | Add, remove, and update includes from the CLI — no manual JSON editing |
| [Hooks](tutorial/hooks.md) | Declare vendor-agnostic lifecycle hooks |
| [MCP Servers](tutorial/mcp-servers.md) | Declare MCP server dependencies per harness |
| [Profiles](tutorial/profiles.md) | Environment-specific overrides with profiles |
| [Focus](tutorial/focus.md) | Bind a prompt and profile for repeatable, non-interactive runs |
| [Project-Local Config](tutorial/project-local-config.md) | Zero-install `.ynh-plugin/plugin.json` in your project root |

### Refine

| Tutorial | What you'll learn |
|---|---|
| [Developer Tools](tutorial/developer-tools.md) | Scaffold, lint, validate, format, compress, inspect with ynd |
| [Developer Preview](tutorial/developer-preview.md) | Preview and diff assembled output across vendors |

### Automate

| Tutorial | What you'll learn |
|---|---|
| [Structured Output](tutorial/structured-output.md) | Use `--format json` for scripts, CI, and tool integration |
| [The Agent Loop](tutorial/agent-loop.md) | Run the loop with budgets, convergence, trajectories, and exit codes |
| [Shadow Mode](tutorial/shadow-mode.md) | Measure a harness against your own git history before trusting it |

### Share & Scale

| Tutorial | What you'll learn |
|---|---|
| [Delegation](tutorial/delegation.md) | Chain harnesses together as subagents |
| [Export](tutorial/export.md) | Produce vendor-native distributable plugins |
| [Marketplace](tutorial/marketplace.md) | Generate marketplace indexes for team distribution |
| [Registry & Discovery](tutorial/registry-and-discovery.md) | Search and install harnesses from curated registries |
| [Docker Images](tutorial/docker-image.md) | Build harness appliance images for CI/CD |
| [Namespacing & Migration](tutorial/namespacing-and-migration.md) | Resolve name collisions across registries and migrate legacy installs |
| [Sensors](tutorial/sensors.md) | Declare observation surfaces a loop driver consumes |
| [Gating with `ynh check`](tutorial/check.md) | Run sensors as a gate, and baseline pre-existing failures |

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

Claude Code is used in most tutorial examples. Codex and Cursor are needed for [Vendors & Symlinks](tutorial/vendors-and-symlinks.md) and [Export](tutorial/export.md).
