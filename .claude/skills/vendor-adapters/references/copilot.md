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
| Hooks Configuration | https://docs.github.com/en/copilot/reference/hooks-configuration |
| CLI Plugin Reference | https://docs.github.com/en/copilot/reference/cli-plugin-reference |
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
caller and silently broken for the other. Each caller now asks for its own
layout: `GeneratePluginManifest` writes the run-dir manifest at
`.copilot/.claude-plugin/plugin.json`, and `GenerateExportPluginManifest`
writes the export manifest at `.claude-plugin/plugin.json`, which the exporter
and marketplace prefer. The layout used to be inferred by testing for
`outputDir/.copilot`, which an export with MCP servers tripped over: it had
just written `.copilot/.mcp.json` itself, so its manifest nested under
`.copilot/` while its skills sat at the root (#471).

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
- Plugin (legacy manifest): `hooks.json` or `hooks/hooks.json`, or the manifest `hooks`
  field (path or inline object). Claude-format hooks (PascalCase events such as
  `PreToolUse`) are accepted with Claude's matcher semantics. CONFIRMED 2026-10-04,
  docs.github.com/en/copilot/reference/cli-plugin-reference and
  docs.github.com/en/copilot/reference/hooks-configuration.
- Repo inline: a `hooks` field in `.github/copilot/settings.json` / `settings.local.json`
  (`.claude/settings.json` is also read). Policy files under `/etc/github-copilot/policy.d/`.
  Combined order: policy, user, project, plugins; every matching entry runs.
  (docs.github.com/en/copilot/reference/hooks-configuration, checked 2026-10-30.)
- ynh now writes Copilot hooks (this section replaces "ynh emits no Copilot hooks"). See
  "How ynh generates hooks for Copilot" below. See "CONFIRMED live" below for what has been run.
- Copilot reads `.claude-plugin/plugin.json`, the file Claude writes, rendered for both by
  `claudePluginManifest`. In a merged package that also targets Claude, its `hooks` field
  names `hooks/claude.json` (Claude format, `"${CLAUDE_PLUGIN_ROOT}"/` anchors, #483), so
  Copilot may find Claude-format hooks there as well as its own `hooks.json`.

## Hook Events (14 — confirmed complete)

Native keys are **camelCase**, nested under a top-level `"hooks"` object:
`{"version": 1, "hooks": {"<eventName>": [...]}}`. Copilot also accepts a PascalCase
VS Code compatible spelling of the same events with Claude-shaped snake_case payloads
(`SessionStart`, `SessionEnd`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`,
`PostToolUseFailure`, `Stop` (= `agentStop`), `SubagentStart`, `SubagentStop`,
`PermissionRequest`, `Notification`, `PreCompact`, `ErrorOccurred`;
docs.github.com/en/copilot/reference/hooks-configuration). `sessionEnd` carries `reason`:
complete, error, abort, timeout, user_exit. Entry fields also include `type` (`command`,
`http`, `prompt`; default `command`), `command` (cross-platform fallback copied to `bash`
and `powershell` when absent), `exec`/`args`, `cwd`, `env`, `timeoutSec` (default 30,
alias `timeout`), `matcher`. Timeouts are always fail-open. Exit 2 is a warning, except a
deny on `preToolUse` and `permissionRequest`.

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

## How ynh generates hooks for Copilot

`Copilot.GenerateHookConfig` and `Copilot.GeneratePluginHookConfig`
(`internal/vendor/copilot.go`) write real hook files. This reverses the earlier decision
to return `nil, nil`, which rested on the hand test below. The plugin route is CONFIRMED
to fire (see below).

| ynh output | File | Generator |
|------------|------|-----------|
| `ynh run`, `ynd preview`, agent loop | `.copilot/hooks/hooks.json`: the `hooks/hooks.json` of the `.copilot/` plugin dir passed as `--plugin-dir` | `Copilot.GenerateHookConfig` (scripts copied to `.copilot/`, `SessionHookScriptDir`) |
| `ynd export -v copilot`, `ynd marketplace build` | `hooks.json` at the plugin root | `Copilot.GeneratePluginHookConfig` |
| `ynd export --format agent-plugin` | `com.github.copilot/hooks/hooks.json` (`AgentPluginLayout().Hooks`) | same generator, via the exporter |

Format chosen, and why:

- **Plugin route, not `.github/hooks/`.** The page says "Repository and inherited-agent
  hooks require workspace admission", and the v1.0.75 test below saw a repository hook
  never fire in an untrusted folder. A plugin's hooks are not mentioned as gated. ynh also
  would not write executable commands into the user's repository, where they would run for
  everyone who clones it. `buildCopilotArgs` therefore projects nothing for hooks (unlike
  instructions and MCP).
- **Root `hooks.json` in a Copilot-only export, not `hooks/hooks.json`; none in a merged package with Claude.** Both are default names Copilot
  reads; Claude Code, Cursor and Codex read `hooks/hooks.json` and none read a root
  `hooks.json`, so a merged package never hands Copilot's format to another vendor (#469).
- **PascalCase events**, flat entries `{"type": "command", "command": ..., "matcher": ...}`,
  `"version": 1`. The page says a PascalCase `PreToolUse` takes Claude-style matchers
  (`*`, `Edit|Write`, runtime `bash` -> `Bash`), so canonical matchers keep their meaning
  and a script reads the same payload as on Claude. `command` rather than `bash`/`powershell`
  so one file serves both platforms. `timeoutSec` is written only when the
  manifest hook sets `timeout` (CONFIRMED live, Copilot CLI 1.0.105, 2026-10-30: of two 3 s SessionStart hooks, `timeoutSec: 1` was killed and `timeoutSec: 10` completed); otherwise the 30 s
  default applies. The page documents no maximum.
- **Mapping:** before_tool PreToolUse, after_tool PostToolUse, before_prompt
  UserPromptSubmit, on_stop Stop (= agentStop), on_session_start SessionStart,
  on_session_end SessionEnd. `after_tool` is **not** fanned out to `PostToolUseFailure`: the
  other adapters map it to success-only events too. `before_prompt` is not
  `userPromptTransformed`, which fires later on already-rewritten content.
- **Matchers** are written on PreToolUse and PostToolUse only; ynh warns where one is
  dropped.
- **`./` scripts** are anchored `"${PLUGIN_ROOT}"/...` (`copilotPluginRootVar`). The CLI
  plugin reference documents `PLUGIN_ROOT` for stdio MCP servers and the
  `CLAUDE_PLUGIN_ROOT`/`COPILOT_PLUGIN_ROOT` aliases only in agent `mcp-servers`
  frontmatter, and says nothing about hooks; live runs show all three are set for hooks.

### CONFIRMED live (Copilot CLI 1.0.105, 2026-10-30)

Run with David's approval, against ynh's assembled and exported output.

- **Session layout** (`ynh run -v copilot`): all six events fire from `.copilot/hooks/hooks.json` via
  `--plugin-dir`, in an UNTRUSTED folder. Plugin hooks need no folder trust. The v1.0.75
  trust finding applies to repository `.github/hooks/`, which ynh does not write and which
  was not retested.
- **Variables (#497):** `PLUGIN_ROOT`, `CLAUDE_PLUGIN_ROOT` and `COPILOT_PLUGIN_ROOT` are all
  set to the plugin dir; `"${PLUGIN_ROOT}"/...` expands.
- **Working directory:** a hook script runs with the PLUGIN dir as cwd, not the project. The
  project dir is the payload's `cwd`.
- **Payload:** Claude-style: `tool_name` is `Bash` for shell; `SessionEnd` has
  `reason: "complete"` in a `-p` run.
- **Matchers:** `Bash` works on `PreToolUse` and `PostToolUse`; an `Edit` matcher did not
  fire for a shell call.
- **Exports via `--plugin-dir`:** each hook fired exactly once in the Copilot-only export
  (root `hooks.json`), the merged package, and the Agent Plugins export
  (`com.github.copilot/hooks/hooks.json`).
- **Merged package (#499):** Copilot loads `hooks/claude.json`, named by the manifest's
  `hooks`, and IGNORES root `hooks.json`; ynh therefore writes no root `hooks.json` when
  Claude is in the package.
- **Installed plugin from a merged package** (local marketplace, scratch `COPILOT_HOME`):
  Copilot loads `.github/mcp.json` (both servers started) and does NOT load the
  manifest-named `mcp/claude.json`. `${PLUGIN_ROOT}` expands in an MCP `command`.

### Still untested

Hooks of a plugin installed with `copilot plugin install` (runs used `--plugin-dir`), and
the repository `.github/hooks/` route.

### The v1.0.75 hand test (history)

**CONFIRMED: hooks silently no-op in an untrusted folder**, with no error and no
warning. A `.github/hooks/copilot-cli-policy.json` carrying a `preToolUse` hook
did not fire at all against a fresh scratch repo — despite `--allow-all-tools`,
`--allow-all-paths` and `--experimental`. It began firing only after the
directory was added to `trustedFolders`.

**CONFIRMED: no CLI flag grants that trust.** `--add-dir` (alone, and combined
with `--allow-all-paths`) still did not make the hook fire. `--add-dir` extends
path *access*; it does not confer trusted-folder status. They are separate
gates.

One further wrinkle if you revisit this: trust did not stay where it was
written. `trustedFolders` was hand-set in `~/.copilot/settings.json`, but the
CLI's auto-managed `~/.copilot/config.json` absorbed it and became the persisted
source — `settings.json` was later found empty. Treat `config.json` as
runtime-authoritative, while being wary of writing to a file its own header
marks as app-managed.

The canonical-event mapping the old section proposed is superseded by the table above.
`subagentStart`, `subagentStop`, `errorOccurred`, `preCompact`, `permissionRequest` and
`notification` have no canonical equivalent and stay unmapped.

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
  The adapter maps the canonical transport: `stdio → local`,
  `streamable-http → http`, `sse → sse`.
- Set `tools` explicitly to `["*"]`. It defaults that way via `copilot mcp add`,
  but that was never verified for a hand-written file.
- SSE is flagged legacy/deprecated by GitHub's own docs.
- No documented OAuth — static credentials only.

**CONFIRMED: a `.mcp.json` bundled inside a `--plugin-dir` plugin is NOT read**
(tested at both `.mcp.json` and `.github/mcp.json` placements inside the plugin
dir). `GenerateMCPConfig` still writes `.copilot/.mcp.json` for interface
consistency, but `buildCopilotArgs` re-reads it and projects it into the real
project's `.github/mcp.json`, the path that actually works.

An export writes `GeneratePluginMCPConfig`'s `.github/mcp.json` at the plugin
root instead: the Copilot CLI plugin reference lists `.mcp.json` and
`.github/mcp.json` as the default MCP paths of a legacy (`.claude-plugin`)
plugin, and Codex already writes `.mcp.json` into a merged package. Whether an
installed plugin (rather than one loaded with `--plugin-dir`) reads it has not
been hand-tested.

The reference lists the legacy MCP sources as "`.mcp.json`, `.github/mcp.json`,
or the `mcpServers` manifest field" (fetched 2026-10-04) and does not say how they
combine. A Copilot-only export's `.claude-plugin/plugin.json` names no MCP file,
so Copilot reads `.github/mcp.json`. In a package that also carries Claude, the
shared manifest's `mcpServers` names Claude's `mcp/claude.json` (#481), whose
entries lack `type` and `tools`, and a merged package that includes Codex also
has Codex's `.mcp.json` (same format) at the root. Which of those an installed
Copilot plugin loads: CONFIRMED live (Copilot CLI 1.0.105, 2026-10-30, package installed from a local
marketplace into a scratch `COPILOT_HOME`): `.github/mcp.json` loads (both servers started)
and the manifest-named `mcp/claude.json` does NOT (neither of its servers started).
`${PLUGIN_ROOT}` expands in an MCP `command`. Closes #499.

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

## Auto-Update Drops Launch Args

**CONFIRMED (v1.0.80 -> v1.0.83, 2026-09-14):** Copilot checks for an update on
every launch. When one is available it downloads and swaps its own binary,
then tears down the foreground session it just started — visible in
`~/.copilot/logs/process-*.log` as "Successfully updated binary" immediately
followed by "Unregistering foreground session". Whatever the original
invocation carried, including an `-i` initial prompt, is lost with that
session; the CLI comes back up as a fresh, idle interactive session.

Since `launchCopilot` replaces the ynh process via `syscall.Exec`, there is no
process left to retry after such a mid-launch restart. `buildCopilotArgs`
always passes `--no-auto-update` to prevent the update from happening during
an `ynh run` launch at all. Users still get updates via their own `copilot
update` / the CLI's normal update flow outside of an ynh-launched session.

## Key CLI Details

- Binary: `copilot`
- Auto-update: **disabled by ynh** via `--no-auto-update` on every launch —
  see "Auto-Update Drops Launch Args" above
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
