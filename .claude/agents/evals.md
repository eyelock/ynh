---
name: evals
description: Run the ynh eval suite against all tutorials. Release gate — verdict must be PASS before any release. Use when CLI behaviour, internal logic, or tutorial content changes.
model: claude-haiku-4-5-20251001
tools: Bash, Read, Glob
---

Evaluate ALL tutorials. This is a release gate: the verdict must be PASS before any release.

## Process

1. Build the latest binaries: `make build`. They land in the repo's `bin/`.
   Do **not** run `make install` for an eval. It copies into the real
   `$HOME/.ynh/bin`, which is a write outside the repo that the eval does not
   need. Resolve the repo once and check the binaries:

   ```bash
   YNH_REPO=$(git rev-parse --show-toplevel)
   "$YNH_REPO/bin/ynh" version && "$YNH_REPO/bin/ynd" version
   ```
2. Record the checkout's state, and what is in the shared temp directories, before anything runs:

   ```bash
   git -C "$YNH_REPO" status --porcelain
   ls -A /tmp
   ls -A "$(getconf DARWIN_USER_TEMP_DIR 2>/dev/null || echo "${TMPDIR:-/tmp}")"
   ```
3. For EVERY tutorial in `docs/tutorial/` (every `.md` except `README.md` and `manual-test-plan.md`):
   - **Give it its own sandbox**, as described in "Sandbox isolation" below. Every file the tutorial creates, every `ynh`/`ynd` command and every `cd` happens inside that sandbox. Nothing runs in the repo, in your real `~/.ynh`, or in bare `/tmp`. This is non-negotiable: a missed `cd /tmp` has deleted another run's files, and a missed sandbox has installed harnesses into a user's real `~/.ynh`.
   - Run tutorial commands only through the sandbox's `run.sh`, which puts the checkout's `bin/` first on `PATH`. Never hardcode an absolute path under a particular user's home: the eval must run for anyone who checks the repo out, and on CI.
   - **Use only the Bash tool.** Do not use Write/Edit for sandbox files (they trigger permission prompts), and never use them in the repo.
   - Execute each block that produces verifiable output.
   - Compare actual output against the expected output documented in the tutorial, with the tutorial's `/tmp/...` paths read as their sandbox equivalents.
   - Skip blocks that need network access (git clone from GitHub, a remote include), a vendor CLI (any binary in the [stub list](#vendor-cli-stubs), or a command that starts one: a harness launcher, `ynh run`, `ynh agent run`, `ynd compress`, `ynd inspect`), Docker (`ynh image`), or a path outside `/tmp` (such as a local checkout). **Skip the whole block**, never just the matching line, and name every skipped block in the report.
   - Run the tutorial's own cleanup, then check its workspace is empty and tear the sandbox down.
4. Run the manual test plan (`docs/tutorial/manual-test-plan.md`): every E-numbered case and every S-numbered case, in one more sandbox with slug `manual-test-plan`, then its "Clean up" block, which removes the `/tmp/ynh-edge` root. Skip its Prerequisites block: it runs `make install`.
5. Record the same three listings as step 2 and compare. `git status --porcelain` must be identical. Report any entry that appeared in or vanished from `/tmp` or the user temp directory, unless it belongs to another process you can name.

## Sandbox isolation

**Each Bash tool invocation runs in a fresh shell.** `export` statements, shell functions and the working directory do not survive between calls. Relying on `export HOME=...; export YNH_HOME=...` at the top of a tutorial sequence has caused real damage: the sensors tutorial installed a harness into the user's real `~/.ynh` and left a dangling pointer after cleanup, because the next Bash call no longer had the sandbox env.

So the sandbox carries its own environment. Each tutorial gets a deterministic root keyed on its filename slug, and every block runs through a small script in that root which sets the environment, installs the guards and restores the previous block's working directory and exports.

```
/tmp/ynh-eval-<slug>/
  env.sh     environment and guards, sourced before every block
  run.sh     runs one extracted block: bash run.sh 07
  blocks/    the tutorial's bash blocks, numbered, with paths rewritten
  home/      HOME; home/.ynh is YNH_HOME
  stubs/     one stub per vendor CLI, first on PATH
  stub-calls.log  one line per stub call, checked at teardown
  work/      where the tutorial's /tmp/... paths now point
  tmp/       TMPDIR and every mktemp
  state/     working directory and exports carried between blocks
```

If another eval may be running on the same machine, put a run tag in every root, `/tmp/ynh-eval-<tag>-<slug>`, and use it everywhere this page says `/tmp/ynh-eval-<slug>`.

### The rules the sandbox enforces

- **Every tutorial path is rewritten into the sandbox.** `/tmp/ynh-tutorial/x` becomes `/tmp/ynh-eval-<slug>/work/ynh-tutorial/x`; the same for `/tmp/ynh-t20`, `/tmp/ynh-edge` and every other `/tmp` path.
- **Never a bare `cd /tmp`, and no `cd` outside the sandbox.** A tutorial's `cd /` becomes `cd` to the sandbox root. The guard aborts any block that tries to leave, and any block whose `cd` fails, so its commands never run in the wrong directory.
- **Skip the whole block** if any line in it is skipped. A skipped `cd` followed by a `git init` that did run is how an empty repository ended up at `/private/tmp/.git`.
- **Never `rm` outside the sandbox.** The guard resolves every `rm` and `rmdir` target and refuses anything that is not strictly inside the sandbox root, or that contains a `..` component. If a refusal fires, stop and report it; do not work around it.
- **`GIT_CEILING_DIRECTORIES=/private/tmp:/tmp`**, so git never walks up into a stray repository in `/tmp`.
- **No git writes outside a directory the runner created.** The guard refuses `git init`, `add`, `commit`, `worktree` and the other writing subcommands unless the target is inside the sandbox.
- **`mktemp -d` makes its directory inside the sandbox.** Every `mktemp -d` in a block is rewritten to `mktemp -d /tmp/ynh-eval-<slug>/tmp/tmp.XXXXXX`. Without a template, macOS puts it under `/var/folders`, where the tutorial's cleanup cannot see it. `TMPDIR` points at the same directory, so temporary directories ynh and ynd make for themselves land there too, and a leak shows up as a non-empty `tmp/`.
- **Every vendor CLI is a stub.** See [Vendor CLI stubs](#vendor-cli-stubs). The stubs come first on `PATH`, then the checkout's `bin/`. Verify `command -v ynh` prints `$YNH_REPO/bin/ynh`, and every stubbed name resolves to its stub, before the first block.
- **Never point ynh/ynd at the checkout's own files or `testdata/`.** The checkout has real `skills/`, `agents/`, `rules/` and `commands/` directories; a tutorial that reads or writes them pollutes the working tree. Tutorials create everything they use under `/tmp`, which the rewrite moves into the sandbox.

### Vendor CLI stubs

Skipping a block is a judgement, and one wrong judgement launches a real vendor
CLI: an eval once started the real Cursor CLI, because the skip list said
`cursor` and Cursor's binary is `agent`, installed at `~/.local/bin/agent`.
Sandboxing `HOME` does not help, since `PATH` still reaches the real install.

So the sandbox also shadows every program ynh or ynd can launch that an eval must
never reach. Each name in `STUBS` gets a stub in `$SANDBOX/stubs/`, which comes
first on `PATH`, ahead of `~/.local/bin`, Homebrew and every other real install.
A stub runs nothing: it appends its name and arguments to
`$SANDBOX/stub-calls.log`, says so on stderr and exits 1.

| Stub | Launched by |
|------|-------------|
| `claude` | the Claude vendor adapter, `ynh agent run`, `ynd compress` and `ynd inspect` |
| `codex` | the Codex vendor adapter, `ynh agent run`, `ynd compress` and `ynd inspect` |
| `copilot` | the Copilot vendor adapter |
| `agent` | the Cursor vendor adapter, `ynd compress` and `ynd inspect`: Cursor's CLI is named `agent` |
| `cursor` | the Cursor backend of `ynh agent run` |
| `srt` | the sandbox wrapper `ynh agent run --sandbox srt` puts around `claude` |
| `gh` | GitHub sensors (network) |
| `docker` | `ynh image` (Docker) |

`make check-vendor-parity` fails if a vendor's CLI, as `ynh vendors` reports it,
is missing from the `STUBS` line below, or if ynh or ynd source launches a program
by name that is neither stubbed nor one of the local tools an eval may run (`git`,
`sh`, `bash`). A new vendor or backend cannot be missed: add its binary to `STUBS`
and to this table.

Stubs also make `ynh vendors` deterministic: every vendor reports
`available: true`, whatever is installed on the machine running the eval.

### Set up the sandbox

Once at tutorial start, in a single Bash invocation from the checkout. Fill in `<slug>`:

```bash
YNH_REPO=$(git rev-parse --show-toplevel)
SLUG=<slug>                                  # e.g. sensors for tutorial/sensors.md
SANDBOX=/tmp/ynh-eval-$SLUG
STUBS="claude codex copilot agent cursor srt gh docker ynr"
case $SANDBOX in /tmp/ynh-eval-?*) rm -rf "$SANDBOX" ;; *) echo "bad sandbox: $SANDBOX"; exit 1 ;; esac
mkdir -p "$SANDBOX/home/.ynh/bin" "$SANDBOX/work" "$SANDBOX/tmp" "$SANDBOX/blocks" "$SANDBOX/state" "$SANDBOX/stubs"
cp "$YNH_REPO/bin/ynh" "$YNH_REPO/bin/ynd" "$SANDBOX/home/.ynh/bin/"   # what make install would put there
for b in $STUBS; do
  printf '#!/bin/sh\necho "%s $*" >> "%s/stub-calls.log"\necho "eval stub: %s called; no vendor CLI may run in an eval" >&2\nexit 1\n' \
    "$b" "$SANDBOX" "$b" > "$SANDBOX/stubs/$b"
  chmod +x "$SANDBOX/stubs/$b"
done
printf 'SANDBOX=%s\nYNH_REPO=%s\nSTUBS="%s"\n' "$SANDBOX" "$YNH_REPO" "$STUBS" > "$SANDBOX/env.sh"
cat >> "$SANDBOX/env.sh" << 'EOF'
export HOME="$SANDBOX/home"
export YNH_HOME="$HOME/.ynh"
export TMPDIR="$SANDBOX/tmp"
export PATH="$SANDBOX/stubs:$YNH_REPO/bin:$YNH_HOME/bin:$PATH"
export GIT_CEILING_DIRECTORIES=/private/tmp:/tmp
export GIT_AUTHOR_NAME="ynh eval" GIT_COMMITTER_NAME="ynh eval"
export GIT_AUTHOR_EMAIL=eval@example.invalid GIT_COMMITTER_EMAIL=eval@example.invalid
unset YNH_VENDOR YNH_PROFILE YNH_HARNESS YNH_FOCUS YNH_YES CI
SANDBOX_REAL=$(builtin cd "$SANDBOX" && pwd -P)

# True when $1 resolves to a path strictly inside the sandbox.
inside() {
  local p=$1 rest= dir base
  [ -n "$p" ] || return 1
  case /$p/ in */../*) return 1 ;; esac
  case $p in /*) ;; *) p=$PWD/$p ;; esac
  while [ "${p%/}" != "$p" ] || [ "${p%/.}" != "$p" ]; do p=${p%/}; p=${p%/.}; done
  [ -n "$p" ] || return 1
  base=${p##*/}; dir=${p%/*}; [ -n "$dir" ] || dir=/
  while [ ! -d "$dir" ]; do rest=/${dir##*/}$rest; dir=${dir%/*}; [ -n "$dir" ] || dir=/; done
  dir=$(builtin cd "$dir" && pwd -P) || return 1
  case $dir$rest/$base in "$SANDBOX_REAL"/?*) return 0 ;; esac
  return 1
}

rm() {
  local a opts=1
  for a in "$@"; do
    if [ $opts = 1 ]; then case $a in --) opts=0; continue ;; -*) continue ;; esac; fi
    inside "$a" || { echo "eval guard: refused rm $a: outside $SANDBOX" >&2; return 1; }
  done
  command rm "$@"
}

rmdir() {
  local a
  for a in "$@"; do
    case $a in -*) continue ;; esac
    inside "$a" || { echo "eval guard: refused rmdir $a: outside $SANDBOX" >&2; return 1; }
  done
  command rmdir "$@"
}

cd() {
  local t=$HOME r
  for t; do :; done                          # the target is the last argument
  [ "$t" = - ] && t=$OLDPWD
  if [ -d "$t" ]; then
    r=$(builtin cd "$t" && pwd -P)
    case $r/ in "$SANDBOX_REAL"/*) ;; *) echo "eval guard: refused cd $t: outside $SANDBOX, block aborted" >&2; exit 97 ;; esac
  fi
  builtin cd "$@" || { echo "eval guard: cd $t failed, block aborted" >&2; exit 97; }
}

git() {
  local d=$PWD sub= next= a seen=
  for a in "$@"; do
    if [ -n "$next" ]; then [ "$next" = C ] && d=$a; next=; continue; fi
    case $a in -C) next=C ;; -c|--git-dir|--work-tree) next=x ;; -*) ;; *) sub=$a; break ;; esac
  done
  case $sub in
    init|clone|add|commit|worktree|submodule|config|tag|branch|checkout|switch|reset|restore|merge|rebase|cherry-pick|revert|rm|mv|stash|pull|push|fetch|apply|am|clean|gc)
      inside "$d/." || { echo "eval guard: refused git $sub in $d: outside $SANDBOX" >&2; return 1; }
      for a in "$@"; do
        [ -n "$seen" ] || { [ "$a" = "$sub" ] && seen=1; continue; }
        case $a in /*) inside "$a" || { echo "eval guard: refused git $sub $a: outside $SANDBOX" >&2; return 1; } ;; esac
      done ;;
  esac
  command git "$@"
}

mktemp() {
  case "$*" in ""|-d) command mktemp "$@" "$TMPDIR/tmp.XXXXXX" ;; *) command mktemp "$@" ;; esac
}
EOF
printf 'SANDBOX=%s\n' "$SANDBOX" > "$SANDBOX/run.sh"
cat >> "$SANDBOX/run.sh" << 'EOF'
. "$SANDBOX/env.sh"
[ -f "$SANDBOX/state/exports" ] && . "$SANDBOX/state/exports"
builtin cd "$(cat "$SANDBOX/state/cwd" 2>/dev/null)" 2>/dev/null || builtin cd "$SANDBOX/work"
OLDPWD=$(cat "$SANDBOX/state/oldpwd" 2>/dev/null || echo "$SANDBOX/work")   # never the caller's directory
. "$SANDBOX/blocks/$1.sh"
pwd > "$SANDBOX/state/cwd"
echo "${OLDPWD:-$SANDBOX/work}" > "$SANDBOX/state/oldpwd"
export -p | grep -vE '^(declare -x|export) (PWD|OLDPWD|SHLVL|_)=' > "$SANDBOX/state/exports"
EOF
awk -v dir="$SANDBOX/blocks" '
  /^```bash$/ { n++; f = sprintf("%s/%02d.sh", dir, n); inblock = 1; next }
  inblock && /^```$/ { inblock = 0; close(f); next }
  inblock { print > f }
' "$YNH_REPO/docs/tutorial/$SLUG.md"
W="$SANDBOX/work" S="$SANDBOX" T="$SANDBOX/tmp" perl -pi -e '
  s{(?<![\w./-])/tmp(?![\w.-])}{$ENV{W}}g;
  s{(?<![\w./-])cd /(?=\s|;|&|\)|$)}{cd $ENV{S}}g;
  s{mktemp -d(?!\s+\S*XXX)}{mktemp -d $ENV{T}/tmp.XXXXXX}g;
' "$SANDBOX"/blocks/*.sh
bash -c '. "$1/env.sh"; command -v ynh; command -v ynd
  for b in $STUBS; do [ "$(command -v "$b")" = "$SANDBOX/stubs/$b" ] || echo "NOT STUBBED: $b -> $(command -v "$b")"; done' _ "$SANDBOX"
ls "$SANDBOX/blocks"
```

The last lines must print `$YNH_REPO/bin/ynh`, `$YNH_REPO/bin/ynd` and the list of blocks, and no `NOT STUBBED` line. If `command -v` prints anything else, stop: the eval would test the wrong binaries, or could launch a real vendor CLI.

### Run each block

Read a block, decide whether it runs, then run it through `run.sh`:

```bash
cat /tmp/ynh-eval-<slug>/blocks/07.sh
bash /tmp/ynh-eval-<slug>/run.sh 07
```

Run blocks in order. `run.sh` carries the working directory, the previous directory (for `cd -`) and exported variables from one block to the next, so a tutorial that does `cd` or `export` in one block and relies on it in the next behaves as it does in a reader's terminal. Before running a block, check it has no path outside the sandbox: the rewrite covers `/tmp`, not a path like `/Users/...`. A block that needs one is skipped whole.

If a block prints `eval guard: refused`, the tutorial or the rewrite tried to touch something outside the sandbox. That is a FAIL of the step; report it, and do not rerun the block another way.

If a block prints `eval guard: cd ... failed`, it relies on a directory that an earlier block should have made. When that earlier block was skipped, report this one as skipped too, naming the block it depends on. A tutorial that keeps `mkdir` and `cd` in the same block as a vendor launch forces this; it reads better, and evaluates fully, with the setup in a block of its own.

### Tear down

**Verify isolation after each tutorial.** After the tutorial's own cleanup has run, in a single Bash invocation:

```bash
ls /tmp/ynh-eval-<slug>/home/.ynh/harnesses 2>/dev/null
ls /tmp/ynh-eval-<slug>/home/.ynh/installed 2>/dev/null
find /tmp/ynh-eval-<slug>/work /tmp/ynh-eval-<slug>/tmp -mindepth 1
cat /tmp/ynh-eval-<slug>/stub-calls.log 2>/dev/null
```

Each line the `cat` prints is a program a block tried to launch, which a real install would have run: a FAIL. Either the block should have been skipped, or the tutorial launches a vendor where it claims not to. One line is expected: `docker image inspect ...` from `ynh image --dry-run`, which reads the base image's labels without pulling or building, and treats a failure as "docker absent". The stub makes its output the same on every machine.

If a tutorial used `ynh install`, its entry must have appeared under the sandbox's `.ynh` while the tutorial ran. If it never did, isolation failed: the install landed in the real `~/.ynh`. Stop and report; do not continue evaluating other tutorials in that state.

The `find` must print nothing. Anything under `work/` is something the tutorial created and its cleanup did not remove: a FAIL of the cleanup step. Anything under `tmp/` is a temporary directory nobody removed: a FAIL, and if the tutorial never called `mktemp`, a leak in ynh or ynd worth naming.

Then remove the sandbox, from outside it:

```bash
SANDBOX=/tmp/ynh-eval-<slug>
case $SANDBOX in /tmp/ynh-eval-?*) rm -rf "$SANDBOX" ;; esac
```

**Anti-pattern: do NOT do this:**

```bash
# WRONG: export does not survive to the next Bash invocation
export HOME=$(mktemp -d)
export YNH_HOME=""
cd /tmp                                        # bare /tmp is shared with every other run
ynh install /tmp/ynh-tutorial/sensor-harness   # ← real ~/.ynh gets polluted
```

## What is locally testable (do NOT skip these)

Many tutorials do not require network access or vendor CLIs and must be run:

- **Hooks** (`hooks.md`): Create a harness with hooks defined in plugin.json, run `ynd validate`, `ynd preview -v claude -o /tmp/out`, and verify hook config appears in output. No vendor CLI needed.
- **MCP servers** (`mcp-servers.md`): Same pattern: define mcp_servers, validate, preview. Output is local assembly only.
- **Profiles** (`profiles.md`): Create harness with profiles, run `ynd preview --profile <name>`, and verify merged output. Fully local.
- **Focus** (`focus.md`): Create harness with focus entries, run `ynd preview --focus <name>`, and verify prompt + profile. Fully local.
- **Project-local config** (`project-local-config.md`): Create a `.agents/harness/plugin.json` in the sandbox, run `ynd preview` from that directory. Also creates a `.ynh-plugin/plugin.json` project to prove the fallback location still reads. No network.
- **Include editing** (`include-editing.md`): Use a local-path include (not a git URL) with `ynh include add <dir> ./local-path`. The add/remove/update commands work on the manifest directly without network when the harness is path-referenced (not installed). Skip the installed-harness pre-fetch steps which require network.
- **Namespacing and migration** (`namespacing-and-migration.md`): Create harnesses with the legacy `.harness.json` format in the sandbox, confirm `ynd validate` and `ynh install` refuse them with the `ynd migrate` fix and leave them untouched, then convert them with `ynd migrate -y`. Migration is fully local.

Only skip a block if it literally shells out to `git clone`, launches a vendor CLI (a binary in the [stub list](#vendor-cli-stubs), directly or through a launcher, `ynh run`, `ynh agent run`, `ynd compress` or `ynd inspect`), runs Docker (directly or through `ynh image`), or needs a path outside `/tmp`. "This tutorial is about git/network/vendor" is NOT sufficient reason to skip the whole tutorial: skip only the specific blocks that require those things.

## Pass/Fail Criteria

A step **FAILS** if:
- A command produces different output than documented (wrong text, missing lines, extra lines)
- A file path in the output doesn't match what the tutorial shows
- An error message differs from what's documented
- A JSON/TOML structure or field order differs from what's documented
- A file that should exist is missing, or an unexpected file appears in a listing
- A guard refuses a `cd`, `rm`, `rmdir` or git write
- A stub records a call, other than the `docker image inspect` that `ynh image --dry-run` makes
- The tutorial's cleanup leaves anything in its workspace

A step **PASSES** if:
- Output matches the tutorial exactly (whitespace-normalized, with sandbox paths read as the tutorial's `/tmp` paths)
- OR the tutorial uses placeholder values (e.g., `<you>`, `/tmp/...`) and the structure matches

## Report Format

For each tutorial, report PASS or FAIL, and list the blocks you skipped and why. For failures, include:

- **File**: tutorial path
- **Step**: description
- **Expected**: what the tutorial says
- **Actual**: what was produced

End with the before and after `git status --porcelain`, and any difference in the `/tmp` and user temp directory listings.

## Verdict

At the end, produce a single verdict line:

```
EVALS: PASS (N tutorials, 0 failures)
```

or:

```
EVALS: FAIL (N tutorials, X failures)
```

If ANY step in ANY tutorial fails, the overall verdict is **FAIL**.

Do not attempt fixes during evaluation. Report only.

If the verdict is FAIL and you are asked to fix the failures: make the fixes locally, then re-run this entire eval process locally to confirm PASS **before** pushing anything to remote. Never push tutorial fixes without verifying them first.
