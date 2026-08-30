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

## Decisions

B3 is decided and recorded below. C5 is still David's — do not build it unasked.

**B3 — guard composition. DECIDED: option (c), merge at authoring time.**

`ynh include sync` copies an included harness's hook, sensor and MCP
declarations into the **root** manifest as clearly marked generated blocks,
with drift detection in `ynh doctor`. Nothing is resolved from includes at run
time.

*Why (c) over (b).* It keeps the property that makes the design auditable: the
answer to "what governs this repository" stays one committed file a reviewer
can read. Under (b) that answer requires resolving remote includes at runtime,
which is exactly the audit-evidence problem. And (c) does not convert inert
included content into an execution surface — under (b) an include gains command
execution every turn. The honest cost is that every repo's `plugin.json` grows
and needs re-syncing on upgrade; that cost is visible and diffable, which is
the right kind.

*Correcting this file's earlier framing.* An earlier reconstruction of this
roadmap described (b) as "additive to the schema" and framed the change as
reversing a documented refusal. Both are wrong.
`internal/resolver/resolver.go:91` — `resolveWith` iterates `p.Includes` once,
flat, with no recursion, and returns file paths (`BasePath` + `Pick`). **It
never opens the included harness's `plugin.json`.** So "parent" and "root" are
the same thing, includes do not nest, and nothing is being *dropped* — it is
never read. (b) is therefore not a schema addition but new resolver machinery:
loading included manifests, merge semantics, conflict resolution, precedence,
and `ynh info` surfacing. (c) is a generator plus a drift check.

*Usability is an acceptance criterion, not a nicety:*
- Generated blocks delimited and labelled with their source and ref
- `ynh doctor` reporting drift in plain language — "guard-common has moved on
  since this was synced"
- `ynh include sync --check` exiting non-zero for CI

If a reader cannot tell at a glance which declarations are theirs and which are
generated, the option has failed.

**C5 — is assembling the evidence bundle ynh's job?** Doctrine says ynh returns
raw signal and pass/fail policy lives above it. A set difference between two
check reports is arguably arithmetic rather than policy, but the boundary is
real. Raise early even though the work is later: shadow mode's review-time arm
depends on it.

## Outside this plan, still owed

- **`.claude/rules/branching.md` additions:** merging a stacked parent with
  `--delete-branch` irreversibly closes the child PR; always assert
  `git rev-list --count origin/develop..HEAD` after a `rebase --onto`.
