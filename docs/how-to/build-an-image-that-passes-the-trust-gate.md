<!-- How-to. Checked against a build of ynh v0.18.0 (develop), and a base image built from the same commit, in a scratch YNH_HOME, 2026-11-27. -->

# Build an Image That Passes the Trust Gate

The ynh inside a harness image checks the harness at every start, like any other run. After these
steps your image carries an accepted trust record, so it starts; a harness changed after the build is
refused.

Pick the route for where the harness comes from:

- [An installed harness](#from-an-installed-harness), already accepted on the build machine.
- [A source that is not installed](#from-a-source-that-is-not-installed), a Git URL or a folder, with
  `--from`.

The image flags themselves are in [Docker: Harness Images](../docker.md#harness-images).

## Before you start

- Docker.
- A base image with ynh v0.18.0 or later. The image runs its base's ynh, not the ynh that builds it.
  The default base is `ghcr.io/eyelock/ynh:latest`; pass another with `--base <image>`.
- For a Git source: the 40-character commit SHA to build, and every remote include that brings hooks,
  MCP servers or sensors pinned to a full SHA in the manifest.

## From an installed harness

1. Accept it on the build machine, by review:

   ```bash
   ynh trust show <id>
   ynh trust accept <id>
   ```

   or at install, at a pinned commit:

   ```bash
   ynh install https://github.com/<org>/<repo> --ref <40-character-sha> --trust
   ```

2. Build it:

   ```bash
   ynh image <id> --tag <your-tag>
   ```

   ```
   Harness image built: <your-tag>
   Run: docker run --rm -v $(pwd):/workspace -e ANTHROPIC_API_KEY <your-tag> -- "your prompt"
   ```

   A harness you have not accepted, or that changed since, is refused with exit 1, before Docker
   starts:

   ```
   Error: refusing to build an image of local/reviewer: you have not accepted what it executes.
   It executes:
     hook before_tool: ./hooks/guard.sh
     sensor lint (command): echo lint ok from include <include>
   Review them: ynh trust show local/reviewer
   Then accept:  ynh trust accept local/reviewer
   ```

## From a source that is not installed

1. Build with `--from` and `--trust`, and with `--ref` at a full SHA for a Git source:

   ```bash
   ynh image <name> --from https://github.com/<org>/<repo> --ref <40-character-sha> --trust --tag <your-tag>
   ```

   For a local folder, leave out `--ref`; its remote includes still need full SHAs.

   `--trust` accepts what the source runs and bakes that acceptance into the image. Without it, a
   harness with hooks, MCP servers, sensors or workspace folders is refused at build time, exit 1.
   While a remote include is not in this machine's include cache yet (the usual case the first time),
   ynh cannot read what it executes, and says so:

   ```
   Error: refusing to build an image of local/reviewer: local/reviewer cannot be reviewed yet: what it executes could not be read in full.
     the harness: include github.com/<org>/<include-repo> is not in the cache, so what it executes cannot be read: resolving github.com/<org>/<include-repo>: not in the include cache
   Fix: review it, then build with 'ynh image reviewer --from <source> --trust', which fetches its includes (each remote include must be pinned to a 40-character commit SHA).
   ```

   Once its includes are cached, or when it has no remote includes, it lists what it executes:

   ```
   Error: refusing to build an image of reviewer from <source>: you have not accepted what it executes.
   It executes:
     hook before_tool: ./hooks/guard.sh
     sensor lint (command): echo lint ok from include <include>
     Fix: review it, then build with 'ynh image reviewer --from <source> --ref <40-character commit SHA> --trust'
   ```

   A ref that can move is refused too:

   ```
   Error: --trust refuses to accept this image: it is not pinned to a full commit SHA, so what runs could change under the acceptance:
     the source's own ref: "main" is a tag or branch, which can move
   ```

   A harness with nothing to accept builds without `--trust`.

## Check the image

1. Preview the Dockerfile without building, with `--dry-run`. It copies the record in and labels the
   digest:

   ```
   # Schema version of the layout above, so ynh does not try to migrate it, the
   # include cache (cache/), so included sensors are read from the build, and the
   # accepted trust record (trust.json), which the run's trust gate checks
   COPY --chown=ynh:ynh home/ /home/ynh/.ynh/
   ...
   LABEL dev.ynh.harness="reviewer" \
         ...
         dev.ynh.harness.trust-digest="sha256:b6a68a3387879e68" \
   ```

2. After the build, read the label and the record inside the image:

   ```bash
   docker inspect --format '{{ index .Config.Labels "dev.ynh.harness.trust-digest" }}' <your-tag>
   docker run --rm --entrypoint ynh <your-tag> trust
   ```

   ```
   sha256:b6a68a3387879e68
   HARNESS         STATE     VIA    COMMANDS  SOURCE
   local/reviewer  accepted  image  2         <source>
   ```

   The record is keyed `local/<name>`, the id the image runs the harness under, with `VIA image`. An
   image built with `--from` shows `(local)` as its source.

3. If the harness has sensors, check that the gate passes without starting a vendor:

   ```bash
   docker run --rm --entrypoint ynh <your-tag> check local/<name>
   ```

   ```
     ✓  lint  pass  1ms

   ok: 1 passed
   ```

## When the image is refused later

- **A layer on top changes the harness.** The gate inside the image works out the digest again at
  every start, so a `RUN` step that edits a hook makes the run refuse, exit 1:

  ```
  Error: refusing to run local/reviewer: its commands differ from what you accepted on 2026-11-27T16:52:07Z.
  ```

  Change the harness before the build, accept it, and build again.
- **The machine that runs the image is never asked.** Its own `trust.json` is not read: the
  acceptance in the image is the build machine's.
