<!-- How-to. Re-checked 2026-11-28 against a build of ynh develop at 960ecb6f (P0 to P4 of ynh #1058 merged) plus this pull request, in empty scratch YNH_HOMEs, with the harness and its delegate served over file:// and a stand-in vendor CLI on PATH. First checked against v0.18.0 (develop), 2026-11-27. Lines marked TODO(live) need a vendor session, which the stand-in cannot give; they are checked in the release's live check on all five vendors. -->

# Run a Harness on CI

A CI job has nobody to read `ynh trust show`, so it accepts a harness in the same step as it fetches it,
at a commit that cannot move. After these steps your job installs or fetches the harness and its
delegates, accepts them, and runs it at its permission level; a harness that is not pinned is refused
instead.

Pick the route for where the harness lives:

- [A harness from another repository](#a-harness-from-another-repository), installed by URL.
- [A harness in the repository the job builds](#a-harness-in-the-repository-the-job-builds), run
  from its folder.

## Before you start

- The job has ynh v0.18.2 or later.
- Every **remote include** in the harness's manifest that brings hooks, MCP servers or sensors has a
  `"ref"` that is a full 40-character commit SHA. A local include, and an include of the harness's own
  repository, need none.
- Every **`delegates_to` entry** from another repository has a `"ref"` that is a full 40-character
  commit SHA, whichever its transport. An entry naming the harness's own repository, or a local folder,
  needs none.
- For a harness from another repository: the 40-character SHA of the commit to run. A tag, a branch or
  a short SHA is refused. `git ls-remote https://github.com/<org>/<repo> refs/heads/main` prints the
  SHA a branch points to now.
- The job does not run as root, if the harness runs on Claude Code at `all` (the default). See
  [Run as a non-root user](#run-as-a-non-root-user).

## Give the job a home of its own

In every job, before any `ynh` command, point ynh at an empty folder the job owns:

```bash
export YNH_HOME="$RUNNER_TEMP/ynh-home"
```

The job then starts with no trust records, and accepts only what it pins itself.

## A harness from another repository

1. Install it at its commit and accept it, with its delegates, in one step:

   ```bash
   ynh install https://github.com/<org>/<repo> --ref <40-character-sha> --trust
   ```

   ynh fetches the harness, its includes and its delegates, checks that everything is pinned, and
   prints what it accepted. For a harness `team` with one delegate `reviewer`:

   ```
   Fetching 0 include(s) and 1 delegate(s)...
     Fetched <reviewer source>
   Installed harness "team"
     Location: <YNH_HOME>/harnesses/<id>
     Launcher: <YNH_HOME>/bin/team
     Vendor:   claude
   A2A delegate <reviewer source> of team, opens at edits when run unattended (entry):
   It executes:
     hook on_session_start: echo Reviewer session started.
   Installed harness "reviewer"
     Location: <YNH_HOME>/harnesses/<reviewer id>
     Launcher: <YNH_HOME>/bin/reviewer
     Vendor:   claude
   Accepted <reviewer id> (install --trust, with <id>): 1 command(s), sha256:abab6f64cd7a3573, opens at edits when run unattended
   Accepted <id> (install --trust): 0 command(s), sha256:855db543ebead33f, opens at all when run unattended
   ```

   Here team's entry narrows reviewer to `edits`. `<id>` is the harness's id, which `ynh ls` also
   prints. Every a2a delegate below the harness, nested ones included, gets a record of its own, all
   or nothing (`ynh trust` lists the delegate as `with <id>`). If the harness's ref, or any delegate's or
   include's, is not a full SHA, the install stops with exit 1, before anything is cloned:

   ```
   Error: --trust refuses to accept this install: it is not pinned to a full commit SHA, so what runs could change under the acceptance:
     a2a delegate <reviewer source>: "main" is a tag or branch, which can move
   Pin it to a 40-character commit SHA and try again. Acceptance is a step for a person who has reviewed what runs, and it covers only a commit that cannot move
   ```

   For the harness's own ref the line reads `the harness's own ref: "main" is a tag or branch, which
   can move`.

2. Run it by its id:

   ```bash
   ynh check <id>                                                # its sensors
   ynh run <id> -- "review this PR"                              # one-shot, at the harness's level
   ynh run <id> --auto-approve edits -- "review this PR"         # one-shot, narrowed to edits
   ynh agent run --harness <id> --task "<task>" --format json    # the agent loop, at the harness's level
   ```

   A one-shot run opens at the harness's [permission level](../permission-levels.md): `all` unless the
   harness declares less, and each delegate it starts opens no wider than the run. `--auto-approve
   none|edits|all` narrows one run; a level wider than the harness's is refused before anything starts.
   Each one-shot run and agent run prints its level and where it came from first, on stderr:

   ```
   auto-approve: all (default)
   Opened 1 A2A delegate endpoint(s): reviewer
   Launching claude...
   ```

   With `--auto-approve edits` the first line reads `auto-approve: edits (flag)`.

## A harness in the repository the job builds

The harness folder is part of the change the job tests, so its own hooks, MCP servers and sensors are
reviewed with the rest of the pull request. ynh checks only what comes from elsewhere: the commands
its remote includes bring, and its a2a delegates.

**To start a session or the agent loop**, run the folder with `--trust`. It fetches each remote
include and each delegate, accepts them, and runs, opening at the harness's level (`all` unless it
declares less), on every vendor:

```bash
ynh run "$PWD/harness" --trust -- "<prompt>"
ynh agent run --harness "$PWD/harness" --trust --task "<task>" --format json
```

For a folder `harness` with one delegate `reviewer`, which its entry narrows to `edits`:

```
A2A delegate <reviewer source> of team, opens at edits when run unattended (entry):
It executes:
  hook on_session_start: echo Reviewer session started.
Installed harness "reviewer"
  Location: <YNH_HOME>/harnesses/<reviewer id>
  Launcher: <YNH_HOME>/bin/reviewer
  Vendor:   claude
Accepted <reviewer id> (run --trust, with local/team): 1 command(s), sha256:abab6f64cd7a3573, opens at edits when run unattended
ynh: team runs from a folder, so its own commands are not trust-checked; the commands its remote includes bring are.
auto-approve: all (default)
Opened 1 A2A delegate endpoint(s): reviewer
Launching claude...
```

A remote include adds its own line first, such as
`Accepted <include>//@957cf50c86d6637322df322d91765d21d7c7d123 (run --trust): 1 command(s)`.

The same runs on Copilot CLI (`-v copilot`), at the same level.

Without `--trust`, a one-shot run of a folder is refused, exit 1, naming `--trust`:

```
ynh: team runs from a folder, so its own commands are not trust-checked; the commands its remote includes bring are.
Error: refusing to run team one-shot from a folder: it opens at all with nobody to approve, and a folder is not trust-checked.
Run it with --trust (its remote includes and a2a delegates must be pinned at a full SHA), or install it and accept it (ynh install <folder>, then ynh trust accept local/team).
```

**To run its sensors with `ynh check`**, install the folder with `--trust` and use the id it prints:

```bash
ynh install "$PWD/harness" --trust
ynh check local/<name>
```

```
Accepted local/reviewer (install --trust): 2 command(s), sha256:b6a68a3387879e68
...
  ✓  lint  pass  30ms

ok: 1 passed
```

## Run as a non-root user

Claude Code refuses to open at `all` as root, and ynh refuses first, before Claude starts, exit 1:

```
claude code refuses auto-approve all as root (--permission-mode bypassPermissions). Fix one of:
  - run as a non-root user
  - use ynh's agent image, which runs as uid 1001
  - narrow the run with --auto-approve edits
  - in a container that is the sandbox, set IS_SANDBOX=1 (or CLAUDE_CODE_BUBBLEWRAP), which Claude Code honours
```

(The text is ynh's, from `internal/permission/root.go`; it was not run as root for this page.)

A hosted runner (`runs-on: ubuntu-latest`) runs steps as the user `runner`, not root, so a job on it
needs nothing. A **container job** runs as the image's user, which is often root. Run it as a non-root
user:

```yaml
jobs:
  review:
    runs-on: ubuntu-latest
    container:
      image: <your image>
      options: --user 1001
```

or run the job in ynh's own image, `ghcr.io/eyelock/ynh:<version>`, which runs as the user `ynh`, not
root. If neither is possible, narrow the run with `--auto-approve edits`, or `none`.

## If the repository sets its own mode

If the repository the job works in sets a permission mode narrower than the run's level
(`permissions.defaultMode` in `.claude/settings.json` or `.claude/settings.local.json`; a top-level
`approval_policy` or `sandbox_mode` in `.codex/config.toml`), the run opens at the repository's mode
and prints a line saying so:

```
auto-approve: none (project)
auto-approve: opening at the project's mode (none) rather than all: <repo>/.claude/settings.json sets permissions.defaultMode "default"
```

The run is not refused. To let it open at the harness's level, remove the setting from the repository,
or accept the narrower level. A wider repository mode never widens the run. How each mode places on
`none`, `edits` and `all` is in
[Permission levels: the project's own mode](../permission-levels.md#the-project-s-own-mode).

## Fail the job on a refusal

A refusal exits 1 before any vendor or sensor starts. Retrying gives the same answer, so treat it as a
configuration error. With `--format json`:

- `ynh check` writes an error envelope with the code `untrusted`:

  ```json
  {"error":{"code":"untrusted","message":"refusing to run local/reviewer: you have not accepted what it executes.\n..."}}
  ```

- `ynh agent run` carries `"error_code": "untrusted"` and `"exit_code": 1` in its result.

## Keep the acceptance in the job

- Accept with `--trust` in the job, every run. Do not copy a `trust.json` into the job or keep
  `YNH_HOME` between jobs: a record made elsewhere vouches for code this job did not pin.
- Leave `ynh trust accept` to people. It accepts whatever the harness holds at that moment, which is
  what a person who has just read `ynh trust show` means, and not what a pipeline should do.
- Pass `--trust` only to `ynh install`, or to a run of a folder. An installed id refuses it at run time:

  ```
  Error: --trust is for a run from a folder; an installed harness is accepted with 'ynh trust accept <id>', or at install with 'ynh install --trust'
  ```

To bake the acceptance into an image instead, see
[Build an image that passes the trust gate](build-an-image-that-passes-the-trust-gate.md).
