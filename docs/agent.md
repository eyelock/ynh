# Agent Loop

`ynh agent run` drives a coding agent in a loop: it sends a task, runs the
harness's declared [sensors](sensors.md) between turns, feeds the results back,
and stops when the sensors agree the work is done or a budget is spent.

It is the one place ynh runs an agent rather than assembling a harness for one.
Everything it decides comes from declarations already in the manifest — sensors,
tolerances, focuses, profiles. Between turns it runs
[`ynh check`](sensors.md#ynh-check) itself rather than re-implementing it, so
the loop and a human at a terminal cannot reach different conclusions about the
same manifest.

> Sensors are what the loop iterates against. If a harness declares none, the
> loop has nothing to verify and cannot converge.

## Usage

```bash
ynh agent run --harness <name> --task "<what to do>" [flags]
ynh agent run --resume <session-dir> [flags]
```

## Flags

| Flag | Meaning |
|---|---|
| `--harness <name>` | Harness whose sensors, artifacts and hooks drive the run |
| `--task "<text>"` | What the agent is being asked to do |
| `--focus <name>` | Use a declared focus for the task and its profile |
| `--profile <name>` | Apply a profile overlay |
| `--backend <name>` | Vendor backend to drive (default: the harness's) |
| `--model <name>` | Model override passed to the backend |
| `--convergence-sensor <name>` | Sensor consulted once all blocking sensors pass |
| `--sensor-overlay <json>` | Per-run sensor overrides |
| `--worktree <dir>` | Directory the agent works in and sensors run against |
| `--sandbox <mode>` | Sandbox mode passed to the backend |
| `--auto-commit` | Commit after each converged turn |
| `--interactive` | Pause for approval at turn boundaries |
| `--no-plan` | Skip the plan phase and act immediately |
| `--max-turns <n>` | Cap on act-phase turns |
| `--max-tokens <n>` | Cap on total tokens |
| `--max-wall <dur>` | Wall-clock cap, e.g. `30m` |
| `--max-plan-iterations <n>` | Cap on plan revisions before acting |
| `--emit-jsonl <path>` | Write the trajectory; `-` for stdout |
| `--resume <dir>` | Continue a previous session from its directory |

### Budgets

Every run is bounded. Caps resolve in order — **flag, then harness manifest,
then built-in default** — so there is no unlimited state to fall into:

| Cap | Default |
|---|---|
| `--max-turns` | 25 |
| `--max-tokens` | 2,000,000 |
| `--max-wall` | 60m |

A harness can carry its own envelope rather than every caller passing flags:

```json
"agent": { "max_turns": 40, "max_tokens": 4000000, "max_wall": "90m" }
```

The `session_start` trajectory event records the caps in force **and where each
came from** (`flag`, `manifest`, `default`). Aggregating a batch of runs, a cap
nobody chose that fires is noise in the result and a chosen cap that fires is a
finding — they have to be told apart.

## What the agent can see

The worker receives only the environment variables the harness declares in
`env_passthrough` — not the operator's environment. An agent that inherits the
parent process environment holds every credential the operator holds, which is
not a default anyone chose. See [MCP credentials](mcp.md) for the declaration
and how a profile narrows it.

`--sandbox srt` is honoured by the `claude` backend only. Requesting it with
`codex` or `cursor` is an **error**, not a warning — a containment control that
silently does not apply is worse than an absent one, because it gets relied
upon. ynh does not provide isolation; it runs inside one you configured.

## Convergence

After each turn the loop runs `ynh check` over the harness's sensors and stops
when its verdict is `pass`.

- Only **blocking** sensors gate. `advisory` and `report` sensors are reported
  and fed back, but never hold convergence open — the same rule `ynh check`
  applies, from the same `tolerance` declaration.
- **Failures already in the [baseline](sensors.md#baseline--inheriting-a-repo-that-already-fails)
  do not gate.** A sensor whose every failure is recorded reports `known`, and
  the loop treats it as debt the run inherited rather than work it owes. Only
  the lines a turn actually introduced are fed back, so the agent is not asked
  to clean a repository it was pointed at.
- When the gate is green and a `--convergence-sensor` is declared, that sensor
  is consulted as the final say. It stays a direct `ynh sensors run`: resolving
  a focus sensor needs an agent runtime, which is why `ynh check` reports one as
  `deferred` rather than judging it.
- **A run that expected verification and produced no sensor results does not
  converge.** This matters on resume: a session whose harness cannot be restored
  has no sensors, and a verdict with no evidence behind it is worse than no
  verdict. The loop reports why and exits non-zero. The same applies when
  nothing that ran could ever block — only a blocking *command* sensor can
  produce a blocked verdict, so a harness whose only blocking sensor is a file
  glob verifies nothing and is told so.

A run started with no `--harness` is a plain agent runner — nothing was asked to
verify it, so the worker finishing is the only available signal and the loop
converges on it.

## Stuckness

Two detectors stop a loop that is not going anywhere:

- **No progress** — the sensor picture is unchanged for several turns. The
  comparison covers each sensor's *output*, not just its status, so fixing some
  findings while a sensor still fails counts as progress. File positions are
  normalised, so a finding moving down a file does not. Where a baseline is in
  play it compares the *new* failures only: churn among findings that were
  already forgiven is not progress either.
- **Edit loop** — the agent repeats itself across turns.

## Resume

Interrupting a run leaves a session directory containing a checkpoint and the
trajectory. `--resume <dir>` continues it:

```bash
ynh agent run --resume ~/.ynh/agent/sessions/<id>
```

The checkpoint records the run's identity — harness, profile, convergence
sensor and budget caps — as well as its counters, so a resume restores the run
it is actually resuming. Flags passed on the resume take precedence; anything
omitted comes from the checkpoint.

Budgets carry across: consumption is restored alongside the caps, so resuming
does not hand the run a fresh allowance.

If a checkpoint predates those fields and no `--harness` is given, the loop
warns and continues, but cannot converge — it has no sensors to converge on.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | converged |
| 10 | turn cap reached |
| 11 | token budget exceeded |
| 12 | wall-clock limit reached |
| 13 | stuck |
| 14 | tamper detected — the baseline moved during the run |
| 15 | plan-iteration cap reached |
| 20 | worker error |
| 21 | resume error |
| 22 | gate error — `ynh check` could not run |
| 30 | aborted by user |
| 31 | interrupted |

Anything non-zero means the loop stopped without the sensors agreeing the work
was done. Codes 10–12 are budgets, 13–15 are the loop deciding to stop, 20–22
are failures to run, and 30–31 are external interruption.

Code 14 is the one a pipeline must **escalate rather than retry**. It means the
gate's own reference point moved while the run was in progress: the
[baseline](sensors.md#baseline--inheriting-a-repo-that-already-fails) changed,
or stopped being readable. `ynh check --update-baseline` refuses inside an
agent session, but that only closes the front door — nothing stops a worker
editing the baseline files directly, and an agent that cannot converge has
every incentive to. The loop checks before consulting the gate, not after, so a
widened baseline never gets the chance to forgive the failures that turn
introduced.

Code 22 mirrors `ynh check`'s own exit 2 and is an operator fault, not the
agent's: the harness is missing, a sensor command cannot be executed, or the
gate crashed. It is distinct from 20 so a batch of runs can tell "this agent
failed" from "this harness is broken and every run will hit it". The loop stops
at the first occurrence rather than continuing against no signal — spending a
whole budget on turns nothing could verify, and then reporting the exhaustion as
the agent's failure, hides the real fault.

## Trajectory

`--emit-jsonl` writes one JSON object per line, which is how a consumer follows
a run without parsing terminal output.

| Event | Emitted when |
|---|---|
| `session_start` | Run begins — carries model, ynh version, harness version, base commit, and the resolved budgets with their sources |
| `session_resumed` | Resumed run begins, before the first new turn |
| `plan` / `plan_revised` | Plan produced or revised |
| `plan_approval_required` | Plan phase is waiting for approval |
| `turn_start` | Act-phase turn begins |
| `assistant_message` | Agent output for the turn |
| `sensor_run` / `sensor_result` | A sensor is run, and its result |
| `feedback_sent` | Sensor results sent back to the agent |
| `turn_approval_required` | Act phase is waiting for approval |
| `stuck_detected` | A stuckness detector fired |
| `tamper_detected` | The baseline moved during the run |
| `budget_snapshot` / `budget_exceeded` | Budget state, and a cap being hit |
| `converged` | Sensors agree the work is done |
| `session_end` | Run finished, with exit code and totals |

`sensor_result` carries the gate's `status` word (`pass`, `fail`, `known`,
`reported`, `deferred`) alongside `passed` and `tolerance`, plus `new_count` and
`known_count` when a baseline is in play. `passed` alone cannot express
"failing, but every failure is already recorded" — which is the difference
between a regression this run caused and debt it inherited.

## Relationship to `ynh check`

They apply the same policy to the same declarations and differ in who drives:

|  | `ynh check` | `ynh agent run` |
|---|---|---|
| Runs sensors | once | between every turn |
| Drives an agent | no | yes |
| Gating rule | blocking sensors only | blocking sensors only |
| Baseline / ratchet | yes | yes — the loop runs `ynh check` |
| Typical use | a gate, in CI or a hook | unattended iteration |

There is one policy, in one place. The loop shells out to `ynh check --format
json` between turns rather than running sensors itself, so the ratchet, the
tolerance rules and the verdict are the same ones a human gets at a terminal.

The loop may not write the baseline. `--update-baseline` refuses inside an agent
session and records the attempt; a baseline that changes by any other route
ends the run with exit 14. Nothing being gated may rewrite the gate's reference
point.

## See also

- [Sensors](sensors.md) — declaring what the loop observes
- [Tutorial 20](tutorial/20-check.md) — the gate, and baselines
- [Harness Engineering](harness-engineering.md) — where the loop sits
