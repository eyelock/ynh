---
name: evals
description: Run the ynh eval suite against all tutorials. Release gate — verdict must be PASS before any release. Use when CLI behaviour, internal logic, or tutorial content changes.
model: claude-haiku-4-5-20251001
tools: Bash, Read, Glob
---

Evaluate ALL tutorials. This is a release gate — the verdict must be PASS before any release.

## Process

1. Build and install the latest binaries: `make build && make install`
2. For EVERY tutorial in `docs/tutorial/` (every `.md` except `README.md` and `manual-test-plan.md`):
   - **Isolate per tutorial** — see "Sandbox isolation" below. Failure to isolate pollutes the user's real `~/.ynh` and leaves dangling pointers; this is non-negotiable.
   - Use binaries at `/Users/david/.ynh/bin/ynh` and `/Users/david/.ynh/bin/ynd`
   - **ALL file creation and commands MUST run in `/tmp/`** — never in the repo directory. The repo has real `skills/`, `agents/`, `rules/`, `commands/` directories; creating test files there pollutes the working tree. Use `cd /tmp` or absolute `/tmp/...` paths for all tutorial commands.
   - **Use only the Bash tool** for creating files outside the repo. Do NOT use Write/Edit tools for `/tmp/` files (they trigger permission prompts).
   - Execute each step that produces verifiable output
   - Compare actual output against the expected output documented in the tutorial
   - Skip steps that require: network access (git clone from GitHub), vendor CLIs (claude, codex, cursor), or Docker
   - **Tear down the sandbox** with `rm -rf /tmp/ynh-eval-<slug>` after the tutorial passes
3. Run the manual test plan (`docs/tutorial/manual-test-plan.md`) error-case section (all E-numbered cases)

## Sandbox isolation

**Each Bash tool invocation runs in a fresh shell.** `export` statements do not survive between calls — anything you set in one Bash call is gone by the next. Relying on `export HOME=...; export YNH_HOME=...` at the top of a tutorial sequence has caused real damage: the sensors tutorial installed a harness into the user's real `~/.ynh` and left a dangling pointer after cleanup, because the next Bash call no longer had the sandbox env.

**Use deterministic per-tutorial sandbox paths, keyed on the tutorial's filename slug, and prefix every `ynh`/`ynd` invocation inline.** The pattern:

1. **Once at tutorial start** (single Bash invocation):

   ```bash
   SANDBOX=/tmp/ynh-eval-<slug>          # e.g. sensors for tutorial/sensors.md
   rm -rf "$SANDBOX"
   mkdir -p "$SANDBOX/.ynh"
   ```

2. **Every subsequent Bash invocation** for that tutorial prefixes each command:

   ```bash
   HOME=/tmp/ynh-eval-<slug> YNH_HOME=/tmp/ynh-eval-<slug>/.ynh \
       /Users/david/.ynh/bin/ynh install /tmp/ynh-tutorial/sensor-harness
   ```

   The `VAR=val cmd` form sets the variable for that invocation only — no `export`, no reliance on shell state. Works identically across every fresh Bash shell.

3. **Once at tutorial end** (single Bash invocation):

   ```bash
   rm -rf /tmp/ynh-eval-<slug>
   ```

**Verify isolation after each tutorial.** Before tearing down, run:

```bash
ls /tmp/ynh-eval-<slug>/.ynh/harnesses 2>/dev/null
ls /tmp/ynh-eval-<slug>/.ynh/installed 2>/dev/null
```

If a tutorial used `ynh install`, the installed entry must appear in the sandbox. If the sandbox is empty AND the tutorial called `ynh install`, isolation failed — the install landed in the real `~/.ynh`. Stop and report; do not continue evaluating other tutorials in that state.

**Anti-pattern — do NOT do this:**

```bash
# WRONG: export does not survive to the next Bash invocation
export HOME=$(mktemp -d)
export YNH_HOME=""
ynh install /tmp/ynh-tutorial/sensor-harness   # ← real ~/.ynh gets polluted
```

## What is locally testable (do NOT skip these)

Many tutorials do not require network access or vendor CLIs and must be run:

- **Hooks** (`hooks.md`): Create a harness with hooks defined in plugin.json, run `ynd validate`, `ynd preview -v claude -o /tmp/out` — verify hook config appears in output. No vendor CLI needed.
- **MCP servers** (`mcp-servers.md`): Same pattern — define mcp_servers, validate, preview. Output is local assembly only.
- **Profiles** (`profiles.md`): Create harness with profiles, run `ynd preview --profile <name>` — verify merged output. Fully local.
- **Focus** (`focus.md`): Create harness with focus entries, run `ynd preview --focus <name>` — verify prompt + profile. Fully local.
- **Project-local config** (`project-local-config.md`): Create a `.harness.json` in /tmp, run `ynd preview` from that directory. No network.
- **Include editing** (`include-editing.md`): Use a local-path include (not a git URL) with `ynh include add <dir> ./local-path` — the add/remove/update commands work on the manifest directly without network when the harness is path-referenced (not installed). Skip the installed-harness pre-fetch steps which require network.
- **Namespacing and migration** (`namespacing-and-migration.md`): Create harnesses with `.harness.json` format, run `ynd validate` and `ynh install` from /tmp — migration is fully local.

Only skip a step if it literally shells out to `git clone`, launches `claude`/`codex`/`cursor`, or runs Docker. "This tutorial is about git/network/vendor" is NOT sufficient reason to skip the whole tutorial — skip only the specific steps that require those things.

## Pass/Fail Criteria

A step **FAILS** if:
- A command produces different output than documented (wrong text, missing lines, extra lines)
- A file path in the output doesn't match what the tutorial shows
- An error message differs from what's documented
- A JSON/TOML structure or field order differs from what's documented
- A file that should exist is missing, or an unexpected file appears in a listing

A step **PASSES** if:
- Output matches the tutorial exactly (whitespace-normalized)
- OR the tutorial uses placeholder values (e.g., `<you>`, `/tmp/...`) and the structure matches

## Report Format

For each tutorial, report PASS or FAIL. For failures, include:

- **File**: tutorial path
- **Step**: description
- **Expected**: what the tutorial says
- **Actual**: what was produced

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
