# OpenCode: Vendor Reference

ynh targets **OpenCode 2.x only**. OpenCode 1.x (the `opencode-ai` npm
package, and most of what `opencode.ai/docs/` still describes) differs in its
launch model, config schema and plugin API; it is refused, not supported.

Findings marked **CONFIRMED** were hand-tested against v2.0.27 (Homebrew
formula `opencode-v2`) using the free `opencode/nemotron-3-ultra-free` model,
which needs no login. The rest are documentation claims.

The repository moved from `sst/opencode` to `anomalyco/opencode`.

## Documentation URLs

| Area | URL |
|------|-----|
| v2 overview | https://opencode.ai/v2/docs |
| v2 Config | https://opencode.ai/v2/docs/config |
| v2 Plugins (loading) | https://opencode.ai/v2/docs/plugins |
| v2 Build a plugin (hooks) | https://opencode.ai/v2/docs/build/plugins |
| v2 MCP servers | https://opencode.ai/v2/docs/mcp-servers |
| v2 Commands | https://opencode.ai/v2/docs/commands |
| Providers | https://opencode.ai/docs/providers/ |
| Config schema | https://opencode.ai/config.json |
| GitHub Repo | https://github.com/anomalyco/opencode |

## Identity

- Binary: `opencode`. `opencode --version` prints `opencode v2.x.y`.
- Install: `brew install anomalyco/tap/opencode-v2` (formula `opencode-v2`).
- The 1.x and 2.x installs both provide an `opencode` binary and can clash.

## Launch model

2.x runs every command through a shared **background service**
(`opencode service status`). **CONFIRMED:** the service ignores the client's
environment, so `OPENCODE_CONFIG_DIR` and `OPENCODE_CONFIG` have no effect on
a plain `opencode` or `opencode run`.

**CONFIRMED:** `--standalone` starts a private server for the invocation, and
that server honours `OPENCODE_CONFIG_DIR=<dir>`. Agents defined in the bundle
are "not found" without `--standalone` and load with it.

So ynh launches `opencode --standalone` with `OPENCODE_CONFIG_DIR` set to the
run dir, via `syscall.Exec`, no symlinks.

## Config bundle layout (`OPENCODE_CONFIG_DIR`)

| Path in bundle | Loaded | Status |
|---|---|---|
| `opencode.json` / `opencode.jsonc` | yes (`config.json` is not) | **CONFIRMED** |
| `AGENTS.md` | yes, as instructions | **CONFIRMED** |
| `agents/<name>.md` | yes | **CONFIRMED** |
| `skills/<name>/SKILL.md` | yes, via the `skill` tool | **CONFIRMED** |
| `plugins/<name>.ts` | yes, auto-discovered | **CONFIRMED** |
| `commands/<name>.md` | unclear: `/name` in an `opencode run` message was not expanded | unconfirmed; recheck in the TUI |

Project `.opencode/` in the working directory also loads, alongside the
bundle. `~/.claude/skills` is also watched and loaded; 2.x has no
`OPENCODE_DISABLE_CLAUDE_CODE*` switch (those 1.x variables are gone from the
binary).

## Config schema (2.x)

Top-level keys (v2 config docs): `$schema, shell, model, default_agent,
update, share, username, permissions, experimental, agents, snapshots,
watcher, formatter, media, tool_output, websearch, mcp, compaction, warming,
skills, commands, instructions, references, worktree, plugins, providers`.

Changes from 1.x that matter to ynh:

- Plural keys: `agents`, `commands`, `permissions`, `plugins`.
- `permissions` is an ordered array of `{action, resource, effect}` rules.
- MCP servers move under `mcp.servers`.
- **`instructions` is accepted but not loaded.** The docs say so and a test
  agrees (**CONFIRMED**). Only `AGENTS.md` carries instructions.

## Instructions

- **CONFIRMED:** the bundle's `AGENTS.md` is read.
- Rules have no separate concept: concatenate them into the bundle's
  `AGENTS.md`. **CONFIRMED:** a rule composed that way is followed (the model
  stated a codeword that only the rule carried).
- No system-prompt flag. Runtime instructions append to the bundle's
  `AGENTS.md` (file-based, like Cursor's `.cursorrules`).

## Agents

Markdown with frontmatter, file name is the agent name. `description`
(required), `mode` (`primary`, `subagent`, `all`), `model`, `permissions`.
JSON form: `agents.<name>` with `description`, `mode`, `system`,
`permissions`. **CONFIRMED:** both forms load from the bundle.

**CONFIRMED by bisecting `opencode run --agent <name>`:** an agent file is
silently "Agent not found" when it lacks `description`, or has any of
Claude's comma-string `tools: Read, Grep`, a bare alias `model: sonnet`
(`provider/model` is accepted) or `color: blue`. Unknown keys such as `name:`,
`allowed-tools:` and `foo:` are harmless, and `mode: subagent|primary` loads.
So the adapter implements `ArtifactTransformer`: it keeps `description`,
`mode`, `permissions` and a `provider/model` model, drops the rest, and
humanises a description from the file name when none is set.

**Permissions in agent markdown (CONFIRMED v2.0.27):** a YAML list of
`{action, resource, effect}` (`effect`: allow, deny, ask). The base policy for
every agent is `* * allow`, and **the last matching rule wins**: `shell "echo *"
deny` blocks `echo SHELLRAN` with the tool error `Permission denied: shell`
(`state.status: "error"`, `executed: false`), while the same deny followed by
an allow lets it run. Actions: `read`, `edit` (edit, write, patch), `glob`,
`grep`, `shell`, `webfetch`, `websearch`, `subagent`, `skill`, `question`,
`execute` (Code Mode's wrapper; MCP tools do not need it, see below),
`external_directory`.

Claude's `tools:` allow-list is therefore translated to `* * deny` followed
by an `allow` per mapped action (Read read, Write/Edit/MultiEdit edit, Bash
shell, Glob, Grep, WebFetch, WebSearch, Task/Agent subagent, Skill skill).
An MCP tool `mcp__server__tool` is allowed (and, when disallowed, denied) as
the action `server_tool`; other unmapped names get no allow and stay denied.
An agent with no `tools:` (or an empty one) gets no rules; an agent that
already has `permissions` keeps them, with only denies appended (below). CONFIRMED with `* deny` plus `shell allow`
that shell runs. Caveat: an agent that denies `shell` (including `* deny`
without a shell allow) cannot be run on the `opencode/*-free` models at all:
the request fails `403 FreeTierError: OpenCode's free tier can only be used
from within OpenCode`, apparently because the free tier insists on the usual
tool set. On v2.2.3 the free tier went further and refused every agent whose
rules start with `* * deny`, even one that allows `shell`, reporting it as
`Free usage limit reached` (an agent with no rules, run just before and after
on the same model, worked). A paid provider (`anthropic/*`, below) runs them.

**Granted MCP tools (CONFIRMED v2.2.3, `anthropic/claude-haiku-4-5`,
ynw-kst.7):** the generated rules work as written. A `tools: Bash,
mcp__echo__ynh_ping` agent (`* * deny`, `allow shell`, `allow echo_ynh_ping`)
called `echo_ynh_ping` and the stub server received it; `ynh_other`, not
granted, was not in its tool list. `execute` does not need an allow: with
`execute` denied, Code Mode is off for the agent and its granted MCP tools are
ordinary tools named `<server>_<tool>`. With `execute` allowed as well, the
same grant puts only `tools.echo.ynh_ping` in the Code Mode catalog and the
model calls it through `execute`. Either way, MCP tools are often missing from
the model's tool list on its first step, while the local server is still
connecting, with or without any rules: an unrestricted agent told to call the
tool as its very first action did not see it. Once the model has made any
other call (a `shell` command, `list_mcp_resources`) the tools are there.

## Skills

Agent Skills `SKILL.md`; `name` must match the directory. **CONFIRMED:** the
model loads a bundle skill through the `skill` tool. A `skills` config key
takes extra directories or URLs.

## MCP

```jsonc
{ "mcp": { "servers": { "name": { "type": "local", "command": ["bunx", "@playwright/mcp"] } } } }
```

**CONFIRMED:** a local stdio server written to the bundle's `opencode.json`
connects and its tool is callable. OpenCode 2 exposes MCP tools through "Code
Mode" by default (`codemode: true`): the model sees one `execute` tool and
calls `tools.<server>.<tool>({})` inside it. Code Mode is on only for an agent
that may use `execute`; an agent with a Claude `tools:` list is denied it, so
its granted MCP tools are ordinary tools (see "Granted MCP tools" above). Each MCP tool it calls still
fires the `tool` hooks under its own id, `<server>_<tool>` (see "MCP tools and
`tool` hooks" below). Local fields: `type`, `command` (array),
`cwd`, `environment`, `disabled`, `codemode`, `timeout`. Remote fields:
`type: "remote"`, `url` (Streamable HTTP), `headers`, `oauth` (default on;
`false` to disable), `disabled`. **Env substitution is `{env:NAME}`**;
`$NAME` and `${NAME}` are not expanded in JSON strings, so ynh rewrites
`${NAME}`. Remote servers were not connected to in testing.

## Hooks: a generated plugin

2.x has no declarative hooks. ynh generates a plugin into the bundle's
`plugins/` that runs the harness's hook commands.

**CONFIRMED** module shape (no `npm install` needed, the runtime provides the
import):

```ts
import { Plugin } from "@opencode/plugin"
export default Plugin.define({
  id: "ynh.hooks",
  setup(ctx) {
    ctx.tool.hook("execute.before", (x) => { /* x.tool, x.input, x.sessionID, x.agent */ })
    ctx.tool.hook("execute.after", (x) => { /* x.status, x.result | x.error */ })
    ctx.permission.hook("evaluate", (x) => { /* x.action, x.resources; set x.effect, x.message */ })
    ctx.session.hook("prompt", (x) => { /* x.prompt.text, x.sessionID */ })
    const ac = new AbortController()
    ;(async () => { for await (const ev of ctx.event.subscribe({ signal: ac.signal })) { /* ev.type */ } })()
    return () => ac.abort()
  },
})
```

Hook domains are singular: `ctx.tool`, `ctx.permission`, `ctx.session`. The
docs' table says "tools" and "permissions"; those are `undefined`.

| ynh event | OpenCode 2 | Status |
|---|---|---|
| `before_tool` | `tool` `execute.before`; **throwing blocks the call** and the message is the tool error | **CONFIRMED** |
| `before_tool` (deny) | `permission` `evaluate`, `x.effect = "deny"`, `x.message` is the reason | **CONFIRMED** |
| `after_tool` | `tool` `execute.after` | **CONFIRMED** |
| `before_prompt` | `session` `prompt` | **CONFIRMED** |
| `on_session_start` | event `session.created` | **CONFIRMED** (event, not a hook) |
| `on_stop` | event `session.execution.succeeded` / `session.execution.failed` | **CONFIRMED** (event, not a hook) |
| `on_session_end` | none: no hook, and no session-end event in the stream (`session.deleted` / `session.archived` are not the end of a run) | **declared unsupported** (`opencodeUnsupportedHookEvents`) |

Tool names differ from Claude's. **CONFIRMED** ids (from `x.tool` in a
`tool` hook): `shell`, `read`, `write`, `edit`, `glob`, `grep`, `webfetch`,
`subagent` (Claude's Task). `skill` is the skill tool by the docs; `websearch`
is unprobed. Matchers need a mapping table, and the generated plugin tests a
matcher (as an anchored regex) against the OpenCode id and its Claude names.

The matcher is a **JavaScript** regex: the plugin builds it with
`new RegExp("^(?:" + matcher + ")$")`. The other vendors' matchers are RE2,
so ynh holds an OpenCode matcher to the syntax both read the same.
`validateOpencodeMatcher` (`internal/vendor/opencode.go`) runs at generate
time: it compiles the matcher as RE2, then `opencodeMatcherNotJS` refuses
anything JavaScript rejects or reads differently: any `(?` group but `(?:`
(`(?i)`, `(?P<name>...)`), a POSIX class (`[[:alpha:]]`), escapes outside
`\d \D \s \S \w \W \f \n \r \t \v`, `\b`/`\B` outside a class, `\xHH` and
escaped punctuation (so `\A`, `\z`, `\Q...\E`, `\pL`, `\x{41}`, octal), and a
`]` first in a class (`[]a]`, `[^]a]`). Why: a matcher JavaScript rejects used
to throw inside `execute.before` and block every tool call (ynw-kst.2). The
plugin now compiles each matcher once in a `try`/`catch` and a bad one matches
nothing (its hook never runs, reported on stderr), and syntax JavaScript reads
differently without throwing would silently match the wrong tools; so the
check refuses both kinds before the plugin is written. It is over-strict in
one known case: any `[:` inside a class is taken for a POSIX class, so `[[:]`
(the set of `[` and `:`, the same in both dialects) is refused; write `[\[:]`.
Keep the check, `opencodeSameEscapes` and the user doc (`docs/hooks.md`) in
step when changing one of them.

Further **CONFIRMED** behaviour of the generated plugin (v2.0.27):

- `before_tool` with `exit 2` throws; the model sees the hook's stderr as the
  tool error (`Error: blocked: use make`), the tool does not run. A matcher of
  `Bash` fires on `shell`; `Edit` does not fire on `shell` but fires on `edit`;
  a native `shell` matcher also works.
- `before_prompt` throwing **does block**: the run fails before the model is
  called, but the surfaced error is an opaque `UnexpectedStatus: 500`, not the
  hook's message. Prefer logging in a prompt hook over blocking.
- `after_tool` fires with `x.status`, `x.result.output`. The payload's `input`
  keys are OpenCode's (`path`, `oldString`, `newString`, `command`), so the
  plugin adds Claude's `file_path`, `old_string`, `new_string`.
- `session.created` and `session.execution.succeeded` fire once per run; the
  session id is `ev.durable.aggregateID` (also `ev.data.sessionID`).
- `import.meta.url` resolves to the plugin file, so a hook's `./` script is
  anchored at `plugins/..` (the plugin sets `YNH_HOOK_ROOT`).
- The prompt text a `prompt` hook sees from `opencode run "<msg>"` arrives
  wrapped in literal double quotes.
- `ctx.location.directory` honours `$PWD`, not the process's real cwd; a
  process that only `chdir`s (a Go test) still sees the old directory.

Contract declarations (read by `make check-vendor-parity`):

- Timeout: `opencodeHookTimeoutField = "timeout"`. Each hook's timeout, in
  seconds, rides in the plugin's `HOOKS` table and is passed to `spawnSync`;
  60 s when none is set. No caps: the plugin enforces it, not OpenCode.
  CONFIRMED live on v2.2.3 (ynw-kst.7): a `before_tool` hook that sleeps 8 s
  and exits 2 was killed after its `timeout: 3` and the shell call went
  ahead; with `timeout: 15` it ran to the end and blocked the call.
- Session-start context: `unsupported:`. `on_session_start` runs from an
  event-stream subscriber, which cannot add context; its stdout is discarded.
- Matchers: honoured on `execute.before` / `execute.after` only
  (`opencodeMatcherEvents`); elsewhere `HookWarnings` says it is ignored.
- Workspace: no flag (`opencodeWorkspaceUnsupported`); `--help` on 2.2.3 lists
  no add-dir equivalent.

## CLI

```
opencode [--standalone] [--auto] [-c] [-s <id>] [--prompt <text>] [<directory>]
opencode run [--standalone] [-c] [-s <id>] [--fork] [-m provider/model#variant]
             [--agent <name>] [--format default|json] [-f <file>] [--title] [--auto] [<message...>]
```

- Interactive: `opencode`. Initial prompt: `--prompt`.
- One-shot: `opencode run`. **CONFIRMED:** piped stdin is the prompt.
- Effort: no flag; a model variant, `-m provider/model#variant`.
- Auto-approve: `--auto` approves everything not explicitly denied. No
  edits-only mode; that would need generated `permissions` rules.
- Removed since 1.x: `--variant`, `--command`, `--dir`.

## Sessions and resume

- **CONFIRMED:** `-s <id>` resumes that session; `-c` continues the last one.
  Neither opens a picker.
- **CONFIRMED** (v2.2.3, 2026-10-30): a prompt reaches a resumed session.
  One-shot: `opencode run --standalone -s <id> -- <prompt>` (and `-c`).
  Interactive: `opencode --standalone -s <id> --prompt <prompt>` submits it into
  the resumed session. The model answered from the earlier turns both ways.
- **CONFIRMED:** `--format json` emits newline-delimited events, each with
  `sessionID` (types `step_start`, `text`, `tool_use`, `tool`, `step_finish`,
  `error`).
- The store is SQLite (`~/.local/share/opencode/opencode.db`), so
  `ResolveLastSession` returns `ErrSessionLookupUnavailable`, as Codex does.

## Plugin distribution and marketplace

- npm or local plugins via the `plugins` key; `opencode plugin add`.
- No plugin manifest format and no marketplace: `GeneratePluginManifest` and
  `GenerateMarketplaceIndex` return nil.
- Does not read agent-plugins.org packages.

## Auth for testing

`opencode/*-free` models work with no login. The free list changes
(`exo-free`, once the default, is deprecated), so always pin `-m`. The
Anthropic provider takes an API key; Anthropic prohibits using a Claude
Pro/Max subscription through OpenCode. CONFIRMED (v2.2.3): with
`ANTHROPIC_API_KEY` exported, `ynh run ... -v opencode -m
anthropic/claude-haiku-4-5` works with no `opencode auth`, as ynh launches
OpenCode standalone. A bare `opencode models` asks the shared background
service, which does not see the key and lists no `anthropic/*` model;
`opencode models --standalone` does.

## Known gaps

- Slash commands from the bundle are unconfirmed under `opencode run`.
- Remote MCP servers were not connected to; only local stdio was.
- A Claude `tools:` list becomes deny-by-default `permissions`. The live
  check that a `tools: Read` agent cannot run `shell` needs a paid provider:
  the free models refuse any agent with `* * deny` rules. It was not run
  with Anthropic in ynw-kst.7, which probed only the MCP grant.
- No edits-only auto-approve.
- `on_session_start` and `on_stop` ride the event stream, not a documented
  hook, so they are the most likely to move.

## Further CONFIRMED findings (review round)

- **Tool ids for file edits:** `edit` and `write` are the ones the free model
  calls. The binary also carries `apply_patch` (the patch tool used with GPT
  models) and `todowrite`; neither could be triggered with
  `nemotron-3-ultra-free`, so they are mapped (apply_patch to Edit/Write,
  todowrite to TodoWrite) but unprobed. The permission action `edit` covers
  edit, write and patch.
- **MCP tools and `tool` hooks:** Code Mode's `execute` tool fires
  `execute.before` and `execute.after` for itself (input is the JS `code`),
  and then again for each tool it calls, under the id `<server>_<tool>`
  (`echo_ynh_ping`). A generated hook matcher `mcp__echo__ynh_ping` blocked
  the MCP call with the hook's message. The plugin skips the `execute`
  wrapper for hooks with no matcher so they do not run twice. Re-confirmed
  on v2.2.3 with `deepseek-v4.1-flash-free` (ynw-kst.4): a matcherless hook
  saw `opencode_list_mcp_resources`, `echo_ynh_ping` and `echo_ynh_other` but
  never `execute`; the matcher blocked `echo_ynh_ping` before the stub server
  received it; an unmatched `echo_ynh_other` reached the server. The block
  throws inside the `execute` code, so a later call in the same code
  (`ynh_other` after `ynh_ping`) did not run either. With Code Mode off, the
  same matcher still blocks: on v2.2.3 with `anthropic/claude-haiku-4-5`
  (ynw-kst.10), a `tools: Bash, mcp__echo__ynh_ping` agent called
  `echo_ynh_ping` as an ordinary tool, no `execute` call appeared, the hook
  ran and the model got its message, and the stub server logged no call; with
  the hook removed, the same agent's call reached the server. The hook payload's
  `tool_name` is the OpenCode id `echo_ynh_ping`, not Claude's
  `mcp__echo__ynh_ping`.
- **`--` works in `opencode run`:** `run --standalone -m x --format json --
  "--help me"` sent the text as a message. ynh puts extra args before `--`.
- **Scoped shell permissions:** an `allow shell "echo *"` after `* deny`
  permits `echo hi` and refuses `ls`; a later `deny shell "echo bad *"` wins
  for `echo bad thing` (last match wins). Claude's `Bash(git status:*)` maps to
  resource `git status *`. Scoped Read/Edit/WebFetch patterns use Claude's own
  syntax, so they get no allow and, as a disallowed entry, deny the whole
  action.
- **`disallowedTools` / `disallowed-tools`** translate to deny rules after the
  allows. With `permissions` already present those rules are kept as written
  and only denies are appended (a deny for every known action a `tools:` list
  does not allow, plus the disallowed ones), so access never widens.
- **Inline `permissions:` is refused with a tool list:** an agent with
  `tools:` or `disallowedTools` whose `permissions:` is written inline
  (`permissions: [{...}]`) fails assembly, naming the agent. Those rules
  cannot be appended to an inline value, so the agent would otherwise run
  with every tool. The fix is to write `permissions:` as a block list, or to
  remove it. An empty `permissions: []` counts as no permissions and the
  lists are translated as usual; inline permissions on an agent without tool
  lists are kept as written.
- **MCP permission actions (CONFIRMED v2.2.3):** `deny echo_ynh_ping` withdrew
  that tool from Code Mode (the model was told `Unknown tool
  'echo.ynh_ping'`) while `echo_ynh_other` on the same server stayed
  callable, so `disallowedTools: mcp__echo__ynh_ping` maps to a working deny.
