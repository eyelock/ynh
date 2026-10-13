# Hooks

Hooks are shell commands that vendors execute at specific lifecycle events during an agent session. They bridge the **guide layer** (what ynh manages) to the **sensor layer** (linters, tests, validators) by declaring *when* a command should run, without embedding the tool itself.

A harness declares hooks in `.agents/harness/plugin.json` at the top level. At assembly time, ynh translates them into the vendor-native config format. A hook command is a regular shell command: a tool on the host machine, a script in the project, or a script the harness ships in its own tree. A command that starts with `./` names a script the harness ships; ynh carries it with the hooks and rewrites the path to reach it, in a session and in a plugin alike. See [Hook script paths](#hook-script-paths).

> **Note:** Hooks can vary by [profile](harnesses.md#profiles). When a profile is selected, its `hooks` field is merged per event: an event the profile declares replaces the default for that event, and events it does not declare are inherited. See [Profiles](profiles.md#merge-semantics).

## Why Hooks Matter

Martin Fowler's harness engineering framework distinguishes feedforward controls (guides) from feedback controls (sensors). Hooks are the connection point: a harness declares "run this linter before every tool use" and the vendor runtime enforces it. The harness author defines the *intent*; the hook script provides the *mechanism*.

OpenAI's harness engineering guidance emphasizes that hook blocking messages should contain **agent-legible remediation instructions** — when a hook blocks an action, the error output should tell the agent *what to do differently*, not just *what went wrong*.

## Canonical Events

ynh defines six canonical hook events. Each vendor translates these to its native event names.

| Canonical Event | Description |
|----------------|-------------|
| `before_tool` | Runs before a tool/command is invoked. Can block execution. |
| `after_tool` | Runs after a tool/command completes. Can reject the result. |
| `before_prompt` | Runs before a user prompt is submitted to the model. |
| `on_stop` | Runs when the agent finishes responding, at the end of **every turn**, not once per session. For once-per-session work use `on_session_end`. See [on_stop output semantics](#on-stop-output-semantics-claude). |
| `on_session_start` | Runs when a session starts **and when it resumes** (and, on Claude Code, on `/clear` and after compaction too), so the script must be idempotent. Claude Code and Codex filter on the source (`startup`, `resume`, `clear`, `compact`, ...) through the hook entry's existing `matcher` field. |
| `on_session_end` | Runs when a session ends. Best-effort and time-boxed; see [on_session_end semantics](#on-session-end-semantics). |

`on_stop` and `on_session_end` are different events: a session of ten turns runs `on_stop` ten times and `on_session_end` once, if the vendor gets to run it at all.

### on_session_start output becomes model context

On every vendor but OpenCode, what an `on_session_start` hook prints reaches the model. **Write plain text to stdout**; ynh does the rest. A hook that prints a line of orientation, today's tasks or a wake block works the same on Claude Code, Codex, Copilot and Cursor.

The vendors do not agree on how the output is read. Verified on 2026-10-30 with a hook printing a unique token and a one-shot prompt asking the model to repeat any token in its context:

| Vendor | Plain stdout reaches the model? | The field that does | ynh |
|--------|---------------------------------|---------------------|-----|
| Claude Code | Yes | also `hookSpecificOutput.additionalContext` | writes the command as declared |
| Codex | Per its docs ("Plain text on stdout is added as extra developer context"); **not verified live** | also `hookSpecificOutput.additionalContext` | writes the command as declared |
| Copilot CLI 1.0.105 | No | `{"additionalContext": "..."}` | wraps the command |
| Cursor (cursor-agent 2026.10.01) | No | `{"additional_context": "..."}` | wraps the command |
| OpenCode 2 | No | none: the hook runs from the event stream (`session.created`), which cannot add context, and the generated plugin discards its stdout | declares `unsupported`, so assembly warns |

On Copilot and Cursor ynh writes the hook as `sh <root>/hooks/ynh-session-context.sh <field> '<your command>'`. The wrapper is a small generated POSIX `sh` and `awk` script shipped inside the session layout and inside every exported package (`hooks/ynh-session-context.sh`), so it works on a machine where ynh is not installed, with no python or jq. It:

- passes the hook payload on stdin to your command, keeps its stderr, and exits with its exit status;
- turns your stdout into `{"<field>": "<stdout>"}`, escaping quotes, backslashes, newlines, tabs and control characters (UTF-8 passes through);
- passes stdout through unchanged when it is already a JSON object, so a hook can still choose its own output;
- emits nothing when your command prints nothing.

In a package that Claude Code and Copilot share one hook file for, the wrapper emits both shapes at once. Other events are untouched.

Each vendor declares how it delivers this as `<vendor>SessionStartContext` in its adapter (`plain`, `json-field:<name>` or `unsupported:<reason>`), and `make check-vendor-parity` requires it. A vendor that declares `unsupported` makes `ynd validate`, `ynd lint` and assembly warn that the hook's output does not reach the model there.

### on_session_end semantics

`on_session_end` is for cleanup and bookkeeping that is worth doing and safe to lose: flush a log, write a summary, remove a scratch file. Do not put anything on it that must happen.

- **Best-effort and time-boxed.** The vendor gives it a short budget and then moves on. Claude Code gives every `SessionEnd` hook a shared budget of **1.5 seconds** by default; a hook that sets a longer `timeout` raises the shared budget to match, up to **60 seconds** ([code.claude.com/docs/en/hooks](https://code.claude.com/docs/en/hooks)). Codex defaults to **1 second** and allows at most 3 ([learn.chatgpt.com/docs/hooks](https://learn.chatgpt.com/docs/hooks)). **Without a `timeout`** on the hook, every vendor's default applies, so on Claude Code `on_session_end` work must finish within about 1.5 seconds or it is cancelled (CONFIRMED live: a 3 s hook was cancelled). A plugin hook's own `timeout` does **not** raise that budget (code.claude.com/docs/en/hooks, SessionEnd: "Timeouts set on plugin-provided hooks don't raise the budget."; CONFIRMED live, Claude Code, 2026-10-30: `"timeout": 10` on a 3 s hook was still "Hook cancelled" at about 1.5 s). The override is the environment variable `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS` (milliseconds), which sets the budget and the timeout of each hook without its own `timeout` (Claude Code 2.1.268 and later). See [Claude SessionEnd budget](#claude-sessionend-budget).
- **It cannot block.** There is no decision control. Exit code 2 only shows stderr to the user; output is advisory on every vendor. A `SessionEnd` hook cannot keep a session open.
- **It fires on more than quitting.** On Claude Code it also runs on `/clear`, on `/resume` into another session, on logout and on exit from the prompt (the matcher takes `clear`, `resume`, `logout`, `prompt_input_exit` or `other`). Codex also runs it when a conversation is archived or deleted, and when one has been idle for 30 minutes and no connected client holds it open.
- **It may not fire at all** on a crash, a `kill -9`, a lost connection, or in a cloud agent. Anything that depends on it needs a fallback.
- **Keep scripts fast and idempotent.** The budget is short, and a session that is cleared, resumed and ended can run the same hook several times.

Per vendor:

| Vendor | Native event | Notes |
|--------|--------------|-------|
| Claude Code | `SessionEnd` | 1.5 s shared budget, raised only by `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS`, up to 60 s (a plugin hook's `timeout` does not raise it); cannot block. CONFIRMED live: fires at the end of a `-p` run, after `Stop`, and a 3 s hook was cancelled ("Hook cancelled") (Claude Code, 2026-10-30) |
| Codex | `SessionEnd` | **Main thread only**, not subagents. 1 s default, 3 s maximum. Always runs synchronously, even with `async`. Its matcher filters `reason`, which is currently always `other` |
| Cursor | `sessionEnd` | Tied to the **IDE session**: cloud agents do not run it. CONFIRMED live to fire from the **CLI** in print mode (cursor-agent 2026.10.01-e373342, 2026-10-30); the IDE is untested. Fire-and-forget; the response is logged and not used |
| Copilot | `SessionEnd` | `reason` is one of complete, error, abort, timeout, user_exit; CONFIRMED live to fire at the end of a `-p` run with `reason: "complete"` (Copilot CLI 1.0.105, 2026-10-30) |
| OpenCode | none | Not supported: OpenCode 2 has no session-end hook, and its event stream has no session-end event (a session persists so it can be resumed; `session.deleted` and `session.archived` are not the end of a run). The hook is not written, and assembly warns |

## Hooks Cursor's CLI does not run

Cursor's docs do not say which hooks the CLI runs. Tested live in print mode (`ynh run -v cursor --trust -- "<prompt>"`, cursor-agent 2026.10.01-e373342, 2026-10-30) with all six events in `.cursor/hooks.json`:

| Canonical event | Cursor event | Print mode |
|-----------------|--------------|------------|
| `before_tool` | `preToolUse` | fires for every tool type seen (Grep, Read, Shell) |
| `after_tool` | `postToolUse` | fires |
| `on_session_start` | `sessionStart` | fires |
| `on_session_end` | `sessionEnd` | fires |
| `before_prompt` | `beforeSubmitPrompt` | **does not fire** |
| `on_stop` | `stop` | **does not fire** |

Also CONFIRMED there: a `Shell` matcher fires only for shell calls, a `Bash` matcher never fires, and a hook script runs with the run directory as its working directory, so a bare `./scripts/x.sh` works. Interactive CLI and IDE behaviour is untested.

So when ynh launches Cursor non-interactively (`ynh run` with a prompt and no `--interactive`, and the Cursor worker of `ynh agent run`, which uses `--print`) and the assembled hooks include `before_prompt` or `on_stop`, it prints a warning naming each event:

```
  warning: hook event on_stop: Cursor's CLI does not run stop hooks in print mode (confirmed live), so those hooks will not fire this session
```

## Events a vendor cannot express

ynh never drops a hook silently. If a vendor has no equivalent for a canonical event, assembly and export print a warning naming the event and the vendor, and leave that hook out of that vendor's file; every other vendor still gets it:

```
  warning: hook event on_session_end has no equivalent on cursor, so it is not written
```

`ynd validate` and `ynd lint` report the same thing as a **warning**, not an error, for the vendors the harness targets (its `default_vendor`, or every vendor when it sets none). Today all six events map on Claude Code, Codex, Cursor and Copilot; OpenCode maps five and declares `on_session_end` unsupported, so this fires for an `on_session_end` hook on OpenCode and for a matcher the vendor cannot honour (see [Matchers](#matchers)). `make check-vendor-parity` requires every canonical event to be either mapped or declared unsupported, with a reason, for every vendor, so a new event cannot be forgotten on one of them.

## Manifest Format

Hooks are declared under the top-level `hooks` key in `.agents/harness/plugin.json`. Each event maps to an array of hook entries:

```json
{
  "name": "my-harness",
  "version": "0.1.0",
  "hooks": {
    "before_tool": [
      {
        "matcher": "Bash",
        "command": "/usr/local/bin/check-dangerous-commands.sh"
      }
    ],
    "after_tool": [
      {
        "command": "/usr/local/bin/run-linter.sh"
      }
    ],
    "on_stop": [
      {
        "command": "/usr/local/bin/cleanup.sh"
      }
    ]
  }
}
```

Each hook entry has:

| Field | Required | Description |
|-------|----------|-------------|
| `command` | Yes | Shell command to execute |
| `matcher` | No | Pattern to scope the hook. Tool name for `before_tool` and `after_tool`; on Claude Code and Codex it also filters the `on_session_start` source and the `on_session_end` reason. See [Matchers](#matchers) |

| `timeout` | No | Most seconds the vendor lets the command run: a whole number from 1 to 3600. See [Timeout](#timeout) |

### Timeout

`timeout` is written to the vendor's own field when it has one. Left out, the vendor's default applies (Claude 600 s for a command hook and a shared 1.5 s for `SessionEnd`, Codex 600 s and 1 s for `SessionEnd`, Copilot 30 s, Cursor platform-dependent, OpenCode 60 s).

| Vendor | Field written | Cap ynd applies | Source |
|--------|---------------|-----------------|--------|
| Claude Code | `timeout` (seconds) | `on_session_end`: 60 | [code.claude.com/docs/en/hooks](https://code.claude.com/docs/en/hooks): "Seconds before canceling". It bounds the one hook but, for a plugin's `SessionEnd` hook, does not raise the shared budget; see [Claude SessionEnd budget](#claude-sessionend-budget) |
| Codex | `timeout` (seconds) | `on_session_end`: 3 | [learn.chatgpt.com/docs/hooks](https://learn.chatgpt.com/docs/hooks): "The `timeout` field is in seconds"; `SessionEnd` defaults to 1 and supports up to 3 |
| Cursor | `timeout` (seconds) | none documented | [cursor.com/docs/hooks](https://cursor.com/docs/hooks), "Command format": per-script `timeout` in seconds |
| OpenCode | `timeout` (seconds), in the generated plugin's hook table | none | OpenCode has no hook config; the generated plugin runs each command with that many seconds, 60 when none is set, and a hook that times out lets the call proceed and is reported on stderr |
| Copilot | `timeoutSec` (seconds) | none documented | [docs.github.com: hooks configuration](https://docs.github.com/en/copilot/reference/hooks-configuration): "Timeout in seconds. Default: 30". CONFIRMED live (Copilot CLI 1.0.105, 2026-10-30): of two 3 s `SessionStart` hooks, `timeoutSec: 1` was killed before finishing and `timeoutSec: 10` completed |

The 3600-second schema maximum is ynh's own bound; no vendor documents a general one. Where a vendor caps an event below the request, ynd writes the cap and prints a warning naming the event, the vendor, the requested value and the cap, rather than refuse: the hook still runs, with the longest budget the vendor allows, and the other vendors get the value as asked. If a vendor has **no** timeout field, ynd writes none and warns that the vendor's default applies instead; `ynd validate` and `ynd lint` report the same warning for the vendors the harness targets. All five vendors have a field today, and `make check-vendor-parity` requires every vendor to declare whether it does.

```
  warning: hook timeout 120s on on_session_end exceeds the 60s claude allows for it, so 60s is written
```

The vendors' own timeouts and `SessionEnd` budgets are described under [on_session_end semantics](#on-session-end-semantics).

#### Claude SessionEnd budget

Claude Code ignores the `timeout` of a plugin-provided `SessionEnd` hook when it sets the budget, so writing `timeout` alone does not let an `on_session_end` hook run past about 1.5 s. The override is `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS`, in milliseconds (it sets the budget and the timeout of each hook without its own, since Claude Code 2.1.268). ynh handles the two cases differently:

- **Sessions ynh launches** (`ynh run`, `ynd preview`'s launch, and the Claude worker of `ynh agent run`): when any assembled `on_session_end` hook has a `timeout`, ynh sets the variable in Claude's environment to the largest such timeout times 1000, capped at 60000 (CONFIRMED live, Claude Code, 2026-10-30: a 3 s `on_session_end` hook with `timeout: 10` is cancelled at about 1.5 s with the per-hook timeout alone, and runs to completion once ynh sets the variable). The per-hook `timeout` is still written, since it bounds that hook. A value you already set is never overridden; if it is lower than the hooks ask for, ynh warns. (The agent worker's environment is an allowlist, so your value counts there only if you passed it through with `env_passthrough`.)
- **Exports and marketplace packages**: ynh cannot set an environment, so the export warns when an `on_session_end` hook has a `timeout` above 1.5 s: `claude ignores a plugin hook's timeout for SessionEnd, so on_session_end will be cancelled after about 1.5 s; set CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS=10000 in the environment that launches Claude Code`. Set it yourself.

### Matchers

A matcher is written exactly as declared. Claude Code and Codex take it on every event; the others scope it more narrowly:

| Vendor | Where the matcher is written | Matched against |
|--------|------------------------------|-----------------|
| Claude Code | every event | tool name (`Bash`, `Edit\|Write`), session source or end reason |
| Codex | every event (ignored by `UserPromptSubmit` and `Stop`) | tool name, session source, end reason |
| Cursor | `preToolUse` and `postToolUse` only | Cursor's tool type as a regex: `Shell`, `Read`, `Write`, `Grep`, `Delete`, `Task`, `MCP:<tool>` |
| Copilot | `PreToolUse` and `PostToolUse` only | the tool name; `PreToolUse` takes Claude-style names (`Bash`, `Edit\|Write`) |
| OpenCode | `before_tool` and `after_tool` only (ynh warns when a matcher is set on another event) | an anchored regex tested against OpenCode's tool id and the Claude names that map to it (`Bash` matches `shell`); see [OpenCode hooks are a generated plugin](#opencode-hooks-are-a-generated-plugin) |

On a Cursor event with no documented matcher the matcher is left out and a warning says the hook runs unfiltered. On Cursor `preToolUse`, a Claude tool name such as `Bash` will not match (Cursor calls it `Shell`); ynd writes the matcher as-is and warns when it sees `Bash`, `Edit` or `MultiEdit`.

## Vendor Translation

Each vendor uses different event names and config file formats. GitHub Copilot CLI's hooks are generated too and were verified live (Copilot CLI 1.0.105); read [Copilot format and trust](#copilot-format-and-trust) for what was and was not tested.

### Event Name Mapping

| Canonical | Claude Code | Cursor | Codex | Copilot | OpenCode |
|-----------|-------------|--------|-------|---------|----------|
| `before_tool` | `PreToolUse` | `preToolUse` | `PreToolUse` | `PreToolUse` | `tool` hook `execute.before` |
| `after_tool` | `PostToolUse` | `postToolUse` | `PostToolUse` | `PostToolUse` | `tool` hook `execute.after` |
| `before_prompt` | `UserPromptSubmit` | `beforeSubmitPrompt` | `UserPromptSubmit` | `UserPromptSubmit` | `session` hook `prompt` |
| `on_stop` | `Stop` | `stop` | `Stop` | `Stop` | event `session.execution.succeeded` / `session.execution.failed` |
| `on_session_start` | `SessionStart` | `sessionStart` | `SessionStart` | `SessionStart` | event `session.created` |
| `on_session_end` | `SessionEnd` | `sessionEnd` | `SessionEnd` | `SessionEnd` | *none*: no session-end hook or event |

Sources: [Claude Code](https://code.claude.com/docs/en/hooks), [Codex](https://learn.chatgpt.com/docs/hooks) (12 events; `developers.openai.com/codex/hooks` redirects there), [Cursor](https://cursor.com/docs/hooks), [Copilot](https://docs.github.com/en/copilot/reference/hooks-configuration).

Cursor's `before_tool` and `after_tool` used to map to `beforeShellExecution` and `afterFileEdit`, which cover shell commands and file edits only. They now map to `preToolUse` and `postToolUse`, which fire for any tool (Shell, Read, Write, Grep, Delete, Task, MCP), so the two events mean the same on Cursor as on Claude Code and Codex. A harness whose Cursor hooks relied on the narrower scope will now see them run on more tools; scope them with a `matcher` (`Shell`, `Write`).

Copilot accepts the same events in camelCase (`preToolUse`, `agentStop`, `userPromptSubmitted`, ...) with camelCase payload fields, or in PascalCase with the VS Code compatible, Claude-shaped snake_case payload. ynd writes **PascalCase**, so a hook script reads the same payload fields as on Claude Code and a canonical matcher keeps its Claude tool names.

OpenCode's entries are hooks and events of a generated plugin, not config keys; see [OpenCode hooks are a generated plugin](#opencode-hooks-are-a-generated-plugin). OpenCode has nothing that fires when a session ends, so an `on_session_end` hook is not written for it and ynh warns.

### Config File Locations

| Vendor | Format |
|--------|--------|
| Claude Code | Three-level nesting: event > matcher group > hook array |
| Cursor | Flat: event > hook array (with `"version": 1` required) |
| Codex | Three-level nesting: event > matcher group > hook array (same structure as Claude) |
| Copilot | Flat: event > array of `{"type": "command", "command": ..., "cwd": ".", "matcher": ...}` entries, with `"version": 1` |
| OpenCode | No config format: a generated TypeScript plugin that runs each hook command |

Each vendor reads hooks from a different file depending on how the harness reaches it: a session in a project directory, or an installed plugin. ynh writes each file only where that vendor reads it:

| Vendor | Session (`ynh run`, `ynd preview`, `ynh agent`) | Plugin (`ynd export`, `ynd marketplace build`) |
|--------|--------------------------------------------------|-----------------------------------------------|
| Claude Code | `.claude/hooks/hooks.json`: `ynh run` passes `.claude/` as `--plugin-dir`, so this is `hooks/hooks.json` at that plugin's root | `hooks/claude.json`, named by `"hooks"` in `.claude-plugin/plugin.json` |
| Cursor | `.cursor/hooks.json`, the only project-level path Cursor reads ([cursor.com/docs/hooks](https://cursor.com/docs/hooks)) | `hooks/cursor.json`, named by `"hooks"` in `.cursor-plugin/plugin.json` |
| Codex | `.codex/hooks.json`, read from a trusted project's `.codex/` layer ([developers.openai.com/codex/hooks](https://developers.openai.com/codex/hooks)) | `hooks/codex.json`, named by `"hooks"` in `.codex-plugin/plugin.json` |
| Copilot | `.copilot/hooks/hooks.json`: the `hooks/hooks.json` of the `.copilot/` plugin directory `ynh run` passes as `--plugin-dir`. CONFIRMED, all six events | `hooks.json` at the plugin root for a Copilot-only export (CONFIRMED). In a merged package with Claude, none: Copilot reads `hooks/claude.json` |
| OpenCode | `.opencode/plugins/ynh-hooks.ts`, auto-discovered from the `OPENCODE_CONFIG_DIR` bundle `ynh run` launches with | none: OpenCode has no plugin manifest, so an export carries the same generated file but nothing loads it as a plugin |

The document is the same in a vendor's session file and its plugin file except, on Cursor, Codex and Copilot, for commands that start with `./`, which reach the harness's script from a different root in each (see [Hook script paths](#hook-script-paths)). A plugin never reads a session path, so an export does not carry `.claude/hooks/`, `.cursor/hooks.json`, `.codex/hooks.json` or `.copilot/hooks/`, and a session assembly does not carry `hooks/<vendor>.json` or a root `hooks.json`.

#### Why a plugin's hooks are not in `hooks/hooks.json`

Every vendor's plugin loader has a default hooks file, `hooks/hooks.json` at the plugin root, and a manifest `"hooks"` field that can name another file. They differ in how the two combine:

| Vendor | Default plugin hooks file | Manifest `"hooks"` field | Source |
|--------|---------------------------|--------------------------|--------|
| Claude Code | `hooks/hooks.json` | Path, inline object or array. **Merged with** `hooks/hooks.json`, which loads whenever it exists | [code.claude.com/docs/en/plugins-reference](https://code.claude.com/docs/en/plugins-reference) |
| Cursor | `hooks/hooks.json` | Path or inline object. **Replaces** folder discovery: the default file is not also read | [cursor.com/docs/reference/plugins](https://cursor.com/docs/reference/plugins) |
| Codex | `hooks/hooks.json` | Path, inline object or array, in `.codex-plugin/plugin.json` for a legacy package. **Replaces** default-file discovery | [developers.openai.com/codex/plugins/build](https://developers.openai.com/codex/plugins/build) |
| Copilot CLI | `hooks.json` or `hooks/hooks.json` | Path or inline object. The docs do not say whether it replaces or adds to the defaults | [docs.github.com: CLI plugin reference](https://docs.github.com/en/copilot/reference/cli-plugin-reference) |

A merged export and a marketplace package put every vendor's manifest in one plugin root, and the vendors' formats differ (Cursor's flat `preToolUse` against Claude's nested `PreToolUse`). A shared `hooks/hooks.json` could therefore serve at most one of them, and because Claude Code loads that file even when its manifest names another, no other vendor's format could ever go there. So ynh writes no `hooks/hooks.json` into a plugin. Each vendor gets its own file, `hooks/<vendor>.json`, and its manifest names it in `"hooks"`; Cursor and Codex then read that file instead of the default, and Claude Code reads it alongside a default that is not there. No vendor reads another's format. Copilot is the exception to the rule: its hooks go in a root `hooks.json`, a default name that Claude Code, Cursor and Codex do not read, because its manifest is Claude's and cannot name a second file (see [Copilot format and trust](#copilot-format-and-trust)).

A single-vendor export uses the same layout, so every plugin ynh writes follows one rule and a per-vendor export can be combined with another by hand without a clash. A manifest names a hooks file only when the harness has hooks for that vendor, because a plugin loader rejects a `"hooks"` path that does not exist.

Copilot has no manifest of its own: it reads `.claude-plugin/plugin.json`, the file Claude writes, which names `hooks/claude.json` when the package carries Claude's hooks. Which file Copilot loads depends on the layout (CONFIRMED live, Copilot CLI 1.0.105, 2026-10-30; each hook fired exactly once in all three):

| Layout | Copilot loads | Why |
|--------|---------------|-----|
| Copilot-only export | root `hooks.json` | the manifest names no hooks file, so the default is read |
| Merged package that includes Claude | `hooks/claude.json`, the file the manifest's `hooks` field names; it **ignores** a root `hooks.json` | an explicit path replaces the default. ynd therefore writes no root `hooks.json` there |
| Merged package without Claude | root `hooks.json` | as for a Copilot-only export |
| Agent Plugins export | `com.github.copilot/hooks/hooks.json` | the namespace path |

### OpenCode hooks are a generated plugin

OpenCode 2 has no declarative hook config, so ynh generates `.opencode/plugins/ynh-hooks.ts` in the run directory and OpenCode discovers it. The plugin runs each hook command with Claude-shaped JSON on stdin; edits to it are overwritten on the next run. The mapping:

| ynh event | Behaviour on OpenCode |
|-----------|-----------------------|
| `before_tool` | Exit code 2 throws, which blocks the call; the hook's stderr is the tool error the model sees. The tool does not run |
| `after_tool` | Runs after the call and cannot reject the result. The payload carries Claude's `file_path`, `old_string` and `new_string` beside OpenCode's own input keys |
| `before_prompt` | Exit code 2 does block, before the model is called, but OpenCode surfaces only an opaque `UnexpectedStatus: 500`, not the hook's message. Prefer logging in a prompt hook over blocking |
| `on_session_start` | Comes from the event stream (`session.created`). Cannot block; a matcher is ignored; its output does not reach the model (see [on_session_start output becomes model context](#on_session_start-output-becomes-model-context)) |
| `on_stop` | Comes from the event stream (`session.execution.succeeded` / `.failed`), once per turn. Cannot block; a matcher is ignored |
| `on_session_end` | Not supported: nothing in OpenCode 2 fires when a session ends. Not written; assembly warns |

Matchers on `before_tool` and `after_tool` are written with Claude's tool names and mapped to OpenCode's ids, so `Bash` fires on OpenCode's `shell`, `Edit` on `edit`, and a native id such as `shell` works too. A matcher is an anchored regex tested against the OpenCode id and every Claude name that maps to it.

`on_session_start` and `on_stop` ride OpenCode's event stream rather than a documented hook, so they are the most likely to change in a later OpenCode release. OpenCode reaches MCP tools through its Code Mode (one `execute` tool), so a `before_tool` matcher does not see an MCP call as its own tool.

### Hook script paths

A command such as `./scripts/guard.sh` names a script relative to the harness that declares it: `scripts/guard.sh` in the harness's own tree. Wherever the hooks end up, in a session's run directory or an installed plugin, the harness tree is not there, so ynh copies the script alongside the hooks and makes the command reach the copy. It does this for a command whose first characters are `./`, and only those; an absolute path (`/usr/local/bin/lint.sh`), a command already anchored to a variable (`$CLAUDE_PROJECT_DIR/x.sh`) and a PATH-style command (`make check`) are written exactly as declared.

| Vendor | Session (`ynh run`, `ynd preview`, `ynh agent`) | Plugin (`ynd export`, `ynd marketplace build`) |
|--------|--------------------------------------------------|-----------------------------------------------|
| Claude Code | `"${CLAUDE_PLUGIN_ROOT}"/scripts/guard.sh`, script copied to `.claude/scripts/guard.sh`: the `--plugin-dir` plugin root | `"${CLAUDE_PLUGIN_ROOT}"/scripts/guard.sh`: the installed plugin ([code.claude.com/docs/en/plugins-reference](https://code.claude.com/docs/en/plugins-reference)) |
| Cursor | `./scripts/guard.sh`, unchanged, script copied to `scripts/guard.sh` in the run directory | `"${CURSOR_PLUGIN_ROOT}"/scripts/guard.sh`, which Cursor expands in a plugin hook command ([cursor.com/docs/reference/plugins](https://cursor.com/docs/reference/plugins)) |
| Codex | `./scripts/guard.sh`, unchanged, script copied to `scripts/guard.sh` in the run directory | `"${PLUGIN_ROOT}"/scripts/guard.sh`; Codex exports `PLUGIN_ROOT` to plugin hook commands ([developers.openai.com/codex/plugins/build](https://developers.openai.com/codex/plugins/build)) |
| Copilot | `"${PLUGIN_ROOT}"/scripts/guard.sh`, script copied to `.copilot/scripts/guard.sh`: the `--plugin-dir` plugin root (CONFIRMED to expand) | `"${PLUGIN_ROOT}"/scripts/guard.sh`; in a merged package with Claude it reads `hooks/claude.json`, with Claude's `${CLAUDE_PLUGIN_ROOT}`, which Copilot also sets |
| OpenCode | `./scripts/guard.sh`, script copied to `.opencode/scripts/guard.sh`; the generated plugin anchors the command to its own location (`YNH_HOOK_ROOT`), not the working directory | none: no OpenCode plugin export |

The variable is quoted so an install path with spaces stays one word, as Claude Code's reference recommends.

**In a plugin, a `./` script is part of the plugin.** The plugin is installed somewhere else, away from the harness and from any project, so the only script its hooks can rely on is one that ships inside it. The export therefore copies each `./` script a hook runs from the harness directory to the same path in the plugin, keeping its mode: a harness with `scripts/guard.sh` and the hook `./scripts/guard.sh --strict` exports a plugin carrying `scripts/guard.sh`. Only the command's first word is taken as the script. A `./` script that is not a file in the harness, or that climbs out of it (`./../x.sh`), is not copied, and the export prints a warning naming it:

```
  warning: hook script ./scripts/missing.sh is not a file in the harness, so the plugin does not carry it
```

The hook is still written, anchored to the plugin root, so it will not find the script. To run a script that belongs to the project rather than to the plugin, anchor it yourself (`$CLAUDE_PROJECT_DIR/scripts/x.sh` on Claude Code) and it is left alone. `ynd marketplace build` copies scripts the same way and prints the same warnings on stderr, each prefixed with the entry's name (`warning: reviewer: hook script ...`).

Copilot's documentation does not say whether a plugin-root variable reaches a hook command, but a live run showed that it does: `PLUGIN_ROOT`, `CLAUDE_PLUGIN_ROOT` and `COPILOT_PLUGIN_ROOT` are all set to the plugin directory, and `"${PLUGIN_ROOT}"/...` expands (Copilot CLI 1.0.105, 2026-10-30, answering #497). ynd anchors Copilot's `./` scripts to `${PLUGIN_ROOT}`.

**In a session, a `./` script is part of the run directory.** `ynh run`, `ynd preview` and the agent loop copy each `./` script a hook runs into the run directory, by the same rules as an export (first word only, a regular file inside the harness, mode kept), and warn about any they cannot carry:

```
  warning: hook script ./scripts/missing.sh is not a file in the harness, so the session does not carry it
```

Where the copy goes follows where each vendor runs session hooks from:

- **Claude Code** reads the session hooks from the `.claude/` directory `ynh run` passes as `--plugin-dir`. Claude loads a `--plugin-dir` plugin in place and sets `${CLAUDE_PLUGIN_ROOT}` to it for that plugin's hook commands ([code.claude.com/docs/en/plugins/loading](https://code.claude.com/docs/en/plugins/loading)), so the script is copied to `.claude/scripts/guard.sh` and the command is anchored there. `ynh run` still starts Claude in your project, so the command no longer depends on the agent's working directory.
- **Copilot** reads the session hooks from the same `.copilot/` plugin directory, so the script is copied to `.copilot/scripts/guard.sh` and anchored with `${PLUGIN_ROOT}` (CONFIRMED to expand).
- **Cursor** runs project hooks from the project root ([cursor.com/docs/hooks](https://cursor.com/docs/hooks)), and **Codex** runs hooks in the session's working directory ([developers.openai.com/codex/hooks](https://developers.openai.com/codex/hooks)). `ynh run` launches both with the run directory as their working directory, so the script is copied to the run directory's root and the bare `./scripts/guard.sh` reaches it.

A `./` command therefore means the harness's script in a session too, not a file in your project. To run a script that belongs to the project, anchor it yourself (`$CLAUDE_PROJECT_DIR/scripts/x.sh` on Claude Code) or give an absolute path, and it is left alone. `ynh hook export`, which writes your project's own `.claude/settings.json`, is the exception: there the hooks belong to the project, and a `./` command is anchored to `$CLAUDE_PROJECT_DIR` (see [Running hooks in a plain Claude session](#running-hooks-in-a-plain-claude-session)).

### Running hooks in a plain Claude session

`ynh run` activates the harness's hooks through `--plugin-dir`, but that only covers sessions started by `ynh run`. To make hooks, and the sensors that depend on them, fire when you simply open the repo in Claude Code, declare them in the project's own `.claude/settings.json`, the file Claude auto-loads for every session in that directory. This is a separate deployment mode from `ynh run`:

| Mode | Hooks come from | When hooks fire |
|------|-----------------|-----------------|
| `ynh run` (staging dir + `--plugin-dir`) | assembled `.claude/hooks/hooks.json` | every `ynh run` session, no install step |
| Plain `claude` in the project | project `.claude/settings.json` | every session, automatically |

For an always-on, sensor-driven repo, declare the hooks once in `.agents/harness/plugin.json` (canonical names) and let ynh write them into the settings file:

```bash
ynh hook export <harness> --target settings   # → .claude/settings.json (committed, team-wide)
ynh hook export <harness> --target local      # → .claude/settings.local.json (gitignored, personal)
ynh hook export <harness> --target settings --dry-run   # preview, write nothing
```

`hook export` translates canonical events to Claude-native names, applies the nested shape, and anchors relative command paths to `$CLAUDE_PROJECT_DIR` (see rules below). It **merges** — non-hook keys (`permissions`, `env`, …) and your own existing hooks are preserved, and re-running adds nothing already present, so it's safe to run repeatedly. `--target` is required; there is no default, so you always choose committed vs. personal explicitly.

Then confirm the wiring:

```bash
ynh doctor   # among its checks: .claude/settings.json + settings.local.json for the traps below
```

`ynh doctor`'s hook-wiring check flags canonical names that leaked into a settings file (where Claude ignores them), cwd-relative hook commands, and a project with no settings file at all (hooks declared but not wired).

If you hand-author the settings file instead, three rules:

1. **Use Claude-native event names and the nested shape** — `PreToolUse`, `PostToolUse`, `UserPromptSubmit`, `Stop`, each `{ "matcher": …, "hooks": [ { "type": "command", "command": … } ] }`. The canonical names (`before_tool`, `on_stop`, …) are valid **only** in `.agents/harness/plugin.json`; Claude silently ignores them in `settings.json`. Don't copy the `plugin.json` shape into `settings.json`.
2. **Anchor command paths to `$CLAUDE_PROJECT_DIR`** — `$CLAUDE_PROJECT_DIR/tools/hooks/foo.sh`. Claude runs each hook via `/bin/sh` in the **agent's current working directory**, not the project root, so a relative path like `./tools/hooks/foo.sh` silently breaks the moment the agent does `cd` into a subdirectory — and a *blocking* guard hook then fails open (stops guarding) without erroring. `$CLAUDE_PROJECT_DIR` is cwd-independent; it's also more portable than an absolute path, since `settings.json` is checked in and shared across machines.
3. **Keep the canonical declarations in `plugin.json` too** if you also use `ynh run` or `ynd export`, since they activate there through `--plugin-dir` for Claude, and for Codex and Cursor.

Example `.claude/settings.json` (what `hook export` produces):

```json
{
  "hooks": {
    "PostToolUse": [
      { "matcher": "Edit|Write", "hooks": [ { "type": "command", "command": "$CLAUDE_PROJECT_DIR/tools/hooks/after-edit-sensor.sh" } ] }
    ],
    "Stop": [
      { "hooks": [ { "type": "command", "command": "$CLAUDE_PROJECT_DIR/tools/hooks/on-stop-sensors.sh" } ] }
    ]
  }
}
```

The `Stop` entry above needs the loop-guard and output-routing discipline described in [on_stop output semantics](#on-stop-output-semantics-claude).

### Claude Code Format

Claude uses a three-level structure. Hook entries are grouped by matcher, and each group contains an array of inner hooks:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          { "type": "command", "command": "/usr/local/bin/check-dangerous-commands.sh" }
        ]
      }
    ]
  }
}
```

### Cursor Format

Cursor uses a flat structure with a required `"version": 1` field. A matcher is written only on `preToolUse` and `postToolUse`, where Cursor matches it against its tool type (`Shell`, `Read`, `Write`, ...); on other events all hooks fire unconditionally:

```json
{
  "version": 1,
  "hooks": {
    "preToolUse": [
      { "command": "/usr/local/bin/check-dangerous-commands.sh", "matcher": "Shell" }
    ]
  }
}
```

### Codex Format

Codex uses the same three-level nesting structure as Claude. Hook entries are grouped by matcher, and each group contains an array of inner hooks:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          { "type": "command", "command": "/usr/local/bin/check-dangerous-commands.sh" }
        ]
      }
    ]
  }
}
```

### Copilot Format and Trust

Copilot uses a flat structure with `"version": 1`, like Cursor, and PascalCase event names ([docs.github.com: hooks configuration](https://docs.github.com/en/copilot/reference/hooks-configuration)):

```json
{
  "version": 1,
  "hooks": {
    "PreToolUse": [
      { "type": "command", "command": "/usr/local/bin/check-dangerous-commands.sh", "matcher": "Bash" }
    ]
  }
}
```

`command` is the cross-platform field; Copilot copies it to `bash` and `powershell` when those are absent. ynd writes `timeoutSec` only when the hook declares a `timeout` (CONFIRMED live that Copilot honours it, Copilot CLI 1.0.105), otherwise Copilot's default of 30 seconds applies, and it writes a matcher only on `PreToolUse` and `PostToolUse`.

**Where Copilot reads hooks.** Copilot combines hooks from several places: policy files, `.github/hooks/*.json` in the repository, a `hooks` field in `.github/copilot/settings.json` (and `.claude/settings.json`), `~/.copilot/hooks/*.json`, and the `hooks.json` or `hooks/hooks.json` of an installed plugin. ynd generates the **plugin** route only: `.copilot/hooks/hooks.json` for a session (the `--plugin-dir` directory) and `hooks.json` at the root of an export. It does not write into your repository's `.github/hooks/`, because that is the route the docs gate behind trust (below), and a hook written there would run for everyone who clones the repository. Copilot's Agent Plugins export (`ynd export --format agent-plugin`) carries the same file at `com.github.copilot/hooks/hooks.json`, the path the specification and Copilot's reference give.

**Verified live (Copilot CLI 1.0.105, 2026-10-30).** Run with `ynh run -v copilot` in a scratch folder:

- All six events fire from `.copilot/hooks/hooks.json` loaded by `--plugin-dir`, **in an untrusted folder**. Plugin hooks do not need folder trust.
- `PLUGIN_ROOT`, `CLAUDE_PLUGIN_ROOT` and `COPILOT_PLUGIN_ROOT` are all set to the plugin directory, so `"${PLUGIN_ROOT}"/...` expands (#497).
- **Without `cwd`, a hook runs in the plugin directory**, not the project. ynh writes `"cwd": "."` on every Copilot hook entry, which Copilot resolves against the repository root ([Copilot hooks reference](https://docs.github.com/en/copilot/reference/hooks-configuration): "Working directory for the command (relative to repository root or absolute)"). So Copilot hooks run from the repository root, like the other vendors, and a relative command such as `bash scripts/wake.sh` finds the project's script (CONFIRMED live, Copilot CLI 1.0.105, 2026-10-30). For a session started in a repository subfolder the root is the repository root, whereas Claude Code runs a hook in the agent's current working directory. ynh's own `./` scripts are anchored with `"${PLUGIN_ROOT}"` and are unaffected.
- **A merged package with Claude** is covered too. Copilot loads Claude's `hooks/claude.json` there, so ynh writes `"cwd": "."` on those handlers as well, only when the package also targets Copilot. Claude Code tolerates the extra field and runs hooks in its current directory regardless, so it is a no-op for Claude (CONFIRMED live, Claude Code, 2026-10-30). A Claude-only export is unchanged.
- The payload is Claude-shaped: `tool_name` is `Bash` for a shell call, and `SessionEnd` carries `reason: "complete"` in a `-p` run.
- A `Bash` matcher fires on both `PreToolUse` and `PostToolUse`; an `Edit` matcher did not fire for a shell call.
- Each export layout fires each hook exactly once when loaded with `--plugin-dir`: the Copilot-only export, the merged package and the Agent Plugins export (table above). In a merged package Copilot loads `hooks/claude.json` and ignores the root `hooks.json`.

**Trust, for the repository route only.** Copilot's docs say "Repository and inherited-agent hooks require workspace admission", and a v1.0.75 hand test saw a `.github/hooks/` hook never fire in an untrusted folder; `--add-dir` and `--allow-all-paths` did not help, and adding the folder to `trustedFolders` did. ynd does not use that route. If you copy the generated file to `.github/hooks/<name>.json` yourself, that route is the one that may need the folder trusted; it was not tested in 1.0.105.

#### Not yet tested

- Hooks from a plugin installed with `copilot plugin install` (the runs above used `--plugin-dir`).
- The repository `.github/hooks/` route.
- Whether the merged package's installed form loads hooks the same way (the installed MCP result is in [MCP](mcp.md)).

## Blocking Hooks

A hook can block an action by using exit code 2. To provide the agent with context about why the action was blocked, the hook script should output a JSON object or a text message to stdout.

### Portable Hook Script Pattern

To write a hook that works across all vendors, output remediation instructions and exit with code 2:

```bash
#!/bin/bash
# check-dangerous-commands.sh — block destructive git operations
if echo "$@" | grep -qE 'git (push --force|reset --hard|clean -fd)'; then
  echo '{"error": "Destructive git operation blocked. Use --no-force or create a backup branch first."}' >&2
  exit 2
fi
exit 0
```

The blocking message should be **agent-legible**: tell the agent what to do instead, not just what failed. For example, "Use git push without --force" rather than "Force push not allowed."

## on_stop Output Semantics (Claude)

The `Stop` hook is the subtlest event, and it behaves differently from the other three in ways that matter for sensor sweeps.

**It fires every turn, not once per session.** On Claude Code, `Stop` runs each time the agent finishes responding — not at session end. An on-stop sensor sweep therefore runs *per turn*, so it must be cheap and idempotent.

**Its stdout does not reach the model.** For a `Stop` hook, `stdout` + `exit 0` goes to the transcript (user-visible), **not** into the model's context. A sweep that prints its verdict to stdout and exits 0 runs every turn but the agent never sees the result. To surface a verdict to the model you must either:

- write the message to **stderr** and `exit 2` — Claude feeds stderr to the model and blocks the stop, or
- print JSON `{"decision": "block", "reason": "…"}` to stdout.

This is why a `PostToolUse` sensor that exits 2 + stderr reaches the model, while an on-stop one that prints to stdout appears dead.

**A naive `exit 2` infinite-loops.** Blocking the stop makes the agent continue; at the next stop the hook fires and blocks again, forever. Claude passes `stop_hook_active: true` in the hook's stdin JSON when the current continuation was itself caused by a stop-hook block. The script must read it and **not block again**.

### Canonical on_stop sensor-sweep template

```bash
#!/usr/bin/env bash
set -uo pipefail
# Claude passes the hook payload as JSON on stdin. stop_hook_active=true means we are
# already continuing because of a prior block — blocking again would loop forever.
# Absent (and stdin is a tty) when the script is run manually.
if [ -t 0 ]; then HOOK_INPUT=""; else HOOK_INPUT=$(cat 2>/dev/null || true); fi
STOP_ACTIVE=$(printf '%s' "$HOOK_INPUT" | python3 -c 'import json,sys
try:    print(str(json.load(sys.stdin).get("stop_hook_active", False)).lower())
except: print("false")' 2>/dev/null || echo false)

# … run sensors, compute fail_count and fail_detail, print a human summary to stdout …

if [ "$fail_count" -gt 0 ] && [ "$STOP_ACTIVE" != "true" ]; then
  { echo "sensor sweep: $fail_count FAILING — fix before ending the turn:"; printf '%s' "$fail_detail"; } >&2
  exit 2   # surfaces to the model AND blocks the stop, exactly once
fi
exit 0     # green → quiet, end normally
```

Cursor's `stop` and Codex's `Stop` route output and guard against loops differently; verify per vendor before relying on this exact pattern elsewhere.

## Root-Harness-Only Rule

Only the root harness's hooks are used. An included harness contributes `skills/`, `agents/`, `rules/` and `commands/` — files — and nothing else.

Nothing is *dropped*: `resolveWith` iterates includes flat, with no recursion, and returns file paths. **It never opens an included harness's `plugin.json`,** so a hook declared there is never read in the first place. Root-only is a property of the resolver, not a filter applied afterwards.

That is deliberate. A hook is command execution on every lifecycle event, so an include that could contribute one would turn inert composed content into an execution surface the root author never declared.

If an included harness needs hooks, copy its hook declarations into the root harness's `.agents/harness/plugin.json`. Merging that copy at authoring time — generated, labelled blocks with drift detection — is the agreed direction rather than resolving includes at run time.

## Portable Hook Script Advice

When writing hook scripts for use across vendors:

1. **Output correct JSON for the event type** — Claude expects `{"type": "command"}` wrapper; Cursor and Codex do not. Your *script output* (blocking messages) should be plain text or simple JSON that any vendor can display.
2. **Use exit code 2 for blocking**: Claude Code, Codex, Cursor and OpenCode (on `before_tool` and `before_prompt`) treat exit code 2 as "block this action" on the events that can block. Copilot CLI does so only for `preToolUse` and `permissionRequest`; on its other events exit 2 is a warning, and a hook blocks through its JSON output instead. `on_session_end` cannot block on any vendor.
3. **Include remediation instructions** — tell the agent how to fix the problem, not just that there is one.
4. **Keep scripts idempotent**: hooks may fire multiple times per session: `on_stop` runs at the end of every turn, `on_session_start` on start and on resume (and, on Claude Code, on `/clear` and compaction), and `on_session_end` may run on `/clear` as well as at the end. Use `on_session_end` for once-per-session work, and expect it might not run.
5. **Make command paths cwd-independent**: hooks run in the agent's current working directory, not the project root, and that cwd changes as the agent navigates. A relative command (`./tools/hooks/foo.sh`) breaks after any `cd`. Anchor to the vendor's project-root variable (`$CLAUDE_PROJECT_DIR` on Claude Code) or use an absolute path. For a session or a plugin export, ynh anchors a `./` script for you and ships the script alongside the hooks; see [Hook script paths](#hook-script-paths).

## Pairing with Sensors

Hooks fire mid-session (push) and can produce artifacts that [sensors](sensors.md) declare a contract over (pull). The most common production pattern is `after_tool` writing a results file that a `files`-sourced sensor reads — implicit coupling by shared file path. See [Sensors §"Relationship to hooks"](sensors.md#relationship-to-hooks) for the full push/pull comparison and the canonical pairing pattern.

## CLI Editing

Hooks can be added and removed from the command line as well as authored directly in the manifest. The CLI distinguishes harness-level (default) hooks from profile-level overrides:

```bash
# Top-level harness hooks
ynh hook add <harness> <event> "<command>" [--matcher <pattern>] [--timeout <seconds>]
ynh hook remove <harness> <event> <index>

# Profile-level hooks (override the harness-level set when the profile is active)
ynh profile hook add <harness> <profile> <event> "<command>" [--matcher <pattern>] [--timeout <seconds>]
ynh profile hook remove <harness> <profile> <event> <index>
```

`<event>` is validated against the canonical set: `before_tool`, `after_tool`, `before_prompt`, `on_stop` (end of every turn), `on_session_start` (start and resume) and `on_session_end` (best-effort, once per session). `<index>` is zero-based. When the last entry for an event is removed, the event key is dropped from the manifest entirely.

To translate a harness's declared hooks into a Claude settings file (so they fire in a plain session — see [Running hooks in a plain Claude session](#running-hooks-in-a-plain-claude-session)):

```bash
ynh hook export <harness> --target <settings|local> [-v claude] [--dry-run]
```

And to check that the project's settings files are correctly wired:

```bash
ynh doctor
```

See [reference.md](reference.md) for the complete flag matrix and [profiles.md](profiles.md#cli-editing) for the surrounding profile-editor surface.

## See Also

- [Hooks](tutorial/hooks.md) — step-by-step walkthrough
- [Sensors](sensors.md) — observation surfaces a loop driver consumes
- [Harness Engineering](harness-engineering.md) — how hooks bridge guides to sensors
- [Vendor Support](vendors.md) — vendor capabilities and differences
