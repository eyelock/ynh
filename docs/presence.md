# Session presence

Reference for the record `ynh run` keeps for every session it launches, and for `ynh ps`, which lists them ([ynh #894](https://github.com/eyelock/ynh/issues/894)). Built for v0.18.0.

## What it is

Each `ynh run` writes one record while its session lives. `ynh ps` reads the records and checks each against the live process table, so the listing says which sessions are running, on every OS and for every vendor (claude, codex, copilot, cursor, opencode). Read presence through `ynh ps` or its MCP tool, never by opening the files: the layout is not a contract.

## `ynh ps`

```
ynh ps [--all] [--format text|json | --json]
```

| Flag | Meaning |
|---|---|
| `--all` | Include ended and dead sessions (history). Without it, only live and orphaned sessions are listed. |
| `--format text\|json` | Output format, text by default. |
| `--json` | Same as `--format json`. |

JSON output is `{"sessions": [...]}`, oldest first; an empty listing is `{"sessions": []}`. Each session has the fields of the record below plus `status`.

### Status

| `status` | Meaning |
|---|---|
| `live` | The wrapper process is running. |
| `orphaned` | A delegate that is running while the session that started it (`owner_session`) is not. |
| `dead` | The wrapper process is gone and the record has no clean-exit marker: a crash or a kill. |
| `ended` | The session exited cleanly (`ended` is true). |

## The record

| Field | Meaning |
|---|---|
| `session_id` | ynh's own id for the session, a random UUID. Not the vendor's session id. |
| `harness` | The harness's run name. |
| `vendor` | `claude`, `codex`, `copilot`, `cursor` or `opencode`. |
| `vendor_pid` | The vendor CLI's process id; absent until it has started. |
| `wrapper_pid` | The `ynh run` process id. ynh stays the vendor's parent for the whole session. |
| `start_time` | The wrapper's start as the OS reports it (see below). Opaque: compare, do not interpret. |
| `started_at` | Wall-clock start, for people. |
| `cwd` | The folder the session was launched from. |
| `mode` | `interactive`, `one-shot` (a prompt and no interactive session) or `delegate` (a run started for a parent's `delegates_to`). |
| `owner_session` | For a delegate, the live session of the parent harness in the same folder that started it. Empty when it cannot be found. |
| `address` | How the session is named: `ynh:<session_id>`. |
| `state` | `alive`, `busy`, `idle` or `unknown`. A session reports `alive`; `busy` and `idle` are reserved for vendors whose hooks can say so, and none do yet. |
| `last_beat` | When the wrapper last refreshed the record, every 15 seconds. |
| `ended` | True once the session exited cleanly. |

## How a dead session is told from a live one

A record is live only if the wrapper pid exists and its start time equals `start_time`. A pid that has been reused by another process started at a different time, so it does not read as alive. The start time comes from one function per OS:

| OS | Source |
|---|---|
| Linux | `starttime` in `/proc/<pid>/stat` |
| macOS and the BSDs | `ps -o lstart=` |
| Windows | `GetProcessTimes` creation time, and the process must still be active |

`last_beat` is a courtesy for people; liveness never depends on it.

## MCP tool

A session whose harness has A2A delegates gets ynh's delegate MCP server (see [Delegate sessions](delegate-sessions.md)). It carries the tool `ynh_ps`, which returns the same listing as `ynh ps --json`; its `all` argument is `--all`.

## Where the files are, and history

Records live in `$YNH_HOME/presence/<session_id>.json`. Ended and dead records stay as history and are shown only by `--all`. They are not removed automatically yet, and the older `$YNH_HOME/sessions` files (which bind a `--session-name` to a vendor session) are a separate thing and are untouched.

## Not covered

Delegates' A2A endpoints are not yet queried as a second source, and there is no `ynh stop`, reap, live-session cap or `--on-live` option; those are [ynh #895](https://github.com/eyelock/ynh/issues/895) and [ynh #896](https://github.com/eyelock/ynh/issues/896). Presence is per machine and per `YNH_HOME`.
