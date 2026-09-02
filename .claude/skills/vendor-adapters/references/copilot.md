# GitHub Copilot CLI — Vendor Reference

Copilot CLI ships weekly and GitHub's own docs warn that "commands, flags, and
available models change often." Verify every flag against `copilot help` before
trusting it. Findings below marked **CONFIRMED** were hand-tested against
v1.0.75, except the agent- and skill-discovery section, which was tested
against v1.0.80. Treat the rest as documentation claims.

## Documentation URLs

| Area | URL |
|------|-----|
| Install | https://docs.github.com/en/copilot/how-tos/set-up/install-copilot-cli |
| CLI Reference | https://docs.github.com/en/copilot/reference/copilot-cli-reference |
| Configure CLI | https://docs.github.com/en/copilot/how-tos/copilot-cli/set-up-copilot-cli/configure-copilot-cli |
| Custom Instructions | https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/add-custom-instructions |
| Agent Skills | https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/add-skills |
| Custom Agents | https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/create-custom-agents-for-cli |
| MCP Servers | https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/add-mcp-servers |
| Hooks | https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/use-hooks |
| Hooks Reference | https://docs.github.com/en/copilot/reference/hooks-reference |
| Plugins & Marketplace | https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/plugins-marketplace |
| Plugin Reference | https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-plugin-reference |
| GitHub Repo | https://github.com/github/copilot-cli |
| Changelog | https://github.com/github/copilot-cli/blob/main/changelog.md |

## Plugin Format

Identity fields only — `name` is the sole required key.

```json
{
  "name": "my-harness",
  "version": "0.1.0",
  "description": "...",
  "author": { "name": "..." },
  "keywords": ["..."]
}
```

**CONFIRMED:** the manifest is required, not optional. Removing it entirely
(skills present, no manifest anywhere) made a previously-working skill stop
loading, silently and with no error.

**CONFIRMED:** four manifest locations all work identically via
`--plugin-dir <dir>` — `.plugin/`, repo root, `.github/plugin/`, and
`.claude-plugin/` (a documented Claude-compatibility path). ynh emits the
`.claude-plugin/` form; see `internal/vendor/copilot.go`.

## Plugin Directory Structure

```
<plugin-dir>/
  .claude-plugin/
    plugin.json          # required — skills silently fail to load without it
  skills/
    <name>/SKILL.md
  agents/
    <name>.md
```

**The manifest must sit alongside wherever skills and agents actually landed.**
This is the one Copilot-specific trap in the adapter: `ynh run` nests artifacts
under `.copilot/` (matching what `--plugin-dir` is given), while `ynd export`
flattens them to the export root. A fixed manifest path was correct for one
caller and silently broken for the other. `copilotRunDirLayout(outputDir)`
detects the caller by testing for `outputDir/.copilot`.

## Native discovery: agents and skills

Tested against **v1.0.80** with `copilot --agent <name>`, which resolves the
name before contacting the model and lists what it found — a free, deterministic
probe:

```console
$ copilot --agent __probe__ -p x --allow-all-tools
No such agent: __probe__, available: native, plainmd
```

### Agents

**CONFIRMED: project agents live in `.github/agents/`.** A file there is
discovered with no configuration:

```console
$ (empty project)                        available:
$ .github/agents/native.agent.md          available: native
```

**CONFIRMED: both `.agent.md` and plain `.md` are accepted there.** GitHub's
docs describe agents as *"defined by a Markdown file with an `.agent.md`
extension"*; `.md` also works, so the extension is not the discriminator.

```console
$ + .github/agents/plainmd.md             available: native, plainmd
```

**CONFIRMED: a plugin-bundled `agents/<name>.md` IS loaded via `--plugin-dir`,
and is namespaced by the plugin.** This is the one that matters for ynh, and the
namespacing is easy to miss:

```console
$ copilot --agent __probe__ --plugin-dir ./canary-plugin
No such agent: __probe__, available: ynh-canary:probe-agent
```

The agent is `<plugin-name>:<agent-name>`, taken from the plugin manifest's
`name`. **A user of a ynh-exported Copilot plugin selects
`ynh-guide:code-review`, not `code-review`.** Both `.md` and `.agent.md` work
inside a plugin.

This is the opposite of the `AGENTS.md` and `.mcp.json` findings below, where
bundled files are *not* read — so "bundled in a plugin" is not one rule in
Copilot, it is per artifact type.

Personal agents in `~/.copilot/agents/` and home-wins-on-collision are
documented by GitHub but **not tested here**; they would require writing to the
tester's home directory.

### Skills

**CONFIRMED from the CLI itself** — `copilot skill --help` on v1.0.80 lists the
discovery order, which is stronger evidence than the web docs and is pinned to a
version:

```
Project   .github/skills/, .agents/skills/, or .claude/skills/
Personal  ~/.copilot/skills/ or ~/.agents/skills/
Plugin    Installed plugins that bundle skills
Custom    Directories added with `copilot skill add <directory>`
```

**CONFIRMED: `.github/skills/<name>/SKILL.md` is discovered** — it appears in
`copilot skill list` under "Project skills".

**UNRESOLVED: a plugin skill loaded via `--plugin-dir` does not appear in
`copilot skill list`.** Note the wording above is "**Installed** plugins", and
`copilot plugin install` accepts only a marketplace, `owner/repo` or a URL —
there is no local-directory install. So this may be `skill list` not consulting
a session's `--plugin-dir` rather than the skill failing to load. Deciding it
needs a live session, which `copilot plugins list` says outright:

> Custom agents and session-scoped hooks are not yet covered — both require a
> live session and will be added in a follow-up.

### What could not be probed for free

`copilot plugins` (plural) reports "The plugins command is not available" on
this install, so its richer inspection was unavailable. Everything above was
established without a single model call, using `--agent`'s resolution error and
the CLI's own help.

## Hook Config Paths

- `.github/hooks/*.json` (repo scope, any filename)
- `~/.copilot/hooks/*.json` (user scope)

## Hook Events (14 — confirmed complete)

All keys are **camelCase**, nested under a top-level `"hooks"` object:
`{"version": 1, "hooks": {"<eventName>": [...]}}`.

| JSON key | Fires when | Output processed? |
|---|---|---|
| `sessionStart` | New or resumed session begins | **No — output is ignored** (see below) |
| `sessionEnd` | Session terminates | No |
| `userPromptSubmitted` | User submits a prompt | No |
| `userPromptTransformed` | Runtime has rewritten the prompt into model-facing content | Yes — can rewrite |
| `preToolUse` | Before each tool executes | Yes — allow/deny/modify |
| `postToolUse` | After a tool completes **successfully** | Yes — modify result, inject `additionalContext` |
| `postToolUseFailure` | After a tool completes with a **failure** | Yes — recovery guidance |
| `agentStop` | Main agent finishes a turn | Yes — `decision: "block"` forces continuation |
| `subagentStart` | A subagent is spawned | Optional — `additionalContext` prepended |
| `subagentStop` | A subagent completes | Yes — block/force continuation |
| `errorOccurred` | An error occurs during execution | No |
| `preCompact` | Context compaction about to begin | No — notification only |
| `permissionRequest` | Before the permission service runs | Yes — `behavior: allow`/`deny`. **CLI only** |
| `notification` | CLI emits a system notification | Optional. **CLI only** |

**CONFIRMED: `sessionStart` output is ignored.** The hook fires (verified via a
marker-file side effect), but neither a flat `{"additionalContext": "..."}` nor
Claude's nested `{"hookSpecificOutput": {...}}` reached the model's context.
Do not use it for context injection.

## Hook Format

```json
{
  "version": 1,
  "hooks": {
    "preToolUse": [
      {
        "type": "command",
        "bash": "...",
        "powershell": "...",
        "cwd": "...",
        "timeoutSec": 30,
        "env": {},
        "matcher": "regex"
      }
    ]
  }
}
```

Top-level also accepts `"disableAllHooks": false`.

## Why ynh does not generate hooks for Copilot

`GenerateHookConfig` returns `nil, nil` in `internal/vendor/copilot.go`. This is
deliberate, and it is the single most important thing on this page.

**CONFIRMED: hooks silently no-op in an untrusted folder**, with no error and no
warning. A `.github/hooks/copilot-cli-policy.json` carrying a `preToolUse` hook
did not fire at all against a fresh scratch repo — despite `--allow-all-tools`,
`--allow-all-paths` and `--experimental`. It began firing only after the
directory was added to `trustedFolders`.

**CONFIRMED: no CLI flag grants that trust.** `--add-dir` (alone, and combined
with `--allow-all-paths`) still did not make the hook fire. `--add-dir` extends
path *access*; it does not confer trusted-folder status. They are separate
gates.

A hook config that silently never fires is worse than an honest "not supported",
so ynh declines to emit one. `docs/vendors.md` states this for users.

One further wrinkle if you revisit this: trust did not stay where it was
written. `trustedFolders` was hand-set in `~/.copilot/settings.json`, but the
CLI's auto-managed `~/.copilot/config.json` absorbed it and became the persisted
source — `settings.json` was later found empty. Treat `config.json` as
runtime-authoritative, while being wary of writing to a file its own header
marks as app-managed.

**Canonical event mapping**, should the trust problem ever be solved:

- `before_tool` → `preToolUse`
- `after_tool` → **both** `postToolUse` and `postToolUseFailure`. Copilot splits
  by outcome what the other three vendors combine; fanning out to both loses
  less than picking one.
- `before_prompt` → `userPromptSubmitted` (not `userPromptTransformed`, which
  fires later on already-rewritten content)
- `on_stop` → `agentStop`
- No canonical equivalent for `sessionEnd`, `subagentStart`, `subagentStop`,
  `errorOccurred`, `preCompact`, `permissionRequest`, `notification`. Leave
  unmapped — same precedent as Cursor's missing `afterShellExecution`.

Do not confuse these JSON config keys with the `@github/copilot-sdk` callback
method names (PascalCase / `onXxx`), which are a separate surface.

## MCP Format

`.mcp.json` (project root) or `.github/mcp.json` (repo-shared). **CONFIRMED**
both work, both are live-read with no restart, both report as "Workspace"
source, and they are interchangeable — verified by moving the file between them
mid-session. `~/.copilot/mcp-config.json` is user scope, lower precedence.

```json
{
  "mcpServers": {
    "name": {
      "type": "local",
      "command": "...",
      "args": [],
      "tools": ["*"]
    }
  }
}
```

- **`type` is required**: `local` for stdio, `http` or `sse` for remote.
  Translate `command present → local`, `url present → http`.
- Set `tools` explicitly to `["*"]`. It defaults that way via `copilot mcp add`,
  but that was never verified for a hand-written file.
- SSE is flagged legacy/deprecated by GitHub's own docs.
- No documented OAuth — static credentials only.

**CONFIRMED: a `.mcp.json` bundled inside a `--plugin-dir` plugin is NOT read**
(tested at both `.mcp.json` and `.github/mcp.json` placements inside the plugin
dir). `GenerateMCPConfig` still writes `.copilot/.mcp.json` for interface
consistency, but `buildCopilotArgs` re-reads it and projects it into the real
project's `.github/mcp.json` — the path that actually works.

**Never shell out to `copilot mcp add`**: it only writes user-level
`~/.copilot/mcp-config.json` and has no flag to target the workspace file.

## Instructions

Copilot reads **multiple formats simultaneously with no defined precedence**:
`AGENTS.md`, `.github/copilot-instructions.md`,
`.github/instructions/*.instructions.md`, `CLAUDE.md` / `.claude/CLAUDE.md`,
`GEMINI.md`, plus user-level `~/.copilot/copilot-instructions.md`.

**CONFIRMED: an `AGENTS.md` bundled inside a `--plugin-dir` plugin is NOT read
as instructions.** Verified with a behavioral canary — positive control (same
file at repo root) adopted, negative (same file at plugin-dir root) ignored;
tested twice, and again with `--add-dir`, all negative.

So `GenerateSystemPrompt` emits `AGENTS.md` for consistency with the other three
adapters, but the delivery mechanism for `ynh run` is different: `buildCopilotArgs`
reads the assembled `AGENTS.md` and projects it into the project as
`.github/instructions/ynh-harness.instructions.md` with `applyTo: "**/*"`
frontmatter — **confirmed required**; path-scoped instructions files do nothing
without it. That filename is uniquely ynh-owned, so overwriting it each run
touches nothing the user authored.

**Never shell out to `copilot init`** — it is an LLM-invoking scaffolder that
infers its own content from the repo, fundamentally incompatible with ynh's
model of authoritative harness-authored instructions.

## Marketplace Format

`marketplace.json` at `.github/plugin/`. Two pre-registered defaults:
`copilot-plugins`, `awesome-copilot`. Install via `copilot plugin install`,
`/plugin install`, or declarative `enabledPlugins` in `settings.json`.

## Key CLI Details

- Binary: `copilot`
- Config dir: **no single project dotfolder** — spread across `.github/*`
  subpaths plus root files. User home is `~/.copilot/` (override: `COPILOT_HOME`)
- Plugin loading: `--plugin-dir <directory>` (repeatable), same pattern as Claude
- Interactive: `syscall.Exec`, `NeedsSymlinks() == false`
- Non-interactive: `-p, --prompt <text>` — **requires `--allow-all-tools`** or
  the run hangs on a permission prompt with no TTY
- Initial prompt into interactive: `-i, --interactive <prompt>`
- Model: `--model <name>`, in-session `/model`
- Auto-approve: `--allow-all` / `--yolo`, plus granular `--allow-all-tools`,
  `--allow-all-paths`, `--allow-all-urls`, `--allow-tool`, `--deny-tool`,
  `--allow-url`, `--deny-url`
- Add-dir / cwd: `--add-dir <dir>` (repeatable), `-C <directory>`
- Session resume: `--session-id <id>` sets the UUID for a *new* session;
  `-r, --resume[=id]` resumes by session ID, task ID, ID prefix (7+ hex), or
  exact case-insensitive name; `--continue` resumes the most recent. **ynh can
  choose the UUID up front** — better than Cursor, whose backend must generate
  and track its own.
- Session-scoped MCP: `--additional-mcp-config <json-or-@file>`

## What Copilot Does NOT Support (in plugins)

- **Hooks via ynh** — see the trust-folder section above. Not a format gap; a
  silent-failure gap.
- **Custom slash commands** — no user-definable `.prompt.md` equivalent as of
  2026-07-29. Open requests: github/copilot-cli#618, #942, #1113. Plugin
  manifests can declare a `commands` component, but how it surfaces is
  unconfirmed.
- **Sandbox flag** — **CONFIRMED ABSENT** (`copilot help permissions`). No
  `--sandbox` / `--no-sandbox`. The permission model is tool/URL/path-scoped
  instead. There is nothing to map.
- **Plugin-bundled instructions and MCP** — both confirmed unread from inside
  `--plugin-dir`; ynh projects them into the project instead.

## Known ynh Discrepancies (as of 2026-08-29)

- `copilot skill list` does **not** show `--plugin-dir`-bundled skills at all —
  only Project/Personal/Builtin categories. This is a display gap in that
  command, not a functional one. Do not treat its output as truth about whether
  a plugin's skills loaded; ask the live model instead.
- Copilot is the only one of the four vendors with no project config dotfolder,
  so `ConfigDir()` returning `.copilot` is a ynh staging-directory convention,
  not a Copilot-native path.
