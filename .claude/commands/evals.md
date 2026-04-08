Evaluate ALL tutorials. This is a release gate — the verdict must be PASS before any release.

## Process

1. Build and install the latest binaries: `make build && make install`
2. For EVERY tutorial in `docs/tutorial/` (01 through 13, excluding README.md):
   - Set up an isolated environment: `export HOME=$(mktemp -d) && export YNH_HOME=""`
   - Use binaries at `/Users/david/.ynh/bin/ynh` and `/Users/david/.ynh/bin/ynd`
   - Execute each step that produces verifiable output
   - Compare actual output against the expected output documented in the tutorial
   - Skip steps that require network access (git clone from GitHub), vendor CLIs (claude, codex), or Docker
3. Run the manual test plan (`docs/tutorial/manual-test-plan.md`) error-case section (E1–E17)

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
