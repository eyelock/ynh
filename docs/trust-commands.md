<!-- Reference. Each command and output was run on a build of ynh v0.18.0 (develop) in a scratch YNH_HOME, 2026-11-27; the permission level and the a2a delegates re-run 2026-11-28 on a build of develop at 960ecb6f (P0 to P4 of ynh #1058 merged). Lines marked TODO(live) need a vendor session, which the stand-in cannot give; they are checked in the release's live check on all five vendors. -->

# ynh trust Commands

The commands that read or make trust records, and the `--trust` flag of other commands. Records are
kept in `$YNH_HOME/trust.json`. What each state means for a run is in the
[Trust reference](trust.md#the-gate).

## `ynh trust`

```
ynh trust [ls|show|accept] [harness] [--format text|json]
```

| Command | What it does |
|---|---|
| `ynh trust`, `ynh trust ls` | One line per installed harness: `HARNESS`, `STATE`, `VIA`, `COMMANDS`, `SOURCE` |
| `ynh trust show <id>` | Everything the harness runs, the scripts behind it, the [permission level](permission-levels.md) it opens at when run unattended, each `a2a` delegate with its own state and level, its digest, and, when accepted, the accepted digest, date and route |
| `ynh trust accept <id>` | Records the harness's current digest as accepted, and accepts each of its `a2a` delegates under the delegate's own id, at the commit it is installed at, installing one that is not installed. A remote include that brings commands, or a native delegate entry from another repository, with no full 40-character SHA ref is accepted at the commit it is at now, which is recorded (`floating` in the record) and printed; nothing is refused for floating |
| `ynh trust accept <folder>` | Records the remote includes of a folder, for runs of that folder |

| Flag | Values | Default |
|---|---|---|
| `--format` | `text`, `json` | `text` |

`ynh trust show` takes an installed id only, not a folder.

### Output

`ynh trust accept <id>`:

```
Accepted local/greeter (accept): 2 command(s), sha256:aff830900fd54227
```

`ynh trust accept <id>` of a parent with one `a2a` delegate, not yet installed:

```
A2A delegate file:///work/reviewer of team, opens at all when run unattended (default):
It executes:
  hook on_session_start: echo Reviewer session started.
Installed harness "reviewer"
  Location: <YNH_HOME>/harnesses/local--reviewer
  Launcher: <YNH_HOME>/bin/reviewer
  Vendor:   claude
Accepted local/reviewer (accept, with local/team): 1 command(s), sha256:abab6f64cd7a3573, opens at all when run unattended
Accepted local/team (accept): 0 command(s), sha256:9ec767637d0d49d6, opens at all when run unattended
  opens at all when run unattended (default)
```

What each delegate runs is shown first; the whole tree of a2a delegates, nested ones included, is
accepted all or nothing, and the parent last.

`ynh trust show <id>`, after what the harness executes:

```
Permission level when run unattended (a delegate session, a one-shot 'ynh run' or 'ynh agent run'):

  opens at all when run unattended (not declared; the default for an accepted harness)
  delegate file:///work/reviewer: edits

A2A delegates, the whole tree, each its own harness with its own record. 'ynh trust accept local/team' accepts them all, or none:

  file:///work/reviewer: local/reviewer [accepted], declared by team
    opens at edits when run unattended (entry)
    hook on_session_start: echo Reviewer session started.
```

The first level line reads `(declared by the harness)` when the harness sets `auto_approve`. A
`delegate <source>: <level>` line follows for each `delegates_to` entry that declares one. Each a2a
delegate is listed with the harness that declares it, then the level it opens at and where that comes
from (`default`, `harness` or `entry`), then what it runs; one not installed reads
`(not installed; accepting installs it) [not-installed]`. A harness
with no commands prints `This harness declares no commands.` and then the same two sections.

A harness that only asks for workspace folders: `Accepted local/ws (accept): 0 command(s), 1 workspace folder(s), sha256:fcec517cd7869c57`.

`ynh trust accept <folder>`:

```
Accepted 1 remote include(s) of /work/proj/harness: 1 command(s)
```

`ynh trust`:

```
HARNESS         STATE     VIA              COMMANDS  SOURCE
local/reviewer  accepted  install --trust  2         https://github.com/<org>/reviewer
```

`--format json` prints an array of objects, one per harness, with `harness`, `source`, `source_type`,
`ref`, `sha`, `digest`, `executables`, `hooks`, `mcp_servers`, `state`, `accepted_digest`,
`accepted_at`, `accepted_via` and `undeclared_env`. `ynh trust show <id> --format json` prints one such
object, with `auto_approve`, `auto_approve_from`, `delegate_levels` and `a2a_delegates` (each with
`delegate`, `id`, `state`, `auto_approve` and `auto_approve_from`) as well. The full shape is in [Structured CLI output](cli-structured.md) and `ynh schema trust`.

### States

| State | Meaning | Runs |
|---|---|---|
| `unreviewed` | never accepted | refused |
| `accepted` | what it runs matches what you accepted | yes |
| `changed` | what it runs differs from what you accepted, or the acceptance was made by an older ynh | refused |
| `unavailable` | an include is missing from the cache, so what it runs cannot be read in full | refused |
| `no-commands` | it runs nothing and asks for no folders | interactive runs: yes. One-shot runs (`ynh run <id> -- "<task>"`, `ynh agent run`, a delegate session): refused until accepted, because a one-shot run opens at its permission level with nobody to approve |

A one-shot run of an unaccepted `no-commands` harness is refused, exit 1:

```
Error: refusing to run local/team one-shot: you have not accepted it. It executes no commands, but a one-shot run opens at all with nobody to approve (default).
Review it: ynh trust show local/team
Then accept:  ynh trust accept local/team
```

Accepting it records the state `accepted`.

### `VIA`: how an acceptance was made

| Value | Made by |
|---|---|
| `accept` | `ynh trust accept` |
| `install --trust` | `ynh install ... --trust` |
| `run --trust` | `ynh run <folder> --trust`, `ynh agent run --harness <folder> --trust` |
| `update --trust` | `ynh update <id> --trust` |
| `include --trust` | `ynh include add ... --trust`, `ynh include update ... --trust`, `ynh include remove ... --trust` |
| `image` | the record `ynh image` bakes into an image |
| `with <parent>` | accepted through its parent: `ynh trust accept <parent>`, `ynh install <parent> --trust` or `ynh run <folder> --trust`. The `Accepted` line names the route too, such as `(accept, with local/team)` |

`ynh trust` prints `with <parent>`; `--format json` carries the route as `accepted_via`.

## The `--trust` flag

`--trust` accepts in the same step as the command, without a separate `ynh trust accept`. Everything it
accepts (unlike `ynh trust accept`, which accepts what you reviewed at the commit installed) must be pinned to a full 40-character commit SHA; otherwise the command is refused, exit 1,
naming what can move. A local folder needs no pin for its own files; its remote includes and its delegate entries from another repository, `a2a` or `native`, do. Each `a2a` delegate is accepted with its own record, installed first if it is not. `ynh install <url> --ref <branch> --trust` is refused before anything is cloned.

| Command | Accepts | Pins required |
|---|---|---|
| `ynh install <url> --ref <sha> --trust` | the installed harness and its `a2a` delegates | `--ref`, and every remote include that brings commands, and every delegate entry from another repository |
| `ynh install ./<folder> --trust` | the installed harness, as `local/<name>`, and its `a2a` delegates | every remote include that brings commands, and every delegate entry from another repository |
| `ynh run ./<folder> --trust` | the folder's remote includes, fetching any not in the cache, and its `a2a` delegates | every remote include that brings commands, and every delegate entry from another repository |
| `ynh agent run --harness ./<folder> --trust` | the same | the same |
| `ynh image <name> --from <source> --trust` | the harness built into the image, baked as `via: image` | `--ref` for a Git source, and every remote include that brings commands, and every delegate entry from another repository |
| `ynh update local/<name> --trust`, for a harness installed from a folder | the harness again, after the update | every remote include that brings commands, and every delegate entry from another repository |
| `ynh include add local/<name> <url> --ref <sha> --trust`, `ynh include update ... --trust`, `ynh include remove ... --trust`, for a harness installed from a folder | the harness again, after the change | every remote include that brings commands, and every delegate entry from another repository |

Refusals:

| Command | Output |
|---|---|
| `ynh trust accept <id>` or `<folder>` with a remote include or native delegate entry not at a full SHA | not a refusal: `Accepted ...`, then `Floating, accepted at what is installed now (what it executes reads as changed if it moves):` and one line each, for example `native delegate ...: no ref, so it floats with the default branch; accepted at commit <sha>, the one installed` (or `no commit could be told, so none is recorded`) |
| a ref that can move | `Error: --trust refuses to accept this install: it is not pinned to a full commit SHA, so what runs could change under the acceptance:` then each ref, such as `the harness's own ref: "main" is a tag or branch, which can move` or `a2a delegate <source>: "main" is a tag or branch, which can move` |
| `ynh run <id> --trust` on an installed id | `Error: --trust is for a run from a folder; an installed harness is accepted with 'ynh trust accept <id>', or at install with 'ynh install --trust'` |
| `ynh include add`, `update` or `remove ... --trust`, when a remote include that stays in the harness is not pinned | `Error: --trust refuses to accept <id> after this change: it is not pinned to a full commit SHA, so what runs could change under the acceptance:` then each include that can move, then `The include change was not made.` and the way out; nothing is written |

`ynh check` has no `--trust`. `ynh run --trust` is ynh's own flag and is not passed on to the vendor.

## Related

- [What invalidates trust](what-invalidates-trust.md): the events that change a state.
- [Trust reference](trust.md): what the digest covers, the gate, folder runs.
