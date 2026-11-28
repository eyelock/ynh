<!-- Reference. Written 2026-11-28 against a build of ynh develop at 960ecb6f (P0 to P4 of ynh #1058 merged) plus this pull request, in scratch YNH_HOMEs with a stand-in vendor CLI on PATH; the per-vendor cells from internal/permission/vendor.go, settled by the P0 spike (ynh #1059). Lines marked TODO(live) need a vendor session, which the stand-in cannot give; they are checked in the release's live check on all five vendors. -->

# Permission Levels

What a session of a harness may do when nobody is there to approve: its permission level. A level
is one of three words, the same for `ynh run`, delegates and `ynh agent run`
([ynh #1058](https://github.com/eyelock/ynh/issues/1058)).

| Level | Approves |
|---|---|
| `none` | nothing: no file edits and no commands |
| `edits` | file edits; commands are refused |
| `all` | everything the session asks to do |

The order is `all` > `edits` > `none`. Finer control, such as which commands, is
[ynh #900](https://github.com/eyelock/ynh/issues/900) and is not part of the level.

## Where the level applies

| Launch | Level applies |
|---|---|
| A delegate session (`transport: a2a`), started by its parent | yes |
| `ynh run <id> -- "<task>"`, a one-shot run | yes |
| `ynh agent run --harness <id>`, its worker | yes |
| `ynh run <id>`, an interactive session | no: a person approves each prompt. `--auto-approve <level>` on it caps the delegates it starts |
| `ynh agent run` with no `--harness` | `--auto-approve` is the caller's own grant; there is no harness level |

## Where a level comes from

| Field or flag | Where | Can it widen? |
|---|---|---|
| `auto_approve` | top level of the harness manifest (`.agents/harness/plugin.json`). A profile cannot change it | it sets the harness's ceiling |
| `auto_approve` | on a `delegates_to` entry of the parent | no: narrows the delegate only |
| `auto_approve` | the per-call argument of `delegate_to_<name>` (A2A metadata `ynh.auto_approve`; the native bridge's `--auto-approve`) | no |
| `--auto-approve none\|edits\|all` | `ynh run`, one-shot or interactive | no |
| `--auto-approve none\|edits\|all` | `ynh agent run` | no, with `--harness`; without it, it is the grant |
| the caller's level | a delegate started by a one-shot session | no: a delegate never opens wider than the session that called it |
| the project's own mode | the working folder's settings (below) | no: a narrower project mode wins; a wider one loses |

A harness that declares no level opens at `all` once accepted, on every vendor. A harness that is
not accepted does not run unattended at all: a one-shot run is refused, one with no commands
included.

A request wider than the ceiling is refused before any session starts:

```
$ ynh run local/writer --auto-approve all -- "<task>"
Error: --auto-approve: flag: auto-approve all is wider than the ceiling edits; a request may only narrow (all > edits > none)
```

The word after `--auto-approve:` names where the wider request came from: `flag`, `entry` or `call`.

A level that is not one of the three words is refused when the harness is installed or loaded:

```
Error: bad: auto_approve: unknown auto-approve level "some" (supported: none, edits, all)
```

### Where the level is shown

| Command | Shows |
|---|---|
| `ynh trust show <id>` | `opens at <level> when run unattended`, `(declared by the harness)` or `(not declared; the default for an accepted harness)`; each declaring `delegates_to` entry as `delegate <source>: <level>`; each a2a delegate with its own level and source |
| `ynh info <id>` | `Auto-approve: <level> (harness)` or `(default)`; `auto_approve=<level> (entry)` beside a delegate whose entry declares one |
| `ynh ls --format json`, `ynh info --format json` | `auto_approve` and `auto_approve_from` on each harness and on each declaring `delegates_to` entry |
| `delegate_status`, the delegate's result | `auto_approve` and `auto_approve_from` |
| a one-shot `ynh run` and `ynh agent run` | `auto-approve: <level> (<source>)` on stderr before the vendor starts, and, when the project narrowed it, a line naming the file and setting |

<!-- TODO(live): delegate_status and the result carry auto_approve and auto_approve_from in internal/delegatemcp/server.go at develop 960ecb6f; not seen in a live session here (the stand-in vendor calls no tool). -->

`auto_approve_from` is one of:

| Value | The level came from |
|---|---|
| `default` | nothing was declared: `all` |
| `harness` | the harness's own `auto_approve`, on any run of it |
| `delegate_default` | the delegate harness's own `auto_approve`, as the ceiling its delegate session opens under |
| `entry` | the parent's `delegates_to` entry |
| `call` | the per-call argument of `delegate_to_<name>` |
| `flag` | `--auto-approve` |
| `caller_cap` | the level of the session that started the delegate |
| `project` | the working folder's own permission mode |
| `interactive` | a task queued to a live interactive session of the delegate, where a person approves each action; no level is recorded |

## What each vendor is passed

A level is held exactly with each vendor's own restricting setting, so a user's config cannot widen
it. A level a vendor cannot hold exactly is refused at start, never widened.

| Level | Claude Code | Codex | Copilot CLI | Cursor | OpenCode |
|---|---|---|---|---|---|
| `none` | `--permission-mode default` | `--sandbox read-only` | `--no-ask-user --deny-tool shell --deny-tool write` | refused | `--standalone`, with `OPENCODE_CONFIG_CONTENT={"permission":{"edit":"deny","bash":"deny"}}` |
| `edits` | `--permission-mode acceptEdits --disallowedTools Bash` | refused | `--no-ask-user --allow-tool write --deny-tool shell` | refused | `--standalone`, with `OPENCODE_CONFIG_CONTENT={"permission":{"edit":"allow","bash":"deny"}}` |
| `all` | `--permission-mode bypassPermissions` | `--dangerously-bypass-approvals-and-sandbox` | `--no-ask-user --allow-all-tools` | `--force` | `--auto` |

Why a cell is refused:

| Cell | Reason |
|---|---|
| Codex `edits` | Codex has no mode that approves file edits while still withholding commands: `workspace-write` lets the shell write |
| Cursor `edits` | the same: Cursor has no such mode |
| Cursor `none` | print mode without `--force` still writes files, and a project's `.cursor/cli.json` allow list widens it |

The refusal reads:

```
codex cannot hold auto-approve edits exactly: it has no mode that approves file edits while still withholding commands; use all, or a different vendor
```

On Codex use `none` or `all`; on Cursor use `all`; or run the harness on another vendor.

### Claude Code as root

Claude Code refuses `bypassPermissions` as root. ynh refuses a Claude Code one-shot run, delegate
session or agent worker at `all` as root itself, before Claude starts, exit 1:

```
claude code refuses auto-approve all as root (--permission-mode bypassPermissions). Fix one of:
  - run as a non-root user
  - use ynh's agent image, which runs as uid 1001
  - narrow the run with --auto-approve edits
  - in a container that is the sandbox, set IS_SANDBOX=1 (or CLAUDE_CODE_BUBBLEWRAP), which Claude Code honours
```

The text is from `internal/permission/root.go` at develop 960ecb6f; it was not run as root.

It does not refuse when `IS_SANDBOX=1` or `CLAUDE_CODE_BUBBLEWRAP` is set, the two settings under
which Claude Code itself allows `bypassPermissions` as root.

### The mode a session opened in

Claude Code reports the mode it opened in. If a managed or user setting (such as
`permissions.disableBypassPermissionsMode`) made it open in another mode than ynh chose, ynh stops the
run:

```
asked for --permission-mode bypassPermissions but the session started in "default"; a managed or user setting (such as permissions.disableBypassPermissionsMode) overrides it, so --auto-approve does not apply
```

## The project's own mode

When the working folder's project settings set a mode narrower than the run's level, the run opens at
the project's mode, ynh passes the vendor that mode explicitly (so a user setting cannot widen it), and
the run prints a line naming the file and setting. A wider project mode loses to ynh's level. A project mode never refuses a run.
User-level settings are not read: ynh's flags override them.

```
$ ynh run local/team -- "<task>"
auto-approve: none (project)
auto-approve: opening at the project's mode (none) rather than all: /work/repo/.claude/settings.json sets permissions.defaultMode "default"
```

| Vendor | File | Setting | Level |
|---|---|---|---|
| Claude Code | `.claude/settings.json`, then `.claude/settings.local.json` | `permissions.defaultMode`: `default`, `plan`, `dontAsk` | `none` |
| | | `acceptEdits` | `edits` |
| | | `bypassPermissions` | `all` |
| Codex | `.codex/config.toml`, top-level keys only | `sandbox_mode`: `read-only` / `workspace-write` / `danger-full-access` | `none` / `edits` / `all` |
| | | `approval_policy`: `untrusted` / `on-request`, `on-failure` | `none` / `edits` |
| | | `approval_policy = "never"` | sets no bound |
| Cursor, OpenCode, Copilot CLI | none | they have no project approval mode | |

A value ynh cannot place counts as `none`, so it never widens a run.

## Trust

A declared level is part of what `ynh trust accept` reviews: declaring or changing `auto_approve`, on
the harness or on a `delegates_to` entry, changes the digest, and the harness must be accepted again.
An undeclared level is not part of the digest, so an acceptance made before levels existed still
holds, and that harness opens at `all` on its next one-shot run.

## Related

- [The Agent Loop: permissions](agent.md#permissions-and-auto-approve)
- [Delegates reference](delegates-reference.md)
- [ynh trust commands](trust-commands.md)
- [Vendor feature parity](vendor-parity.md)
