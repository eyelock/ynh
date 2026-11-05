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

## Known gaps

ynh claims more than each vendor yet gets. In v0.16.0:

- **Codex** does not yet receive commands. Hooks and session context reach it under `ynh run` for an accepted harness only, not under `ynh agent run` ([Codex hook trust](hooks.md#codex-hook-trust)). Instructions (written to `AGENTS.md`), rules (folded into them), MCP servers, skills, agents, focuses and profiles do. Under `ynh run`, Codex starts in the run directory without `-C`, so the project's own `AGENTS.md` is never read.
- **Rules** reach Claude Code, Codex and Copilot only by being folded into their instructions under `ynh run`: always on, with no path scoping.
- **OpenCode**: delegates cannot run as subagents (a harness's own agents can), `on_session_start` does not fire, `on_session_end` cannot fire (OpenCode has no such event, a vendor limit), and session context is not injected. `ynd export -v opencode` drops delegates silently.
- **Cursor**: print mode skips `before_prompt` and `on_stop`. A vendor limit: `cursor-agent` run directly, without ynh, skips them too ([evidence](hooks.md#hooks-cursor-s-cli-does-not-run)).
- **Delegates** carry only their instructions on Codex, Cursor and Copilot. On Claude Code a delegate also carries its MCP servers, not its hooks or workspace. A delegate does not run on OpenCode. "A full harness" holds for includes, not yet for delegates.
- **Resume**: Cursor and Copilot refuse a prompt on resume. On Codex a resume with a prompt passes workspace folders as `writable_roots`, which takes effect only under the `workspace-write` sandbox (ynh warns on stderr otherwise; ynh does not change Codex's sandbox, and `codex exec` defaults to read-only).
- **Telemetry**: `ynh run` emits none on any vendor; only `ynh agent run` does.
- **Project instructions** (the project's own `AGENTS.md` or `CLAUDE.md` under `ynh run`) are unverified on every vendor except Codex, where they are not read.

These are fixed in v0.17.0 (parity) and later, except what a vendor cannot do. See [ynh #679](https://github.com/eyelock/ynh/issues/679).

## Feature Support

What each vendor receives from a harness in v0.16.0, from the vendor parity audit (live runs of every vendor CLI against ynh develop, 2026-11). **Y** works, **N** does not yet work (ynh can fix it), **X** the vendor cannot, **U** unverified.

| Feature | Claude Code | Codex | Cursor | Copilot CLI | OpenCode |
|---|---|---|---|---|---|
| Instructions | Y | Y | Y | Y | Y |
| Project instructions under `ynh run` | U | N | U | U | U |
| Skills | Y | Y | Y | Y | Y |
| Agents | Y | Y | Y | Y | Y |
| Rules | Y | Y | Y | Y | Y |
| Commands | Y | N | Y | Y | U |
| MCP servers | Y | Y | Y | Y | Y |
| Hooks | Y | Y, accepted harnesses only | X, `before_prompt` and `on_stop` in print mode; the rest Y | Y | N |
| Session context | Y | Y, accepted harnesses only | Y | Y | N |
| Profiles | Y | Y | Y | Y | Y |
| Focuses, includes, sensors | Y | Y | Y | Y | Y |
| Workspace folders | Y | Y | U | Y | Y |
| `delegates_to` | N | N | N | N | N |
| Resume | Y | Y | U | U | Y |
| `ynd export` | Y | Y | Y | Y | N |
| Telemetry from `ynh run` | N | N | N | N | N |

Notes: Cursor receives `--add-dir` for a granted folder, but its effect is unverified: in print mode with the sandbox on, Cursor wrote to `/tmp`, `/var/tmp` and `/Users/Shared` with or without the flag. OpenCode has no flag; ynh appends an `external_directory` allow rule per folder to each agent, which a deny-by-default agent needs (live run, 2.2.3: denied without the rule, read with it). Cursor print mode skips `before_prompt` and `on_stop`. OpenCode has no `on_session_end` event. Codex commands should ship as skills. Cursor and Copilot refuse a prompt on resume. OpenCode `ynd export` drops delegates silently. Only `ynh agent run` emits telemetry.

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

Codex reads a project's custom agents only as TOML files in `.codex/agents/` ([Codex's subagent docs](https://learn.chatgpt.com/docs/agent-configuration/subagents)), so a harness's own agents are written there as `<name>.toml`, not linked as Markdown. Each sets the three fields a custom agent must: `name` and `description` from the agent's frontmatter (the file name, and a description naming the agent, when it has none) and `developer_instructions` from its body. An `effort` of `low`, `medium` or `high` becomes `model_reasoning_effort` at the same level, the mapping `ynh agent run --effort` uses for Codex ([Effort](agent.md#effort)); `xhigh` and `max` are dropped, as ynh maps only those three. Codex has no tool allowlist, so `tools` and `disallowedTools` cannot carry over: the run prints a warning naming the agent for each of them it declares, such as `warning: agent "reviewer": Codex custom agents cannot restrict tools, so its tools list is dropped`. When `tools` leaves out `Bash` (whole, or as `Bash(...)` for some commands), the agent file sets `features.shell_tool = false`, so the agent has no shell tool, and the warning says so; whether a custom agent honours that setting has not been checked live. No other frontmatter field carries over: Claude Code's `model` names are not Codex models, and a custom agent inherits the session's permissions and MCP servers. Codex registers custom agents only from enabled config layers, and a project's `.codex/` is enabled only when the project is trusted. So on every Codex launch, `ynh run` (interactive, one-shot, with a first prompt, or resumed) and `ynh agent run` register each agent file they wrote in the run directory with `-c agents.<name>.config_file="<absolute path>"` and `-c agents.<name>.description=...`. Codex treats those flags as a config layer that is always enabled, so no trust is needed, whatever Codex's working directory. Codex splits a `-c` key on every `.`, so an agent whose `name` holds anything but letters, digits, `-` and `_` (a `.`, for instance) is not registered, and the run prints a warning naming it. OBSERVED live on codex-cli 0.166.0 (2026-10-31, ynw-t7i): through `ynh run` one-shot and resumed, and through `ynh agent run --backend codex`, a `spawn_agent` call naming the agent or delegate as its `agent_type` succeeded, and Codex's session log for the subagent records it under that agent role. The interactive `codex` has not been run live. A Codex export carries no agents, since Codex plugins have none.

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

**OpenAI Codex** - Full interactive and non-interactive support. Uses symlink-based artifact installation. Harness instructions are composed into `AGENTS.md` in the run directory, the file Codex reads (OBSERVED live on codex-cli, 2026-11-04: a codeword set only in the harness instructions was returned by `codex exec` run in the run directory). **Known limit: the project's own `AGENTS.md` is not read under `ynh run`.** Codex reads `AGENTS.md` from the working directory it is started in (walking up to the Git root, plus `~/.codex/AGENTS.md`), and ynh starts it in the run directory, which is not a Git repository and not inside the project, so only the harness's `AGENTS.md` is found (OBSERVED live on codex-cli 0.166.0, 2026-11-04: with a different codeword in each file, `codex exec` in a run-directory-like folder returned only that folder's word, and `codex exec -C <project>` returned only the project's). This was already so before ynh wrote `AGENTS.md`; it is not yet fixed. Requires `codex` CLI installed. Codex requires a Git working tree — if running in a non-Git directory (e.g. Docker containers), pass `--skip-git-repo-check` as a vendor flag. See [openai.com/codex](https://openai.com/codex). `ynh agent run --backend codex` does not approve the harness's MCP tools, so a worker's MCP calls fail under any `--auto-approve` level short of `all` ([MCP servers](agent.md#mcp-servers)); a one-shot `ynh run` does, see [mcp_manual_approval](harnesses.md).

**Cursor Agent** - Full interactive and non-interactive support. Uses symlink-based artifact installation. Requires `agent` CLI installed (`curl https://cursor.com/install -fsS | bash`). Uses `-p` for non-interactive prompts. See [cursor.com/cli](https://cursor.com/cli).

**GitHub Copilot CLI** - Full interactive and non-interactive support. Uses `--plugin-dir` for artifact loading, like Claude. Requires `copilot` CLI installed. Non-interactive runs need `--allow-all-tools` (added automatically). Harness instructions are projected into the calling project's `.github/instructions/ynh-harness.instructions.md`, because Copilot doesn't read plugin-bundled `AGENTS.md` via `--plugin-dir`; that file is left behind after the run and holds no secrets. MCP servers are not projected: Copilot doesn't read a plugin-bundled `.mcp.json` either, so ynh passes the run directory's 0600 `.copilot/.mcp.json` with `--additional-mcp-config @<file>`, which keeps the expanded credentials out of your project. The instructions file gets one frontmatter block, `applyTo: "**/*"` so it is always on, followed by any fields of the harness instructions' own frontmatter except `applyTo`. **Hooks** are written into the `--plugin-dir` plugin (`.copilot/hooks/hooks.json`), not into your repository, and fire in an untrusted folder (verified on Copilot CLI 1.0.105). See [Hooks: Copilot format and trust](hooks.md#copilot-format-and-trust). See [github.com/features/copilot/cli](https://github.com/features/copilot/cli).

**OpenCode** - Full interactive and non-interactive support, for **OpenCode 2.x only**; 1.x is refused with a clear message (the two differ in launch model, config schema and plugin API, and both install an `opencode` binary). Install 2.x with `brew install anomalyco/tap/opencode-v2`. OpenCode 2 runs every command through a shared background service that ignores the client's environment, so ynh launches `opencode --standalone` with `OPENCODE_CONFIG_DIR` pointing at the run directory's `.opencode/`; no symlinks. Skills load from `skills/`. Agents load from `agents/`, with their frontmatter translated: OpenCode silently drops an agent that has a comma-string `tools:`, a bare model alias such as `sonnet`, or a `color:`, so ynh keeps `description`, `mode` (an agent with none gets `mode: all`, since OpenCode refuses to run a mode-less agent as a subagent), `permissions` and a `provider/model` model, and Claude `tools:` become deny-by-default permissions (an agent whose `permissions:` is written inline as well is refused at assembly, as its tool lists could not be enforced; write `permissions:` as a block list instead). Commands go to `commands/`; whether a bundle slash command expands under `opencode run` is unconfirmed. OpenCode 2 reads `AGENTS.md` only (its `instructions` config key is accepted but ignored) and has no concept of rules, so ynh composes `.opencode/AGENTS.md` at launch from the harness instructions, the rules and the runtime context. MCP servers go in `.opencode/opencode.json` under `mcp.servers`; by default OpenCode reaches MCP tools through its Code Mode, so the model calls them from inside one `execute` tool; an agent with a Claude `tools:` list may not use `execute`, so Code Mode is off for it and the MCP tools it is granted are ordinary tools. **Hooks** are supported through a generated plugin, `.opencode/plugins/ynh-hooks.ts`, because OpenCode 2 has no declarative hooks; see [Hooks](hooks.md#opencode-hooks-are-a-generated-plugin) for the event mapping and caveats. Resume with `-s <id>` or `-c` works, but ynh cannot pin the session itself because OpenCode keeps sessions in SQLite, so it is the same as Codex. There is no plugin manifest and no marketplace format, and OpenCode does not read Agent Plugins packages: `ynd export -v opencode` writes the run-directory layout with no manifest (see [Export Output by Vendor](#export-output-by-vendor)), and `ynd marketplace build` adds no OpenCode manifest or index, only its `.opencode/` MCP and hooks files when the harness has them. For `ynh agent run --backend opencode`, effort is refused (select a variant with `--model provider/model#variant`) and auto-approve accepts `all` only. OpenCode is not a [`ynd compress`/`ynd inspect`](ynd.md) LLM vendor: it has no argument that turns its tools off. OpenCode's free models need no login, which makes one handy for a smoke test: pick one from `opencode models` (for example `opencode/deepseek-v4.1-flash-free`), as the free list changes. See [opencode.ai/v2/docs](https://opencode.ai/v2/docs).

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
| **Hooks** | `hooks/claude.json` | `hooks/cursor.json` | `hooks/codex.json` | `hooks.json` (Copilot-only; a merged package with Claude uses `hooks/claude.json`) | `.opencode/plugins/ynh-hooks.ts` |
| **MCP servers** | `mcp/claude.json` | `mcp.json` | `.mcp.json` | `.github/mcp.json` | `.opencode/opencode.json` |
| **Marketplace** | `.claude-plugin/marketplace.json` | `.cursor-plugin/marketplace.json` | `.agents/plugins/marketplace.json` | `.github/plugin/marketplace.json` (best-effort, unverified) | *not supported* |

Key differences between runtime and export:

- **Runtime** places artifacts inside the vendor config directory (e.g., `.claude/skills/`)
- **Export** places artifacts at the plugin root (e.g., `skills/`) — the standard distributable layout
- Claude export writes `AGENTS.md` for instructions, not `CLAUDE.md` (which would conflict with the installing project's own)
- Codex export is limited to skills — agents, rules, commands, and delegates are excluded with warnings
- Hooks go to the session path at runtime (`.claude/hooks/hooks.json`, `.cursor/hooks.json`, `.codex/hooks.json`, `.copilot/hooks/hooks.json`) and to `hooks/<vendor>.json` at the plugin root in an export, named by the `"hooks"` field of that vendor's manifest, never both. A plugin never reads the session path, and there is no shared `hooks/hooks.json`, so a merged package never hands one vendor's format to another (see [Hooks: Config File Locations](hooks.md#config-file-locations))
- MCP config goes to the session path at runtime (`.claude/.mcp.json`, `.cursor/mcp.json`) and to the plugin path in an export (`mcp/claude.json`, named by the `"mcpServers"` field of `.claude-plugin/plugin.json`, and Cursor's root `mcp.json`), never both, for the same reason. Every vendor in a merged package has its own MCP file, and each manifest names only a file that is there (see [MCP Servers: Config File Locations](mcp.md#config-file-locations))
- Codex is included in merged export (`--merged`) and in `ynd marketplace build`, which exports each harness the same way: `.codex-plugin/plugin.json` sits beside the other vendors' manifests and points only at `skills/`, so Codex ignores the agents, rules and commands the other vendors read from the same directory. `ynd export --merged` says so with the same warning a per-vendor Codex export prints. Pass `-v` without `codex` to leave it out
- OpenCode has no plugin manifest, no marketplace format and no Agent Plugins support. `ynd export -v opencode` writes the same layout the run directory holds (artifacts, `AGENTS.md`, `.opencode/opencode.json` and the generated hooks plugin), but nothing installs it as a plugin and no marketplace entry is produced. Rules are exported as files rather than composed into `AGENTS.md`, which only happens at launch. In `--merged` mode and in `ynd marketplace build` OpenCode adds no manifest and no index, only its `.opencode/` MCP and hooks files when the harness has them
- Copilot export is limited to skills and agents — rules and commands are excluded with warnings
- Under `ynh run`, rules reach Claude Code, Copilot and Codex by being appended to the instructions they read, always on and without path scoping; an export package cannot carry them to those vendors. See [Rules](artifacts.md#rules). Codex has no way to read commands, so a harness's commands do not reach it
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
| Copilot / VS Code | yes | `com.github.copilot/agents/` (same subset as the vendor export) and `com.github.copilot/hooks/hooks.json` |
| Cursor | yes, core only | nothing: Cursor has published no extension namespace, and does not expand `${PLUGIN_ROOT}` or `${PLUGIN_DATA}` |
| OpenCode | no | nothing: OpenCode does not read Agent Plugins packages |
| Claude Code | no | `.claude-plugin/plugin.json`, `mcp/claude.json` and `hooks/claude.json` (named by that manifest, as in its own plugin export), `agents/`, `rules/`, `commands/`, `CLAUDE.md` at the root: a compatibility package, as the spec's migration guide describes |

Anything the selected vendors cannot receive is reported as a warning rather
than dropped in silence. See [ynd export](ynd.md#export) for the rules the
portable `mcp.json` applies to each server.

## Vendor Spec Tracking

Vendor plugin formats evolve frequently. The `vendor-adapters` skill (`.claude/skills/vendor-adapters/`) maintains current documentation links, format mappings, and known discrepancies for each vendor. Consult it when updating adapters or verifying spec compliance.

## Adding a New Vendor

Adding a vendor touches more than its adapter file. The [`add-vendor-adapter`](https://github.com/eyelock/ynh/blob/main/.claude/skills/add-vendor-adapter/SKILL.md) skill is the checklist of every file to change, and [CONTRIBUTING.md](https://github.com/eyelock/ynh/blob/main/.github/CONTRIBUTING.md#vendor-adapters) describes the `Adapter` interface and the launch patterns.
