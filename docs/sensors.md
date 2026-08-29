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

### `command`

A shell command. The loop driver runs it with the cwd of its choosing and captures stdout, stderr, and exit code.

```json
"build": {
  "source": { "command": "make check" },
  "output": { "format": "text", "channel": "stdout+exit" }
}
```

Use this for build/lint/test/typecheck — anything where running a command IS the observation. Same script can be hooked at `after_tool` for in-session enforcement *and* declared as a sensor for between-turn observation; the two are not redundant.

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

### Why `format` is freeform

The space of feedback formats is moving fast — SARIF, NDJSON event streams, LLM-emitted JSON-schemas, vendor-specific report formats. ynh does not maintain a vocabulary; it stores whatever string the harness author and the loop driver agree on. Conventional names you'll see: `json`, `junit-xml`, `lcov-summary`, `sarif`, `markdown`, `text`, `ndjson`. Coining a new one is fine.

### Channel inference

If `channel` is omitted, `ynh sensors run` infers it from the source kind:

| Source | Default channel |
|---|---|
| `files` | `files` |
| `command` | `stdout+exit` |
| `focus` | `stdout` |

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

Sensors declared in *included* harnesses are dropped during assembly, identical to the existing rule for hooks. Composed harnesses cannot silently inject observation surfaces the root harness author did not declare. If an included harness needs a sensor, copy its declaration into the root harness's `plugin.json`.

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

> **Making the hook actually fire.** For an always-on sensor loop in a plain Claude session, the hooks must live in the project's `.claude/settings.json`, not just `.ynh-plugin/plugin.json` — and an `on_stop` sweep that feeds a verdict back to the agent has specific output and loop-guard requirements. See [Hooks §"Running hooks in a plain Claude session"](hooks.md#running-hooks-in-a-plain-claude-session) and [Hooks §"on_stop output semantics"](hooks.md#on_stop-output-semantics-claude).

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
- [Focus](tutorial/14-focus.md)
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

Only **command** sensors can gate. A `files` sensor has no mechanically
derivable verdict, so it reports (`status: reported`); a `focus` sensor needs
an agent runtime ynh does not own, so it defers (`status: deferred`). Neither
ever blocks, whatever tolerance is declared — a gate that guesses is worse
than one that admits what it cannot judge.

Everything richer than this — thresholds, severity filters, convergence
judgments — still belongs to a loop driver. `ynh check` answers "did the
declared command succeed", nothing more.

Sensors are also what [`ynh agent run`](agent.md) iterates against between
turns, applying the same `tolerance` rule.

## `ynh check`

Runs every declared sensor and returns a gate verdict.

```bash
ynh check local/demo                      # run all sensors, text report
ynh check local/demo --only fmt,vet       # filter — the edit-time loop
ynh check local/demo --format json        # machine-readable, for CI and consumers
```

### Baseline — inheriting a repo that already fails

Blocking on the exit code alone makes the first run unwinnable on any repo
that is not already clean, and a gate nobody can satisfy is a gate everybody
disables. So `ynh check` records what was already failing and blocks only on
what is new.

```bash
ynh check local/demo --update-baseline   # accept current state; writes .ynh/baseline.json
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

**Commit `.ynh/baseline.json`.** The ratchet is a property of the repository,
not of one developer.

Entries are scoped by harness id, because sensor names are only unique within a
harness — two harnesses in one repository each declaring `lint` would otherwise
share, and overwrite, a single entry. `--update-baseline` refreshes only the
sensors that actually ran and leaves every other entry untouched, so combining
it with `--only` is safe.

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

Baselines only tighten by an explicit act. **CI cannot write one** — with
`CI` set, `--update-baseline` refuses. A gate that rewrites its own reference
point from a feature branch forgives whatever that branch introduced.

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
| 2 | ynh could not run the check at all |

1 and 2 are deliberately distinct: a red CI job has to distinguish "your code
is failing" from "the gate itself is broken".

Failing sensor output is printed verbatim rather than summarised, because that
output is the remediation an agent acts on.
