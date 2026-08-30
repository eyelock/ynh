---
name: ynh-troubleshoot
description: Diagnose a ynh setup that is not working — a harness that will not install, a launcher that is not found, a vendor that cannot see the skills, instructions that are not applied, or a check that gates for no visible reason. Use when something is broken and it is not obvious why.
---

# Troubleshoot a broken setup

The commands to diagnose almost anything here already exist. What is missing is
knowing **which one to reach for**, so this skill is a routing table, not a
tutorial.

## The rule that saves the most time

**Establish where the truth diverges before changing anything.** There are four
places a harness can be wrong, and the fix is different in each:

```
the manifest  →  what composition resolved  →  what the vendor received  →  what the vendor did
   ynd validate      ynd compose                  ynd preview                 vendor's own logs
```

Walk it left to right and stop at the first surprise. Most sessions that go
wrong start by editing the manifest when the manifest was fine.

## Start here, always

```bash
ynh doctor
```

It checks the setup as a whole — vendor availability, installed-harness
integrity, manifest validity of what is installed, symlink health, launcher and
PATH, and anything sitting in quarantine — and reports findings with the command
that fixes each. It is read-only: it will tell you what is wrong and never
change it.

If `ynh doctor` is clean and the problem persists, the fault is in the harness
content rather than the installation, so go to the symptom table.

(If its output covers only Claude hook wiring, you are on a build from before
`doctor` was widened — go straight to the symptom table, which needs nothing
from it.)

## Symptom → command

| Symptom | Run this first | Why |
|---|---|---|
| **Install fails** | `ynd validate <dir>` | Almost always a manifest or frontmatter error. Validation names the file and line. |
| **Installed, but the command is not found** | `ynh paths`, then check `bin` is on `$PATH` | `ynh install` reports success and the launcher lives in `~/.ynh/bin`. If that is not on PATH nothing is runnable. |
| **The vendor cannot see my skills** | `ynh ls` then `ynd preview <dir> -v <vendor>` | `ynh ls` shows artifact counts per harness (`7s 2a 1r`). If the count is 0 the harness has nothing; if it is right, preview shows whether the vendor layout is right. |
| **My instructions are ignored** | `ynd preview <dir> -v <vendor>` and look for the instructions file | The commonest cause: the harness root has `CLAUDE.md`, which is **not** read. It must be `AGENTS.md` or `instructions.md`. |
| **An included skill is missing** | `ynd compose <dir> --format text` | Shows what resolved and from where. A missing include failed to resolve; a present one with the wrong content is a `ref` problem. |
| **Wrong vendor launched** | `ynh info <name>` | Resolution is `-v` flag > harness `default_vendor` > global config. `info` shows what the harness declares. |
| **It worked yesterday** | `ynh installed <name>` | Shows source and install time. A local-path install follows the source directory — if that moved, the harness is stale or gone. |
| **Symlinks look wrong (Codex/Cursor)** | `ynh status`, then `ynh prune` | `status` lists symlink installations across projects; `prune` clears orphaned ones. |
| **`ynh check` gates and I cannot see why** | `ynh check <name> --format json` | See the section below — this has its own routing. |
| **After an upgrade, a harness vanished** | `ynh quarantine list` | A failed auto-migration quarantines rather than deletes. `quarantine restore` brings it back. |
| **Manifest is the old format** | `ynd migrate <dir>` | Converts `.harness.json` to `.ynh-plugin/plugin.json` as a reviewable diff. |

## When `ynh check` gates

`ynh check` exits `0` pass, `1` a blocking sensor failed, `2` the gate could not
run. **Exit 2 is not a failing check — it is a check that could not happen**, and
the two get confused constantly.

```bash
ynh check <name> --format json     # the full result, including per-sensor status
ynh check <name> --only <sensor>   # narrow to one
ynh sensors show <name> <sensor>   # what that sensor actually resolves to
```

**In JSON these are two fields, not one.** The text output merges them into a
single column, which misleads when you go looking for `"status": "stale"` and
find `"status": "fail"`:

```json
{ "name": "e2e-status", "status": "fail", "freshness": "absent" }
```

| `status` | Meaning |
|---|---|
| `pass` / `fail` | a command sensor's exit code |
| `reported` | a `files` sensor that is **fresh** — it passed |
| `deferred` | a `focus` sensor — needs an agent runtime, never gates |

| `freshness` (files sensors only) | Meaning |
|---|---|
| `fresh` | the artifact still describes the tree — pairs with `reported` |
| `stale` | an observed input changed after the artifact was written |
| `absent` | the declared file is not there |
| `unknown` | ynh could not tell — fails, because a gate that cannot see is not a gate that passed |

So a `files` sensor that fails reports `status: fail` **plus** a `freshness`
saying why. If `freshness` is set, the content was never the problem.

Three specific traps:

- **`absent` on a sensor that used to pass** — the artifact is not being
  produced any more. The sensor is right; the producer broke.
- **`stale` on every files sensor after one commit** — `observes` is not
  declared, so the whole tree counts. One line of config fixes it.
- **A blocking sensor that never fails** — it may have stopped observing.
  `ynh check <name> --calibrate` runs each against its reference fixture.
  `uncalibrated` means nobody has proved it bites.

Note `ynh check` takes an **installed id**, not a path. `ynh check ./my-harness`
fails, including the `./<path>` form the error message suggests. Install first,
then `--cwd` to point at the tree.

## Reading the four layers

When the symptom table does not settle it, walk the pipeline explicitly and show
the user each step:

```bash
ynd validate <dir>                     # 1. is the manifest and are the artifacts well-formed?
ynd compose <dir> --format text        # 2. what resolved, and from where?
ynd preview <dir> -v <vendor>          # 3. exactly what will the vendor receive?
ynd diff <dir> claude cursor           # 3b. is this vendor-specific?
```

The first one that surprises them is where the fault is. Say which layer you are
in as you go — "the manifest is fine, the composition is fine, the preview is
missing your rules" is a diagnosis; "it doesn't work" is not.

## How to answer

- **Run the command; do not describe it.** Paste the real output.
- **Say which layer the fault is in** before proposing a fix.
- **One change at a time**, re-running the command that showed the problem. A
  fix that is not verified by the same command that found the fault is a guess.
- If you cannot reproduce it, say so and ask for the output of `ynh doctor` and
  the relevant `ynd preview` rather than speculating.
