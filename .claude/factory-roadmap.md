# Factory Roadmap — remaining work

Reconstructed 2026-08-29 after `.claude/plans/2026-08-28-harness-verification-loop.md`
was lost (it was gitignored and lived only on one disk). **This file is tracked
on purpose.** Scratch plans can stay in `.claude/plans/`; anything that would
hurt to lose belongs here.

Completed items are not repeated — they are in the merged PRs below.

## Landed on `develop` — 2026-08-29

| PR | Item | What |
|---|---|---|
| #209 | — | CI on every PR, including stacked ones |
| #207 | — | `ynh check`, `tolerance`, exit 0/1/2, one schema tree |
| #216 | — | Baseline ratchet; six baseline bugs; tutorial 20 |
| #210 | — | Five agent-loop bugs |
| #211 | — | `docs/agent.md`, fifteen broken links |
| #212 | A2, B2, B6 | `--sandbox` errors not ignores; MCP `env_passthrough`; containment doctrine |
| #213 | A1, B5a | Budget defaults; baseline not agent-writable; trajectory provenance |
| #214 | **B1** | Loop consumes `ynh check --format json`; `internal/gate`; exit 22 |
| #215 | **B4** | Per-sensor baselines; `ExitTamper` made real; path traversal fixed |

## Tranche 1 — blocks shadow mode

### C1. `ynh agent run` has no machine-readable result
Every other command takes `--format json`. `agent run` takes only `--emit-jsonl`,
which is an event *stream*, not a result. A CI job must request the stream, tail
it, find the last `session_end`, and reconstruct the rest by inference.

**Acceptance:** one object at the end of the run carrying exit code and reason;
turns / tokens / wall consumed **and which cap bound**; the convergence sensor
and its final result; harness name with resolved ref and SHA; worktree path; the
set of changed files. Most of it already exists inside the loop —
`SessionEndData` carries three fields and `BudgetSource` already records whether
each cap came from flag, manifest or default.

### C2. Sensor results carry no tool version
A corpus graded across weeks cannot defend a yield number if the linter changed
underneath it.

**Acceptance:** each sensor result records the version of the tool that produced
it, on the trajectory and in `ynh check --format json`.

### C4 (image-digest half). Run provenance stops one field short
#213 landed model, ynh version, harness version and base commit. Still missing:
**harness SHA** and **image digest**. Without them a run cannot be pinned to one
toolchain and is not reproducible.

## Tranche 2 — blocks a production factory, not the experiment

Deliberately deferred. If shadow mode misses its yield gate, most of this is
work you are glad you did not do.

- **C3** — worker env allowlist and trajectory redaction. Needed the moment a
  run touches real credentials. `StartOptions.Env` exists but the worker still
  inherits the parent environment wholesale.
- **C5** — evidence bundle. **Decision required first** (below).
- **B7** — harness images: labels lack the harness SHA, and the entrypoint is
  `ynh run`.
- **B5b** — suppression-delta sensor. The agreed replacement for the withdrawn
  content-anchor idea: the gaming vector for a ratchet is *suppression*, not
  relocation. Note it does not cover a deleted test.

## Tranche 3 — reviewability

- **C6** — `ynh baseline` read surface. The ratchet has no read surface at all
  and stores hashes rather than findings, so forgiveness cannot be audited from
  the file.

## Decisions — David's, not the implementer's. Do not build unasked.

**B3 — guard composition.** Should an *included* harness contribute hooks,
sensors and MCP to its parent? Today they are dropped at assembly by design
(`docs/sensors.md:180`, `docs/hooks.md:257`) because silent injection of
executable config through composition is the poisoned-template attack. Options:
(a) keep the rule, duplicate guards per root harness with a drift check;
(b) explicit per-capability `trust:` opt-in named in the root manifest.
(b) fits the factory and is additive to the schema, but **reverses a documented
refusal** and needs `docs/sensors.md` and `docs/harness-engineering.md` updated
as a stance change rather than a feature.

**C5 — is assembling the evidence bundle ynh's job?** Doctrine says ynh returns
raw signal and pass/fail policy lives above it. A set difference between two
check reports is arguably arithmetic rather than policy, but the boundary is
real. Raise early even though the work is later: shadow mode's review-time arm
depends on it.

## Outside this plan, still owed

- **~24 `TermQ` references** in shipped source and tests, against the standing
  rule that ynh artifacts never name it. Its own sweep — fixing two of twenty
  inside an unrelated PR is worse than none.
- **`fix/marketplace-clean-guard`** — `--clean` called `os.RemoveAll` on the `-o`
  path with no guard. `cmd/ynd/clean.go` written; tests must follow
  `.claude/rules/destructive-operations.md` before anything runs.
- **`.claude/rules/branching.md` additions:** merging a stacked parent with
  `--delete-branch` irreversibly closes the child PR; always assert
  `git rev-list --count origin/develop..HEAD` after a `rebase --onto`.
