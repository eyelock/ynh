<!-- Reference. Checked against a build of ynh v0.18.0 (develop), 2026-11-27. -->

# Trust Reference

What the trust digest covers, how the gate decides, folder runs, the full-SHA rule and the trust
record in an image. Why ynh asks for trust is in [Why ynh asks you to trust a harness](why-trust.md);
the commands are in [ynh trust commands](trust-commands.md) and the JSON in
[`trust.schema.json`](https://github.com/eyelock/ynh/blob/develop/docs/schema/cli/trust.schema.json).

The trust pages, in reading order:

1. Tutorial: [Your first trusted harness](tutorial/first-trusted-harness.md)
2. Explanation: [Why ynh asks you to trust a harness](why-trust.md)
3. How-to: [Accept a harness again after it changes](how-to/accept-a-harness-again.md),
   [Run a harness on CI](how-to/run-a-harness-on-ci.md),
   [Build an image that passes the trust gate](how-to/build-an-image-that-passes-the-trust-gate.md),
   [Trust a delegate](how-to/trust-a-delegate.md)
4. Reference: [What invalidates trust](what-invalidates-trust.md),
   [ynh trust commands](trust-commands.md),
   [What an accepted harness gets, by vendor](trust-by-vendor.md), and this page

## What the digest covers

The digest covers everything `ynh trust show` prints, as resolved for a run, for the harness and each of its profiles:

- sensor `command` and `version_command` strings, including sensors an include contributes;
- hook commands with their event and matcher, including hooks of an included harness that consented with `"hooks": true`;
- MCP servers: command, arguments, URL, `cwd` and the values of `env` entries and headers (hashed, never printed), including those a native delegate carries on its own and those of every harness the includes reach;
- the `workspace` folders, the `launch_focus` prompt, the `delegates_to` entries and the focus prompts they select.

It also covers **the scripts those commands run**, so changing a script's body, with its command text untouched, is noticed.

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
- **A file on this host.** A script at an absolute path outside the harness (`/usr/local/bin/format-check.sh`), or one named from the home directory (`~/bin/check.sh`, `$HOME/bin/check.sh` or `${HOME}/bin/check.sh`), is whatever is installed on the machine, not part of the project. `ynh trust show` says it is not covered, and labels it differently from project code.
- **Anything a covered script reaches outside its own folder.** A script that sources `../lib/common.sh`, or runs another script elsewhere, is covered for its own folder only.
- **Tools found on `PATH`.** A command such as `gofmt`, `golangci-lint` or `go` has its command string covered, not the binary.
- **What a skipped link points at**, as above. A skipped script is not silently accepted: it blocks the run (see [Links and other files](#links-and-other-files)).

The digest is a review aid, not a sandbox: it tells you when what you reviewed has moved, and the list above is what it cannot tell you.

### What `ynh trust show` prints

Under each hook, MCP server or sensor command that names a script, `ynh trust show` prints one `script:` line:

```
script:  ./tools/lint.sh (covered with its folder tools: 3 file(s), sha256:f49d58b15cc65fee)
script:  scripts/wake.sh (not covered: project code, outside what this digest reads)
script:  /usr/local/bin/format-check.sh (not covered: a file on this host, outside the harness)
script:  ./hooks/z.sh (NOT covered: refused; this blocks a run)
           skipped z.sh: a link that leads outside the harness, not read (this blocks a run)
```

The JSON has the same facts in a `script` object on each executable, hook and MCP server: `path`, `covered`, `folder`, `files`, `skipped` and `digest`.

### When the scheme changes

A record carries the digest scheme it was made under. A harness accepted under an older scheme reads `changed` once even if nothing in it moved, and `ynh trust show` says why: the old acceptance never saw what the new scheme adds. An acceptance that covered only command text (ynh v0.17 and earlier) sees the note about scripts; one that covered only sensors sees the note about hooks and MCP servers. Old records are never accepted on your behalf: review, then `ynh trust accept` again.

## What a record is keyed by

A record is keyed by the harness's installed id, and it carries the `source` and, for a remote harness, the commit (`sha`) it was accepted from. It counts only while all of these hold: the digest matches, the harness is installed from the same source, and it is at the same commit. A harness that matches the digest but was installed from another source, or is at another commit, reads `changed` with a `moved` reason: `ynh trust show` prints it as a `moved:` line and `--format json` carries it as `moved`, and the [gate](#the-gate) names it in its refusal. Review it and `ynh trust accept` again.

An image's baked record is keyed the same way, by what the image will see. `ynh image <name> --from <source> --trust` from a remote source writes the `--from` source, ref and commit into the image's copy of the harness as its install record, and the baked record names the same source and commit, so `ynh ls` and `ynh trust show` inside the image report where the harness came from and the record reads accepted. A local `--from` folder keeps neither: its path means nothing inside an image, so it reads as `(local)` and its record carries no source. The full-SHA rule is enforced at build time either way. An image of an installed harness (`ynh image <id>`) carries that harness's install record, so its baked record keeps the source and commit.

`ynh uninstall` removes the harness's record, so a harness installed later under the same id starts `unreviewed`. The source and commit check is what keeps a record safe if one is left behind (for instance, by editing `trust.json` or a failed removal, which `ynh uninstall` reports as a warning).

## The gate

`ynh run`, `ynh agent run` (and its `--resume`), `ynh check` and `ynh sensors run` refuse an **installed** harness, before any vendor starts, unless its record says `accepted` or `no-commands`. Refused states are `unreviewed` (never accepted), `changed` (what it executes differs from what you accepted, or you accepted under an older digest scheme) and `unavailable` (an include is not in the cache, so what runs cannot be read in full).

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

For a `changed` harness the first line says when you accepted it. Longer lists stop after eight entries and point at `ynh trust show`, which lists them all. Under `--format json`, `ynh check` and `ynh sensors run` (which only emits JSON) write the error envelope with code `untrusted`, and `ynh agent run` carries `error_code: "untrusted"` in its result, also with exit code 1 (capabilities `0.11.0`; see [Structured output](cli-structured.md)). A harness whose workspace folders are not allow-listed has them granted by accepting it.

What counts as something to review is the commands (sensors, hooks, MCP servers), the workspace folders and the focuses of delegates. A harness that only requests workspace folders is refused until you accept it, like any other: its refusal has no `It executes:` list, only the `It requests workspace folders:` list and the `Review them:` and `Then accept:` lines, and accepting grants the folders. Only a harness with none of these (`no-commands`) runs without acceptance.

Three refusals are not "you have not accepted this":

- **A skipped script blocks the run**, accepted or not (see [Links and other files](#links-and-other-files)). The refusal names the script and the reason and ends in a Fix line: make the file a regular file inside the harness, or add the missing script, then run `ynh trust show`.
- **An include that cannot be read refuses the run.** The refusal says what could not be read and names `ynh update`, which fetches it again.
- **An invalid profile refuses the whole harness.** The digest resolves every profile of a harness, so one that cannot be resolved leaves the set incomplete, and `ynh run` and `ynh check` refuse the harness as a whole, not only that profile. This is intended: a profile nobody can resolve is a profile nobody reviewed.

If the trust record itself cannot be read, the run is refused too.

### The messages as printed

Every way of accepting prints the same verb, `Accepted`, then what was accepted and, in brackets, the route (`accept`, `install --trust`, `run --trust`, `update --trust`, `include --trust`, `image`). `ynh image <name> --from <source> --trust` prints it on stderr, as `Accepted local/<name> (image): <n> command(s), <digest>`, so a `--dry-run` Dockerfile on stdout stays clean; a build that accepts nothing prints nothing:

```
$ ynh trust accept local/acme
Accepted local/acme (accept): 2 command(s), sha256:95539ec8b7d7abe6
$ ynh install ./acme --trust
Accepted local/acme (install --trust): 2 command(s), sha256:95539ec8b7d7abe6
It executes:
  hook before_tool: ./hooks/guard.sh
  sensor lint (command): gofmt -l .
```

A refusal says which act it refused. The gate says `refusing to run`; an acceptance that cannot go ahead says `refusing to accept`, for example when a remote include of `./dir` cannot be read:

```
$ ynh trust accept ./dir
Error: refusing to accept dir: what its includes execute could not be read in full.
  the harness: include github.com/you/shared is not in the cache, so what it executes cannot be read: resolving github.com/you/shared: not in the include cache
Fetch them by running it with --trust, once each remote include is pinned to a full 40-character commit SHA (ynh run <folder> --trust, or ynh agent run --harness <folder> --trust). ynh update takes installed harnesses only.
```

`ynh check` and `ynh run` refuse the same untrusted harness alike: exit 1, error code `untrusted`, the same message, whether the harness is installed or a folder, and including a folder whose remote include cannot be read.

A refusal names each executable by its own kind: `hook <event>`, `MCP server <name>` or `sensor <name> (<field>)`. The note about an acceptance made under an older digest scheme names only the kinds the harness declares, so a harness with only an MCP server reads `the scripts that MCP servers run`, not `sensors`.

Delegates keep their own gate: an A2A delegate that declares commands is refused until you accept it, and workspace grants, Codex's hook trust and Cursor's `--trust` follow the record. See [Sensors: what a command sensor can reach](sensors.md#what-a-command-sensor-can-reach), [Hooks](hooks.md#codex-hook-trust) and [Workspace Folders](workspace.md).

What the gate does not do: it does not sandbox. It decides whether a run starts, on what you reviewed; it does not limit what an accepted command can reach.

## Folder runs

A **folder run** is a run named by a path, not an installed id: `ynh run ./dir`, `ynh check ./dir`, `ynh run --harness-file <file>`, a manifest in the current folder, and `ynh agent run --harness <path>`. A folder has no record: it is yours, and you are looking at it.

"Folder" is not the same as "installed from a folder". `ynh install ./dir` copies nothing: it records a pointer to the folder under `$YNH_HOME` as `local/<name>`, and from then on it is an installed harness like any other: the gate refuses it until you accept it, by its id, whether or not the folder is a Git checkout. Because the installed harness is the folder itself, editing the folder changes the installed harness: it reads `changed` and is refused until you accept it again. Only a run by path is a folder run.

- The folder's **own commands run unchecked**, and ynh says so once on stderr: `ynh: <name> runs from a folder, so its own commands are not trust-checked; the commands its remote includes bring are.`
- The commands a **remote include** brings are refused unless accepted. A remote include is a Git include of another repository, at any depth; a local include and an include of the harness's own repository are the folder's own files. Each remote include that brings commands needs a record of its own, keyed by repository, subpath and ref, over that include's own commands, so the record means the same to every folder that includes it and accepting one folder never changes another's. An include whose commands cannot be read is refused. For an installed harness the Fix line names `ynh update`; for a folder run it names `--trust`, because `ynh update` takes installed ids only.
- **A run checks the gate offline.** Without `--trust` it never reaches the network: a remote include that is not in the cache is refused, and the Fix line says to run it again with `--trust`, once the include is pinned to a full SHA.
- **Fetch and accept them in one step with `--trust`**: `ynh run <dir> --trust`, `ynh check <dir> --trust` and `ynh agent run --harness <dir> --trust` fetch each remote include that is missing from the cache, once, then record it as in [the full-SHA rule](#the-full-sha-rule-and-trust-flags). Only an include pinned to a full 40-character SHA is fetched; an uncached include at a tag, a branch, a short SHA or no ref is refused and nothing is fetched. The refusal says to pin it and run again with `--trust`; it does not offer `ynh trust accept`, which reads an include from the cache and so cannot help. An include already in the cache is not fetched again. `ynh update <dir>` is not a route: it takes installed ids only. `ynh check <dir>` reaches the network only with `--trust`, so a CI job that only runs sensors can fetch and accept pinned includes itself; without it, an uncached remote include fails before the gate (exit 2) and the refusal says to fetch it with `--trust`.
- **Review them first with `ynh trust show <dir>`**, which lists each remote include that brings commands with its key, ref, commit SHA (when pinned to a full SHA), state (`unreviewed`, `accepted` or `changed`) and the commands it executes, in text or with `--format json` (`{harness, includes[]}`, published as [trust-show-folder.schema.json](https://github.com/eyelock/ynh/blob/develop/docs/schema/cli/trust-show-folder.schema.json)). It changes nothing, and refuses an include that is not in the cache as the gate does.
- **Or accept them by hand with `ynh trust accept <dir>`**, once they are in the cache, which records each remote include and prints how many commands. With none to accept it says so. A folder run gets none of the [pre-approvals that follow trust](#what-follows-trust): its MCP tools are not pre-approved, a one-shot Copilot run is refused and `--auto-approve` is refused.
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
| `ynh image <name> --from <source> --trust` (with `--ref <sha>` for a remote source) | the harness built into the image, baked as a record with `via: image` | before the image is built: the source's own ref and its remote includes' refs, after the includes are fetched |
| `ynh run <dir> --trust`, `ynh agent run --harness <dir> --trust` | the remote includes of a folder run | before the run launches |
| `ynh update local/<name> --trust`, for a harness installed from a folder | the harness again, after the update | before anything is fetched |
| `ynh include add local/<name> <url> --trust` (with `--replace` too), `ynh include update local/<name> <url> --trust` and `ynh include remove local/<name> <url> --trust`, for a harness installed from a folder | the harness again, after the change | as the change would leave it, judged before the change is written (the include is fetched first, since what it brings is only known then); a refusal writes nothing, so nothing is changed |

Notes on the routes:

- `ynh run --trust` is ynh's own flag. It is not passed on to Cursor.
- An **installed harness never takes `--trust` at run time**: it is refused, with the commands that do accept it (`ynh trust accept <harness>`, or `ynh install --trust`). A run-time `--trust` that you type from habit would otherwise accept whatever the harness had become.
- `include add --trust`, `include update --trust` and `include remove --trust` build the harness as it would be after the change, in memory, and judge that before writing anything. What an include brings is only known once it is fetched, so `include add` and `include update` fetch it first. If something is unpinned, or the includes cannot be read in full, the call is refused with the reason and nothing is written: the harness's manifest and its trust record are exactly as they were, and nothing is printed as done.
  The refusal names a way out that works on a harness left unchanged: pin each remote include to a full SHA and run it again with `--trust`, or make the change without `--trust`. For an include that cannot be read in full it says to make the change without `--trust`, run `ynh update <harness>` to fetch the includes, and then `ynh trust accept <harness>` once each is pinned; `ynh update` has nothing to fetch before the change is made.
- `include remove` changes what the harness executes, so an accepted harness reads `changed` afterwards. Without `--trust` it says so ("no longer trusted") and names `ynh trust show` and `ynh trust accept`. With `--trust` it records the new digest as accepted, under the same full-SHA rule as the other routes: what stays in the harness must be pinned.
- `--trust` is for an installed id on `update` and `include`, not a path.

### Without `--trust`

`ynh update` and `ynh include` print, when the change moved what an accepted harness executes, that it is **no longer trusted**, with `ynh trust show <harness>` and `ynh trust accept <harness>` to review and accept. Until you accept, the gate refuses it.

### How an acceptance was made

A trust record carries `via`: `accept` (`ynh trust accept`), `install --trust`, `run --trust`, `update --trust` or `include --trust`. A further value, `image`, is written by [`ynh image`](#ynh-image) into the record it bakes. `ynh trust show` and `ynh trust` print it, and `--format json` carries it as `accepted_via`. It records how you accepted, not what was safe: an acceptance by `--trust` is as strong as the pins it required.

## What follows trust

Some approvals let a run act without asking you. ynh gives them only to a harness that passed the gate as **trusted**: installed, and accepted or with nothing to accept. A harness run from a folder is never trusted, because the gate does not check its own commands.

| What | A trusted harness | A folder run, or an installed harness you have not accepted |
|---|---|---|
| On Codex, MCP tools of the harness's own servers on a one-shot run (`default_tools_approval_mode="approve"` for each server), unless the manifest sets `mcp_manual_approval` | pre-approved | not pre-approved; Codex asks as it always did, and ynh says so once on stderr |
| A one-shot Copilot run, which approves every tool (`--allow-all-tools`) | runs | refused before launch, exit 1 |
| `ynh agent run --auto-approve` with a `--harness` | runs | refused, exit 1, error code `untrusted` |

An installed harness you have not accepted is refused by the gate before any of this is reached, so in practice the right-hand column is the folder run. Copilot has no narrower grant than "every tool", so ynh refuses the run rather than approve it. A run with no `--harness` runs no harness commands, so `--auto-approve` there is your own grant. Every vendor's row is in [What an accepted harness gets, by vendor](trust-by-vendor.md). See also [MCP Servers](mcp.md), [Agent Loop](agent.md) and [Vendors](vendors.md).

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

An accepted harness is built, and the image **bakes the accepted record**: the trust record, with `via` set to `image`, is written as `trust.json` in the image's home, keyed `local/<name>`, the id the image's entrypoint runs the harness under, so the run inside the image passes its own gate. The image carries the label `dev.ynh.harness.trust-digest` with the digest. A harness with nothing to accept gets no record and no label, and the image runs it without one.

### `ynh image --from`

`ynh image <name> --from <source>` builds from a Git URL or a local path rather than an installed harness, so nothing has accepted it yet. A harness with hooks, MCP servers, sensors or workspace folders is refused at build time, with exit 1, the `untrusted` error code and a `Fix:` line naming `--trust`; a harness with nothing to accept builds, with no record and no label.

`--trust` accepts what the source executes and bakes the record, with `via` set to `image`, as above, so the image's start passes its gate. It follows the full-SHA rule of `ynh install --trust`: a remote source must be pinned with `--ref` at a 40-character commit SHA, and so must every remote include that brings commands, or the build is refused, naming what floats (a tag, a branch, a short SHA or no ref). A local folder is your own files and needs no pin; its remote includes still do.

```bash
ynh image acme --from github.com/org/acme --ref 0123456789abcdef0123456789abcdef01234567 --trust
```

`--trust` is only valid with `--from`; an installed harness is accepted with `ynh trust accept`. `--ref` is likewise `--from` only, and not valid for a local folder. A baked record is the host's acceptance carried to where the harness runs; it is not a fresh review.

## Related

- [What invalidates trust](what-invalidates-trust.md): each event that makes ynh ask again, and what restores trust.
- [Sensors](sensors.md#what-a-command-sensor-can-reach), [Hooks](hooks.md), [MCP Servers](mcp.md) and [Workspace Folders](workspace.md): what else follows the record.
- [Delegate Sessions](delegate-sessions.md): the delegate gate.
