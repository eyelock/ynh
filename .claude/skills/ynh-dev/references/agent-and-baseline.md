# `internal/agent` and `internal/baseline`

The two largest subsystems, and the two that had no reference. `internal/agent`
is ~7,000 lines across 15 files with 22 test files; `internal/baseline` is one
file that decides whether a gate is usable on a real repository.

They are documented together because they are the same loop seen from two ends:
the agent iterates until sensors pass, and the baseline decides what "pass"
means on a repository that was never clean.

**Read the package doc comments before changing either.** They carry the
reasoning, usually with the incident that produced it. This page is a map, not
a replacement.

## The shape

```
ynh agent run --task "…"
        │
        ├── plan phase   ── worker proposes a plan        (--max-plan-iterations)
        │
        └── act phase    ── loop until converged or capped
              │
              ├─ worker turn        (vendor CLI subprocess, NDJSON)
              ├─ ynh check          → internal/gate → sensors
              │                        └─ internal/baseline forgives known failures
              ├─ watchdog           edit-loop / no-progress
              ├─ budget             turns, tokens, wall clock
              ├─ checkpoint         written every phase, for --resume
              └─ trajectory         NDJSON event stream, redacted
```

`RunLoop` in `loop.go` is the entry point. `cmd/ynh/agent.go` is a thin flag
parser over it.

---

# `internal/agent`

## Files, by what they own

| File | Owns |
|---|---|
| `loop.go` | `RunLoop`, phase sequencing, convergence decision |
| `worker.go` | `WorkerBackend` / `WorkerSession` — the vendor abstraction |
| `claude.go`, `codex.go`, `cursor.go` | one backend each; all NDJSON wire detail lives here |
| `budget.go` | turn / token / wall-clock limits, and every exit code |
| `watchdog.go` | stuckness signals |
| `checkpoint.go` | `checkpoint.json`, phases, resume |
| `trajectory.go` | the NDJSON event stream and its event kinds |
| `redact.go` | keeping secrets out of that stream |
| `sensor.go`, `check.go` | running the gate and parsing its result |
| `control.go` | the control channel (pause, abort) |
| `result.go` | `RunResult` — the machine-readable outcome |
| `env.go` | what a subprocess is allowed to inherit |

**All wire-format detail belongs inside a backend.** `worker.go`'s doc is
explicit: *"the loop driver never sees them."* A change that leaks a vendor's
message shape into `loop.go` is the wrong change.

## Convergence — the part with teeth

`checkConvergence` in `loop.go` decides whether a run is done. Its comment
records why it is more careful than it looks:

> An empty result set made `allPassed` vacuously true, so a run whose harness
> went missing — which is what `--resume` without `--harness` produced —
> reported converged and exited 0 after one turn, having verified nothing.

So it distinguishes two cases:

- a run started **with no harness** never asked to be verified; the worker
  declaring itself done is the only signal there is
- a run that **expected a harness** and has no results has lost something, and
  must not claim a verdict it cannot back

**If you touch convergence, the question to ask is not "did everything pass" but
"was there anything to pass".** A gate that cannot see is not a gate that
passed — the same rule `freshness` applies to `files` sensors and `reference`
fixtures apply to command sensors.

## Stuckness

`watchdog.go` implements two signals:

- **edit-loop** — the same response content hash N turns in a row
- **no-progress** — no sensor delta for K consecutive turns

Both are thresholds, both default off when zero. `RecordTurn(content, sensorHash)`
returns a non-empty reason when it fires, and the loop exits `ExitStuck`.

## Budget and exit codes

`budget.go` holds `MaxTurns`, `MaxTokens`, `MaxWall` — and, unusually, every
exit code the loop can produce:

| Code | Meaning |
|---|---|
| 0 | converged |
| 10 | iteration cap |
| 11 | token budget |
| 12 | wall clock |
| 13 | stuck |
| 14 | tamper detected |
| 15 | plan iteration cap |
| 20 | worker error |
| 21 | resume error |
| 22 | gate error |
| 30 | user aborted |
| 31 | interrupted |

These are a consumer contract. A loop driver branches on them, so **adding one
is additive and changing one is breaking** — see the `capability-bump` skill.

## Checkpoint and resume

`checkpoint.json` is written per phase (`PhasePlan`, `PhaseAct`) into the
session directory, with its own `checkpointVersion` to bump when the shape
changes.

The rule that resume must not weaken: a session that was checkpointing sensor
state had a harness, so a resume that cannot restore one must not be allowed to
claim convergence. That is the hole the convergence comment above describes.

## Trajectory and redaction

`trajectory.go` emits NDJSON events — `session_start`, `plan`, `turn_start`,
`assistant_message`, `sensor_run`, `sensor_result`, `feedback_sent`,
`stuck_detected`, `tamper_detected`, and more.

**The stream is not the result.** `result.go` says why: a pipeline wanting to
know what happened had to request the stream, tail it, find the last
`session_end` and reconstruct the rest by inference. `RunResult` exists so it
does not have to.

`redact.go` substitutes environment values whose *names* look secret —
`TOKEN`, `SECRET`, `PASSWORD`, `CREDENTIAL`, `API_KEY`, `_KEY`, `PRIVATE`,
`_PAT`, `AUTH` and others, matched case-insensitively as substrings. Values
shorter than a threshold are left alone deliberately: a two-character value
appears inside ordinary words, and replacing every occurrence would corrupt the
trajectory while protecting nothing.

**Adding an event kind means checking redaction covers it.** A new event that
carries raw worker output is a new way for a credential to reach disk.

## Environment and sandbox

`env.go` separates **mechanics** from **policy**. `PATH`, `HOME`, `TMPDIR` pass
through because without them a subprocess cannot start; anything that is
configuration stays out and must be declared by the harness.

`validateSandbox` refuses `--sandbox srt` on any backend but `claude`, with an
error naming the alternatives rather than silently downgrading. Silently running
unsandboxed when sandboxing was asked for is the failure it prevents.

---

# `internal/baseline`

One file, and the package doc is the best explanation of it. The short version:

> A sensor is an arbitrary command that exits 0 or non-zero. There is no
> violation count to compare against. Blocking on the exit code alone makes the
> very first run unwinnable on any repository that is not already clean — which
> is every real repository — and a gate nobody can satisfy is a gate everybody
> disables.

So a baseline stores **a fingerprint per output line**. A failure whose
fingerprints are all present is pre-existing and does not block; anything new
does, and only the new lines are shown.

## Why one file per sensor

`.ynh/baseline/<harness>/<sensor>.json`, and the layout is a correctness
decision rather than tidiness:

> **Every natural resolution of a baseline conflict widens the amnesty** —
> union keeps both sides' forgiveness, `-X ours` keeps one branch's, and
> regenerating accepts whatever is failing right now. A ratchet is monotonic
> only if nothing concurrent quietly loosens it.

Per-sensor files mean two branches touching different sensors never conflict,
and a conflict that does happen is scoped and legible.

`ErrConflicted` is **deliberately fatal**. A baseline file with git conflict
markers stops the run rather than being merged, because resolving one is a human
decision about which failures a repository accepts — *never an agent's*.

## Scoping, and why it is not cosmetic

`Baseline` is keyed by harness id. Sensor names are unique only within a
harness, so two harnesses each declaring `lint` would otherwise share and
overwrite one entry.

## Public API

`Fingerprints`, `Record`, `CountLines`, `Load`, `Save`, `Root`, `Path`,
`Fingerprint`. Consumed by `cmd/ynh/check.go`, `cmd/ynh/baseline.go`,
`internal/agent/sensor.go` and `internal/agent/loop.go` — the CLI gate and the
agent loop, which is the point.

`maxFingerprints` caps one sensor's recorded lines: a sensor emitting more
distinct lines than that is producing noise rather than a violation list.

`SubDir` **is meant to be committed** — the ratchet is a property of the
repository, not of one developer.

---

# Where they meet: `internal/gate`

`gate.go` runs the declared sensors, applies `tolerance`, and produces an
`Envelope` with a verdict:

| Verdict | Where |
|---|---|
| `pass` | `gate.go` |
| `blocked` | `gate.go` — the only one that exits non-zero |
| `broken` | `calibration.go` — a reference fixture failed |

`internal/freshness` decides whether a `files` sensor's artifact still describes
the tree (`fresh` / `stale` / `absent` / `unknown`). `unknown` fails on purpose.

The agent loop calls the gate through `check.go`, whose `runCheckFn` is a
replaceable function variable so tests need no `ynh` binary on disk.

# Changing any of this

- **Exit codes and trajectory events are consumer contracts.** Additive is
  cheap, changing meaning is not — `capability-bump` covers the process.
- **Sensor result and gate envelope shapes** have published schemas and goldens.
  `ynh check --format json` is validated by `ynd validate-output --schema check`.
- **Tests replace the seams, not the world.** `runCheckFn` for the gate, and
  each backend is an interface — there is no need for a vendor CLI on PATH.
- **Anything that can make a run report converged without evidence is the
  serious class of bug here.** It has happened once, silently, and the comments
  in `loop.go` exist to stop it happening again.
