# Add a delegate to a harness

You have a parent harness and want it to hand work to another harness. After these steps `ynh run` of the
parent starts with that harness as an `a2a` delegate: its own ynh session, which is the default from v0.18.0.

Two routes, by where the delegate lives:

- **A local folder**, for developing the delegate and the parent together.
- **A remote git URL**, for a delegate someone else publishes.

Why a delegate is its own session is in [Delegate sessions](../delegate-sessions.md). The manifest fields are
in [delegates_to](../harnesses.md#delegates-to-optional).

> **Status of this page.** It describes the intended behaviour. What was run, on 2026-11-15 against a build
> of `develop` `4c05c4b5` in a scratch `YNH_HOME`, is marked **run**. Everything else is **intended, not
> run**. The tutorial that follows this page is meant to run all of it; where it does not, the page or ynh is
> wrong, and the finding goes to the roadmap.

## Before you start

You know two ynh terms:

- **Installed harness.** One that `ynh install` has copied or linked into `$YNH_HOME`. Its id is
  `local/<name>` for a local folder and `github.com/<org>/<repo>/<name>` for a remote one.
- **Trust.** `ynh trust accept <id>` records that you have read the commands, hooks and MCP servers a
  harness will run, before ynh runs them. See [Sensors: ynh trust](../sensors.md#what-a-command-sensor-can-reach).

## Route 1: a delegate in a local folder

1. Install the delegate from its folder. This records it as a local install, `local/<name>` (**run**):

   ```
   ynh install /absolute/path/to/delegate
   ```

2. Add the entry to the parent. `<parent>` is the parent's installed id, or the path to its folder, and `.`
   inside it works (**run**: a bare name such as `parent` is refused as not a valid harness id):

   ```
   ynh delegate add <parent> /absolute/path/to/delegate
   ```

   ynh writes the folder into the entry as an absolute `git:` source (**run**):

   ```json
   {"delegates_to": [{"git": "/absolute/path/to/delegate"}]}
   ```

   The path must be absolute. A relative path is resolved against the parent's own folder, and it breaks
   once the parent is installed elsewhere (see [CONTRIBUTING, Delegates: remote-only](https://github.com/eyelock/ynh/blob/develop/.github/CONTRIBUTING.md#delegates-remote-only)).

3. Read what the delegate would run, then accept it. A delegate that runs nothing shows `no-commands`
   (**run**) and needs no accepting; when `ynh trust` shows `unreviewed`, read and accept (**intended, not
   run** for a delegate with commands):

   ```
   ynh trust show local/<name>
   ynh trust accept local/<name>
   ```

4. Check the match. `ynh doctor` reads each delegate's source and reports one that no longer resolves; its
   "delegates" check printed `ok` here (**run**):

   ```
   ynh doctor
   ```

> **Watch out.** The allow-list governs a local source like a network one. If `allowed_remote_sources` in
> `~/.ynh/config.json` is set, add the delegate's folder to it, or the parent is refused. See
> [Getting started: local paths](../getting-started.md#local-paths) (**not run**).

## Route 2: a delegate at a remote git URL

1. Install it:

   ```
   ynh install github.com/<org>/<repo>
   ```

   For a harness in a subfolder of the repository, add `--path <subdir>`.

2. Add the entry to the parent, with `--ref` to pin a tag, branch or commit if you want one:

   ```
   ynh delegate add <parent> github.com/<org>/<repo>
   ```

3. Read and accept it as in route 1, step 3, with the id `ynh install` printed.

The remote route is **intended, not run**: the entry was written with a placeholder URL (**run**), but a
delegate installed from a remote URL or a registry entry and then started by its parent has not been shown
live. See [the evidence on the parity page](../vendor-parity.md#l40-a2a-delegates-cursor-opencode-workspace-folders-and-a-person-answering-not-shown-live).

## After either route

1. Review and accept the parent again. Its digest covers every `delegates_to` entry, so adding one makes it
   read `changed` once:

   ```
   ynh trust show <parent>
   ynh trust accept <parent>
   ```

2. Check the transport. `ynh info <parent-id>` prints each delegate's resolved transport, as
   `transport=a2a (default)` (**intended, not run**; `ynh info` takes an installed id, and refused `.`
   here).

> **Watch out.** A delegate that is not installed, not trusted, changed since it was trusted, or unreadable
> refuses the parent's whole run, before the vendor starts, and the message names the command that fixes it.
> ynh never installs or trusts anything for you. The four messages are under
> [Refusals when a delegate cannot be matched](../delegates-reference.md#refusals-when-a-delegate-cannot-be-matched).

## Findings from writing this page

For the desk to hand to the roadmap. None is filed.

1. `ynh delegate add --help` says "Add a Git delegate" and "`<url>` Git URL of the source", and
   `docs/reference.md` says local paths are not supported. A local absolute path is accepted and written.
   Help and reference are stale, or the command should refuse; which one is a decision.
2. `ynh delegate add` takes `.` or a path for `<harness>` but refuses a bare name; `ynh info` refuses `.` and
   takes only an installed id. The same argument means different things on sibling commands.
3. [CONTRIBUTING](https://github.com/eyelock/ynh/blob/develop/.github/CONTRIBUTING.md#delegates-remote-only)
   still says delegate entries "must be remote git URLs", against what `ynh delegate add` and
   [ynh #855](https://github.com/eyelock/ynh/pull/855) now do. I can't change that file; it needs a worker.

## Checked against

`ynh` built from `develop` `4c05c4b5`, 2026-11-15, in a scratch `YNH_HOME` and `HOME`: `ynh delegate add`
with a local folder and a placeholder remote, `ynh install` of a local folder, `ynh trust`, `ynh doctor` and
`ynh info`. Everything marked **intended, not run** is from
[reference.md](../reference.md), [harnesses.md](../harnesses.md) and
[delegates-reference.md](../delegates-reference.md).
