# The harness layer — review

**Date:** 2026-08-31 · **Against:** `origin/develop` @ `cd064c0`, built via
`make build`. Every finding reproduced by running the command shown.

**Scope: the harness *package* — the manifest, its sensors, profiles, focuses
and hooks, and the plugin manifests beside it.** Not the content of the skills
and agents, which is `artifact-defects.md`, and not the Go code, which is
`code-defects.md`.

This is the layer nobody reviewed. The artifacts got three phases of attention;
the container they ship in got none.

## Summary

| # | Finding | Severity | Fixable without a decision |
|---|---|---|---|
| Y1 | Repo-specific sensors are inherited by every user, and one blocks | **P1** | No — design call |
| Y2 | ynh's own sensors are uncalibrated, and the repo's tooling prevents fixing that | **P1** | No — design call |
| Y3 | The `learn` focus reads a file the harness does not ship | P2 | Yes |
| Y4 | Three plugin manifests, two names, one stale version | P2 | No — needs intent |
| Y5 | No `commands/` ship, so one artifact type is undemonstrated | P3 | Yes |
| Y6 | `.claude/settings.json` pre-approves two write-capable commands | P2 | No — your ergonomics |

Y1 and Y2 are the same root cause seen from two sides, and both trace to one
gap in the product: **sensors cannot be scoped to a profile.**

---

## Y1 — Every user inherits sensors written for this repository

**Severity: P1.** The most user-visible defect in the harness layer.

`.ynh-plugin/plugin.json` declares three sensors at the **top level**:

```
fmt     sh -c 'out=$(gofmt -l cmd internal); ...'
vet     go vet ./...
check   make check
```

All three are specific to the ynh Go repository. Sensors are root-only and
**cannot be declared inside a profile** — `docs/sensors.md` says so plainly:
*"Profiles — out of scope for v1."* So the `ynh-dev` profile, which exists
precisely to separate contributor mode from user mode, cannot hold them.

The result: anyone who installs `ynh-guide` — the install the README opens
with — inherits all three.

### Reproduction

```console
$ ynh install .                       # into an isolated YNH_HOME
$ cd ~/some-project                   # a plain project, no Go
$ ynh check local/ynh-guide
  ✗  check  1 new             16ms
  ✓  fmt    pass              19ms
  ✗  vet    1 new (advisory)  65ms

check:
make: *** No rule to make target `check'.  Stop.

vet:
pattern ./...: directory prefix . does not contain main module or its selected dependencies

blocked: 1 of 3 sensors failed
```

Three things wrong in one output:

1. **`check` is blocking and fails.** A user who did nothing wrong gets
   `blocked` from a gate they did not write.
2. **`vet` fails advisory** — noise for the same reason.
3. **`fmt` passes.** This is the worst of the three. `gofmt -l cmd internal`
   found nothing unformatted because there are no Go files at all. A green
   result from a sensor that examined nothing is exactly the failure
   `docs/sensors.md` argues against at length — *"a sensor that has quietly
   stopped working makes every other control decorative while appearing to
   function"* — and the shipped harness demonstrates it on first run.

### The options

This is a design call, not a chore.

**(a) Make the sensors harness-generic.** Replace the Go-specific set with
sensors that observe *the thing this harness is about* — harness artifacts —
and are therefore meaningful to anyone who installed it:

```json
"harness-valid":  { "source": { "command": "ynd validate ." } },
"artifacts-lint": { "source": { "command": "ynd lint ." } }
```

Verified to degrade honestly: in a non-harness directory `ynd validate .` exits
0 with *"No harness directories found"* rather than failing. In the ynh repo it
validates the real harness, so dogfooding is preserved and improved — the repo
would be gating on the artifacts it ships rather than on Go formatting that
`make check` and CI already cover twice.

**Cost:** the repo's own `fmt`/`vet`/`check` sensors go away. They are already
enforced by `make check` and by CI, so the loss is demonstration, not coverage.

**(b) Guard each sensor** so it no-ops outside this repo, the way #263 fixed the
`on_stop` hook. Honest about *why* it skipped, but it still reports a pass for a
sensor that observed nothing — trading a loud wrong answer for a quiet one.

**(c) Fix it properly: allow sensors in profiles.** The real answer, and a
product change. `docs/sensors.md` deferred it as "out of scope for v1... if a
real use case emerges, it will be revisited". **This is that use case**, arriving
from ynh's own harness, which is about as strong a signal as one gets.

My recommendation: **(a) now, (c) when there is appetite.** (a) makes the
shipped harness better rather than merely less broken, and it is the change that
turns the sensors from a liability into the feature's best advertisement.

---

## Y2 — The sensors are uncalibrated, and the repo's own tooling blocks fixing it

**Severity: P1.**

None of the three sensors declares `reference`, `version_command`, `ratchet` or
`role`:

```console
$ python3 -c "...json.load(open('.ynh-plugin/plugin.json'))['sensors']..."
  fmt     ['category', 'output', 'source', 'tolerance']
  vet     ['category', 'output', 'source', 'tolerance']
  check   ['category', 'output', 'source', 'tolerance']
```

They are exactly the naive subset a first-time user would write, shipped as the
worked example a user installs — while `docs/sensors.md` spends forty lines
arguing that a blocking sensor without a `reference` fixture is a gate nobody
has proved bites.

### And the obstacle is self-inflicted

The natural fix is a fixture per sensor under `testdata/calibration/`. For
`fmt`, that means a deliberately badly-formatted Go file. **The repo's own
format tooling walks the whole tree and will not allow one.** Confirmed:

```console
$ printf 'package main\nfunc  main( ){\nx:=1\n_ = x\n}\n' > testdata/_fmtprobe/bad.go
$ gofmt -s -l .
testdata/_fmtprobe/bad.go
```

`make format` runs `gofmt -s -w .` — it would silently *reformat* the fixture,
destroying it. CI's Format Check runs `gofmt -s -l .` and would fail on it.

So the repository cannot calibrate its own formatting sensor without first
excluding `testdata/` from its own formatter. That is a small change with a
real argument behind it: `testdata/` already holds deliberately malformed
fixtures for the migration chain, and formatting them is wrong for the same
reason.

This is worth stating plainly because it is the sharpest illustration in the
whole review: **the thing that would prove the sensor works is prevented by the
repo's own tooling.** Which is a smaller version of the same problem Y1
describes — a control that looks fine and observes nothing.

Note that option (a) in Y1 sidesteps this entirely. A fixture for
`ynd validate` is a malformed **markdown** harness, which no Go tooling touches.

`version_command` needs no fixture and can be added today regardless of which
option is chosen.

---

## Y3 — The `learn` focus reads a file the harness does not ship

**Severity: P2.** Fixable now.

```json
"learn": { "prompt": "Walk me through ynh as a first-time user, starting from the README.md. ..." }
```

`learn` is the focus for someone's first contact with ynh. The assembled
harness contains no `README.md`:

```console
$ ynd preview . -v claude -o /tmp/final && ls /tmp/final/README.md
ls: /tmp/final/README.md: No such file or directory
```

Same defect class as the shipped skills that read `testdata/` (fixed in #256)
and the guide agent that read `docs/` (fixed in #249) — here at the *manifest*
level rather than in an artifact body, which is why the earlier sweeps missed
it.

**Fix:** point the prompt at what the reader actually has — the installed CLI,
the `ynh-guide` agent, and the published docs.

---

## Y4 — Three plugin manifests, two names, one stale version

**Severity: P2.** Needs your intent, not a chore.

```
.ynh-plugin/plugin.json      name=ynh-guide  version=0.1.0   ← the real manifest
.claude-plugin/plugin.json   name=ynh        version=0.1.0   ← hand-written, #70
.cursor-plugin/plugin.json   name=ynh        version=0.1.0   ← hand-written, #70
```

Three problems:

1. **The two root manifests are hand-written duplicates of generated output.**
   `ynd export . -v claude` produces `.claude-plugin/plugin.json` with
   `name=ynh-guide`. The committed one says `name=ynh`. They disagree, and only
   one of them is maintained by anything.
2. **`.claude-plugin/plugin.json` is mislabelled.** Its description reads
   *"development plugin with skills and agents for contributing"*, but it sits
   at the repo root over `skills/` and `agents/` — which are the **user-facing**
   artifacts. The contributor ones are in `.claude/`.
3. **`version: 0.1.0` against a product at `v0.6.0`.** The harness a user
   installs is stamped `0.1.0` regardless of release, so version means nothing
   to a consumer.

Nothing references the root manifests except my own review documents. Their
plausible purpose is making the repo loadable directly via `--plugin-dir .`

**Decision needed:** delete them (and let `ynd export` be the only producer), or
regenerate them so name and description match reality. I raised this in #247 and
it was never settled; it is still open.

---

## Y5 — No `commands/` ship

**Severity: P3.** Fixable now.

The format supports four artifact types. `ynd create command` scaffolds them.
`artifact-formats.md` documents them. The shipped harness contains none, so a
user reading their own installed harness sees three of the four demonstrated.

Cheap to fix and worth fixing precisely because this harness is the reference
example.

---

## Y6 — `.claude/settings.json` pre-approves two write-capable commands

**Severity: P2.** Your call — it is your session ergonomics.

```
44:      "Bash(find *)",
46:      "Bash(sed *)",
```

Both can write: `find … -exec rm` deletes, `sed -i` rewrites in place. Neither
prompts. This sits badly against `.claude/rules/destructive-operations.md`,
which exists because a mutation run destroyed a home directory on 2026-08-29.

`sed` is used overwhelmingly for reading here (`sed -n '1,40p'`); it is the
`-i` form that warrants a prompt.

---

## What is already fixed

Recorded so the list is not re-walked:

- **The `on_stop` hook** referenced `local/ynh-guide`, an id that exists only
  after `ynh install .`, and errored on every stop for a fresh contributor.
  #263 made it probe first and exit 0 with a one-line explanation. That fix is
  the precedent option (b) in Y1 would follow.

## Recommended order

1. **Y3 and Y5 now** — no decision required, and both make the reference harness
   a better example.
2. **`version_command` on the sensors now** — additive, no fixture needed,
   independent of the Y1 choice.
3. **Y1: choose (a), (b) or (c).** Until then, every `ynh install
   github.com/eyelock/ynh` ships a gate that blocks in a non-Go project.
4. **Y4: decide delete or regenerate.**
5. **Y2 follows Y1** — the fixtures are trivial under option (a) and blocked by
   the formatter under (b).
6. **Y6 whenever you want to look at it.**
