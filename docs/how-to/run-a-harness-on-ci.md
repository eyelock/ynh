<!-- How-to. Checked against a build of ynh v0.18.0 (develop) in empty scratch YNH_HOMEs, 2026-11-27, with the harness and its include served over file:// and a stand-in vendor CLI on PATH. -->

# Run a Harness on CI

A CI job has nobody to read `ynh trust show`, so it accepts a harness in the same step as it fetches it,
at a commit that cannot move. After these steps your job installs or fetches the harness, accepts it,
and runs it; a harness that is not pinned is refused instead.

Pick the route for where the harness lives:

- [A harness from another repository](#a-harness-from-another-repository), installed by URL.
- [A harness in the repository the job builds](#a-harness-in-the-repository-the-job-builds), run
  from its folder.

## Before you start

- The job has ynh v0.18.0 or later.
- Every **remote include** in the harness's manifest that brings hooks, MCP servers or sensors has a
  `"ref"` that is a full 40-character commit SHA. A local include, and an include of the harness's own
  repository, need none.
- For a harness from another repository: the 40-character SHA of the commit to run. A tag, a branch or
  a short SHA is refused. `git ls-remote https://github.com/<org>/<repo> refs/heads/main` prints the
  SHA a branch points to now.

## Give the job a home of its own

In every job, before any `ynh` command, point ynh at an empty folder the job owns:

```bash
export YNH_HOME="$RUNNER_TEMP/ynh-home"
```

The job then starts with no trust records, and accepts only what it pins itself.

## A harness from another repository

1. Install it at its commit and accept it in one step:

   ```bash
   ynh install https://github.com/<org>/<repo> --ref <40-character-sha> --trust
   ```

   ynh fetches the harness and its includes, checks that everything is pinned, and prints what it
   accepted:

   ```
   Fetching 1 include(s) and 0 delegate(s)...
     Fetched <include>
   Installed harness "reviewer"
     Location: <YNH_HOME>/harnesses/<id>
     Launcher: <YNH_HOME>/bin/reviewer
     Vendor:   claude
   Accepted <id> (install --trust): 2 command(s), sha256:b6a68a3387879e68
   It executes:
     hook before_tool: ./hooks/guard.sh
     sensor lint (command): echo lint ok from include <include>
   ```

   `<id>` is the harness's id, which `ynh ls` also prints. If the ref is not a full SHA, the install
   stops with exit 1:

   ```
   Error: --trust refuses to accept this install: it is not pinned to a full commit SHA, so what runs could change under the acceptance:
     the harness's own ref: "main" is a tag or branch, which can move
   Pin it to a 40-character commit SHA, or review it and accept it by hand with 'ynh trust accept'
   ```

2. Run it by its id:

   ```bash
   ynh check <id>                                         # its sensors
   ynh run <id> -- "<prompt>"                             # a one-shot session
   ynh agent run --harness <id> --task "<task>" --format json   # the agent loop
   ```

## A harness in the repository the job builds

The harness folder is part of the change the job tests, so its own hooks, MCP servers and sensors are
reviewed with the rest of the pull request. ynh checks only the commands its remote includes bring.

**To start a session or the agent loop**, run the folder with `--trust`. It fetches each remote
include, accepts it, and runs:

```bash
ynh run "$PWD/harness" --trust -- "<prompt>"
ynh agent run --harness "$PWD/harness" --trust --task "<task>" --format json
```

```
Accepted <include>//@957cf50c86d6637322df322d91765d21d7c7d123 (run --trust): 1 command(s)
It executes:
  sensor lint (command): echo lint ok
ynh: reviewer runs from a folder, so its own commands are not trust-checked; the commands its remote includes bring are.
Resolving 1 include(s)...
  Cached <include>
Launching claude...
```

**To run its sensors with `ynh check`, or to get the extras only an accepted harness gets** (MCP tools
pre-approved, a one-shot Copilot CLI run, `ynh agent run --auto-approve`), install the folder with
`--trust` and use the id it prints:

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
