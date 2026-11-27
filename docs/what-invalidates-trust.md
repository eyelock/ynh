<!-- Reference. Each row was produced live on a build of ynh v0.18.0 (develop) in a scratch YNH_HOME, 2026-11-27; the upgrade row with an acceptance made by ynh v0.17.2. -->

# What Invalidates Trust

Each event that makes ynh refuse a harness it ran before, what you see, and the command that lets it run
again. `ynh trust` prints the state; a refusal exits 1 before any vendor or sensor starts, with the
JSON error code `untrusted`. The steps are in
[Accept a harness again after it changes](how-to/accept-a-harness-again.md).

## The harness

| Event | State | What you see | What restores trust |
|---|---|---|---|
| Installed, never accepted | `unreviewed` | `refusing to run <id>: you have not accepted what it executes.`, then the list, `Review them:` and `Then accept:` | `ynh trust show <id>`, then `ynh trust accept <id>` |
| Installed, only asks for workspace folders, never accepted | `unreviewed` | `refusing to run <id>: you have not accepted what it asks for.`, then `It requests workspace folders:` | the same |
| A hook, MCP server or sensor is added, removed or changed | `changed` | `refusing to run <id>: its commands differ from what you accepted on <date>.` | the same |
| A script that a hook, MCP server or sensor runs changes, with the command text the same | `changed` | the same | the same |
| A workspace folder it asks for, its launch focus prompt, or a delegate's focus prompt changes | `changed` | the same | the same |
| A folder you installed with `ynh install ./<folder>` is edited in any of the ways above | `changed` | the same | the same |
| `ynh update <id>` moves a Git harness to a new commit, whatever changed in it | `changed` | `refusing to run <id>: your acceptance on <date> does not cover it: it was accepted at commit <old>, and is now at <new>.`; `ynh trust show` adds a `moved:` line | `ynh trust show <id>`, then `ynh trust accept <id>` |
| `ynh update <id>` brings any of the changes above | `changed` | at the update: `<id> is no longer trusted: what it executes changed since you accepted it on <date>, so ynh run, ynh agent run and ynh check refuse it.` | the same |
| `ynh include add`, `include update` or `include remove` changes what runs | `changed` | at the change: the same `is no longer trusted` message | `ynh trust show <id>`, then `ynh trust accept <id>`; for `include add` or `include update` on a harness installed from a folder, the same command with `--trust` accepts in one step (`include remove` refuses `--trust`) |
| The harness is uninstalled and installed again | `unreviewed`: uninstalling removes its record | the `unreviewed` refusal of the first rows: `refusing to run <id>: you have not accepted what it executes.` (or `what it asks for.`) | `ynh trust show <id>`, then `ynh trust accept <id>` |
| The harness was accepted with ynh v0.17 or earlier, and ynh is now v0.18.0 or later | `changed` | the `changed` refusal, then `Your acceptance predates the digest covering the contents of the scripts ...` | `ynh trust show <id>`, then `ynh trust accept <id>` |
| `env_passthrough` changes, at the top level or in a profile (digest scheme 4) | `changed` | the `changed` refusal | `ynh trust show <id>`, then `ynh trust accept <id>` |
| `mcp_manual_approval` changes, at the top level or in a profile (scheme 4) | `changed` | the same | the same |
| The harness's own `default_vendor` changes (scheme 4) | `changed` | the same | the same |
| The harness was accepted under digest scheme 3, and ynh is now at scheme 4 | `changed` once, with a note | the `changed` refusal, then a note about `env_passthrough`, `mcp_manual_approval`, `default_vendor` and the full-length digest | the same |
| An include is missing from ynh's cache | `unavailable` | `refusing to run <id>: <id> cannot be reviewed yet: what it executes could not be read in full.` | `ynh update <id>` |
| A script is a link that leads outside the harness, or a covered script is missing | refused, accepted or not | `a script its commands run cannot be read in full, so what runs cannot be reviewed:` and a `Fix:` line | make it a regular file inside the harness, as the `Fix:` line says; accepting does not help |

## What does not invalidate trust

| Event | State |
|---|---|
| Instructions, skills, agents, rules or commands change in a folder you installed | stays `accepted` |
| A run is given `-v <vendor>` or `YNH_VENDOR` | stays `accepted` |
| Project code a hook or sensor runs changes, such as `scripts/wake.sh` or a sensor's `./lint.sh` | stays `accepted` |
| A program found on `PATH` changes, such as `gofmt` or a package `npx` downloads | stays `accepted` |

## Delegates

| Event | Whose run is refused | What you see | What restores trust |
|---|---|---|---|
| An `a2a` delegate is not installed | the parent's | `delegate <source> of <parent> is not installed`, and the `Run:` line with the install command | `ynh install <source> --ref <ref>`, then `ynh trust accept <id>` |
| An `a2a` delegate is installed, never accepted | the parent's | `delegate <source> of <parent> is installed as <id> but not trusted` | `ynh trust show <id>`, then `ynh trust accept <id>` |
| An `a2a` delegate's commands change | the parent's | `delegate <source> of <parent> is installed as <id> but its commands changed since you accepted them` | the same |
| A `native` delegate's MCP servers change | the parent's, as `changed` | the `changed` refusal; the list names the server `of delegate <name>` | `ynh trust accept <parent>` |

## Runs from a folder

A folder run (`ynh run ./<folder>`, `ynh agent run --harness ./<folder>`) does not check the folder's
own commands. It checks the commands its remote includes bring:

| Event | What you see | What restores trust |
|---|---|---|
| A remote include is not in the cache | `refusing to run <name>: what its includes execute could not be read in full.`, and `Fetch them by running it with --trust ...` | `ynh run ./<folder> --trust`, with each remote include pinned to a full commit SHA |
| A remote include is in the cache, never accepted for this folder | `refusing to run <name>: its remote includes bring commands you have not accepted:`, and `Accept them for this folder: ynh trust accept <folder>` | `ynh trust accept <folder>`, or `ynh run ./<folder> --trust` |
| `ynh check ./<folder>` with a remote include not in the cache | the same refusal; `ynh check` has no `--trust` | `ynh run ./<folder> --trust` once, then check again |

## Images

| Event | What you see | What restores trust |
|---|---|---|
| `ynh image <id>` of a harness that is `unreviewed` or `changed` | `refusing to build an image of <id>: ...`, before Docker starts | accept it, then build |
| `ynh image <name> --from <source>` of a harness with commands, without `--trust` | `refusing to build an image of <name> from <source>: ...` and a `Fix:` line | build with `--trust`, and `--ref <40-character-sha>` for a Git source |
| A layer built on top of the image changes the harness | the image's run: `refusing to run local/<name>: its commands differ from what you accepted on <date>.` | change the harness before the build, accept it, and build again |

## Related

- [ynh trust commands](trust-commands.md): every command and flag that reads or makes a record.
- [Trust reference](trust.md): what the digest covers, the gate, and the full-SHA rule.
