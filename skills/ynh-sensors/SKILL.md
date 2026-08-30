---
name: ynh-sensors
description: Choose, declare and calibrate the sensors a project needs, then gate on them with ynh check. Use when a harness has no sensors, when ynh check reports nothing useful, or when setting up an agent loop that needs something to converge against.
---

# Set up sensors for a project

You are helping someone decide **what their project should observe**, declare it
in their harness, and make `ynh check` a gate they trust.

Work through this one step at a time. Ask, wait for the answer, then move on —
do not dump the whole plan at once.

## Before you start

- `references/starter-sets.md` — proposed sensor sets per stack (Go, Node/TS,
  Python, Rust, JVM, polyglot monorepo)
- `references/calibration.md` — reference fixtures, ratchets, freshness and
  `version_command`, with the reasoning for each

Both ship with this skill. Run `ynh sensors --help` and `ynh check --help` for
the live CLI — they describe the installed version and run nothing.

## The vocabulary is a closed set

Get these wrong and `ynd validate` rejects the manifest, so establish them
first rather than discovering them:

| Field | Values | Meaning |
|---|---|---|
| `category` | `maintainability` · `architecture` · `behaviour` | What kind of health this observes |
| `tolerance` | `blocking` (default) · `advisory` · `report` | What a failure does |
| `source` | exactly one of `files` · `command` · `focus` | How the observation is made |
| `role` | `regular` · `convergence-verifier` · `stuck-recovery` | How a loop driver finds it |
| `ratchet` | `fingerprint` (default) · `count` | How the baseline forgives |

## Step 1 — What is already there?

```bash
ynh sensors ls <harness>     # nothing? then this is a green field
ynh check <harness>          # already declared? see what it says today
```

**`<harness>` is an installed id, not a path.** `ynh check ./my-harness` fails,
including the `./<path>` form its own error message suggests. Install first, then
check by id, using `--cwd` to point at the tree being observed:

```bash
ynh install .
ynh check local/<name> --cwd .
```

`ynh sensors ls` and `ynd validate` both accept a path, so use those while
iterating on the declaration and `ynh check` once it is installed.

Also look at what the project *already* runs — a Makefile, `package.json`
scripts, the CI workflow. **The best first sensors are commands the team
already trusts**, not new ones you invent. If CI runs `make lint`, that is a
sensor; it just has not been declared.

Ask: *what do you already run before you'd call a change ready?*

## Step 2 — Propose a set across all three categories

This is where most people stop too early. Left alone, they declare three
`maintainability` sensors — format, lint, build — and call it done.

Prompt deliberately for the other two:

- **`maintainability`** — formatting, linting, build. Easy, fast, always first.
- **`behaviour`** — tests, coverage, e2e results. *Does it still do what it did?*
- **`architecture`** — dependency direction, layering, public API surface,
  bundle size. **Nobody asks for these**, and they are the ones that catch slow
  decay. Ask directly: "is there a boundary in this codebase you'd be unhappy to
  see crossed?" Then make that a sensor.

`references/starter-sets.md` has concrete sets per stack. Propose, do not
impose — a sensor the team does not believe in gets switched off.

## Step 3 — Place each on the tolerance ladder

```
report  →  advisory  →  blocking
```

**Start new sensors at `advisory`.** A sensor that blocks on day one, in a repo
that has never run it, blocks everything and gets removed. Advisory means it
reports in full and does not gate — you learn what it says before it has power.

Graduate to `blocking` once the repo is clean for it. Use `report` for pure
observation a loop driver reads but nobody gates on.

Note the default is `blocking` when omitted, so say `"tolerance": "advisory"`
explicitly for anything new.

## Step 4 — Adopt into a repo that is already failing

Real projects are not clean. Do not ask anyone to fix 400 lint findings before
they can have a gate.

```bash
ynh check <harness> --update-baseline
```

That accepts today's failures so **only new ones gate**. The repo stops getting
worse immediately, which is the whole point, and the existing debt is paid down
on whatever schedule they choose.

Pick the ratchet per sensor:

- **`fingerprint`** (default) — forgives *these specific* findings. A new
  finding gates even if the total went down. Right for lint and tests.
- **`count`** — forgives *the number*. Right when the count is the finding and
  the identities churn: TODO counts, `//nolint` suppressions, `any` usages.

`references/calibration.md` covers when each is wrong.

## Step 5 — For a `files` sensor, declare `observes`

A `files` sensor reads an artifact some other process left behind. ynh does not
read what the artifact *says* — but it does check whether it is still entitled
to be believed.

| State | Meaning | Gate |
|---|---|---|
| `fresh` | describes the tree as it stands | passes (`status: reported`) |
| `stale` | an observed input changed after it was written | **fails** |
| `absent` | a declared file is not there | **fails** |
| `unknown` | ynh could not tell | **fails** |

`unknown` failing is deliberate: "no evidence either way" is not "nothing was
wrong", and a gate that cannot see is not a gate that passed.

```json
"e2e-status": {
  "category": "behaviour",
  "source":   { "files": ["tests/e2e/last-run.json"] },
  "observes": ["services/**", "tests/e2e/**"],
  "output":   { "format": "json" }
}
```

**Always declare `observes`.** Omitting it means the whole git-tracked tree
counts, so *any* commit stales the sensor. That is strict on purpose — the noise
is what pushes you to declare the real inputs — but it is one line to fix, so
fix it.

Two things to tell the user:

- Patterns are expanded by ynh, not `filepath.Glob`. A pattern resolving to a
  directory means its whole subtree, so `services`, `services/*` and
  `services/**` all agree.
- Freshness is decided on modification times (`"freshness_basis": "mtime"`),
  which is reliable on a working checkout and unreliable where timestamps are
  rewritten wholesale — fresh clones, `git worktree add`, container builds, CI
  caches.

## Step 6 — Calibrate every blocking sensor

**This is the step that separates a gate from decoration, and the one people
skip.**

A sensor is a command plus an expectation about its exit code. If the command
quietly stops examining anything — a config change excluding a directory, an
upgrade renaming a rule, a path that no longer matches — it exits 0 and the gate
reports green forever. Nothing else in the system notices.

```json
"reference": { "path": "testdata/sensor-fixtures/lint-fail", "expect": "fail" }
```

```bash
ynh check <harness> --calibrate
```

Every blocking sensor should have one, with `expect: fail` — a fixture designed
to trip it. A sensor that passes that fixture has stopped observing.

The fixture must live **outside the agent's write path**. A reference an agent
can edit calibrates nothing.

Offer to generate the fixtures. `references/calibration.md` has the pattern.

Add `version_command` at the same time, so a change in findings and a change in
the tool are distinguishable later:

```json
"version_command": "golangci-lint --version"
```

Declare it rather than letting anything guess: `make lint` would report make's
version, not the linter's.

## Step 7 — If this feeds an agent loop

`ynh agent run` iterates against these sensors between turns. It applies no
policy of its own — it runs `ynh check --format json` and takes the verdict.

Mark the sensor that decides "done":

```json
"role": "convergence-verifier"
```

A `files` sensor **cannot** be one, and `ynd validate` rejects it: no verdict is
derivable from a file glob, and the path sits inside the agent's own write path,
so the run could manufacture its own convergence.

## Finish

```bash
ynd validate <harness>      # the enums are checked here
ynh sensors ls <harness>
ynh check <harness>         # 0 pass · 1 blocking failure · 2 could not run
```

Leave them with the exit codes, because that is the CI contract.
