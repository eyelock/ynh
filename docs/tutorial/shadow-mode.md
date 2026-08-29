# Shadow Mode

Before trusting a loop with real work, measure it against work whose correct
answer is already known. Your git history is full of such work: every closed fix
is a task with a graded answer attached.

Shadow mode runs the loop against the commit *before* a known fix, never merges
anything, and compares what came out to what a human actually did. The output is
a yield number — the `y` in the [break-even
equation](factory-pattern.md#the-economics-as-a-method).

This is built from `ynh`, `git` and a shell loop. Nothing else. If it starts
needing a bespoke tool, that is a finding about ynh worth reporting rather than
routing around.

**Prerequisites.** [The Agent Loop](tutorial/agent-loop.md), a repository with
history, and a harness whose sensors cover the class of fix you are sampling.

The rig, end to end — one historical fix yields one graded attempt, and the
answer is withheld from the agent that has to reproduce it:

<svg viewBox="0 0 720 330" role="img" aria-label="Shadow mode rig: a historical fix commit is split into a base state and a ground-truth fix, the agent works from the base, and the two patches are compared into five graded outcomes">
<defs><marker id="a5_R" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse">
<polygon points="0,1 9,5 0,9" fill="currentColor"/></marker></defs>
<g font-family="IBM Plex Mono, monospace" font-size="10.5" fill="currentColor">

<rect x="14" y="44" width="128" height="52" fill="none" stroke="currentColor" stroke-width="1.5"/>
<text x="78" y="32" text-anchor="middle" font-size="9" opacity=".65" font-weight="600">GIT HISTORY</text>
<text x="78" y="64" text-anchor="middle" font-size="10.5" font-weight="600">fix commit</text>
<text x="78" y="80" text-anchor="middle" font-size="9" opacity=".75">known-good</text>

<rect x="14" y="128" width="128" height="52" fill="none" stroke="currentColor" opacity=".55"/>
<text x="78" y="148" text-anchor="middle" font-size="10">parent commit</text>
<text x="78" y="164" text-anchor="middle" font-size="9" opacity=".75">= the base state</text>
<line x1="78" y1="96" x2="78" y2="126" stroke="currentColor" marker-end="url(#a5_R)"/>

<rect x="14" y="212" width="128" height="60" fill="none" stroke="#8f6714" stroke-width="1.5"/>
<text x="78" y="232" text-anchor="middle" font-size="10" fill="#8f6714" font-weight="600">TASK SPEC</text>
<text x="78" y="248" text-anchor="middle" font-size="9" opacity=".8">issue text · linter</text>
<text x="78" y="261" text-anchor="middle" font-size="9" opacity=".8">output at that time</text>
<line x1="78" y1="180" x2="78" y2="210" stroke="currentColor" marker-end="url(#a5_R)"/>
<text x="152" y="242" font-size="9" fill="#8f6714">never the fix message —</text>
<text x="152" y="255" font-size="9" fill="#8f6714">that leaks the answer</text>

<rect x="196" y="112" width="180" height="96" fill="none" stroke="currentColor" stroke-width="1.5"/>
<text x="286" y="100" text-anchor="middle" font-size="9" opacity=".65" font-weight="600">CONTAINER</text>
<text x="286" y="136" text-anchor="middle" font-size="11" font-weight="600">ynh agent run</text>
<text x="286" y="155" text-anchor="middle" font-size="9.5" opacity=".8">factory harness</text>
<text x="286" y="169" text-anchor="middle" font-size="9.5" opacity=".8">sensors + ratchet</text>
<text x="286" y="185" text-anchor="middle" font-size="9" fill="#93372c" font-weight="600">no --auto-commit · never merges</text>
<path d="M142 242 L170 242 L170 160 L194 160" fill="none" stroke="currentColor" marker-end="url(#a5_R)"/>

<rect x="424" y="128" width="120" height="52" fill="none" stroke="currentColor" opacity=".55"/>
<text x="484" y="148" text-anchor="middle" font-size="10">agent patch</text>
<text x="484" y="164" text-anchor="middle" font-size="9" opacity=".75">+ trajectory</text>
<line x1="376" y1="154" x2="422" y2="154" stroke="currentColor" marker-end="url(#a5_R)"/>

<path d="M142 70 L400 70 L400 126" fill="none" stroke="currentColor" opacity=".6" stroke-dasharray="4 3"/>
<text x="270" y="62" text-anchor="middle" font-size="9" opacity=".7">ground truth, withheld from the agent</text>

<rect x="424" y="212" width="120" height="44" fill="none" stroke="#1d5a66" stroke-width="1.5"/>
<text x="484" y="231" text-anchor="middle" font-size="10.5" font-weight="600" fill="#1d5a66">GRADE</text>
<text x="484" y="246" text-anchor="middle" font-size="9" opacity=".8">blind, by a human</text>
<line x1="484" y1="180" x2="484" y2="210" stroke="currentColor" marker-end="url(#a5_R)"/>

<text x="588" y="120" font-size="9.5" font-weight="600" opacity=".8">FIVE OUTCOMES</text>
<line x1="576" y1="128" x2="706" y2="128" stroke="currentColor" opacity=".3"/>
<circle cx="584" cy="142" r="3.5" fill="#37704a"/><text x="596" y="146" font-size="9.5">equivalent</text>
<circle cx="584" cy="160" r="3.5" fill="#37704a"/><text x="596" y="164" font-size="9.5">different, valid</text>
<circle cx="584" cy="178" r="3.5" fill="#8f6714"/><text x="596" y="182" font-size="9.5">superficial</text>
<circle cx="584" cy="196" r="3.5" fill="#93372c"/><text x="596" y="200" font-size="9.5">wrong</text>
<circle cx="584" cy="214" r="3.5" fill="#93372c"/><text x="596" y="218" font-size="9.5">broken</text>
<line x1="544" y1="234" x2="574" y2="234" stroke="currentColor" opacity=".5"/>
<text x="580" y="238" font-size="9" opacity=".7">yield = green ÷ all</text>

<line x1="14" y1="292" x2="706" y2="292" stroke="currentColor" opacity=".25"/>
<text x="14" y="310" font-size="9.5" opacity=".75">Every graded unit is also one labelled example for evaluating a judge — obtained as a by-product rather than as a research project.</text>
</g></svg>

## Select candidates

Find closed fixes:

```bash
git log --oneline --grep='^fix' -15
```

```
8382382 fix(uninstall): preserve bare-name resources claimed by another install (#173)
f79758b fix(plugin): ingest root .mcp.json as MCP server fallback (#179)
c6d7f99 fix(cursor): write rules as .mdc with frontmatter, not plain .md (#201)
```

**Reconstruct the task from what was available at the time** — the issue text,
the failing test, the linter output. **Never the fix commit message.** It was
written afterwards by someone who already knew the answer, and it usually
contains that answer. A task built from it measures reading comprehension, not
repair.

For `8382382` the honest prompt is the issue title (`#173`) and the failing
behaviour. Not "preserve bare-name resources claimed by another install", which
tells the agent both the diagnosis and the strategy.

## Build the base state

```bash
FIX=8382382
W=/tmp/shadow/base
git worktree add --detach "$W" "$FIX^"
```

```
fix:    8382382 fix(uninstall): preserve bare-name resources claimed by another install (#173)
parent: 1ca2a94 feat(hooks): wire sensors into plain Claude sessions (#172)
```

`$FIX^` is the parent — the tree as it stood with the bug present. A detached
worktree keeps your own checkout untouched.

## Pin the harness, do not inherit it

The obvious move is to use the harness as it existed at the base commit. It is
the wrong move, and the repository will usually tell you so:

```bash
cd /tmp/shadow/base
ynh install ./
ynh check local/ynh-guide --only fmt --format json
```

```json
{
    "error": {
        "code": "not_found",
        "message": "sensor \"fmt\" not declared in harness \"ynh-guide\""
    }
}
```

At that commit the harness declared **no sensors at all** — they were added
later. Had the base commit been three months further back, the sensors might
have existed but with different commands.

So **pin one harness for the whole sample and run it against every historical
tree**. The harness is the instrument; changing it mid-run means the early and
late halves of your sample were measured with different rulers, and the number
you get is the average of two experiments.

## Capture the finding as it stood

Record the failing state before the agent touches anything:

```bash
ynh check local/<pinned-harness> --format json > /tmp/shadow/before.json
```

This is the "before" half of the [reproducible
negative](factory-pattern.md#what-actually-reduces-review-time). Without it you
cannot tell a fix from a change.

If the gate already passes at the base commit, discard the candidate. Your
sensors do not detect this class of bug, and no result the agent produces will
mean anything.

## Run the loop against the base state

```bash
ynh agent run \
  --harness local/<pinned-harness> \
  --task "$(cat /tmp/shadow/task-173.txt)" \
  --worktree /tmp/shadow/base \
  --max-turns 15 \
  --emit-jsonl /tmp/shadow/173.jsonl
echo "exit=$?"
```

**Nothing merges.** `--auto-commit` is opt-in, so never-merging is already the
default — there is no flag to remember and no mistake to make. The loop leaves
its work in the worktree as uncommitted changes.

Record the exit code with the result. A run that ended at `10` (turn cap) is a
different observation from one that ended at `0`, and lumping them together
hides the shape of the failure. See the [exit code
table](tutorial/agent-loop.md#exit-codes).

## Compare

```bash
git -C /tmp/shadow/base diff > /tmp/shadow/agent-173.patch
git show "$FIX" > /tmp/shadow/human-173.patch
```

Now the important instruction: **a diff against the human fix is not a score.**

Different valid fixes exist. The human patch is one correct answer, not the
definition of correctness. An agent patch that shares no lines with it may be
equally right; one that matches it closely may still be wrong, because it
matched the shape and missed the condition.

Compare behaviour, not text. The `before.json` capture and the sensor verdict
after the run are the objective part. The rest is judgement, which is why the
next step is a human one.

## Grade blind

Grade against a fixed rubric, without knowing which patch came from where:

| Grade | Meaning |
|-------|---------|
| **Equivalent** | Fixes the same defect by substantially the same means |
| **Different, valid** | Fixes the defect by other means. Counts as success |
| **Superficial** | Silences the sensor without addressing the defect |
| **Wrong** | Changes behaviour incorrectly |
| **Does not build** | Fails to compile or run |

`y` is (equivalent + different-valid) ÷ all attempts. **Superficial is the grade
that matters most**, because it is the one an automated comparison cannot catch
and the one a factory produces under pressure. A patch that deletes the failing
assertion passes the gate.

Blind grading is not ceremony. The grader who knows which patch is the machine's
grades it differently, in both directions.

## Read the result honestly

Five things inflate the number. Every one of them makes a factory look better
than it is.

**Selection bias.** Historical fixes are the ones somebody chose to fix, which
skews toward tractable. The hard problems were deferred and never became
commits. Treat any shadow-mode yield as an **upper bound**.

**Training contamination.** If the repository is public and the fix predates the
model's training cutoff, the model may have memorised it. Split the sample at
the cutoff date and report both halves. A large gap between them is the
finding — it means you measured recall, not repair.

**Toolchain drift.** [Pin the harness, do not inherit it](#pin-the-harness-do-not-inherit-it) is the version of this you can see. The subtler one:
a linter minor version landing midway through a long run silently splits the
sample. Pin the whole run — harness, linter versions, container image — and
record what you pinned alongside the result.

**Sample size.** Twenty attempts at 60% is `12/20`, and its 95% confidence
interval runs roughly 36–81%. That is not "60% yield", it is "somewhere between
a third and four fifths". State the interval, not the point estimate.

**Repository sampling.** A result drawn from two repositories measures those two
repositories. Report per-repository as well as pooled — a pooled number hides
the case where one repository carries the whole result.

## Aggregate

```bash
for FIX in $(cat /tmp/shadow/candidates.txt); do
  W=/tmp/shadow/$FIX
  git worktree add -q --detach "$W" "$FIX^"
  ynh check local/<pinned-harness> --format json > "$W.before.json"
  ynh agent run --harness local/<pinned-harness> \
      --task "$(cat /tmp/shadow/task-$FIX.txt)" \
      --worktree "$W" --max-turns 15 --emit-jsonl "$W.jsonl"
  echo "$FIX exit=$?" >> /tmp/shadow/results.txt
  git -C "$W" diff > "$W.patch"
done
```

Clean up when finished:

```bash
git worktree remove --force /tmp/shadow/<name>
```

That loop is the whole rig. What it produces is a defensible `y`, its confidence
interval, and a pile of graded patches. Compare `y` against the `y* = r/h` you
[measured](factory-pattern.md#the-economics-as-a-method) and you have an
evidence-based answer to whether the factory is worth running — before anything
is merged and before anyone's time is committed.

A by-product worth keeping: every graded attempt is a labelled example. If you
later want a judge model, you built its evaluation set as a side effect rather
than as a project.

## Summary

- Shadow mode measures yield against fixes whose answer is already in history.
- Reconstruct the task from what was known *then*; the fix message leaks the
  answer.
- Pin one harness across the whole sample. The historical harness is usually not
  fit for the job, and often does not exist.
- Nothing merges: `--auto-commit` is opt-in.
- A diff against the human fix is not a score. Grade blind, against a rubric,
  and watch for *superficial*.
- Report the confidence interval and the per-repository split. Treat the number
  as an upper bound.

## Next

[Delegation](tutorial/delegation.md) — chain harnesses together as subagents.
