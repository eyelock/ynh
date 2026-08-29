# Tutorial 21: The Agent Loop

`ynh agent run` prompts a model, lets it act, re-observes with your sensors, and
halts on convergence or on a budget. This tutorial builds a harness with one
sensor, runs the loop against it, and reads what came out.

**Prerequisites.** A configured vendor CLI (this tutorial uses `claude`). No
other credentials are needed. The loop consumes [`ynh check`](tutorial/20-check.md), so
read that tutorial first — everything about tolerance and the
[ratchet](harness-engineering.md#sensor-gate-ratchet-loop) applies here unchanged.

## T21.1: A harness with one sensor and a focus

```bash
mkdir -p /tmp/loop-demo/.ynh-plugin && cd /tmp/loop-demo
cat > .ynh-plugin/plugin.json <<'EOF'
{
  "name": "demo",
  "version": "0.1.0",
  "description": "Agent loop tutorial harness",
  "focuses": {
    "tidy": { "prompt": "Remove TODO markers from notes.txt without deleting the surrounding text." }
  },
  "agent": { "max_turns": 8 },
  "sensors": {
    "lint": {
      "category": "maintainability",
      "role": "gate",
      "tolerance": "blocking",
      "source": { "command": "sh check.sh" },
      "output": { "format": "text" }
    }
  }
}
EOF
cat > check.sh <<'EOF'
#!/bin/sh
grep -rn "TODO" notes.txt 2>/dev/null && exit 1
exit 0
EOF
echo "TODO: tidy this up" > notes.txt
git init -q . && git add -A && git commit -qm "initial"
ynh install ./
```

A **focus** is a named prompt. Declaring the task in the manifest rather than
passing it every time is what makes a run reproducible.

Confirm the gate fails before you start — a loop with nothing to fix teaches
nothing:

```bash
ynh check local/demo
```

```
  ✗  lint  1 new  10ms

lint:
notes.txt:1:TODO: tidy this up

blocked: 1 of 1 sensor failed
```

Exit code `1`.

## T21.2: Run the loop

```bash
ynh agent run --harness local/demo --focus tidy --max-turns 5 --max-wall 4m --emit-jsonl run.jsonl
echo "exit=$?"
```

```
exit=0
```

That is the entire output. **On success the loop prints nothing** — no summary,
no banner. This is deliberate and T21.7 explains why.

The work did happen:

```bash
cat notes.txt
```

```
tidy this up
```

`--task "..."` passes a one-off instruction instead of a declared focus. The two
are mutually exclusive:

```
Error: cannot use --focus and --task together (focus includes a prompt)
```

## T21.3: Budgets, and where the defaults come from

Every run is bounded on three axes. A loop with no caps is not an unbounded
loop by choice — it is the absence of a control, and token consumption between
runs of the same task varies by more than an order of magnitude.

| Flag | Default | Manifest key |
|------|---------|--------------|
| `--max-turns` | `25` | `agent.max_turns` |
| `--max-tokens` | `2000000` | `agent.max_tokens` |
| `--max-wall` | `60m` | `agent.max_wall` |

Precedence is **flag → manifest → default**. The harness above sets
`"max_turns": 8`, so a run that passes no flag is capped at 8, not 25.

The loop records which of the three applied, per axis, as `"flag"`,
`"manifest"` or `"default"`. That distinction matters when reading a batch of
runs: a cap nobody chose that fires is noise, a cap someone chose that fires is
a finding.

Budgets are validated before anything starts:

```bash
ynh agent run --harness local/demo --task "x" --max-turns -1
```

```
Error: --max-turns must be a non-negative integer
```

## T21.4: Convergence

The loop does not decide for itself when it is done, and it does not ask the
model. After each turn it runs `ynh check --format json` and takes that verdict.
Converged means **the gate passes** — every blocking sensor green, with the
ratchet applied.

This is why there is one policy in one place. The verdict the loop acts on is
the same verdict a human gets at a terminal, and neither can be talked out of it
by the agent.

`--convergence-sensor <name>` narrows the question to a single sensor when a
task is scoped to one signal.

The loop is also refused the ability to move its own goalposts:
`--update-baseline` is rejected inside an agent run. An agent that can rewrite
the record of what counts as failure can declare itself finished.

## T21.5: Running against a scratch checkout

```bash
ynh agent run --harness local/demo --focus tidy --worktree /path/to/scratch
```

`--worktree` runs the loop with its working directory set elsewhere — typically
a `git worktree` created for the purpose. The harness stays where it is; only
the tree being modified moves. This is the basis of shadow mode (Tutorial 22),
where the loop runs against a historical commit and must not touch your
checkout.

The path must exist:

```
Error: starting worker: starting claude: chdir /nope/nothing: no such file or directory
```

## T21.6: Reading a trajectory

`--emit-jsonl <file>` writes one JSON object per event. From the run in T21.2:

```bash
python3 -c "
import json
for l in open('run.jsonl'):
    d = json.loads(l)
    print('%-18s turn=%-3s' % (d['type'], d.get('turn', '')))
"
```

```
session_start      turn=
plan               turn=
assistant_message  turn=
budget_snapshot    turn=
turn_start         turn=1
assistant_message  turn=1
budget_snapshot    turn=1
sensor_run         turn=1
sensor_result      turn=1
converged          turn=1
session_end        turn=1
```

The shape is the mechanism: plan once, then repeat *act → observe → feed back*
until `converged` or a budget event. `session_end` carries the outcome:

```json
{"exit_code": 0, "total_turns": 1, "total_tokens": 3637}
```

A run that hits its cap instead ends like this — the same fixture with
`--max-turns 3` and the write denied:

```json
{"budget": "turns", "reason": "turn cap reached (3/3)"}
{"exit_code": 10, "reason": "turn cap reached (3/3)"}
```

Note what the trajectory is *not*: evidence. It is the agent narrating its own
work. Useful for debugging your harness, worthless for verifying a change — see
[what actually reduces review time](factory-pattern.md#what-actually-reduces-review-time).

## T21.7: Exit codes

A pipeline branches on the exit code. It does not parse output — as T21.2
showed, a successful run has no output to parse.

| Code | Meaning |
|------|---------|
| `0` | Converged — the gate passes |
| `10` | Turn cap reached |
| `11` | Token budget exhausted |
| `12` | Wall-clock budget exhausted |
| `13` | Stuck — no progress between turns |
| `14` | Tamper — the run tried to alter what it is judged against |
| `15` | Plan iteration cap reached |
| `20` | Worker error — the vendor CLI failed |
| `21` | Resume error |
| `22` | Gate error — `ynh check` could not run at all |
| `30` | Aborted by the user |
| `31` | Interrupted |

The bands carry meaning. `10`–`15` are the loop stopping itself as designed:
the run is over, nothing is broken. `20`–`22` are faults, and `22` in
particular says the harness is broken rather than the agent — in a batch, every
run will hit it, and it is the operator's fault not the model's.

## T21.8: Resuming

A run writes `checkpoint.json` into its working directory on every turn:

```json
{
  "version": 1,
  "session_id": "3d1560a18a270e11",
  "backend": "claude",
  "resume_token": "b2381edb-9b38-41fe-9167-f0c59fcd920f",
  "phase": "act",
  "plan_finalized": true,
  "last_completed_turn": 0,
  "budget": { "turns": 0, "tokens": 1870, "wall_consumed_ms": 24600 },
  "max_turns": 5
}
```

Resume by pointing at the directory that holds it:

```bash
ynh agent run --resume /tmp/loop-demo
```

The budget resumes where it stopped — it is not reset. A run interrupted at
turn 20 of 25 gets five more turns, not twenty-five.

```
Error: no checkpoint found in "deadbeef": open deadbeef/checkpoint.json: no such file or directory
```

## T21.9: The controls a run does not have

`ynh agent run` starts a worker. It does not contain one.

The worker inherits whatever permissions your vendor CLI grants it, and ynh
passes no flag to widen them. On a machine where the CLI denies edits, the loop
runs to its cap with the sensor failing identically every turn — the trajectory
shows the agent reporting a blocked write, and the exit code is `10`, not `0`.
Granting the write is the operator's job, done in the vendor CLI's own
configuration.

That is the containment doctrine in practice: **ynh declares and executes; it
does not isolate.** `--sandbox` hands the run to a sandbox you installed, and
fails rather than proceeding if that sandbox is unavailable. A containment
control that cannot be applied is an error, not a warning.

For unattended use, this is a prerequisite rather than a refinement. See
[Where ynh stops](factory-pattern.md#where-ynh-stops).

Environment variables reaching the worker are declared too, via
`env_passthrough`. Empty means none — a worker inheriting the whole environment
holds every credential the operator holds, which is not a default anyone chose.

## Summary

- `ynh agent run` prompts, acts, re-observes and halts. Convergence is
  `ynh check`'s verdict, not the model's opinion.
- Every run is bounded on turns, tokens and wall clock; precedence is
  flag → manifest → default, and the source of each cap is recorded.
- The loop is silent on success. Branch on the exit code.
- `--emit-jsonl` explains a run. It does not evidence one.
- Containment belongs to the runtime. An unattended loop needs a container and
  an egress policy you own.

Next: [Shadow Mode](tutorial/22-shadow-mode.md) — measuring whether the loop is actually
any good, against your own git history.
