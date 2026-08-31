# Gating with `ynh check`

[Sensors](tutorial/sensors.md) declared sensors. This one runs them as a gate.

`ynh check` executes every declared sensor and returns a verdict. It owns the
thinnest possible pass/fail policy — a command sensor passes when it exits 0 —
and `tolerance` decides whether a failure blocks. Everything richer (thresholds,
severity, convergence, when to iterate) still belongs to a loop driver.

## Prerequisites

- `ynh` on your PATH
- A scratch directory; nothing here touches your real projects

```bash
mkdir -p /tmp/ynh-t20/work && cd /tmp/ynh-t20
export YNH_HOME=/tmp/ynh-t20/home
```

## Declare sensors with a tolerance

```bash
mkdir -p gate-demo/.ynh-plugin
cat > gate-demo/.ynh-plugin/plugin.json <<'EOF'
{
  "name": "gate-demo",
  "version": "0.1.0",
  "default_vendor": "claude",
  "focuses": { "review": { "prompt": "review the diff" } },
  "sensors": {
    "build":  { "category": "maintainability", "tolerance": "blocking",
                "source": { "command": "true" }, "output": { "format": "text" } },
    "lint":   { "category": "maintainability", "tolerance": "blocking",
                "source": { "command": "cat issues.txt >&2; exit 1" }, "output": { "format": "text" } },
    "typos":  { "tolerance": "advisory",
                "source": { "command": "exit 1" }, "output": { "format": "text" } },
    "judge":  { "tolerance": "advisory",
                "source": { "focus": "review" }, "output": { "format": "markdown" } }
  }
}
EOF
ynh install ./gate-demo
```

`tolerance` is optional and defaults to `blocking` — the safe default for a gate.

## A clean run

```bash
cd /tmp/ynh-t20/work
: > issues.txt          # no issues yet
ynh check local/gate-demo --only build
echo "exit=$?"
```

```
  ✓  build  pass  4ms

ok: 1 passed
exit=0
```

`--only` filters to named sensors. Skipped sensors are omitted from the text
report — the filtered run is the edit-time path, and it has to cost nothing to
read. They remain in `--format json`.

## A blocking failure gates

```bash
cat > issues.txt <<'EOF'
src/legacy.go:12:5: exported func Old should have comment
src/util.go:8:2: unused variable tmp
EOF
ynh check local/gate-demo
echo "exit=$?"
```

```
  ✓  build  pass            4ms
  ·  judge  deferred        0ms
  ✗  lint   2 new           6ms
  ✗  typos  fail (advisory) 5ms

lint:
src/legacy.go:12:5: exported func Old should have comment
src/util.go:8:2: unused variable tmp

blocked: 1 of 4 sensors failed
exit=1
```

Three things to notice:

- **`typos` failed but did not gate** — it is `advisory`.
- **`judge` is `deferred`.** A focus sensor needs an agent runtime ynh does not
  own, so it never gates.
- **The failing output is printed verbatim.** That output is the remediation an
  agent acts on, so it is not summarised away.

A `files` sensor is the interesting case, and it gets its own section next: ynh
still derives no verdict from what the artifact *says*, but it does decide
whether the artifact is still worth believing.

## A `files` sensor gates on freshness

A `files` sensor reads a result something else produced — an e2e run, a coverage
report, a scan. ynh cannot judge what that result says. It can judge whether it
still describes the code in front of it, and that turns out to be the part that
matters: an artifact from before your last change is not a weaker answer, it is
an answer about different code.

Use a separate harness so the counts above stay as they were:

```bash
cd /tmp/ynh-t20
mkdir -p fresh-demo/.ynh-plugin
cat > fresh-demo/.ynh-plugin/plugin.json <<'EOF'
{
  "name": "fresh-demo",
  "version": "0.1.0",
  "default_vendor": "claude",
  "sensors": {
    "e2e": {
      "category": "behaviour",
      "source":   { "files": ["reports/e2e.json"] },
      "observes": ["src/*"],
      "output":   { "format": "json" }
    }
  }
}
EOF
ynh install ./fresh-demo

mkdir -p /tmp/ynh-t20/app/src /tmp/ynh-t20/app/reports
cd /tmp/ynh-t20/app
echo 'package main' > src/main.go
```

### A missing artifact fails

Nothing has produced `reports/e2e.json` yet:

```bash
ynh check local/fresh-demo --cwd /tmp/ynh-t20/app
echo "exit=$?"
```

```
  ✗  e2e  absent  0ms

e2e:
no file matched the sensor's declared paths

blocked: 1 of 1 sensor failed
exit=1
```

This is the case worth dwelling on. Before freshness existed, this printed
`ok: 0 passed` and exited 0 — a blocking sensor observing nothing, reporting
green. A gate that passes because it is reading a file that is not there is
worse than no gate, because it is trusted.

### Producing the artifact makes it fresh

```bash
sleep 1
echo '{"passed": 12, "failed": 0}' > reports/e2e.json
ynh check local/fresh-demo --cwd /tmp/ynh-t20/app
echo "exit=$?"
```

```
  ·  e2e  reported  0ms

ok: 0 passed
exit=0
```

`reported`, not `pass`: ynh surfaced the artifact and formed no opinion about
its contents. Twelve tests passed, or none did — ynh does not know and does not
claim to. That is still the loop driver's job.

### Changing an observed input makes it stale

```bash
sleep 1
echo '// a behaviour change' >> src/main.go
ynh check local/fresh-demo --cwd /tmp/ynh-t20/app
echo "exit=$?"
```

```
  ✗  e2e  stale  0ms

e2e:
main.go changed after e2e.json was written

blocked: 1 of 1 sensor failed
exit=1
```

The artifact is still valid — it is a true report about the code as it was one
edit ago. It simply no longer describes this tree, so it may not stand in for
one that does. Re-run the suite and the gate goes green again.

### `observes` is what keeps this bearable

`observes: ["src/*"]` says the report depends on `src/`, and nothing else. Edit
something outside it:

```bash
sleep 1
echo '# notes' > README.md
ynh check local/fresh-demo --cwd /tmp/ynh-t20/app --only e2e
echo "exit=$?"
```

```
  ✗  e2e  stale  0ms

e2e:
main.go changed after e2e.json was written

blocked: 1 of 1 sensor failed
exit=1
```

Still stale — and the reason names `main.go`, not `README.md`. The sensor is
carrying the earlier source edit, not the docs one. Prove it
by refreshing the artifact and touching only the README:

```bash
sleep 1
echo '{"passed": 12, "failed": 0}' > reports/e2e.json
sleep 1
echo '# more notes' >> README.md
ynh check local/fresh-demo --cwd /tmp/ynh-t20/app
echo "exit=$?"
```

```
  ·  e2e  reported  0ms

ok: 0 passed
exit=0
```

A docs edit does not invalidate an e2e report, so the gate does not pretend it
does. **Without `observes`, it would** — an undeclared sensor is compared
against the whole tracked tree, and every commit would stale every artifact.
That default is strict on purpose: a harness that will not say what its artifact
depends on gets the only honest assumption, and the noise is what pushes you to
declare the truth.

### The four states

| State | Meaning | Gate |
|---|---|---|
| `fresh` | describes the tree as it stands | passes, shown as `reported` |
| `stale` | an observed input changed after it was written | **fails** |
| `absent` | a declared file is not there | **fails** |
| `unknown` | ynh could not tell | **fails** |

`unknown` failing surprises people. It happens when there is no `observes` *and*
the directory is not a git repository, so there is no input set to compare
against. "No evidence either way" is not "nothing was wrong", and a gate that
cannot see is not a gate that passed.

Freshness respects `tolerance` exactly as an exit code does — set `advisory` or
`report` and a stale artifact will be reported without gating.

```bash
ynh check local/fresh-demo --cwd /tmp/ynh-t20/app --format json \
  | grep -E '"freshness"|"freshness_basis"'
```

```
      "freshness": "fresh",
      "freshness_basis": "mtime"
```

`freshness_basis` says what the answer rests on. Today ynh compares
modification times, which are reliable on a working checkout and unreliable
wherever they get rewritten wholesale — fresh clones, container builds, CI
caches. Read it before trusting a freshness verdict from a machine that is not
yours.

```bash
ynh uninstall local/fresh-demo
cd /tmp/ynh-t20/work
```

## The problem with inheriting a repository

`lint` is failing on two issues that were there before you arrived. As it
stands, the gate blocks every run until someone fixes them — so the first run
on any repository that is not already clean is unwinnable.

A gate nobody can satisfy is a gate everybody disables. That is what a baseline
is for.

## Record a baseline

```bash
ynh check local/gate-demo --update-baseline
echo "exit=$?"
```

```
baseline recorded under .ynh/baseline — commit it
exit=0
```

Run the gate again:

```bash
ynh check local/gate-demo --only lint
echo "exit=$?"
```

```
  ~  lint  known (2)  6ms

ok: 0 passed, 1 known
exit=0
```

`known` means failing, but only in ways the baseline already records — debt,
not a regression. **Commit `.ynh/baseline/`**: the
[ratchet](harness-engineering.md#sensor-gate-ratchet-loop) is a property of
the repository, not of one developer.

It is one file per sensor:

```bash
find .ynh -type f | sort
```

```
.ynh/baseline/gate-demo/lint.json
.ynh/baseline/gate-demo/typos.json
```

`typos` is recorded even though it never gated. A baseline records what was
failing, not what blocked — so if its tolerance is tightened to `blocking`
later, the debt it already had is still forgiven and only new failures gate.

Entries are scoped by harness, so a repository checked by more than one harness
keeps them separate. `--update-baseline` refreshes only the sensors that ran, so
combining it with `--only` never discards what it did not look at.

The split is not filing tidiness. One repository-wide file of hash arrays
conflicts on every concurrent branch, and every natural resolution of such a
conflict — union, `-X ours`, regenerate — *widens* the amnesty. A ratchet is
monotonic only if nothing quietly loosens it. Per sensor, two branches touching
different sensors never conflict; when one does, `ynh check` refuses to run
against it rather than guessing, because deciding which failures a repository
accepts is a human call.

## Read what the ratchet forgives

A baseline that nobody can read is a list of hashes somebody agreed to ignore.
`ynh baseline` says what is in it:

```bash
ynh baseline local/gate-demo
```

```
Baseline for gate-demo

  · build                    nothing recorded — no failures are forgiven
  ● lint                     2 forgiven, accepted 2026-08-29T22:42:23Z

2 sensors: 1 with recorded debt (2 findings forgiven), 1 with none

Run with --explain to resolve the recorded fingerprints into the findings
they forgive. That runs the sensors, so it is not the default.
```

The file stores fingerprints, not findings, so it can answer *how much* is
forgiven and *when* that was accepted — but not *what*. `--explain` re-runs the
sensors and matches their current output against the recorded hashes, which is
the only way back to the lines themselves:

```bash
ynh baseline local/gate-demo --explain
```

```
Baseline for gate-demo

  · build                    nothing recorded — no failures are forgiven
  ● lint                     2 forgiven, accepted 2026-08-29T22:42:23Z
      src/legacy.go:12:5: exported func Old should have comment
      src/util.go:8:2: unused variable tmp

2 sensors: 1 with recorded debt (2 findings forgiven), 1 with none
```

Note `build`: **a sensor with nothing recorded forgives nothing.** An empty
baseline is not a permissive one, and the distinction matters when reviewing
what a repository has agreed to carry.

## Only new failures gate

Add one issue of your own to the two you inherited:

```bash
cat >> issues.txt <<'EOF'
src/feature.go:3:1: exported func New should have comment
EOF
ynh check local/gate-demo --only lint
echo "exit=$?"
```

```
  ✗  lint  1 new, 2 known  6ms

lint — 1 new (2 pre-existing not shown):
src/feature.go:3:1: exported func New should have comment

blocked: 1 of 1 sensor failed
exit=1
```

Only your issue is shown. Listing the two you did not introduce alongside the
one you did is how a useful gate becomes an ignored one.

## Moving code is not a new failure

Insert lines above the existing issues so their reported positions shift:

```bash
cat > issues.txt <<'EOF'
src/legacy.go:112:5: exported func Old should have comment
src/util.go:88:2: unused variable tmp
EOF
ynh check local/gate-demo --only lint
echo "exit=$?"
```

```
  ~  lint  known (2)  6ms

ok: 0 passed, 1 known
exit=0
```

Still `known`. Positions (`:12:5`) are collapsed before fingerprinting, and
absolute paths are made relative to the working directory. Without the first,
inserting a line would report the whole file as new on your next run; without
the second, a baseline recorded on a laptop would not match on a CI runner.

## Paying off debt tightens the ratchet

```bash
cat > issues.txt <<'EOF'
src/legacy.go:12:5: exported func Old should have comment
EOF
ynh check local/gate-demo --only lint
```

```
  ~  lint  known (1)  6ms

ok: 0 passed, 1 known

baseline: 1 recorded failure is now fixed — `ynh check --update-baseline` to lock that in
```

The baseline never narrows itself. It reports the slack and offers the command;
tightening is a deliberate act.

## CI cannot write a baseline

```bash
CI=true ynh check local/gate-demo --update-baseline
echo "exit=$?"
```

```
Error: check execution failed: --update-baseline refuses to run in CI: the baseline
is a repository decision, not a side effect of a build. Run it locally and commit
the result.
exit=2
```

A gate that rewrites its own reference point from a feature branch forgives
whatever that branch introduced.

Use `--no-baseline` when you want the unratcheted truth:

```bash
ynh check local/gate-demo --only lint --no-baseline
echo "exit=$?"          # 1 — the baseline is ignored
```

## Exit codes and structured output

| Code | Meaning |
|------|---------|
| 0 | every blocking sensor passed |
| 1 | a blocking sensor failed — the report is on stdout |
| 2 | ynh could not run the check at all |

1 and 2 are deliberately distinct: a red CI job has to distinguish "your code is
failing" from "the gate itself is broken".

```bash
ynh check local/gate-demo --format json | head -20
```

The payload carries `verdict`, `summary` counts (including `known`), and per
sensor `status`, `tolerance`, `new_count`/`known_count` and `new_output`. When a
baseline is loaded, a top-level `baseline` object reports `known`, `fixed` and
`stale`.

One further field appears only if you ask for it. A sensor that declares
[`version_command`](sensors.md#version-command-which-tool-produced-this) has its
result annotated with `tool_version` — the first line that command printed. The
sensors above declare none, so their results carry none: absent means *cannot
tell*, not *unchanged*.

It is worth declaring as soon as results are compared over time, because it
turns a green run into a green run *of a known tool*. A linter that silently
stopped examining anything still exits 0, and the version string moving is the
only external evidence that the instrument changed rather than the code.
[Shadow mode](tutorial/shadow-mode.md) depends on it directly: a sample split
across a toolchain upgrade measures two different things, and this is the field
that shows it.

## Wire it into the edit loop

A hook turns the gate into something that runs without being asked:

```json
"hooks": {
  "on_stop": [
    { "command": "ynh check local/gate-demo --only build" }
  ]
}
```

`on_stop` fires at the end of every agent turn on Claude Code, so **only put
fast sensors in it**. A gate that adds thirty seconds to every turn — including
turns that only asked a question — is the reason someone disables it. Keep the
slow suite (`ynh check` with no `--only`) for CI.

Vendor support differs: where a vendor fires `on_stop` once per session, or
cannot block on it, the edit-time loop does not exist and the CI gate is the
one that matters.

## Cleanup

```bash
ynh uninstall local/gate-demo
rm -rf /tmp/ynh-t20
```

## What you learned

- `ynh check` runs every declared sensor and returns one verdict.
- `tolerance` (`blocking` / `advisory` / `report`) decides what gates, and maps
  the three enforcement loops onto sensors without ynh owning a scheduler.
- A `focus` sensor defers — it needs an agent runtime ynh does not own.
- A `files` sensor never gates on what its artifact *says*, but does gate on
  whether that artifact is still worth believing: `absent`, `stale` and
  `unknown` all fail.
- `observes` names what an artifact depends on. Omit it and the whole tracked
  tree counts, which is strict on purpose.
- A baseline forgives pre-existing failures so only new ones block, and shows
  only the new lines.
- Baselines tighten deliberately, and never from CI.
- Exit 1 (code failing) and exit 2 (gate broken) are different answers.

## Next

[The Agent Loop](tutorial/agent-loop.md) — prompt, act, re-observe — and halt on the gate's verdict.
