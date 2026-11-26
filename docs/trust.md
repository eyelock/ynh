# Trust

Why ynh asks you to review a harness before it runs, what the review covers, and what it leaves to you. This page explains; the commands are in the [CLI reference](reference.md) and the JSON in [`trust.schema.json`](https://github.com/eyelock/ynh/blob/develop/docs/schema/cli/trust.schema.json).

> **Status.** The digest, the gate and folder runs are built (digest scheme 3, [ynh #949](https://github.com/eyelock/ynh/issues/949); the gate, [ynh #950](https://github.com/eyelock/ynh/pull/970)). The acceptance routes (`--trust` on `install`, `run`, `agent run`, `update` and `include`) and the full-SHA rule land with part C ([ynh #951](https://github.com/eyelock/ynh/pull/973)) and are marked **landing with part C** where they appear. `ynh image` does not check trust yet (part E, [ynh #953](https://github.com/eyelock/ynh/issues/953)).

## What trust is for

A harness is a folder of instructions, and some of what it carries *executes*: hooks, MCP servers and sensor commands run on your machine, with your environment and your credentials. A harness installed from a Git URL lives in `~/.ynh/harnesses/` while its sensors run against a tree somewhere else, so an update can change what runs without changing anything in the directory you are looking at.

`ynh trust` is the review step. `ynh trust show <harness>` lists everything the harness can get executed, `ynh trust accept <harness>` records a fingerprint of it (the **digest**) in `$YNH_HOME/trust.json`, and from then on ynh can tell a harness that is as you reviewed it from one that has changed. The [gate](#the-gate) acts on that: an installed harness that is not accepted does not run. The record lives outside the harness on purpose: a harness carrying its own record would authorise itself.

## What the digest covers

The digest covers everything `ynh trust show` prints, as resolved for a run, for the harness and each of its profiles:

- sensor `command` and `version_command` strings, including sensors an include contributes;
- hook commands with their event and matcher, including hooks of an included harness that consented with `"hooks": true`;
- MCP servers: command, arguments, URL, `cwd` and the values of `env` entries and headers (hashed, never printed), including those a native delegate carries on its own and those of every harness the includes reach;
- the `workspace` folders, the `launch_focus` prompt, the `delegates_to` entries and the focus prompts they select.

Since digest scheme 3 it also covers **the scripts those commands run**, so changing a script's body, with its command text untouched, is noticed.

### How a script is found

For each hook, MCP server and sensor command, ynh picks one word as the script:

1. Leading `NAME=value` words are skipped, including a quoted value that holds spaces (`CI=1 ./run.sh`, `FOO="a b" ./run.sh`).
2. A leading `env` is skipped with its own options (`-i`, `-u NAME`, `-C DIR`, `--unset`, `--chdir`) and any `NAME=value` words that follow. `env -S` hands `env` a whole command line and names no file.
3. If the next word is an interpreter (`sh`, `bash`, `zsh`, `python`, `python3`, `node`, with or without a path such as `/bin/bash`), the script is the first word after its options. An option that takes a value consumes it: `-o` and `-O` (and `+o`, `+O`) for the shells, including a cluster that ends in one (`bash -euo pipefail ./x`), `--rcfile` and `--init-file` for bash, `-W` and `-X` for Python, `-r`, `--require`, `--import` and `--loader` for Node. `--` ends the options. `-c`, `-m`, `-e` and `-p` take code, not a file, so they name no script.
4. Otherwise the script is the first word.

Quotes around the word are removed.

### What is covered

The script is **covered** when the word is `./x` or `$YNH_HARNESS_DIR/x` (`${YNH_HARNESS_DIR}/x` too), and for an MCP server `${PLUGIN_ROOT}/x` or `"${PLUGIN_ROOT}"/x`, and the file is in the harness or include that declares the command. `./x` and `${PLUGIN_ROOT}` are not covered for a hook or a sensor where they do not mean the harness's folder:

| Command | Script word | Covered |
|---|---|---|
| hook `./hooks/guard.sh` | `./hooks/guard.sh` | yes, against the declaring harness (an included harness's script is read from that include) |
| MCP `bash -o pipefail ${PLUGIN_ROOT}/mcp/serve.sh` | `${PLUGIN_ROOT}/mcp/serve.sh` | yes |
| sensor `CI=1 "$YNH_HARNESS_DIR/test-trust/run.sh" strong` | `$YNH_HARNESS_DIR/test-trust/run.sh` | yes, against the declaring harness |
| sensor `./lint.sh` | `./lint.sh` | no: a sensor runs in the measured tree, so `./x` is the project's file |
| hook `bash scripts/wake.sh` | `scripts/wake.sh` | no: a path into the project |

What is hashed is **the script's whole folder**: every regular file in it and in its subfolders, by path and contents. Code a script builds or reads from beside itself, such as the Go checker a sensor's `run.sh` compiles from `test-trust/`, is therefore in the digest. A `.git` folder is left out, because a fetch changes it without changing anything that runs. A script that does not exist yet is recorded as missing, so creating it later changes the digest.

### Links and other files

Only regular files inside the harness are read:

- a link to a regular file inside the harness is read through;
- a link that leaves the harness, one that loops or dangles, and a link to a device such as `/dev/zero` is never opened;
- a link to a folder is not followed, but a script's own folder that is a link to a folder inside the harness is read as that folder; one that leaves the harness is refused.

`ynh trust show` reports every refusal as a `skipped` line under the command, with the reason. A script that is itself refused reads as **not covered**. A skipped entry is recorded in the digest, so adding, removing or changing one is noticed, but what it points at is not read.

**A skipped script blocks the run.** The digest could not read it, so nothing can vouch for it, and accepting does not help. The [gate](#the-gate) refuses the run, accepted or not, naming the script, the reason it was skipped and a Fix line, and `ynh trust show` marks the entry "this blocks a run". A covered script whose folder is missing or unreadable blocks the same way.

### What the digest leaves out

- **Project code.** A script in the project (`bash scripts/wake.sh`, a sensor's `./lint.sh`) is the project's, like `make check`. `ynh trust show` says it is not covered.
- **Anything a covered script reaches outside its own folder.** A script that sources `../lib/common.sh`, or runs another script elsewhere, is covered for its own folder only.
- **Tools found on `PATH`.** A command such as `gofmt`, `golangci-lint` or `go` has its command string covered, not the binary.
- **What a skipped link points at**, as above. A skipped script is not silently accepted: it blocks the run (see [Links and other files](#links-and-other-files)).

The digest is a review aid, not a sandbox: it tells you when what you reviewed has moved, and the list above is what it cannot tell you.

### What `ynh trust show` prints

Under each hook, MCP server or sensor command that names a script, `ynh trust show` prints one `script:` line:

```
script:  ./tools/lint.sh (covered with its folder tools: 3 file(s), sha256:f49d58b15cc65fee)
script:  scripts/wake.sh (not covered: project code, outside what this digest reads)
script:  ./hooks/z.sh (NOT covered: refused; this blocks a run)
           skipped z.sh: a link that leads outside the harness, not read (this blocks a run)
```

The JSON has the same facts in a `script` object on each executable, hook and MCP server: `path`, `covered`, `folder`, `files`, `skipped` and `digest`.

### When the scheme changes

A record carries the digest scheme it was made under. A harness accepted under an older scheme reads `changed` once even if nothing in it moved, and `ynh trust show` says why: the old acceptance never saw what the new scheme adds. A harness accepted under scheme 2 (command text only) sees the note about scripts; one accepted under the first scheme (sensors only) sees the note about hooks and MCP servers. Old records are never accepted on your behalf: review, then `ynh trust accept` again.

## The gate

`ynh run`, `ynh agent run` (and its `--resume`) and `ynh check` refuse an **installed** harness, before any vendor starts, unless its record says `accepted` or `no-commands`. Refused states are `unreviewed` (never accepted), `changed` (what it executes differs from what you accepted, or you accepted under an older digest scheme) and `unavailable` (an include is not in the cache, so what runs cannot be read in full).

A refusal exits 1 and prints, on stderr:

```
Error: refusing to run local/acme: you have not accepted what it executes.
It executes:
  hook PreToolUse: ./hooks/guard.sh
  sensor lint (command): gofmt -l .
Review them: ynh trust show local/acme
Then accept:  ynh trust accept local/acme
```

For a `changed` harness the first line says when you accepted it. Longer lists stop after eight entries and point at `ynh trust show`, which lists them all. Under `--format json`, `ynh check` writes the error envelope with code `untrusted`, and `ynh agent run` carries `error_code: "untrusted"` in its result, also with exit code 1 (capabilities `0.11.0`; see [Structured output](cli-structured.md)). A harness whose workspace folders are not allow-listed has them granted by accepting it.

What counts as something to review is the commands (sensors, hooks, MCP servers), the workspace folders and the focuses of delegates. A harness that only requests workspace folders is refused until you accept it, like any other: its refusal has no `It executes:` list, only the `Review them:` and `Then accept:` lines, and accepting grants the folders. Only a harness with none of these (`no-commands`) runs without acceptance.

Three refusals are not "you have not accepted this":

- **A skipped script blocks the run**, accepted or not (see [Links and other files](#links-and-other-files)). The refusal names the script and the reason and ends in a Fix line: make the file a regular file inside the harness, or add the missing script, then run `ynh trust show`.
- **An include that cannot be read refuses the run.** The refusal says what could not be read and names `ynh update`, which fetches it again.
- **An invalid profile refuses the whole harness.** The digest resolves every profile of a harness, so one that cannot be resolved leaves the set incomplete, and `ynh run` and `ynh check` refuse the harness as a whole, not only that profile. This is intended: a profile nobody can resolve is a profile nobody reviewed.

If the trust record itself cannot be read, the run is refused too.

Delegates keep their own gate: an A2A delegate that declares commands is refused until you accept it, and workspace grants, Codex's hook trust and Cursor's `--trust` follow the record. See [Sensors: what a command sensor can reach](sensors.md#what-a-command-sensor-can-reach), [Hooks](hooks.md#codex-hook-trust) and [Workspace Folders](workspace.md).

What the gate does not do: it does not sandbox. It decides whether a run starts, on what you reviewed; it does not limit what an accepted command can reach.

## Folder runs

A **folder run** is a run named by a path, not an installed id: `ynh run ./dir`, `ynh check ./dir`, `ynh run --harness-file <file>`, a manifest in the current folder, and `ynh agent run --harness <path>`. A folder has no record: it is yours, and you are looking at it.

"Folder" is not the same as "installed from a folder". `ynh install ./dir` copies the harness into `$YNH_HOME` as `local/<name>`, and from then on it is an installed harness like any other: the gate refuses it until you accept it, by its id, whether or not the folder is a Git checkout. Only a run by path is a folder run.

- The folder's **own commands run unchecked**, and ynh says so once on stderr: `ynh: <name> runs from a folder, so its own commands are not trust-checked; the commands its remote includes bring are.`
- The commands a **remote include** brings are refused unless accepted. A remote include is a Git include of another repository, at any depth; a local include and an include of the harness's own repository are the folder's own files. Each remote include that brings commands needs a record of its own, keyed by repository, subpath and ref, over that include's own commands, so the record means the same to every folder that includes it and accepting one folder never changes another's. An include whose commands cannot be read is refused, naming `ynh update`.
- **Accept them with `ynh trust accept <dir>`**, which records each remote include and prints how many commands. With none to accept it says so. Whether a run's MCP tools are pre-approved is a separate part of the design ([ynh #952](https://github.com/eyelock/ynh/issues/952)); it is not built yet, and a folder run is not meant to get it.
- A script the digest could not read in full blocks a remote include's run exactly as it blocks an installed harness.

## The full-SHA rule and trust flags

> **Landing with part C** ([ynh #951](https://github.com/eyelock/ynh/pull/973)). Written to the decisions made for that part; check `ynh <command> --help` on a build that has it.

Accepting by hand needs a review, and no pin: `ynh trust accept` takes a harness at whatever ref it has, and a harness installed from a folder needs no Git checkout. With `--trust`, only its remote includes that bring commands must be at full SHAs. `--trust` accepts in the same step as the action, so it asks for something in return: **everything that runs must be pinned to a commit that cannot move.**

A **full SHA** is a 40-character commit hash. A tag, a branch, a short SHA or no ref at all is not one: each can name another commit tomorrow, so an acceptance of it would cover code you never saw. `--trust` is refused, with the reason named, unless the harness's own ref (for a Git or registry install) and the ref of every remote include that brings commands is a full SHA. A local folder is your own files and needs no pin for them, but its remote includes still do. The refusal ends with the way out: pin it, or review it and accept it by hand with `ynh trust accept`.

| Route | What it accepts | When the pin is checked |
|---|---|---|
| `ynh install <git-or-registry-source> --ref <sha> --trust` | the harness just installed | before anything is written |
| `ynh install ./dir --trust` | the harness just installed from a folder, as `local/<name>` (no `--ref`: a folder has none) | before anything is written, for its remote includes only |
| `ynh run <dir> --trust`, `ynh agent run --harness <dir> --trust` | the remote includes of a folder run | before the run launches |
| `ynh update <harness> --trust` | the harness again, after the update | before anything is fetched |
| `ynh include add <harness> <url> --trust` (with `--replace` too) and `ynh include update <harness> <url> --trust` | the harness again, after the change | after the change, because the includes are only known then |

Notes on the routes:

- `ynh run --trust` is ynh's own flag. It is not passed on to Cursor.
- An **installed harness never takes `--trust` at run time**: it is refused, with the commands that do accept it (`ynh trust accept <harness>`, or `ynh install --trust`). A run-time `--trust` that you type from habit would otherwise accept whatever the harness had become.
- `include add --trust` and `include update --trust` make the change first. If something is then unpinned, the change stays, the acceptance is refused with the reason, and the harness is as untrusted as it would have been without the flag. `--trust` on `include remove` is refused: removing an include needs no acceptance.
- `--trust` is for an installed id on `update` and `include`, not a path.

### Without `--trust`

`ynh update` and `ynh include` print, when the change moved what an accepted harness executes, that it is **no longer trusted**, with `ynh trust show <harness>` and `ynh trust accept <harness>` to review and accept. Until you accept, the gate refuses it.

### How an acceptance was made

A trust record carries `via`: `accept` (`ynh trust accept`), `install --trust`, `run --trust`, `update --trust` or `include --trust`. A further value, `image`, is reserved for part E. `ynh trust show` and `ynh trust` print it, and `--format json` carries it as `accepted_via`. It records how you accepted, not what was safe: an acceptance by `--trust` is as strong as the pins it required.

## `ynh image`

`ynh image` does not check trust yet (part E, [ynh #953](https://github.com/eyelock/ynh/issues/953)). Until it does, an image bakes whatever the harness holds, accepted or not; run `ynh trust show` first.

## Related

- [Sensors](sensors.md#what-a-command-sensor-can-reach): the `ynh trust` commands and the states.
- [Hooks](hooks.md), [MCP Servers](mcp.md) and [Workspace Folders](workspace.md): what else follows the record.
- [Delegate Sessions](delegate-sessions.md): the delegate gate.
