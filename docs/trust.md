# Trust

Why ynh asks you to review a harness before it runs, what the review covers, and what it leaves to you. This page explains; the commands are in the [CLI reference](reference.md) and the JSON in [`trust.schema.json`](https://github.com/eyelock/ynh/blob/develop/docs/schema/cli/trust.schema.json).

> **Status.** All of it is built: the digest (scheme 3, [ynh #949](https://github.com/eyelock/ynh/issues/949)), the gate ([ynh #950](https://github.com/eyelock/ynh/pull/970)), the acceptance routes and the full-SHA rule ([ynh #951](https://github.com/eyelock/ynh/pull/973)), pre-approval that follows trust ([ynh #952](https://github.com/eyelock/ynh/pull/974)) and `ynh image` ([ynh #953](https://github.com/eyelock/ynh/pull/976)).

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
- **Instructions, skills, agents, rules and commands.** A change to any of them, including one `ynh update` brings, leaves the digest as it was and asks nothing. Focus prompts are the exception: the `launch_focus` prompt and the focus prompts `delegates_to` entries select are covered, because a prompt a session starts with steers the model as a command does.
- **`env_passthrough`.** Widening the list of your environment variables that a worker and MCP expansion receive does not change the digest. `ynm client install ynh` writes this list into a harness.
- **The vendor.** `default_vendor` and `-v` are not in the digest, so one acceptance holds on every vendor, though what it unlocks differs by vendor ([What follows trust](#what-follows-trust)).

The digest itself is SHA-256 cut to its first 64 bits, written as `sha256:` and 16 hex characters (`executablesDigest` in `cmd/ynh/trust.go`).

These four are known gaps, not settled policy: which fields the digest should cover, and its length, are the design question of [ynh #1007](https://github.com/eyelock/ynh/issues/1007) (digest scheme 4). A new scheme would make every accepted harness need accepting again, as [When the scheme changes](#when-the-scheme-changes) says.

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

A refusal exits 1 and prints, on stderr (a harness installed from `/home/you/acme`, with a hook, a sensor and two requested workspace folders):

```
Error: refusing to run local/acme: you have not accepted what it executes.
It executes:
  hook before_tool: ./hooks/guard.sh
  sensor lint (command): gofmt -l .
It requests workspace folders:
  ../ynh (resolves to /home/you/ynh)
  ~/code/shared (resolves to /home/you/code/shared)
Review them: ynh trust show local/acme
Then accept:  ynh trust accept local/acme
```

The `It requests workspace folders:` list appears only when the harness requests some, and shows each entry as written and where it resolves to; accepting grants them.

For a `changed` harness the first line says when you accepted it. Longer lists stop after eight entries and point at `ynh trust show`, which lists them all. Under `--format json`, `ynh check` writes the error envelope with code `untrusted`, and `ynh agent run` carries `error_code: "untrusted"` in its result, also with exit code 1 (capabilities `0.11.0`; see [Structured output](cli-structured.md)). A harness whose workspace folders are not allow-listed has them granted by accepting it.

What counts as something to review is the commands (sensors, hooks, MCP servers), the workspace folders and the focuses of delegates. A harness that only requests workspace folders is refused until you accept it, like any other: its refusal has no `It executes:` list, only the `It requests workspace folders:` list and the `Review them:` and `Then accept:` lines, and accepting grants the folders. Only a harness with none of these (`no-commands`) runs without acceptance.

Three refusals are not "you have not accepted this":

- **A skipped script blocks the run**, accepted or not (see [Links and other files](#links-and-other-files)). The refusal names the script and the reason and ends in a Fix line: make the file a regular file inside the harness, or add the missing script, then run `ynh trust show`.
- **An include that cannot be read refuses the run.** The refusal says what could not be read and names `ynh update`, which fetches it again.
- **An invalid profile refuses the whole harness.** The digest resolves every profile of a harness, so one that cannot be resolved leaves the set incomplete, and `ynh run` and `ynh check` refuse the harness as a whole, not only that profile. This is intended: a profile nobody can resolve is a profile nobody reviewed.

If the trust record itself cannot be read, the run is refused too.

Delegates keep their own gate: an A2A delegate that declares commands is refused until you accept it, and workspace grants, Codex's hook trust and Cursor's `--trust` follow the record. See [Sensors: what a command sensor can reach](sensors.md#what-a-command-sensor-can-reach), [Hooks](hooks.md#codex-hook-trust) and [Workspace Folders](workspace.md).

What the gate does not do: it does not sandbox. It decides whether a run starts, on what you reviewed; it does not limit what an accepted command can reach.

## Folder runs

A **folder run** is a run named by a path, not an installed id: `ynh run ./dir`, `ynh check ./dir`, `ynh run --harness-file <file>`, a manifest in the current folder, and `ynh agent run --harness <path>`. A folder has no record: it is yours, and you are looking at it.

"Folder" is not the same as "installed from a folder". `ynh install ./dir` copies nothing: it records a pointer to the folder under `$YNH_HOME` as `local/<name>`, and from then on it is an installed harness like any other: the gate refuses it until you accept it, by its id, whether or not the folder is a Git checkout. Because the installed harness is the folder itself, editing the folder changes the installed harness: it reads `changed` and is refused until you accept it again. Only a run by path is a folder run.

- The folder's **own commands run unchecked**, and ynh says so once on stderr: `ynh: <name> runs from a folder, so its own commands are not trust-checked; the commands its remote includes bring are.`
- The commands a **remote include** brings are refused unless accepted. A remote include is a Git include of another repository, at any depth; a local include and an include of the harness's own repository are the folder's own files. Each remote include that brings commands needs a record of its own, keyed by repository, subpath and ref, over that include's own commands, so the record means the same to every folder that includes it and accepting one folder never changes another's. An include whose commands cannot be read is refused, naming `ynh update`.
- **Accept them with `ynh trust accept <dir>`**, which records each remote include and prints how many commands. With none to accept it says so. A folder run gets none of the [pre-approvals that follow trust](#what-follows-trust): its MCP tools are not pre-approved, a one-shot Copilot run is refused and `--auto-approve` is refused.
- A script the digest could not read in full blocks a remote include's run exactly as it blocks an installed harness.

## The full-SHA rule and trust flags

Accepting by hand needs a review, and no pin: `ynh trust accept` takes a harness at whatever ref it has, and a harness installed from a folder needs no Git checkout. With `--trust`, only its remote includes that bring commands must be at full SHAs. `--trust` accepts in the same step as the action, so it asks for something in return: **everything that runs must be pinned to a commit that cannot move.**

A **full SHA** is a 40-character commit hash. A tag, a branch, a short SHA or no ref at all is not one: each can name another commit tomorrow, so an acceptance of it would cover code you never saw. `--trust` is refused, with the reason named, unless the harness's own ref (for a Git or registry install) and the ref of every remote include that brings commands is a full SHA. A local folder is your own files and needs no pin for them, but its remote includes still do. The refusal ends with the way out: pin it, or review it and accept it by hand with `ynh trust accept`:

```
Error: --trust refuses to accept this install: it is not pinned to a full commit SHA, so what runs could change under the acceptance:
  /home/you/inc//@main: "main" is a tag or branch, which can move
Pin it to a 40-character commit SHA, or review it and accept it by hand with 'ynh trust accept'
```

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

A trust record carries `via`: `accept` (`ynh trust accept`), `install --trust`, `run --trust`, `update --trust` or `include --trust`. A further value, `image`, is written by [`ynh image`](#ynh-image) into the record it bakes. `ynh trust show` and `ynh trust` print it, and `--format json` carries it as `accepted_via`. It records how you accepted, not what was safe: an acceptance by `--trust` is as strong as the pins it required.

## What follows trust

Some approvals let a run act without asking you. ynh gives them only to a harness that passed the gate as **trusted**: installed, and accepted or with nothing to accept. A harness run from a folder is never trusted, because the gate does not check its own commands.

| What | A trusted harness | A folder run, or an installed harness you have not accepted |
|---|---|---|
| MCP tools of the harness's own servers on a one-shot Codex run or an `ynh agent run --backend codex` worker (`default_tools_approval_mode="approve"` for each server) | pre-approved | not pre-approved; Codex stops at its approval policy as it always did. When there are servers to approve, ynh says so once on stderr |
| A one-shot Copilot run, which approves every tool (`--allow-all-tools`) | runs | refused before launch, exit 1 |
| `ynh agent run --auto-approve` with a `--harness` | runs | refused, exit 1, error code `untrusted` |

MCP pre-approval is Codex's only. On Claude Code, trust adds no MCP allow rule to a harness you run directly: the rule `ynh run` adds follows whether the session is an A2A delegate's, not trust ([MCP Servers](mcp.md#claude-code-format), `cmd/ynh/parent_mcp.go`).

An installed harness you have not accepted is refused by the gate before any of this is reached, so in practice the right-hand column is the folder run. Copilot has no narrower grant than "every tool", so ynh refuses the run rather than approve it. A run with no `--harness` runs no harness commands, so `--auto-approve` there is your own grant. See [MCP Servers](mcp.md), [Agent Loop](agent.md) and [Vendors](vendors.md).

### By vendor

What an accepted harness gets that a folder run does not, by the vendor it runs on. The rows marked "every vendor" apply on each:

| Vendor | What an accepted harness gets |
|---|---|
| Claude Code | nothing beyond every vendor's. A one-shot run gets no MCP allow rule; allow the tools yourself (`-- --allowedTools=mcp__plugin_<harness>_<server>`) or in your Claude settings. A delegate's one-shot session gets the rule because it is a delegate's: the delegate gate has already refused one that declares commands and is not accepted ([MCP Servers](mcp.md#claude-code-format)) |
| Codex | MCP tools of the harness's own servers approved on a one-shot run and on every `ynh agent run --backend codex` turn, unless `mcp_manual_approval` is set; its hooks trusted ([Codex hook trust](hooks.md#codex-hook-trust)) |
| Copilot CLI | a one-shot run, which passes `--allow-all-tools` and so approves every tool; a folder run is refused it |
| Cursor | `--trust`, so Cursor's Workspace Trust does not prompt. ynh approves no MCP tools on Cursor |
| OpenCode | nothing beyond every vendor's. ynh approves no MCP tools on OpenCode, which needed no allow rule for `ynh-delegates` either ([Allow rules for the tools](delegates-reference.md#allow-rules-for-the-tools)) |
| every vendor | the harness's requested `workspace` folders, granted when no allow-list is set ([Workspace Folders](workspace.md)); `ynh agent run --auto-approve` with `--harness` |

Codex's hook trust, Cursor's `--trust` and the workspace grant ask that the record is accepted and current; MCP pre-approval, Copilot's one-shot and `--auto-approve` ask that the harness passed the gate as trusted, which a harness with nothing to accept also does. Because the vendor is not in the digest, an acceptance given while running on Claude Code also allows a one-shot Copilot run later.

A one-shot Copilot run of a folder harness, and `--auto-approve` on one, as ynh prints them:

```
ynh: acme runs from a folder, so its own commands are not trust-checked; the commands its remote includes bring are.
Error: refusing to run acme one-shot on copilot: a run from a folder is not trust-checked, and copilot's one-shot mode approves every tool.
Install it and accept what it executes (ynh install <folder>, then ynh trust accept local/acme), run it interactively, or use another vendor.
```

```
ynh: acme runs from a folder, so its own commands are not trust-checked; the commands its remote includes bring are.
Error: refusing --auto-approve edits: acme runs from a folder, which is not trust-checked, so nothing it executes is vouched for.
Install it and accept what it executes (ynh install <folder>, ynh trust accept local/<name>), or run without --auto-approve.
```

## `ynh image`

`ynh image <name>` applies the same gate as a run. An installed harness that is unreviewed, changed or unavailable, or that carries a script the digest could not read, is refused, with the same list and the same `Review them` and `Then accept` lines, worded as a build:

```
Error: refusing to build an image of local/acme: you have not accepted what it executes.
It executes:
  hook before_tool: ./hooks/guard.sh
  sensor lint (command): gofmt -l .
It requests workspace folders:
  ../ynh (resolves to /home/you/ynh)
  ~/code/shared (resolves to /home/you/code/shared)
Review them: ynh trust show local/acme
Then accept:  ynh trust accept local/acme
```

An accepted harness is built, and the image **bakes the accepted record**: the trust record, with `via` set to `image`, is written as `trust.json` in the image's home, keyed `local/<name>`, the id the image's entrypoint runs the harness under, so the run inside the image passes its own gate. The image carries the label `dev.ynh.harness.trust-digest` with the digest. A harness with nothing to accept gets no record and no label, and the image runs it without one. `--from <source>`, which builds from a Git URL or a local path rather than an installed harness, is not checked and bakes no record and no label. On a base image whose ynh has the gate, such an image of a harness with hooks, MCP servers or sensors refuses at start, because it carries no record; a harness with nothing to accept runs. A baked record is the host's acceptance carried to where the harness runs; it is not a fresh review. That is the policy as built (`imageTrustRecord` and `writeImageTrust` in `cmd/ynh/image_trust.go`): the build host's acceptance is the only one. The host that runs the image is never asked, and its own `$YNH_HOME/trust.json` is not read, because the image has a home of its own. Inside the image the gate still computes the digest again at every run, so a harness changed after the build (by a layer on top) no longer matches the baked record and is refused (from code; not run). Who should review an image on the running side, if anyone (a registry policy, a signature), is not decided.

## Related

- [Sensors](sensors.md#what-a-command-sensor-can-reach): the `ynh trust` commands and the states.
- [Hooks](hooks.md), [MCP Servers](mcp.md) and [Workspace Folders](workspace.md): what else follows the record.
- [Delegate Sessions](delegate-sessions.md): the delegate gate.
