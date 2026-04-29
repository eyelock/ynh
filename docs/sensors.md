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
  "focus": {
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
