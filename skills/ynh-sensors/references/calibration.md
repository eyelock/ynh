# Calibration, ratchets and freshness

The three things that decide whether a gate is real. Each exists because the
obvious version fails silently.

## `reference` — proving a sensor still observes

A sensor is a command plus an expectation about its exit code. Nothing else in
the system checks that it still detects what it claims.

If the command quietly stops examining anything — a config change excluding a
directory, a linter upgrade renaming a rule, a path that no longer matches after
a move — it exits 0. The gate reports green. Every control downstream stays
green with it, and the repository looks healthy while nothing is being observed
at all.

```json
"lint": {
  "category": "maintainability",
  "tolerance": "blocking",
  "version_command": "golangci-lint --version",
  "source": { "command": "golangci-lint run" },
  "reference": { "path": "testdata/sensor-fixtures/lint-fail", "expect": "fail" },
  "output": { "format": "text" }
}
```

```bash
ynh check <harness> --calibrate
```

That runs each sensor against its fixture instead of the real tree, and reports
how many are calibrated, failed, errored, or uncalibrated.

### Building a fixture

The fixture is the smallest input that *must* trip the sensor.

```
testdata/sensor-fixtures/
└── lint-fail/
    └── bad.go        # one file with one obvious violation
```

For a linter, one violation of a rule you care about. For a test sensor, one
failing test. For a formatter, one badly formatted file.

**`expect: fail` is the case that matters.** A sensor that passes a fixture
designed to trip it has stopped observing. `expect: pass` catches the opposite
failure — a sensor firing on clean input — and is worth adding where false
positives would erode trust.

### Two rules for where it lives

1. **Outside the agent's write path.** A reference an agent can edit calibrates
   nothing: a run that cannot satisfy the gate can satisfy the fixture instead.
2. **Excluded from the real sensor's scope.** A deliberately broken file inside
   `internal/` makes the real `lint` sensor fail forever. Put fixtures under
   `testdata/` and confirm the sensor's own command does not walk it.

That second rule bites in Go, where `go build ./...` skips `testdata/` but
`grep -rn` does not.

## `ratchet` — adopting a repo that already fails

Nobody fixes 400 findings before they are allowed a gate. The baseline accepts
what is there so only new failures gate:

```bash
ynh check <harness> --update-baseline
```

Which ratchet depends on what the finding *is*.

### `fingerprint` (default)

Forgives *these specific* findings. A new one gates even if the total went down.

Right for lint, tests, type errors — anything where each finding is a distinct
thing with an identity worth tracking.

The failure mode: findings whose identity changes on every refactor. If the
fingerprint includes a line number, moving a function re-reports everything.

### `count`

Forgives *the number*. The gate fails when the count goes up, whatever the
identities.

Right when the count is genuinely the finding and the individual instances
churn:

- TODO / FIXME counts
- `//nolint`, `# type: ignore`, `@SuppressWarnings` suppressions
- `any` in TypeScript, `unsafe` in Rust
- files over some size

The failure mode: it will not notice a swap. Delete one legitimate suppression,
add one bad one, and the count is unchanged and the gate stays green. Do not use
`count` where each instance genuinely matters.

## Freshness — is a `files` artifact still true?

A `files` sensor reads an artifact some other process left behind. ynh does not
judge what the artifact says. It does judge whether it is still entitled to be
believed, because that is decidable.

| State | Meaning | Gate |
|---|---|---|
| `fresh` | describes the tree as it stands | passes, `status: reported` |
| `stale` | an observed input changed after it was written | **fails** |
| `absent` | a declared file is not there | **fails** |
| `unknown` | ynh could not tell | **fails** |

Before this existed, a files sensor pointing at a file that did not exist
reported green — a declared blocking sensor observing nothing, in a gate that
passed.

### `observes`

```json
"observes": ["services/**", "tests/e2e/**"]
```

Patterns are expanded by ynh, **not** `filepath.Glob` — Go's matcher treats `**`
as an ordinary `*` and never descends, so a pattern that looks recursive would
observe the wrong set.

| Pattern | Observes |
|---|---|
| `services` | everything under `services/`, any depth |
| `services/*` | the same — a directory match means its whole subtree |
| `services/**` | the same |
| `services/**/*.go` | every `.go` under `services/`, any depth |
| `services/*.go` | only `.go` directly in `services/` |

**Omitting `observes` means the whole git-tracked tree**, so any commit stales
the sensor. That is deliberate — a harness that will not say what its artifact
depends on gets the only honest assumption — but it is noisy, and the cure is
one line.

### What counts as an input

Git-tracked files, minus four exclusions that would otherwise make the check
invalidate itself:

| Excluded | Why |
|---|---|
| the sensor's own `files` paths | producing the artifact would immediately stale it |
| `.ynh/` | recording a baseline would stale every artifact in the repo |
| untracked and ignored files | `make build` writing to `bin/` would stale everything |
| `.git/` | implied by taking the tree from git |

No `observes` **and** not a git repository means `unknown`, which fails.

### What the answer is worth

Comparison is on modification times, recorded as `"freshness_basis": "mtime"`.

Reliable on a working checkout. **Unreliable wherever timestamps are rewritten
wholesale** — fresh clones, `git worktree add`, container builds, CI caches. A
consumer grading results across machines should read `freshness_basis` and weigh
it accordingly, rather than treating every `stale` as a real change.

This is the one place where a green result deserves suspicion in CI: a container
build can rewrite every timestamp, and everything looks fresh.

## `version_command`

```json
"version_command": "golangci-lint --version"
```

Without it, a change in findings and a change in the tool are the same
observation. A corpus graded over weeks cannot defend a yield number.

Declare it rather than letting anything infer it. Guessing `<first token>
--version` is wrong for a wrapper — `make lint` reports make's version, not the
linter's — and hangs outright on commands that do not support the flag.

## Putting it together

A blocking sensor worth trusting has all four:

```json
"lint": {
  "category": "maintainability",
  "tolerance": "blocking",
  "ratchet": "fingerprint",
  "version_command": "golangci-lint --version",
  "source": { "command": "golangci-lint run" },
  "reference": { "path": "testdata/sensor-fixtures/lint-fail", "expect": "fail" },
  "output": { "format": "text" }
}
```

- `tolerance` says what a failure does
- `ratchet` says what the baseline forgives
- `version_command` says which tool produced the answer
- `reference` says the sensor still works

Check the last one is really there:

```bash
ynh check <harness> --calibrate
```

`uncalibrated` on a blocking sensor is a gate nobody has proved bites.
