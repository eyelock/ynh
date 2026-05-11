---
name: evals
description: Run the ynh eval suite against all tutorials. Release gate — verdict must be PASS before any release. Use when CLI behaviour, internal logic, or tutorial content changes.
model: claude-haiku-4-5-20251001
tools: Bash, Read, Glob
---

Evaluate ALL tutorials. This is a release gate — the verdict must be PASS before any release.

## Process

1. Build and install the latest binaries: `make build && make install`
2. For EVERY tutorial in `docs/tutorial/` (01 through the highest-numbered tutorial, excluding README.md):
   - Set up an isolated environment: `export HOME=$(mktemp -d) && export YNH_HOME=""`
   - Use binaries at `/Users/david/.ynh/bin/ynh` and `/Users/david/.ynh/bin/ynd`
   - **ALL file creation and commands MUST run in `/tmp/`** — never in the repo directory. The repo has real `skills/`, `agents/`, `rules/`, `commands/` directories; creating test files there pollutes the working tree. Use `cd /tmp` or absolute `/tmp/...` paths for all tutorial commands.
   - **Use only the Bash tool** for creating files outside the repo. Do NOT use Write/Edit tools for `/tmp/` files (they trigger permission prompts).
   - Execute each step that produces verifiable output
   - Compare actual output against the expected output documented in the tutorial
   - Skip steps that require: network access (git clone from GitHub), vendor CLIs (claude, codex, cursor), or Docker
3. Run the manual test plan (`docs/tutorial/manual-test-plan.md`) error-case section (all E-numbered cases)

## What is locally testable (do NOT skip these)

Many tutorials do not require network access or vendor CLIs and must be run:

- **Hooks (T10)**: Create a harness with hooks defined in plugin.json, run `ynd validate`, `ynd preview -v claude -o /tmp/out` — verify hook config appears in output. No vendor CLI needed.
- **MCP servers (T11)**: Same pattern — define mcp_servers, validate, preview. Output is local assembly only.
- **Profiles (T13)**: Create harness with profiles, run `ynd preview --profile <name>` — verify merged output. Fully local.
- **Focus (T14)**: Create harness with focus entries, run `ynd preview --focus <name>` — verify prompt + profile. Fully local.
- **Project-local config (T15)**: Create a `.harness.json` in /tmp, run `ynd preview` from that directory. No network.
- **Include editing (T17)**: Use a local-path include (not a git URL) with `ynh include add <dir> ./local-path` — the add/remove/update commands work on the manifest directly without network when the harness is path-referenced (not installed). Skip the installed-harness pre-fetch steps which require network.
- **Namespacing and migration (T18)**: Create harnesses with `.harness.json` format, run `ynd validate` and `ynh install` from /tmp — migration is fully local.

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
