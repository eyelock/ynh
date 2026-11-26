# Trust

Why ynh asks you to review a harness before it runs, what the review covers, and what it leaves to you. This page explains; the commands are in the [CLI reference](reference.md) and the JSON in [`trust.schema.json`](https://github.com/eyelock/ynh/blob/develop/docs/schema/cli/trust.schema.json).

> **Status.** The digest sections below describe what `ynh trust` does today (digest scheme 3, [ynh #949](https://github.com/eyelock/ynh/issues/949)). The gate, folder runs and acceptance routes are part of the same design ([ynh #929](https://github.com/eyelock/ynh/issues/929)) and are marked **not built yet** where they appear. Each is filled in when its part lands, and until then ynh behaves as the sections on "what happens today" say.

## What trust is for

A harness is a folder of instructions, and some of what it carries *executes*: hooks, MCP servers and sensor commands run on your machine, with your environment and your credentials. A harness installed from a Git URL lives in `~/.ynh/harnesses/` while its sensors run against a tree somewhere else, so an update can change what runs without changing anything in the directory you are looking at.

`ynh trust` is the review step. `ynh trust show <harness>` lists everything the harness can get executed, `ynh trust accept <harness>` records a fingerprint of it (the **digest**) in `$YNH_HOME/trust.json`, and from then on ynh can tell a harness that is as you reviewed it from one that has changed. The record lives outside the harness on purpose: a harness carrying its own record would authorise itself.

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

### What the digest leaves out

- **Project code.** A script in the project (`bash scripts/wake.sh`, a sensor's `./lint.sh`) is the project's, like `make check`. `ynh trust show` says it is not covered.
- **Anything a covered script reaches outside its own folder.** A script that sources `../lib/common.sh`, or runs another script elsewhere, is covered for its own folder only.
- **Tools found on `PATH`.** A command such as `gofmt`, `golangci-lint` or `go` has its command string covered, not the binary.
- **What a skipped link points at**, as above.

The digest is a review aid, not a sandbox: it tells you when what you reviewed has moved, and the list above is what it cannot tell you.

### What `ynh trust show` prints

Under each hook, MCP server or sensor command that names a script, `ynh trust show` prints one `script:` line:

```
script:  ./tools/lint.sh (covered with its folder tools: 3 file(s), sha256:f49d58b15cc65fee)
script:  scripts/wake.sh (not covered: project code, outside what this digest reads)
script:  ./hooks/z.sh (NOT covered: refused)
           skipped z.sh: a link that leads outside the harness, not read
```

The JSON has the same facts in a `script` object on each executable, hook and MCP server: `path`, `covered`, `folder`, `files`, `skipped` and `digest`.

### When the scheme changes

A record carries the digest scheme it was made under. A harness accepted under an older scheme reads `changed` once even if nothing in it moved, and `ynh trust show` says why: the old acceptance never saw what the new scheme adds. A harness accepted under scheme 2 (command text only) sees the note about scripts; one accepted under the first scheme (sensors only) sees the note about hooks and MCP servers. Old records are never accepted on your behalf: review, then `ynh trust accept` again.

## What happens today

`ynh trust` **reports**; it does not stop a run. `ynh check` and `ynh run` warn on stderr when a harness has `changed` since you accepted it, and still launch. Delegates are different: an A2A delegate that declares commands is refused until you accept it, and workspace grants, Codex's hook trust and Cursor's `--trust` follow the record. See [Sensors: what a command sensor can reach](sensors.md#what-a-command-sensor-can-reach), [Hooks](hooks.md#codex-hook-trust) and [Workspace Folders](workspace.md).

## The gate

> **Not built yet** ([ynh #950](https://github.com/eyelock/ynh/issues/950), part B). This section is a placeholder, to be written from what ships.

To cover: which commands refuse (`ynh run`, `ynh agent run`, `ynh check`, `--resume`), what a refusal prints and its structured error code, an unreadable include refusing and naming `ynh update`, and the one-line notice a folder run prints.

## Folder runs

> **Not built yet** (part B). Placeholder.

To cover: what a folder run is (`ynh run ./dir`, `--harness-file`, a manifest in the current folder, `ynh agent run --harness <path>`), why its own executables run unchecked while a remote include's are refused unless accepted, and what a folder run never gets (pre-approval of MCP tools).

## The full-SHA rule and `--trust`

> **Not built yet** ([ynh #951](https://github.com/eyelock/ynh/issues/951), part C). Placeholder.

To cover: a full-SHA pin is a 40-character commit (a tag or a short SHA is not one), why `--trust` is allowed only when every executable-bearing remote include is pinned that way, the routes (`ynh install --trust`, `ynh run ./dir --trust`, `ynh agent run --harness <path> --trust`), why an installed harness never takes `--trust` at run time, the `via` field of a record, and `ynh update` leaving a harness refused until it is accepted again.

## Related

- [Sensors](sensors.md#what-a-command-sensor-can-reach): the `ynh trust` commands and the states.
- [Hooks](hooks.md), [MCP Servers](mcp.md) and [Workspace Folders](workspace.md): what else follows the record.
- [Delegate Sessions](delegate-sessions.md): the delegate gate.
