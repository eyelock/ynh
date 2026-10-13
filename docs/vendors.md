# Vendor Support

ynh works with multiple AI coding assistants. The vendor determines which CLI is launched and where config files are placed. Your harnesses and artifacts stay the same regardless of vendor.

## Supported Vendors

Run `ynh vendors` to see what's available:

```
$ ynh vendors
NAME     CLI      CONFIG DIR
claude   claude   .claude
codex    codex    .codex
copilot  copilot  .copilot
cursor   agent    .cursor
opencode opencode .opencode
```

If `~/.ynh/config.json` declares any [local model backends](#local-model-backends), `ynh vendors` (including `--format json`) also lists one extra row per configured `<backend>/<vendor>` pair — the exact spec string `-v` accepts — so a caller can discover what's launchable on this machine without parsing config.json itself.

## Launch Strategies

Each vendor gets the strategy that matches its capabilities:

| Vendor | Launch | Artifacts | Process |
|--------|--------|-----------|---------|
| **Claude** | `syscall.Exec` with `--plugin-dir` | Native plugin loading | Clean handoff (ynh exits) |
| **Codex** | Child process with `cmd.Dir` | Symlinks into project | Managed (signal forwarding) |
| **Cursor** | Child process with `cmd.Dir` | Symlinks into project | Managed (signal forwarding) |
| **Copilot** | `syscall.Exec` with `--plugin-dir` | Native plugin loading | Clean handoff (ynh exits) |
| **OpenCode** | `syscall.Exec` of `opencode --standalone` with `OPENCODE_CONFIG_DIR` | Native config-directory loading | Clean handoff (ynh exits) |

Claude and Copilot support `--plugin-dir` natively, and OpenCode loads a whole config directory named by `OPENCODE_CONFIG_DIR`, so ynh can exec directly into all three. Codex and Cursor don't have plugin loading, so ynh installs symlinks and manages the child process for signal forwarding.

### Symlink Installation (Codex/Cursor)

For vendors that need symlinks:

```bash
david -v cursor --install     # creates .cursor/ symlinks in current project
david -v cursor               # launches normally
david -v cursor --clean       # removes symlinks
```

Installations are tracked in `~/.ynh/symlinks.json`. Use `ynh status` to see all installations and `ynh prune` to clean up orphaned ones.

## Choosing a Vendor

**Per-harness** (in `.agents/harness/plugin.json`):

```json
{
  "default_vendor": "claude"
}
```

**Per-session** (CLI flag overrides everything):

```bash
david -v codex
```

**Global default** (in `~/.ynh/config.json`):

```json
{"default_vendor": "claude"}
```

Resolution order: **CLI flag (`-v`) > `YNH_VENDOR` env var > harness default > global config**.

`YNH_VENDOR` is honored by both `ynh` commands (`ynh run`) and `ynd` commands (`preview`, `export`, `create`, `compress`, `inspect`, `marketplace`).

Any of these — the flag, the env var, or either `default_vendor` — also accepts a **backend-redirected spec** instead of a plain vendor name, to run that vendor against a local model instead of its cloud API. See [Local Model Backends](#local-model-backends) below.

## Local Model Backends

By default a vendor CLI talks to its normal cloud API (Anthropic for Claude Code, OpenAI for Codex). A **backend spec** redirects it at a different model server instead — e.g. a local [Ollama](https://ollama.com) instance — without changing anything else about the session: same CLI, same MCP servers, same tools, same skills. Quality then depends entirely on the local model, not on ynh.

A backend isn't a separate flag — it's the same `-v` you already use for vendor selection, extended with two optional `/`-separated segments:

```
-v <vendor>                    # plain vendor, e.g. -v claude — unchanged, no backend
-v <backend>/<vendor>          # vendor's default model against <backend>
-v <backend>/<vendor>/<model>  # explicit model against <backend>
```

`<backend>` is a name you define in `~/.ynh/config.json`; `<vendor>` and `<model>` are otherwise ordinary vendor/model names. This flows through the exact same `-v` / `YNH_VENDOR` / harness `default_vendor` / global `default_vendor` precedence chain as plain vendor selection — no separate resolution rule to learn.

### Setting up Ollama

1. Install Ollama: `brew install ollama` (macOS) or see [ollama.com/download](https://ollama.com/download).
2. Start the server, if it isn't already running as a background service: `ollama serve`. Check with `curl http://localhost:11434/api/tags`.
3. Pull a model: `ollama pull qwen3` (or any tool-calling-capable model — see caveats below). This can be several GB.
4. Define the `ollama` backend's connection details in `~/.ynh/config.json` (below), then select it per invocation with `-v ollama/<vendor>/<model>` — no separate config entry needed per model.

### Configuring a backend

Use `ynh backend add` rather than hand-editing `~/.ynh/config.json` — it validates the vendor name and won't silently create a malformed entry:

```bash
ynh backend add ollama claude --base-url http://localhost:11434 --auth-token ollama --type ollama
ynh backend add ollama codex  --base-url http://localhost:11434/v1/
```

```bash
ynh backend list                    # human-readable, includes live-discovered models
ynh backend list --format json      # structured: backend, vendor, type, base_url, has_auth_token, models
ynh backend remove ollama codex     # drop just the codex connection
ynh backend remove ollama           # drop the whole backend
```

`--type` (currently only `ollama` is recognized) enables live model discovery — see below. `--env KEY=VALUE` (repeatable) is an escape hatch for anything backend-specific beyond `base_url`/`auth_token`.

This writes to the same `~/.ynh/config.json` `backends` map either way — `ynh backend add` is just a validated way to get there. Directly, the shape is:

```json
{
  "backends": {
    "ollama": {
      "type": "ollama",
      "vendors": {
        "claude": {
          "base_url": "http://localhost:11434",
          "auth_token": "ollama"
        },
        "codex": {
          "base_url": "http://localhost:11434/v1/"
        }
      }
    }
  }
}
```

Then:

```bash
ynh run david -v ollama/claude/qwen3        # Claude Code against local qwen3
ynh run david -v ollama/codex/gpt-oss:120b  # Codex against local gpt-oss:120b
```

With no backend segment (`-v claude`), behavior is unchanged — the vendor talks to its real cloud API. Note the `base_url` format is **not interchangeable** between vendors: Codex's OpenAI-compatible endpoint needs the `/v1/` suffix; Claude's Anthropic-compatible one does not.

Each vendor's connection is applied differently:

- **`claude`** — sets `ANTHROPIC_BASE_URL` to `base_url` verbatim, sets `ANTHROPIC_AUTH_TOKEN` to `auth_token`, and clears `ANTHROPIC_API_KEY` so no real key leaks to the local server. The model segment becomes the `ANTHROPIC_MODEL` env var, not a `--model` CLI flag — Claude Code treats `--model`/`/model` as an explicit user choice and persists it to `~/.claude/settings.json` as the default for *all future sessions*, which would leak a backend's model (e.g. `qwen3`) into unrelated, non-redirected launches. `ANTHROPIC_MODEL` only affects the current process.
- **`codex`** — writes a `[model_providers.<backend>]` block into `~/.codex/config.toml` with `base_url` verbatim and `wire_api = "responses"`, then passes `-c model_provider=<backend>` and `-c model=<model>`.

An `env` map on a `backends.<name>.vendors.<vendor>` entry is an escape hatch for anything backend-specific beyond these fields.

`-v ollama/claude` with no model segment does **not** prompt you to pick one — it launches with the vendor's own default model id, which Ollama won't recognize, and the vendor CLI errors out. Use `ynh vendors` (below) to see which models are actually installed before picking one.

### Discovering installed models

Set `"type": "ollama"` on a backend and `ynh vendors --format json` queries that server's `/api/tags` live and expands its rows into one per **installed** model — `"ollama/claude/qwen3"`, not just `"ollama/claude"` — using whichever vendor connection is configured (the model list comes from the server itself, independent of vendor). This is how a caller enumerates exactly which specs will actually work, without guessing at model names or parsing `config.json`:

```bash
$ ynh vendors
NAME                  DISPLAY NAME                  CLI     CONFIG DIR  AVAILABLE
claude                Claude Code                   claude  .claude     true
codex                 OpenAI Codex                   codex   .codex      true
copilot               GitHub Copilot CLI             copilot .copilot    true
cursor                Cursor                         agent   .cursor     true
opencode              OpenCode                       opencode .opencode  true
ollama/claude/qwen3   Claude Code (ollama · qwen3)   claude  .claude     true
```

If the server is unreachable, or `type` is omitted/unrecognized, `ynh vendors` falls back to the plain `"<backend>/<vendor>"` row instead of failing the whole listing — model discovery is best-effort, not a hard dependency of vendor listing.

### Ollama caveats

These come from Ollama's own docs, not ynh:

- Tool-calling reliability depends entirely on the local model — qwen3-class models are what's documented as working well; not every model on [ollama.com/library](https://ollama.com/library) supports tool calling.
- Use a model with at least a 64k context window for non-trivial repos (`ollama show <model>` reports a model's context length).
- Claude Code's extended-thinking and prompt-caching behavior isn't something Ollama's Anthropic-compatible API actually implements — the parameters are accepted but have no effect.
- `tool_choice` forcing, token-counting, the Batches API, PDF input, and image-by-URL are not supported by Ollama's Anthropic compatibility layer.

## Vendor Notes

**Claude Code** - Full interactive and non-interactive support. Uses `--plugin-dir` for artifact loading and `--append-system-prompt` for harness instructions. Requires `claude` CLI installed. See [claude.ai/code](https://claude.ai/code).

**OpenAI Codex** - Full interactive and non-interactive support. Uses symlink-based artifact installation. Requires `codex` CLI installed. Codex requires a Git working tree — if running in a non-Git directory (e.g. Docker containers), pass `--skip-git-repo-check` as a vendor flag. See [openai.com/codex](https://openai.com/codex).

**Cursor Agent** - Full interactive and non-interactive support. Uses symlink-based artifact installation. Requires `agent` CLI installed (`curl https://cursor.com/install -fsS | bash`). Uses `-p` for non-interactive prompts. See [cursor.com/cli](https://cursor.com/cli).

**GitHub Copilot CLI** - Full interactive and non-interactive support. Uses `--plugin-dir` for artifact loading, like Claude. Requires `copilot` CLI installed. Non-interactive runs need `--allow-all-tools` (added automatically). Harness instructions and MCP servers are projected into the calling project's `.github/instructions/ynh-harness.instructions.md` and `.github/mcp.json`, because Copilot doesn't read plugin-bundled `AGENTS.md`/`.mcp.json` via `--plugin-dir`. The instructions file gets one frontmatter block, `applyTo: "**/*"` so it is always on, followed by any fields of the harness instructions' own frontmatter except `applyTo`. **Hooks are not supported**: Copilot silently no-ops hooks in folders it hasn't marked as trusted, and no CLI flag exists to grant that trust per-invocation, so `ynh`-managed hook config would silently fail rather than run. See [github.com/features/copilot/cli](https://github.com/features/copilot/cli).

**OpenCode** - Full interactive and non-interactive support, for **OpenCode 2.x only**; 1.x is refused with a clear message (the two differ in launch model, config schema and plugin API, and both install an `opencode` binary). Install 2.x with `brew install anomalyco/tap/opencode-v2`. OpenCode 2 runs every command through a shared background service that ignores the client's environment, so ynh launches `opencode --standalone` with `OPENCODE_CONFIG_DIR` pointing at the run directory's `.opencode/`; no symlinks. Skills load from `skills/`. Agents load from `agents/`, with their frontmatter translated: OpenCode silently drops an agent that has a comma-string `tools:`, a bare model alias such as `sonnet`, or a `color:`, so ynh keeps `description`, `mode`, `permissions` and a `provider/model` model, and Claude `tools:` become deny-by-default permissions. Commands go to `commands/`; whether a bundle slash command expands under `opencode run` is unconfirmed. OpenCode 2 reads `AGENTS.md` only (its `instructions` config key is accepted but ignored) and has no concept of rules, so ynh composes `.opencode/AGENTS.md` at launch from the harness instructions, the rules and the runtime context. MCP servers go in `.opencode/opencode.json` under `mcp.servers`; OpenCode reaches MCP tools through its Code Mode, so the model calls them from inside one `execute` tool. **Hooks** are supported through a generated plugin, `.opencode/plugins/ynh-hooks.ts`, because OpenCode 2 has no declarative hooks; see [Hooks](hooks.md#opencode-hooks-are-a-generated-plugin) for the event mapping and caveats. Resume with `-s <id>` or `-c` works, but ynh cannot pin the session itself because OpenCode keeps sessions in SQLite, so it is the same as Codex. There is no plugin manifest and no marketplace, so `ynd export` and `ynd marketplace` skip OpenCode, and it does not read Agent Plugins packages. For `ynh agent run --backend opencode`, effort is refused (select a variant with `--model provider/model#variant`) and auto-approve accepts `all` only. OpenCode is not a [`ynd compress`/`ynd inspect`](ynd.md) LLM vendor: it has no argument that turns its tools off. The `opencode/nemotron-3-ultra-free` model needs no login, which makes it handy for a smoke test. See [opencode.ai/v2/docs](https://opencode.ai/v2/docs).

## Export Output by Vendor

`ynd export` produces vendor-native plugin layouts. Each vendor has a different file structure:

| | Claude | Cursor | Codex | Copilot | OpenCode |
|---|---|---|---|---|---|
| **Manifest** | `.claude-plugin/plugin.json` | `.cursor-plugin/plugin.json` | `.codex-plugin/plugin.json` | `.claude-plugin/plugin.json` | *none* |
| **Skills** | `skills/<name>/SKILL.md` | `skills/<name>/SKILL.md` | `skills/<name>/SKILL.md` | `skills/<name>/SKILL.md` | `skills/<name>/SKILL.md` |
| **Agents** | `agents/<name>.md` | `agents/<name>.md` | *excluded* | `agents/<name>.md` | `agents/<name>.md` |
| **Rules** | `rules/<name>.md` | `rules/<name>.mdc` | *excluded* | *excluded* | `rules/<name>.md` |
| **Commands** | `commands/<name>.md` | `commands/<name>.md` | *excluded* | *excluded* | `commands/<name>.md` |
| **Instructions** | `AGENTS.md` | `.cursorrules` + `AGENTS.md` | `AGENTS.md` | `AGENTS.md` | `AGENTS.md` |
| **Hooks** | `hooks/claude.json` | `hooks/cursor.json` | `hooks/codex.json` | *none* | `.opencode/plugins/ynh-hooks.ts` |
| **MCP servers** | `mcp/claude.json` | `mcp.json` | `.mcp.json` | `.github/mcp.json` | `.opencode/opencode.json` |
| **Marketplace** | `.claude-plugin/marketplace.json` | `.cursor-plugin/marketplace.json` | `.agents/plugins/marketplace.json` | `.github/plugin/marketplace.json` (best-effort, unverified) | *not supported* |

Key differences between runtime and export:

- **Runtime** places artifacts inside the vendor config directory (e.g., `.claude/skills/`)
- **Export** places artifacts at the plugin root (e.g., `skills/`) — the standard distributable layout
- Claude export writes `AGENTS.md` for instructions, not `CLAUDE.md` (which would conflict with the installing project's own)
- Codex export is limited to skills — agents, rules, commands, and delegates are excluded with warnings
- Hooks go to the session path at runtime (`.claude/hooks/hooks.json`, `.cursor/hooks.json`, `.codex/hooks.json`) and to `hooks/<vendor>.json` at the plugin root in an export, named by the `"hooks"` field of that vendor's manifest, never both. A plugin never reads the session path, and there is no shared `hooks/hooks.json`, so a merged package never hands one vendor's format to another (see [Hooks: Config File Locations](hooks.md#config-file-locations))
- MCP config goes to the session path at runtime (`.claude/.mcp.json`, `.cursor/mcp.json`) and to the plugin path in an export (`mcp/claude.json`, named by the `"mcpServers"` field of `.claude-plugin/plugin.json`, and Cursor's root `mcp.json`), never both, for the same reason. Every vendor in a merged package has its own MCP file, and each manifest names only a file that is there (see [MCP Servers: Config File Locations](mcp.md#config-file-locations))
- Codex is included in merged export (`--merged`) and in `ynd marketplace build`, which exports each harness the same way: `.codex-plugin/plugin.json` sits beside the other vendors' manifests and points only at `skills/`, so Codex ignores the agents, rules and commands the other vendors read from the same directory. `ynd export --merged` says so with the same warning a per-vendor Codex export prints. Pass `-v` without `codex` to leave it out
- OpenCode has no plugin manifest, no marketplace format and no Agent Plugins support. `ynd export -v opencode` writes the same layout the run directory holds (artifacts, `AGENTS.md`, `.opencode/opencode.json` and the generated hooks plugin), but nothing installs it as a plugin and no marketplace entry is produced. Rules are exported as files rather than composed into `AGENTS.md`, which only happens at launch. In `--merged` mode and in `ynd marketplace build` OpenCode adds no manifest and no index, only its `.opencode/` MCP and hooks files when the harness has them
- Copilot export is limited to skills and agents — rules and commands are excluded with warnings
- Copilot uses Claude's plugin manifest format (`.claude-plugin/plugin.json`), since Copilot's own plugin loader reads the same schema. At runtime the manifest and MCP config nest under `.copilot/`, the `--plugin-dir` target; an export always puts the manifest at the plugin root beside the skills, and MCP config at `.github/mcp.json` there. In a package that also carries Claude, the shared manifest's `"mcpServers"` names Claude's `mcp/claude.json`; see [MCP Servers: Config File Locations](mcp.md#config-file-locations) for what that means for Copilot

See [ynd export](ynd.md#export) for full command reference.

## Agent Plugins Export

`ynd export --format agent-plugin` writes one package in the
[Agent Plugins](https://agent-plugins.org) open format instead of a tree per
vendor. The portable core (root `plugin.json`, `skills/`, `mcp.json`) is the
same whichever vendors are selected; `-v` chooses which clients' own files
join it, each where that client documents reading them:

| | Loads the format | What `-v` adds to the package |
|---|---|---|
| Codex | yes | `com.openai/hooks/hooks.json`, named under `extensions.com.openai` |
| Copilot / VS Code | yes | `com.github.copilot/agents/` (same subset as the vendor export) |
| Cursor | yes, core only | nothing: Cursor has published no extension namespace, and does not expand `${PLUGIN_ROOT}` or `${PLUGIN_DATA}` |
| OpenCode | no | nothing: OpenCode does not read Agent Plugins packages |
| Claude Code | no | `.claude-plugin/plugin.json`, `mcp/claude.json` and `hooks/claude.json` (named by that manifest, as in its own plugin export), `agents/`, `rules/`, `commands/`, `CLAUDE.md` at the root: a compatibility package, as the spec's migration guide describes |

Anything the selected vendors cannot receive is reported as a warning rather
than dropped in silence. See [ynd export](ynd.md#export) for the rules the
portable `mcp.json` applies to each server.

## Vendor Spec Tracking

Vendor plugin formats evolve frequently. The `vendor-adapters` skill (`.claude/skills/vendor-adapters/`) maintains current documentation links, format mappings, and known discrepancies for each vendor. Consult it when updating adapters or verifying spec compliance.

## Adding a New Vendor

See [CONTRIBUTING.md](https://github.com/eyelock/ynh/blob/main/.github/CONTRIBUTING.md) for how to implement a vendor adapter.
