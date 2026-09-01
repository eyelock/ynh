# Sensors

Sensors are named **observation surfaces** declared in a harness manifest. A loop driver — CI, an orchestrator, a custom tool — runs them between agent turns and feeds the results back into the next turn. ynh declares; the loop driver runs.

A sensor is not a new artifact type. There is no `sensors/` directory and no `SENSOR.md` file. A sensor is a role declaration layered onto the primitives ynh already has (focus, hooks, files on disk). The schema addition gives loop drivers a discoverable contract — *what* observation is available, *where* its result lives, and *what shape* it's in — without ynh taking on any runtime loop responsibility.

## Why declare them

A coding agent that runs in a loop needs feedback between iterations: did the build pass, are the tests still green, did anything new get flagged by the linter, does an LLM judge think the change is adequate. Each of those signals already exists somewhere — as a CLI command, a file on disk, an LLM prompt. Sensors give that surface a name, a format, and a CLI handle so any orchestrator can consume it the same way.

Without sensors, a loop driver has to be hand-coded against a specific harness: "for this project run `make check`, for that one run `npm test`". With sensors, the harness declares its observation contract and loop drivers consume it generically.

## Schema

Sensors live under the top-level `sensors` key in `.ynh-plugin/plugin.json`. Each sensor name maps to a declaration:

```json
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "my-harness",
  "version": "0.1.0",
  "focuses": {
    "infer-vulns": { "prompt": "Identify high-severity vulnerabilities in the changed code" }
  },
  "sensors": {
    "build": {
      "category": "maintainability",
      "source": { "command": "make check" },
      "output": { "format": "text" }
    },
    "tests": {
      "category": "behaviour",
      "source": { "files": ["test-reports/**/*.xml"] },
      "output": { "format": "junit-xml" }
    },
    "security-scan": {
      "category": "behaviour",
      "source": { "focus": "infer-vulns" },
      "output": { "format": "markdown" }
    },
    "coverage-judge": {
      "role": "convergence-verifier",
      "source": {
        "focus": {
          "profile": "ci",
          "prompt": "Assess if test coverage is adequate for the changed surface area"
        }
      },
      "output": { "format": "markdown" }
    }
  }
}
```

### Field reference

| Field | Type | Required | Description |
|---|---|---|---|
| `category` | enum | No | Fowler bucket: `maintainability`, `architecture`, `behaviour`. Free metadata for loop-driver triage. |
| `role` | enum | No | Role hint: `regular` (default), `convergence-verifier`, `stuck-recovery`. Pure metadata — ynh does not enforce semantics. Loop drivers filter sensors by role to discover which one is the loop's done-check or the recovery sensor. |
| `tolerance` | enum | No | How `ynh check` treats a failure: `blocking` (default), `advisory`, `report`. See [Tolerance](#tolerance). |
| `observes` | string[] | No | For `files` sensors: the paths the artifact depends on. Empty means the whole tracked tree. See [Freshness](#freshness-is-this-artifact-still-true). |
| `source` | object | **Yes** | Strict one-of: `files` \| `command` \| `focus`. Discriminates the sensor type. |
| `output` | object | **Yes** | Where the sensor's result lives and what shape it's in. |

## Source variants

Exactly one of `files`, `command`, or `focus` must be set. The set field discriminates the sensor type — there is no separate `kind` field.

### `files`

A glob/path list of pre-existing artifacts to read (test reports, coverage files, SARIF dumps). Pure pass-through — ynh does not resolve, expand, or verify globs at validate time; it does so at `ynh sensors run` time.

```json
"coverage": {
  "source": { "files": ["coverage/lcov.info", "coverage/*.json"] },
  "output": { "format": "lcov-summary" }
}
```

Use this when something else (a hook, a CI step, a human) already produces the artifact and the sensor just needs to surface it.

Because the artifact is produced elsewhere, ynh checks that it still describes the tree before believing it — see [Freshness](#freshness-is-this-artifact-still-true) directly below. This is the one thing about a `files` sensor that can fail the gate.

## Freshness — is this artifact still true?

A `files` sensor reads a result some other process left behind. That creates a
problem no other sensor kind has: **the artifact can be right about a tree that
no longer exists.**

ynh draws a hard line here, and it is worth stating plainly because it is the
part people find confusing at first:

- **What the artifact *says* is never ynh's business.** No verdict is derivable
  from arbitrary JSON. If your e2e report says three tests failed, ynh does not
  know that and does not care — that is the loop driver's job.
- **Whether the artifact is *entitled to be believed* is entirely ynh's
  business.** That is decidable, and before this existed it was nobody's job:
  a sensor pointed at a file that did not exist reported green.

So a `files` sensor still never gates on content. It now gates on freshness.

### The four states

| State | Meaning | Gate |
|---|---|---|
| `fresh` | The artifact describes the tree as it stands. | passes |
| `stale` | Inputs changed after the artifact was written. It is a real observation of a tree that no longer exists. | **fails** |
| `absent` | A declared file is not there at all. | **fails** |
| `unknown` | ynh could not tell. | **fails** |

`unknown` failing is the one that surprises people. It is deliberate: "no
evidence either way" is not the same as "nothing was wrong", and a gate that
cannot see is not a gate that passed.

### Freshness is about change, not time

A coverage report from three weeks ago, against code nobody has touched, is
perfectly valid. One from five minutes ago, against code edited two minutes
ago, is worthless. Elapsed time measures the wrong thing, so ynh does not use
it. An artifact goes stale when its **inputs move**, and never otherwise.

(Sensors that observe something *outside* the repository — a live service, a
remote queue — genuinely do decay with time. Nothing here helps them. That is a
separate axis and is not modelled yet.)

### `observes` — what the artifact depends on

Declare the paths the artifact actually depends on:

```json
"e2e-status": {
  "category": "behaviour",
  "source":   { "files": ["tests/e2e/last-run.json"] },
  "observes": ["services/**", "tests/e2e/**"],
  "output":   { "format": "json" }
}
```

Now editing `README.md` leaves the sensor fresh, and editing
`services/gateway/auth.go` makes it stale.

**Patterns are not `filepath.Glob`.** Go's matcher treats `**` as an ordinary
`*` — it matches inside one path element and never descends — so a pattern that
looks recursive would quietly observe the wrong set. ynh expands them instead:

| Pattern | Observes |
|---|---|
| `services` | every file under `services/`, at any depth |
| `services/*` | the same — a directory match means its whole subtree |
| `services/**` | the same |
| `services/**/*.go` | every `.go` file under `services/`, at any depth |
| `services/*.go` | only `.go` files directly in `services/` |

A pattern resolving to a directory means that directory's whole subtree, so the
three spellings above agree rather than two of them observing nothing.

**If you omit `observes`, the whole tracked tree counts.** That is strict on
purpose: a harness that will not say what its artifact depends on gets the only
honest assumption, which is everything. The consequence is that *any* commit
stales *every* files sensor — which is noisy, and meant to be. The cure is
one line of configuration, and the noise is what pushes you to write it.

### What counts as an input

The tree is **git-tracked files**. Four things are always excluded, because
including them would make the check invalidate itself:

| Excluded | Why |
|---|---|
| The sensor's own `files` paths | Producing the artifact would immediately stale it. |
| `.ynh/` | The gate's own state. Recording a baseline would stale every artifact in the repo. |
| Untracked and ignored files | Otherwise `make build` writing to `bin/` stales everything. |
| `.git/` | Implied by taking the tree from git. |

If there is no `observes` **and** the directory is not a git repository, ynh has
no way to know the input set. That is `unknown`, and it fails.

### How the comparison is made

Today ynh compares modification times: the artifact is stale if any observed
input is newer than the oldest file the sensor declares. The result records this
as `"freshness_basis": "mtime"`.

Be aware of what that basis is worth. Timestamps are reliable on a working
checkout and unreliable anywhere they are rewritten wholesale — fresh clones,
`git worktree add`, container builds, CI caches. A consumer grading results
across machines should read `freshness_basis` and weigh the answer accordingly.

### Reading it

```console
$ ynh check local/collective-dev --only e2e-status
  ✗  e2e-status  absent  0ms

e2e-status:
no file matched the sensor's declared paths

blocked: 1 of 1 sensor failed
```

In JSON, each files sensor carries `freshness` and `freshness_basis`:

```json
{
  "name": "e2e-status",
  "kind": "files",
  "tolerance": "blocking",
  "status": "fail",
  "freshness": "stale",
  "freshness_basis": "mtime",
  "note": "auth.go changed after last-run.json was written"
}
```

### Migrating an existing harness

Nothing is required, and nothing breaks quietly:

- A files sensor whose artifact exists and predates no input keeps passing.
- One pointed at a missing file starts failing — that is the bug being fixed,
  and it was reporting green before.
- One in a non-git directory with no `observes` starts reporting `unknown` and
  failing. Add `observes` to fix it.

If a sensor should genuinely never gate, set `tolerance: advisory` or
`tolerance: report`. Freshness respects tolerance exactly like a command
sensor's exit code does.

### `command`

A shell command. The loop driver runs it with the cwd of its choosing and captures stdout, stderr, and exit code.

```json
"build": {
  "source": { "command": "make check" },
  "output": { "format": "text", "channel": "stdout+exit" }
}
```

#### The working directory is the tree under test

**The cwd is the repository your sensor is being asked to measure.** It is not
the harness, and it is not wherever the script happens to live. A sensor that
works out its own root instead of using the cwd measures the wrong tree, and
does so silently:

```sh
# Wrong in a sensor. Analyses the repository the *script* lives in,
# whatever tree ynh points it at.
REPO_ROOT="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"
```

This matters most when adopting a check that already exists. `$0`-relative
rooting is the *more* robust choice for CI, because it works no matter where
`make` is invoked, so it is the ordinary way these scripts get written. The
bug appears only once the script becomes a sensor, which is why a check being
adopted needs this warning and a sensor written from scratch usually does not.

The failure is silent because the script still succeeds. It reports on its own
repository, passes, and never touches the tree it was handed.

**Proving a sensor honours the cwd takes one command.** Point it at an empty
directory and watch what it says:

```sh
mkdir /tmp/empty && git -C /tmp/empty init -q
ynh check <harness> --only <sensor> --cwd /tmp/empty
```

A sensor that reads the cwd has nothing to measure and says so. A sensor that
found its own root reports a healthy result about a repository nobody asked
about, which is the tell.

Anything the sensor resolves at run time resolves against that tree, including
a command that starts with `$(git rev-parse --show-toplevel)`. That form looks
like it pins the script to your harness and does the opposite: it runs the copy
belonging to whichever tree is under test, and fails with exit 127 on any
commit that predates the script.

**To run a script the harness ships, use `$YNH_HARNESS_DIR`.** It is set for
every `command` sensor and every `version_command`, and holds the harness's own
directory:

```json
"source": { "command": "\"$YNH_HARNESS_DIR/tools/lint.sh\"" }
```

The working directory is unchanged: the script still runs in, and measures, the
tree under test. Only the path to the script is pinned. That is what makes
historical replay work, where today's sensor is run against an older tree that
never contained it.

This mirrors `reference.path`, which has always resolved against the harness.
Commands simply had no equivalent until now, which is why the toplevel form
above looked like the only option.

Quote it. A harness path can contain spaces, and an unquoted expansion would
split on them.

**Calibration checks that this actually holds.** `--calibrate` runs a sensor in
its reference fixture, while `ynh check` runs it in the tree being measured. A
command that resolves relative to the working directory therefore proves
something about the fixture's copy and nothing about the copy the gate will
run. Calibration reports that rather than a tick:

```
  ~ lint            calibrated, but not against what check will run

1 sensor(s) were calibrated against a different program than `ynh check` will run.
```

It is reported, not refused. A sensor invoking the project's own `make lint`
wants exactly that resolution, and forbidding the shape would break it. What
was wrong was the silence: both the calibration and the gate reported green
while running different programs.

`--format json` carries `portable_command` per sensor and `summary.non_portable`,
so a loop driver can act on it.

Use this for build/lint/test/typecheck — anything where running a command IS the observation. Same script can be hooked at `after_tool` for in-session enforcement *and* declared as a sensor for between-turn observation; the two are not redundant.

### `github_status` and `github_check`

Two sensors that observe a verdict reached somewhere else. A `command` sensor
can run your linter; it cannot know what a scanner running on infrastructure
you do not control concluded about this commit. That is the whole reason these
exist, and it is also the boundary: **if a local script can answer the
question, use `command`.** A CVE audit you can run yourself is a command
sensor, not a GitHub one.

They are separate sensor types because the two GitHub APIs disagree about
vocabulary. A commit status has a `state`; a check run has a `status` and a
separate `conclusion`. Collapsing them into one declaration would mean guessing
which the author meant.

```json
"snyk": {
  "category": "behaviour",
  "tolerance": "blocking",
  "source": { "github_status": { "context": "security/snyk", "require": "success" } },
  "output": { "format": "text", "channel": "stdout+exit" }
},
"codeql": {
  "category": "behaviour",
  "tolerance": "blocking",
  "source": { "github_check": { "name": "CodeQL", "app": "github", "require": "success" } },
  "output": { "format": "text", "channel": "stdout+exit" }
}
```

| Field | `github_status` | `github_check` |
|---|---|---|
| selector | `context`, required | `name`, required, plus optional `app` slug |
| `require` | `success` (default), `pending`, `failure`, `error` | a check conclusion: `success` (default), `failure`, `neutral`, `cancelled`, `timed_out`, `action_required`, `stale`, `skipped` |
| `repo` | `owner/name`. Default: inferred from the directory under test | same |
| `ref` | commit to ask about. Default `HEAD`, resolved in the directory under test | same |
| `on_missing` | `broken` (default), `fail`, `pass` | same |

The selector is required on purpose. A sensor that observes "whatever statuses
happen to exist" reports on a set that changes underneath it, which is not an
observation.

`repo` and `ref` default to the directory under test, which is what makes these
work under `--cwd`: point the gate at an older tree and it asks about that
tree's commit, in that tree's repository.

#### Absent is not passing, and pending is not failing

Three outcomes, not two:

| Situation | Result |
|---|---|
| The required state or conclusion is present | pass |
| A different state or conclusion is present | fail, exit 1 |
| No such status or check run, or it has not concluded | **broken, exit 2** |

The third is the one that matters. A status can be renamed, an app can be
uninstalled, a token can be revoked, and in every case there is no verdict to
read. Passing there would mean a deleted scanner silently stops gating, and
failing there would mean the gate races the scanner it is reading. So the
default is `broken`: the gate says it could not see, which is the same rule
freshness applies to `unknown` and `ynh check` applies to a command that could
not run.

`on_missing` overrides it per sensor. Prefer `pass` only for a sensor that is
genuinely optional, and know what you are buying.

#### What these cost

Worth stating plainly, because both are real.

**They reach the network from inside a gate.** These sensors shell out to
`gh`, which must be on `PATH` and authenticated. A blocking GitHub sensor can
therefore fail because GitHub is slow or unreachable. That surfaces as exit 2
with a message naming the failure, not as a verdict about your code, but it is
still a gate that depends on someone else's uptime.

**They cannot be calibrated.** `reference` is rejected on these sensors, and
`ynd validate` says so. Calibration runs a sensor from its fixture directory,
so a GitHub sensor pointed at a fixture would ask about whichever repository
and commit that directory resolves to, usually the harness's own. That is not
a weaker proof; it is a confident answer to a different question. So the
guarantee available to a `command` sensor is not available here, and a GitHub
sensor should be read as an observation you trust rather than one you have
proven.

### `focus`

An agent-driven sensor. The string form references an existing top-level `focus` entry by name; the object form inlines a focus.

```json
"security-scan": {
  "source": { "focus": "infer-vulns" },
  "output": { "format": "markdown" }
}
```

Or inline:

```json
"coverage-judge": {
  "source": {
    "focus": {
      "profile": "ci",
      "prompt": "Assess if test coverage is adequate for the changed surface area"
    }
  },
  "output": { "format": "markdown" }
}
```

Inline focuses are scoped to the sensor that declares them — they do **not** appear in `ynh info` Focus list, and they are not selectable via `--focus` or `YNH_FOCUS`. Use a string reference when the same focus is invoked both standalone and as a sensor; use inline when the focus exists only to drive this sensor.

When the loop driver runs a focus-sourced sensor via `ynh sensors run`, ynh returns the resolved focus declaration; the loop driver invokes the agent runtime itself. ynh owns no agent-invocation surface.

## Output contract

```json
"output": {
  "format": "junit-xml",
  "channel": "files",
  "path": "test-reports/junit.xml"
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `format` | string | **Yes** | Freeform format identifier. Pass-through to the loop driver. |
| `channel` | string | No | Where the result emerges. Defaults are inferred from `source`. |
| `path` | string | No | Relevant when `channel=file`. |
| `match` | string | No | Regular expression selecting which lines are findings. |

### `match`: which lines are findings

**ynh treats every non-blank line a sensor prints as a finding, unless you say otherwise.**

That is fine for a sensor you write yourself, which prints one line per finding and nothing else. It is wrong for a sensor wrapping a real tool, because the tool's decoration gets recorded as accepted debt: headers, source context, caret markers and summary counts.

The consequence is worse than untidy. Take golangci-lint:

```
main.go:10:2: Error return value is not checked (errcheck)
util.go:20:2: Error return value is not checked (errcheck)
api.go:30:2: Error return value is not checked (errcheck)

3 issues:
* errcheck: 3
```

Fix one finding and the last two lines become `2 issues:` and `* errcheck: 2`. Those are strings the baseline has never seen, so **a correct repair is reported as two new findings and the gate turns red.** The tree got better and ynh said it got worse.

`match` fixes that by naming what a finding looks like:

```json
"output": {
  "format": "text",
  "match": "^[^ ]+\\.go:[0-9]+:[0-9]+:"
}
```

Now only those three lines are fingerprinted. Fixing one removes one, and nothing new appears.

**It applies to both ratchets.** The fingerprint ratchet and the count ratchet (`ratchet: "count"`) use the same filter, so a count-based sensor is not measuring a tool's summary either.

A few things worth knowing:

- **Omitting it changes nothing.** Every existing sensor behaves exactly as before, so this is safe to adopt one sensor at a time.
- **ynh does not interpret the pattern beyond matching lines with it.** It stays out of the business of knowing any tool's output format, which is the same reason `format` is freeform.
- **A pattern that does not compile is a validation error**, not a silent no-op. Silently selecting nothing would record the sensor's whole output as accepted debt on the next `--update-baseline`.
- **If a failing sensor's `match` selects none of its output**, `ynh check` says so in the sensor's note rather than recording an empty baseline. That means the pattern is wrong, or the tool changed its output.

`format` does **not** do this job. It is a label passed through to the loop driver; nothing in ynh parses output based on it. If you want ynh to know which lines matter, that is `match`.

### Why `format` is freeform

The space of feedback formats is moving fast — SARIF, NDJSON event streams, LLM-emitted JSON-schemas, vendor-specific report formats. ynh does not maintain a vocabulary; it stores whatever string the harness author and the loop driver agree on. Conventional names you'll see: `json`, `junit-xml`, `lcov-summary`, `sarif`, `markdown`, `text`, `ndjson`. Coining a new one is fine.

### Channel inference

If `channel` is omitted, `ynh sensors run` infers it from the source kind:

| Source | Default channel |
|---|---|
| `files` | `files` |
| `command` | `stdout+exit` |
| `focus` | `stdout` |

### Reading the ratchet

```
$ ynh baseline local/demo
  · build                    nothing recorded — no failures are forgiven
  ● lint                     12 forgiven, accepted 2026-08-20T09:14:02Z

2 sensors: 1 with recorded debt (12 findings forgiven), 1 with none
```

The baseline stores twelve-character fingerprints rather than findings, so the
file answers *how much* is forgiven and *when* it was accepted, but not *what*.
`--explain` runs the sensors and matches current output against the recorded
hashes, which is the only way to turn a fingerprint back into the line it
forgives:

```
$ ynh baseline local/demo --explain
  ● lint                     12 forgiven, accepted 2026-08-20T09:14:02Z
      internal/handler/route.go:41: error return value not checked
      internal/handler/route.go:88: error return value not checked
```

That costs a sensor run, so it is opt-in rather than the default.

A finding that no longer appears is omitted: it has been fixed, and the
baseline can be narrowed. **A sensor with nothing recorded forgives nothing** —
which is different from one recording zero, and the report says so rather than
leaving a reader to infer it.

Shape: [`docs/schema/cli/baseline.schema.json`](https://github.com/eyelock/ynh/blob/main/docs/schema/cli/baseline.schema.json).

### `ratchet` — when the count is the finding

```json
"suppressions": {
  "tolerance": "blocking",
  "ratchet": "count",
  "source": { "command": "grep -rn 'nolint\\|eslint-disable\\|# noqa\\|SuppressWarnings' ." },
  "output": { "format": "text" }
}
```

The baseline normally forgives by **fingerprint**: each finding is hashed, a
fixed one stops being forgiven, and a new one is flagged wherever it appears.
Line numbers are normalised to `:N` so moving code is not a regression, and
identical lines are deduplicated.

That is the right behaviour for a linter and the wrong one for a suppression
scan. **The gaming vector for a ratchet is suppression, not relocation.** An
agent that cannot fix a finding can silence it — and a second `//nolint` in a
file that already has one changes neither the fingerprint set nor the
distinct-line count. It is invisible.

`ratchet: "count"` measures the **total** instead, so the second one is a
regression:

```
$ ynh check local/demo            # after an agent adds one //nolint
  suppressions   fail   count_delta +1
```

Removing suppressions is progress, not a regression, and the reduced total is
recorded on the next `--update-baseline` — which is what stops a count creeping
back to an old high-water mark.

Requires a command source: only a command sensor produces countable findings.
`fingerprint` remains the default, so no existing harness changes behaviour.

**What it does not cover.** A deleted test is not a suppression. This catches
silencing, not removal — a sensor counting tests would catch that, and it is a
different declaration.

### `reference` — proving a sensor still observes

```json
"lint": {
  "tolerance": "blocking",
  "source": { "command": "golangci-lint run ./..." },
  "reference": { "path": "testdata/calibration/lint", "expect": "fail" },
  "output": { "format": "text" }
}
```

Optional. `ynh check --calibrate` runs each sensor against its reference and
reports whether it still detects what it claims to.

**Why it matters.** A sensor is a command plus an expectation about its exit
code. If the command quietly stops examining anything — a config change
excluding a directory, an upgrade renaming a rule, a path that no longer
matches — it exits 0 and `ynh check` reports **green**. Everything else depends
on sensors telling the truth: the ratchet forgives against their output, the
loop converges on their verdicts, and any yield figure derives from them. A
sensor that has quietly stopped working makes every other control decorative
while appearing to function.

Note the asymmetry this closes. Judged sensors already require verdict
stability before they may gate. Deterministic sensors, trusted more, had no
check at all.

**A sensor that passes an `expect: fail` fixture is broken** — it should have
failed. `expect: pass` catches the opposite, a sensor firing on clean input,
which is why `expect` is not a boolean.

```
$ ynh check local/demo --calibrate
  ✓ working-lint     calibrated (testdata/calibration/lint → fail)
  ✗ stopped-lint     FAILED   expected fail, observed pass —
                              the sensor passed a fixture built to trip it — it is no longer observing
  · no-reference     uncalibrated

3 sensors: 1 calibrated, 1 failed, 0 errored, 1 uncalibrated
```

Four properties are load-bearing:

- **A separate mode.** `ynh check` stays fast and never runs references. A gate
  that calibrates on every invocation is a gate people disable — and then
  nothing is calibrated at all.
- **The fixture lives outside the agent's write path.** A reference an agent
  can edit calibrates nothing. `reference.path` is relative to the harness and
  rejects absolute paths and `..`.
- **Absent is not empty.** A sensor with no reference reports *uncalibrated*,
  distinct from *failed calibration*, and never breaks the run. How much of a
  gate is proven to observe is itself a number worth reading.
- **Not mandatory.** Adding it to every sensor would break every existing
  harness.

**An uncalibrated sensor is not yet a sensor.** A fixture is the only thing
that proves a sensor still observes anything, and it is what catches the
working-directory mistake above. A script that found its own root, pointed at
a fixture that must fail, analyses the clean parent repository instead,
returns pass, and calibration reports the mismatch. Without a fixture there is
nothing to notice: the sensor sits declared and trusted, gating on a tree
nobody asked it to look at. That failure is undetectable by any other means,
which is the argument for fixtures on the sensors you actually rely on.

Only a **command** sensor can be calibrated. Calibration compares an exit code
against a declared expectation, and neither a files nor a focus source produces
one. A files sensor's [freshness](#freshness-is-this-artifact-still-true)
verdict is not a substitute: it says whether an artifact is current, not whether
the sensor still detects what it claims to.

Exit codes match `ynh check`: `0` everything behaved as declared, `1` a sensor
did not, `2` ynh could not run the calibration. Shape:
[`docs/schema/cli/check-calibrate.schema.json`](https://github.com/eyelock/ynh/blob/main/docs/schema/cli/check-calibrate.schema.json).

### `convergence-verifier` needs a source that can decide

A sensor with `role: convergence-verifier` tells the loop the run is finished,
so it must be able to produce a verdict. **A `files` source cannot**, and
`ynd validate` refuses the combination.

No verdict about a files sensor's **contents** is mechanically derivable. It
now carries a [freshness](#freshness-is-this-artifact-still-true) verdict, but
freshness answers "is this artifact still about the current tree", not "is the
work done" — and convergence is a question about the work.

A files sensor declared as the verifier would end the run because a path exists
and is newer than its inputs. Contents never read, `output.format` never
consulted. That path sits inside the agent's own write path, so the run could
manufacture its own convergence by writing the file — and freshness does not
close that door: an artifact the agent regenerated without doing the work reads
as perfectly fresh. Freshness catches an observation that has gone out of date.
It cannot catch one that was never made.

The refusal is structural rather than a special case. Convergence is
`status: pass`; a fresh files sensor is `status: reported`, and `reported` is
not `pass`. Its failing states are worse still — converging on `absent` or
`stale` would end a run on the strength of a missing or outdated file.

Use a command source that exits non-zero until the work is done, or a focus
source, which a loop driver resolves with an agent runtime.

## Validation

`ynd validate` checks:

- Each sensor name is non-empty.
- `source` has exactly one of `files`, `command`, `focus`.
- `source.files` is a non-empty array of non-empty strings.
- `source.command` is a non-empty string.
- `source.focus` (string form) references a defined top-level `focus.<name>`.
- `source.focus` (object form) has a non-empty `prompt`; if `profile` is set, it resolves to a defined profile.
- `output.format` is non-empty.
- `category`, if set, is one of the three Fowler enum values.
- `role`, if set, is one of `regular`, `convergence-verifier`, `stuck-recovery`.
- Unknown fields inside a sensor are rejected.

Errors are prefixed with the sensor name:

```
sensor "coverage": source must have exactly one of files, command, focus
sensor "security-scan": source.focus references undefined focus "infer-vulns"
```

## Composition

### Includes — root-only

Only the root harness's sensors are used. An included harness contributes `skills/`, `agents/`, `rules/` and `commands/` — files — and nothing else.

Nothing is *dropped*: `resolveWith` iterates includes flat, with no recursion, and returns file paths. **It never opens an included harness's `plugin.json`,** so a sensor declared there is never read in the first place. Root-only is a property of the resolver, not a filter applied afterwards.

That is deliberate. It keeps the answer to "what observes this repository" in one committed file a reviewer can read, and stops a composed harness turning inert included content into an execution surface the root author never declared.

If an included harness needs a sensor, copy its declaration into the root harness's `plugin.json`. Merging that copy at authoring time — generated, labelled blocks with drift detection — is the agreed direction rather than resolving includes at run time.

### Profiles — out of scope for v1

Profiles do **not** override or add sensors in v1. Sensors are observation declarations, not runtime context. If a real use case emerges, it will be revisited in a follow-up.

### Focus — referenced, not modified

A sensor can reference a top-level focus or inline its own. It cannot mutate a focus. Inline focuses live only as the sensor's source.

## CLI

### `ynh sensors ls <harness>`

List declared sensors with category, role, source kind, and format. Plain text by default; `--format json` for machine consumption.

```
$ ynh sensors ls my-harness
NAME              CATEGORY          SOURCE     FORMAT
build             maintainability   command    text
coverage          maintainability   files      lcov-summary
coverage-judge    behaviour         focus*     markdown
security-scan     behaviour         focus      markdown
tests             behaviour         files      junit-xml

* = inline focus
```

JSON form returns an array of summary objects — the canonical machine-readable form a loop driver consumes:

```json
[
  { "name": "build", "category": "maintainability", "source_kind": "command", "format": "text" },
  { "name": "coverage-judge", "role": "convergence-verifier", "source_kind": "focus", "format": "markdown", "inline_focus": true }
]
```

### `ynh sensors show <harness> <name>`

Print the fully-resolved sensor block as JSON. Inline focuses are kept inline; string-referenced focuses are expanded so the consumer gets a self-contained payload:

```json
{
  "name": "security-scan",
  "category": "behaviour",
  "source": {
    "focus": {
      "name": "infer-vulns",
      "prompt": "Identify high-severity vulnerabilities in the changed code",
      "inline": false
    }
  },
  "output": { "format": "markdown" }
}
```

### `ynh sensors run <harness> <name>`

Mechanically execute a sensor and emit a JSON result. There is no `passed` boolean — ynh returns raw exit codes, output, and file contents. Pass/fail thresholds are loop-driver policy.

For a `command` sensor:

```json
{
  "name": "build",
  "kind": "command",
  "exit_code": 0,
  "duration_ms": 1247,
  "output": {
    "format": "text",
    "channel": "stdout+exit",
    "stdout": "...",
    "stderr": ""
  }
}
```

For a `files` sensor, the result includes file contents (or just metadata with `--no-content`). For a `focus` sensor, ynh returns the resolved focus declaration with a note explaining the loop driver invokes the agent runtime itself.

Flags:
- `--cwd <dir>` — working directory for `command` sensors and base for relative `files` globs. Defaults to current directory.
- `--no-content` — omit file contents for `files` sensors. Use when only metadata (path, size) is needed.

## Relationship to hooks

Hooks and sensors are complementary, not overlapping. Both can run shell commands; the difference is who pulls the trigger and who consumes the result.

| | Hooks | Sensors |
|---|---|---|
| Direction | **Push** — vendor runtime fires them | **Pull** — loop driver invokes them |
| When | Mid-session, on lifecycle events | Between iterations, on demand |
| Purpose | Enforce / observe *during* a turn | Surface signal *for the next* turn |
| Failure mode | Can block the action (exit 2) | Reports state; loop driver decides policy |

A mature harness uses both. Hooks for in-session guardrails (block `git push --force`, run formatter on save). Sensors for between-turn judgment (coverage adequate? new high-severity vulns?).

### Canonical pattern: hook emits → sensor consumes

The most common integration is a hook that produces an artifact a sensor declares as its source:

```json
{
  "hooks": {
    "after_tool": [
      { "matcher": "Edit|Write", "command": "./scripts/run-lint.sh > .lint-results.json" }
    ]
  },
  "sensors": {
    "lint": {
      "category": "maintainability",
      "source": { "files": [".lint-results.json"] },
      "output": { "format": "json" }
    }
  }
}
```

The hook is the runtime mechanism that produces the data; the sensor is the declarative contract over reading it. Coupling is **by shared file path** — implicit, no schema link needed.

> **Making the hook actually fire.** For an always-on sensor loop in a plain Claude session, the hooks must live in the project's `.claude/settings.json`, not just `.ynh-plugin/plugin.json` — and an `on_stop` sweep that feeds a verdict back to the agent has specific output and loop-guard requirements. See [Hooks §"Running hooks in a plain Claude session"](hooks.md#running-hooks-in-a-plain-claude-session) and [Hooks §"on_stop output semantics"](hooks.md#on-stop-output-semantics-claude).

### Same script, different driver

A sensor with `source.command: "make check"` and an `after_tool` hook running `make check` invoke the same program. The difference is who pulls the trigger. Authors should not feel forced to pick one — both can coexist when the script needs to fire both in-session (hook) and on-demand (sensor).

## Consuming sensors (for loop-driver authors)

The intended consumption pattern:

```bash
# Discover what's declared
ynh sensors ls my-harness --format json

# For each sensor the loop wants to run:
result=$(ynh sensors run my-harness build)
exit_code=$(echo "$result" | jq -r '.exit_code')
output=$(echo "$result" | jq -r '.output.stdout')

# Identify the convergence sensor by role:
verifier=$(ynh sensors ls my-harness --format json |
           jq -r '.[] | select(.role == "convergence-verifier") | .name')
```

ynh does **not** ship a loop driver. Orchestration policy — when to run sensors, how to weight them, when the loop is done — belongs to the layer above ynh. See `docs/harness-engineering.md` for the architectural framing.

## Examples

### Go project

```json
{
  "sensors": {
    "build": {
      "category": "maintainability",
      "source": { "command": "go build ./..." },
      "output": { "format": "text" }
    },
    "test": {
      "category": "behaviour",
      "source": { "command": "go test -race -coverprofile=coverage.out ./..." },
      "output": { "format": "text" }
    },
    "coverage": {
      "source": { "files": ["coverage.out"] },
      "output": { "format": "go-coverage" }
    },
    "security": {
      "category": "behaviour",
      "role": "convergence-verifier",
      "source": {
        "focus": { "prompt": "Are there any security regressions in the diff vs main?" }
      },
      "output": { "format": "markdown" }
    }
  }
}
```

### Node project

```json
{
  "sensors": {
    "lint": {
      "source": { "command": "npm run lint --silent" },
      "output": { "format": "text" }
    },
    "tests": {
      "source": { "files": ["junit.xml"] },
      "output": { "format": "junit-xml" }
    }
  }
}
```

## See also

- [Hooks](hooks.md)
- [Profiles](profiles.md)
- [Focus](tutorial/focus.md)
- [Harness engineering](harness-engineering.md)
- [CLI structured output](cli-structured.md)

## Tolerance

`tolerance` is the one piece of pass/fail policy ynh owns. It declares how
`ynh check` treats a failing sensor, which is also how the three enforcement
loops are expressed without ynh owning a scheduler:

| Value | `ynh check` behaviour | Typical loop |
|---|---|---|
| `blocking` (default) | Failure sets the verdict to `blocked` and exits 1 | PR gate |
| `advisory` | Failure is reported in full but does not gate | edit-time feedback |
| `report` | Pure observation | scheduled analysis |

Tolerance means the same thing for every kind, but each kind reaches a failure
differently.

A **command** sensor gates on its exit code. A **files** sensor gates on
[freshness](#freshness-is-this-artifact-still-true) — `absent`, `stale` and
`unknown` fail, `fresh` reports (`status: reported`) — but never on what the
artifact says, which ynh does not read. A **focus** sensor needs an agent
runtime ynh does not own, so it defers (`status: deferred`) and never blocks
whatever tolerance is declared.

A gate that guesses is worse than one that admits what it cannot judge. Where
ynh can decide — an exit code, a missing or outdated artifact — it does. Where
it cannot, it says so rather than inventing an answer.

Everything richer than this — thresholds, severity filters, convergence
judgments — still belongs to a loop driver. `ynh check` answers "did the
declared command succeed", nothing more.

Sensors are also what [`ynh agent run`](agent.md) iterates against between
turns. The loop does not apply its own policy: it runs `ynh check --format
json` and takes its verdict, so the
[ratchet](harness-engineering.md#sensor-gate-ratchet-loop) and the tolerance
rules are the same ones a human gets at a terminal.

### `version_command` — which tool produced this

```json
"lint": {
  "tolerance": "blocking",
  "source": { "command": "golangci-lint run ./..." },
  "version_command": "golangci-lint --version",
  "output": { "format": "text" }
}
```

Optional. When declared, `ynh check` runs it and records the first line on the
sensor's result as `tool_version`, in `--format json` and on the agent
trajectory.

It matters as soon as results are compared over time. A corpus graded across
weeks cannot tell a genuine change in findings from the linter changing
underneath it — those are the same observation without a recorded version.

**Declared, not inferred.** Guessing `<first token> --version` reports make's
version for `make lint`, not the linter's, and hangs outright on commands that
do not support the flag.

The probe is bounded at five seconds and cached for the life of the process, so
an inner loop running the gate every turn does not re-learn a constant. **A
failed probe is never a sensor failure** — a missing tool, a non-zero exit or a
timeout simply leaves the version absent, which honestly says "cannot tell"
rather than implying a stability nobody verified. The version is read from
stdout; stderr counts only on a clean exit, so `java -version` works while
`sh: foo: command not found` is not mistaken for a version.

## `ynh check`

Runs every declared sensor and returns a gate verdict.

```bash
ynh check local/demo                      # run all sensors, text report
ynh check local/demo --only fmt,vet       # filter — the edit-time loop
ynh check local/demo --format json        # machine-readable, for CI and consumers
```

`--sensor-overlay '{"test":{"source":{"command":"go test -short ./..."}}}'`
substitutes a command for a declared sensor for one run. It exists so an inner
loop can buy a faster signal without leaving the gate — a driver that ran the
substituted command directly would be applying its own policy again, which is
the split the gate exists to close. Naming a sensor the harness does not declare
is an error rather than a silent no-op.

### Baseline — inheriting a repo that already fails

Blocking on the exit code alone makes the first run unwinnable on any repo
that is not already clean, and a gate nobody can satisfy is a gate everybody
disables. So `ynh check` records what was already failing and blocks only on
what is new.

```bash
ynh check local/demo --update-baseline   # accept current state; writes .ynh/baseline/
ynh check local/demo                     # blocks only on failures not in the baseline
ynh check local/demo --no-baseline       # ignore the ratchet, show everything
```

```
  ✗  lint  1 new, 3 known  15ms

lint — 1 new (3 pre-existing not shown):
src/feature.go:3:1: exported func New should have comment
```

Only the new failure is shown. Listing the three issues the author did not
introduce alongside the one they did is how a useful gate becomes an ignored
one.

**Commit `.ynh/baseline/`.** The ratchet is a property of the repository, not
of one developer.

#### Layout, and why it is one file per sensor

```
.ynh/baseline/<harness>/<sensor>.json
```

Entries are scoped by harness id, because sensor names are only unique within a
harness — two harnesses in one repository each declaring `lint` would otherwise
share, and overwrite, a single entry. `--update-baseline` refreshes only the
sensors that actually ran and leaves every other entry untouched, so combining
it with `--only` is safe.

The split matters at scale. A single repository-wide file of sorted hash arrays
is maximally conflict-prone and minimally mergeable: with several branches in
flight, every one of them touches the same lines. And **every natural
resolution of a baseline conflict widens the amnesty** — union keeps both
sides' forgiveness, `-X ours` keeps one branch's, and regenerating accepts
whatever happens to be failing now. A ratchet is monotonic only if nothing
concurrent quietly loosens it.

One file per sensor means two branches touching different sensors never
conflict at all, and a conflict that does happen is scoped to one sensor and
readable in the diff. `recorded_at` records when a sensor's debt was *accepted*
rather than when the file was last written, so re-running `--update-baseline`
leaves untouched sensors byte-identical.

`--no-baseline` means *do not forgive*, not *tolerate a corrupt ratchet*: a
baseline that cannot be read is fatal either way, because recording over one
would prune every entry the read could not see.

**A baseline conflict is a human decision, and never an agent's.** `ynh check`
refuses to run against a file carrying conflict markers rather than guessing,
and says so: resolving one means deciding which failures the repository
accepts. A sensor that no longer fails has its file deleted, so debt stops
being forgiven the moment it is paid.

Sensor names are free-form, so one containing a path separator is encoded on
disk with a short hash suffix. Each file records its own harness and sensor
name, so the path is only an organising key.

How it works: each failing sensor's output is reduced to one fingerprint per
line, with file positions (`:12:5`) collapsed and absolute paths made relative
to the working directory. Position collapsing is load-bearing — without it,
inserting a line above an existing issue would report the whole file as new on
the next run. Path relativisation lets a baseline recorded on a laptop match on
a CI runner.

A sensor whose failures are all in the baseline reports `known` and does not
gate. When recorded failures stop appearing, `ynh check` says so:

```
baseline: 2 recorded failures are now fixed — `ynh check --update-baseline` to lock that in
```

Baselines only tighten by an explicit act, and nothing being gated may perform
it. **CI cannot write one** — with `CI` set, `--update-baseline` refuses; a gate
that rewrites its own reference point from a feature branch forgives whatever
that branch introduced. **Nor can an agent** — `--update-baseline` refuses
inside an [agent session](agent.md) and appends the refused attempt to the
session's `gate-write-attempts.jsonl`, because an agent that cannot converge
reaching for blanket amnesty is worth measuring, not just blocking.

A sensor emitting more than 2000 distinct lines is recorded **truncated**: no
fingerprints are stored, and it ratchets on the count of distinct failing lines
alone. Keeping a subset would be worse than keeping none — fingerprints are
sorted, so a subset is whichever hashes happen to sort first, and every line
outside it would be permanently new and permanently blocking. The report says
the comparison is approximate rather than implying precision it does not have.

A sensor that fails with no output at all can still be baselined: forgiveness
depends on the sensor having a recorded entry, not on any fingerprint matching.

Exit codes are the contract:

| Code | Meaning |
|---|---|
| 0 | every blocking sensor passed |
| 1 | a blocking sensor failed — the report is on stdout |
| 2 | ynh could not run the check at all — including an unresolved baseline conflict |

1 and 2 are deliberately distinct: a red CI job has to distinguish "your code
is failing" from "the gate itself is broken". `ynh agent run` makes the same
split — a failing sensor is a turn's feedback, while a gate that cannot run
ends the session with exit 22.

Failing sensor output is printed verbatim rather than summarised, because that
output is the remediation an agent acts on.
