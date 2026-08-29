# Agent Loop

`ynh agent run` drives a coding agent in a loop: it sends a task, runs the
harness's declared [sensors](sensors.md) between turns, feeds the results back,
and stops when the sensors agree the work is done or a budget is spent.

It is the one place ynh runs an agent rather than assembling a harness for one.
Everything it decides comes from declarations already in the manifest — sensors,
tolerances, focuses, profiles — so the loop applies the same policy as
[`ynh check`](sensors.md#ynh-check).

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

After each turn the loop runs the declared sensors and decides whether to stop.

- Only **blocking** sensors gate. `advisory` and `report` sensors are reported
  and fed back, but never hold convergence open — the same rule `ynh check`
  applies, from the same `tolerance` declaration.
- When every blocking sensor passes and a `--convergence-sensor` is declared,
  that sensor is consulted as the final say.
- **A run that expected verification and produced no sensor results does not
  converge.** This matters on resume: a session whose harness cannot be restored
  has no sensors, and a verdict with no evidence behind it is worse than no
  verdict. The loop reports why and exits non-zero.

A run started with no `--harness` is a plain agent runner — nothing was asked to
verify it, so the worker finishing is the only available signal and the loop
converges on it.

## Stuckness

Two detectors stop a loop that is not going anywhere:

- **No progress** — the sensor picture is unchanged for several turns. The
  comparison covers each sensor's *output*, not just its exit code, so fixing
  some findings while a sensor still fails counts as progress. File positions
  are normalised, so a finding moving down a file does not.
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
| 14 | tamper detected |
| 15 | plan-iteration cap reached |
| 20 | worker error |
| 21 | resume error |
| 30 | aborted by user |
| 31 | interrupted |

Anything non-zero means the loop stopped without the sensors agreeing the work
was done. Codes 10–12 are budgets, 13–15 are the loop deciding to stop, 20–21
are failures to run, and 30–31 are external interruption.

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
| `budget_snapshot` / `budget_exceeded` | Budget state, and a cap being hit |
| `converged` | Sensors agree the work is done |
| `session_end` | Run finished, with exit code and totals |

`sensor_result` carries the sensor's `tolerance`, so a consumer can tell why a
failing sensor did not gate.

## Relationship to `ynh check`

They apply the same policy to the same declarations and differ in who drives:

|  | `ynh check` | `ynh agent run` |
|---|---|---|
| Runs sensors | once | between every turn |
| Drives an agent | no | yes |
| Gating rule | blocking sensors only | blocking sensors only |
| Baseline / ratchet | yes | not yet |
| Typical use | a gate, in CI or a hook | unattended iteration |

The loop does not yet honour [baselines](sensors.md#baseline--inheriting-a-repo-that-already-fails),
so on a repository with pre-existing failures it will iterate against debt it
did not create. Use it on a clean tree, or scope `--worktree` to one.

## See also

- [Sensors](sensors.md) — declaring what the loop observes
- [Tutorial 20](tutorial/20-check.md) — the gate, and baselines
- [Harness Engineering](harness-engineering.md) — where the loop sits
