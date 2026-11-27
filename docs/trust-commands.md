<!-- Reference. Each command and output was run on a build of ynh v0.18.0 (develop) in a scratch YNH_HOME, 2026-11-27. -->

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
| `ynh trust show <id>` | Everything the harness runs, the scripts behind it, its digest, and, when accepted, the accepted digest, date and route |
| `ynh trust accept <id>` | Records the harness's current digest as accepted |
| `ynh trust accept <folder>` | Records the remote includes of a folder, for runs of that folder |

| Flag | Values | Default |
|---|---|---|
| `--format` | `text`, `json` | `text` |

`ynh trust show` takes an installed id only, not a folder.

### Output

`ynh trust accept <id>`:

```
Accepted local/greeter (accept): 2 command(s), sha256:a3403ee82e9f674b6ed5e445d4e6fd2f214b6b7c365f74c9d2c734e7a7ab5104
```

A harness that only asks for workspace folders: `Accepted local/ws (accept): 0 command(s), 1 workspace folder(s), sha256:43b4c4b79432c8467d7c8de210972adfcc475c66aaa13ef03edb7aa1b43fa5c1`.

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
object. The full shape is in [Structured CLI output](cli-structured.md) and `ynh schema trust`.

### States

| State | Meaning | Runs |
|---|---|---|
| `unreviewed` | never accepted | refused |
| `accepted` | what it runs matches what you accepted | yes |
| `changed` | what it runs differs from what you accepted, or the acceptance was made by an older ynh | refused |
| `unavailable` | an include is missing from the cache, so what it runs cannot be read in full | refused |
| `no-commands` | it runs nothing and asks for no folders: there is nothing to accept | yes |

### `VIA`: how an acceptance was made

| Value | Made by |
|---|---|
| `accept` | `ynh trust accept` |
| `install --trust` | `ynh install ... --trust` |
| `run --trust` | `ynh run <folder> --trust`, `ynh agent run --harness <folder> --trust` |
| `update --trust` | `ynh update <id> --trust` |
| `include --trust` | `ynh include add ... --trust`, `ynh include update ... --trust` |
| `image` | the record `ynh image` bakes into an image |

`--format json` carries it as `accepted_via`.

## The `--trust` flag

`--trust` accepts in the same step as the command, without a separate `ynh trust accept`. Everything it
accepts must be pinned to a full 40-character commit SHA; otherwise the command is refused, exit 1,
naming what can move. A local folder needs no pin for its own files; its remote includes do.

| Command | Accepts | Pins required |
|---|---|---|
| `ynh install <url> --ref <sha> --trust` | the installed harness | `--ref`, and every remote include that brings commands |
| `ynh install ./<folder> --trust` | the installed harness, as `local/<name>` | every remote include that brings commands |
| `ynh run ./<folder> --trust` | the folder's remote includes, fetching any not in the cache | every remote include that brings commands |
| `ynh agent run --harness ./<folder> --trust` | the same | the same |
| `ynh image <name> --from <source> --trust` | the harness built into the image, baked as `via: image` | `--ref` for a Git source, and every remote include that brings commands |
| `ynh update local/<name> --trust`, for a harness installed from a folder | the harness again, after the update | every remote include that brings commands |
| `ynh include add local/<name> <url> --ref <sha> --trust`, `ynh include update ... --trust`, for a harness installed from a folder | the harness again, after the change | every remote include that brings commands |

Refusals:

| Command | Output |
|---|---|
| a ref that can move | `Error: --trust refuses to accept this install: it is not pinned to a full commit SHA, so what runs could change under the acceptance:` then each ref, such as `the harness's own ref: "main" is a tag or branch, which can move` |
| `ynh run <id> --trust` on an installed id | `Error: --trust is for a run from a folder; an installed harness is accepted with 'ynh trust accept <id>', or at install with 'ynh install --trust'` |
| `ynh include remove ... --trust` | `Error: --trust goes with ynh include add or update: removing an include needs no acceptance` |

`ynh check` has no `--trust`. `ynh run --trust` is ynh's own flag and is not passed on to the vendor.

## Related

- [What invalidates trust](what-invalidates-trust.md): the events that change a state.
- [Trust reference](trust.md): what the digest covers, the gate, folder runs.
