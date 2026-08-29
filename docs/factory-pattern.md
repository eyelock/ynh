# The Factory Pattern

A **factory** here is a narrow, governed pipeline that proposes changes for one
class of task where done-ness is decidable by a command.

It is not a general-purpose autonomous developer, and the distance between those
two ideas is the whole subject of this page. The word carries baggage — it is
worth saying plainly what is being claimed and what is not, before anything
else.

This page is about *whether* to build one and *how to know if it is working*.
For the mechanics, see [Sensors](sensors.md), [Gating with `ynh check`](tutorial/check.md)
and the [Agent Loop](agent.md).

## What a Factory Is Here

Three layers stack up, and only the middle one is a tool you can install.

<svg viewBox="0 0 720 288" role="img" aria-label="Three-layer stack: an acceptance layer that is specific to your repositories, a harness layer that ynh provides, and a runtime layer that provides containment">
<g font-family="IBM Plex Mono, monospace" font-size="10.5" fill="currentColor">

<rect x="14" y="26" width="560" height="66" fill="none" stroke="currentColor" stroke-width="2"/>
<text x="30" y="48" font-size="11.5" font-weight="600">ACCEPTANCE</text>
<text x="30" y="66" font-size="10" opacity=".8">sensor contract &middot; baseline ratchet &middot; the gate &middot; evidence bundle</text>
<text x="30" y="82" font-size="10" opacity=".8">what &ldquo;done&rdquo; means <tspan font-weight="600">in your repositories</tspan></text>
<rect x="586" y="26" width="120" height="66" fill="none" stroke="var(--ynh-external, #93372c)" stroke-width="1.5"/>
<text x="646" y="52" text-anchor="middle" font-size="10.5" font-weight="600" fill="var(--ynh-external, #93372c)">YOURS</text>
<text x="646" y="70" text-anchor="middle" font-size="9" fill="var(--ynh-external, #93372c)" opacity=".85">necessarily</text>

<rect x="14" y="104" width="560" height="66" fill="none" stroke="currentColor" stroke-width="2"/>
<text x="30" y="126" font-size="11.5" font-weight="600">HARNESS</text>
<text x="30" y="144" font-size="10" opacity=".8">guides &middot; focus &middot; profiles &middot; MCP config &middot; distribution &middot; pinning &middot; provenance</text>
<text x="30" y="160" font-size="10" opacity=".8">one definition, many repositories, every supported vendor</text>
<rect x="586" y="104" width="120" height="66" fill="none" stroke="var(--ynh-owns, #1d5a66)" stroke-width="1.5"/>
<text x="646" y="130" text-anchor="middle" font-size="10.5" font-weight="600" fill="var(--ynh-owns, #1d5a66)">ynh</text>
<text x="646" y="148" text-anchor="middle" font-size="9" fill="var(--ynh-owns, #1d5a66)" opacity=".85">this tool</text>

<rect x="14" y="182" width="560" height="76" fill="none" stroke="currentColor" stroke-width="2" opacity=".55"/>
<text x="30" y="204" font-size="11.5" font-weight="600" opacity=".75">RUNTIME</text>
<text x="30" y="222" font-size="10" opacity=".7">model &middot; execution &middot; sandbox &middot; egress &middot; credential scoping</text>
<text x="30" y="238" font-size="10" opacity=".7">queue &middot; scheduling &middot; audit log</text>
<text x="30" y="252" font-size="9.5" opacity=".6">containment is the runtime&rsquo;s, never ynh&rsquo;s &mdash; whoever the runtime is</text>
<rect x="586" y="182" width="120" height="76" fill="none" stroke="currentColor" stroke-dasharray="4 3"/>
<text x="646" y="216" text-anchor="middle" font-size="10.5" font-weight="600">THE OPERATOR&rsquo;S</text>
<text x="646" y="234" text-anchor="middle" font-size="9" opacity=".75">vendor CLI, CI,</text>
<text x="646" y="247" text-anchor="middle" font-size="9" opacity=".75">or a container</text>
</g></svg>

The acceptance layer is the one nobody can hand you. What "done" means in your
repositories is a statement about your code, your debt and your risk appetite,
and it has to be written down locally before any of the rest is worth running.
ynh exists to make that statement declarable, portable and executable; it does
not supply its content.

## The Economics, as a Method

A factory does not remove work from the queue. It removes some fraction of it
and adds a review pass to everything that remains. Whether that trade is worth
making is arithmetic, and you can do it before writing a single sensor.

Let:

- **`h`** — hours to do the work by hand
- **`r`** — hours to review one agent-proposed attempt
- **`y`** — yield, the fraction of attempts that are actually mergeable

Running the factory is worth it when the expected cost is lower than doing it by
hand:

```
r + (1 − y)·h  <  h
```

which reduces to a single break-even yield:

```
y* = r / h
```

Below `y*` you are paying review time for work you then do again anyway.

**Measuring your own inputs.** The equation is general; the numbers never are.

- **`h`** — time a sample of the same task class done by hand. Not estimated,
  timed.
- **`r`** — time reviews of agent-proposed changes honestly, and include the
  attempts you rejected. A review that ends in "no" still cost its full price.
  This is the input most often understated.
- **`y`** — measure it before trusting it, against work whose correct answer is
  already known. That is what [shadow mode](tutorial/shadow-mode.md) is for.

**A worked example — the numbers below are invented, and yours will differ.**
Say a task takes `h = 4` hours by hand, and reviewing one attempt takes
`r = 30` minutes. Then `y* = 0.5 / 4 = 12.5%`. Roughly one attempt in eight has
to be mergeable just to break even. If review instead takes two hours because
the change is sprawling and the evidence is thin, `y*` jumps to 50% — the same
factory, four times harder to justify, purely because review got more expensive.

That is the load-bearing conclusion, and it is safe to state in general:
**the economics live in review time, not model cost.** Model spend is the line
item everyone watches and rarely the one that decides the outcome. Halving `r`
moves `y*` as much as doubling model quality.

## What Actually Reduces Review Time

Since `r` dominates, the useful question is what genuinely shrinks it. This is
the most counter-intuitive part of the pattern, because the obvious answers are
wrong.

Most of what an agent can hand a reviewer is **produced by the system under
review**, and is therefore anti-informative:

- **Sensor results reported by the agent.** The artifact under test can
  influence the instrument. A change that edits a linter's configuration
  alongside the code it lints has adjusted its own examiner.
- **A model's verdict on its own work.** Confidence is not evidence, and it is
  uncorrelated with correctness in exactly the cases that matter.
- **The trajectory.** Thousands of lines of reasoning. Read it and review time
  rises; skip it and it saved nothing. Either way it did not help.

What does work is a **reproducible negative**: a check that demonstrably failed
before the change and demonstrably passes after, produced by a pinned tool
outside the agent's write path. A reviewer verifies it in seconds without
trusting anything the agent said — they re-run it.

Three properties make it trustworthy, and all three are required:

1. **Pinned** — the tool and its version are fixed, so the before and after are
   comparable.
2. **Outside the write path** — the agent cannot modify the checker, its
   configuration, or the baseline it is judged against.
3. **Both ends observed** — a passing check alone proves nothing. The failure
   before the change is what makes the pass meaningful.

This is why the [baseline ratchet](harness-engineering.md#sensor-gate-ratchet-loop)
is refused to the agent: `--update-baseline` is rejected inside an agent run.
An agent that can rewrite the record of what counts as failure can manufacture
its own reproducible negative.

It also turns into a selection criterion. A task that cannot produce a
reproducible negative cannot produce cheap review, and cheap review is the whole
economic case.

## Choosing What to Automate

Domain is the wrong axis. "Automate the security work" and "automate the test
work" are not decisions that predict anything. Two properties do:

- **Is done-ness decidable by a command?** Can a machine, with no judgement,
  say yes or no?
- **How far does one bad merge travel?** If a mistake gets through, what is the
  blast radius?

Only tasks that are decidable **and** low blast radius are viable. Both, not
either.

<svg viewBox="0 0 720 396" role="img" aria-label="Candidate factories plotted against decidability of done-ness and blast radius of one bad merge, showing which are viable">
<g font-family="IBM Plex Mono, monospace" font-size="10.5" fill="currentColor">
<line x1="96" y1="326" x2="676" y2="326" stroke="currentColor" opacity=".5"/>
<line x1="96" y1="40" x2="96" y2="326" stroke="currentColor" opacity=".5"/>
<text x="386" y="362" text-anchor="middle" font-size="11" font-weight="600">DONE-NESS  &rarr;  decidable by a command</text>
<text x="30" y="183" font-size="11" font-weight="600" transform="rotate(-90 30 183)" text-anchor="middle">BLAST RADIUS  &rarr;  small</text>
<text x="112" y="344" font-size="9" opacity=".6">needs judgement</text>
<text x="664" y="344" font-size="9" opacity=".6" text-anchor="end">exit code</text>
<text x="80" y="320" font-size="9" opacity=".6" text-anchor="end" transform="rotate(-90 80 320)">incident-grade</text>

<rect x="96" y="184" width="290" height="142" fill="var(--ynh-external, #93372c)" opacity=".07"/>
<rect x="386" y="40" width="290" height="144" fill="var(--ynh-owns, #1d5a66)" opacity=".09"/>
<text x="241" y="204" text-anchor="middle" font-size="9.5" font-weight="600" fill="var(--ynh-external, #93372c)" opacity=".9">DO NOT BUILD</text>
<text x="531" y="60" text-anchor="middle" font-size="9.5" font-weight="600" fill="var(--ynh-owns, #1d5a66)">VIABLE</text>

<circle cx="600" cy="94" r="5" fill="var(--ynh-owns, #1d5a66)"/>
<text x="612" y="92" font-size="10.5" font-weight="600">Lint-debt paydown</text>
<text x="612" y="105" font-size="9" opacity=".7">linter exit code &middot; worst case is a lint regression</text>
<circle cx="556" cy="136" r="5" fill="var(--ynh-owns, #1d5a66)"/>
<text x="568" y="134" font-size="10.5" font-weight="600">Doc / comment drift</text>
<text x="568" y="147" font-size="9" opacity=".7">no runtime effect at all</text>
<circle cx="494" cy="168" r="5" fill="var(--ynh-owns, #1d5a66)"/>
<text x="506" y="166" font-size="10.5" font-weight="600">Codemod / migration</text>
<text x="506" y="179" font-size="9" opacity=".7">build + tests green &middot; mechanical</text>

<circle cx="470" cy="216" r="5" fill="var(--ynh-caution, #8f6714)"/>
<text x="482" y="214" font-size="10.5" font-weight="600">Test coverage uplift</text>
<text x="482" y="227" font-size="9" opacity=".7">coverage-threshold gaming is a known red flag</text>
<circle cx="430" cy="258" r="5" fill="var(--ynh-external, #93372c)"/>
<text x="442" y="256" font-size="10.5" font-weight="600">Dependency bumps</text>
<text x="442" y="269" font-size="9" opacity=".7">supply-chain radius &middot; unreviewable lockfiles</text>

<circle cx="300" cy="118" r="5" fill="var(--ynh-caution, #8f6714)"/>
<text x="312" y="116" font-size="10.5" font-weight="600">Flaky-test quarantine</text>
<text x="312" y="129" font-size="9" opacity=".7">root cause needs judgement</text>
<circle cx="214" cy="266" r="5" fill="var(--ynh-external, #93372c)"/>
<text x="226" y="264" font-size="10.5" font-weight="600">Vulnerability remediation</text>
<text x="226" y="277" font-size="9" opacity=".7">a plausible patch can close a finding that is still live</text>
<circle cx="150" cy="302" r="5" fill="var(--ynh-external, #93372c)"/>
<text x="162" y="300" font-size="10.5" font-weight="600">Credential rotation</text>
<text x="162" y="313" font-size="9" opacity=".7">effects outside git &middot; irreversible</text>
</g></svg>

**Viable, and dull on purpose.** Lint-debt paydown: the linter's exit code is
the oracle, and the worst outcome of a bad merge is a lint regression. Doc and
comment drift: no runtime effect whatsoever. Mechanical codemods: the build and
the test suite decide, and the change is structurally repetitive.

**Not viable, with reasons.** Coverage uplift looks decidable — coverage is a
number — but the number is gameable, and tests written to move it are a known
failure mode rather than a hypothetical one. Dependency bumps fail on radius:
the diff is a lockfile no reviewer reads line by line, and the supply chain is
an active attack surface [3][4]. Anything incident-grade fails on blast radius
no matter how decidable it looks.

**The uncomfortable part.** The tasks teams most want to automate first —
security fixes, credential rotation — usually fail *both* axes. Security
remediation is not decidable by a command: a patch that satisfies the scanner
can leave the vulnerability reachable, so the check says yes while the finding
is still live [5][6]. And it is maximum blast radius by construction, because
it is what you reach for under time pressure. Credential rotation is worse
still: its effects land outside version control and cannot be reverted with a
revert [7].

Start with the boring end. The value of the first factory is not the work it
does; it is the measurement it produces.

## An Adoption Ladder

Each rung ends in a measurement rather than a date. A rung is complete when you
can show the number, not when the calendar says so.

1. **Declare sensors and gate on them.** Get [`ynh check`](tutorial/check.md)
   running with explicit `tolerance` and a pipeline that branches on its exit
   code. *Measurement: the gate runs on every change and its verdict is
   trusted.*
2. **Take a baseline.** Record the debt that already exists so the gate becomes
   reachable on a real repository. *Measurement: a clean run on unchanged code.*
3. **Measure before trusting.** Run [shadow mode](tutorial/shadow-mode.md)
   against your own history. *Measurement: an observed `y`, with its confidence
   interval stated.*
4. **Run the loop propose-only.** [`ynh agent run`](tutorial/agent-loop.md)
   without `--auto-commit`, output routed to code owners as a proposal. Never
   auto-merge. *Measurement: an observed `r` from real reviews.*
5. **Widen only on evidence.** A second task class is justified by the first
   one's numbers, not by its promise.

Rungs three and four are the ones under pressure to skip. They are the two that
produce the only numbers that matter.

## Stop Conditions

Write these down **before** starting, in a place that requires deliberate effort
to change. Fix five things:

- A **yield floor** below which the effort stops
- A **review-time ceiling** above which the evidence bundle is not working
- An **escaped-defect limit** — issues that reached production through the
  factory
- A **rubber-stamping detector** — review durations trending toward zero mean
  the reviews stopped happening, not that quality improved
- A **queue-divergence auto-pause** — proposals accumulating faster than they
  are reviewed

The thresholds are yours. They derive from your cost base and publishing
somebody else's would be worse than useless.

Why write them first? Because **they are renegotiated under pressure by people
who want the programme to succeed.** Not by cynics or saboteurs — by its
advocates, in good faith, at the exact moment the evidence turns against it.
A number agreed in advance is the only thing that survives that conversation.

## Governance

Everything above measures whether a factory works. Governance is what makes a
change it produced defensible six months later, when whoever merged it has moved
on and somebody is asking how it got in.

The split is the one [§The Economics](#the-economics-as-a-method) already drew.
The practices below transfer; the policy does not. Who may sign, what the
approval chain looks like, how long a trajectory is kept and what any of it has
to satisfy are yours to decide. **This is not compliance guidance and could not
be** — a tool's documentation telling you what discharges your obligations would
be worse than silence. ynh produces evidence. Whether that evidence is
sufficient is your determination, not this page's.

**Attribution has to survive the merge.** A machine-proposed change should
durably record that it was one, and the mechanic matters more than the
intention. A squash-merge collapses the branch and leaves PR comments behind, so
attribution that lives in a comment does not survive contact with the merge
button. A commit trailer does, because it is part of the message the squash
keeps.

This is not ceremony. Attribution is the join key for everything in
[§Stop Conditions](#stop-conditions). An escaped-defect limit is unenforceable
if you cannot say which escaped defects came from the factory, and you cannot
say that if the provenance was discarded at merge time. Choose the mechanism
before the first merge rather than after the first incident.

**A named human owns the merge.** The tool proposes; approving is a human act
that happens outside it, and [§Where ynh Stops](#where-ynh-stops) is the
architectural form of the same sentence. *The agent did it* is not an answer to
who approved a change, and a review that cannot be attributed to a person is not
a review [10]. This is also what the rubber-stamping detector is watching for:
in the record, sign-off that has gone reflexive is indistinguishable from
sign-off that never happened.

**Evidence a reviewer can check without trusting the agent.**
[§What Actually Reduces Review Time](#what-actually-reduces-review-time) rejects
anything the system under review produced about itself. Audit evidence answers
to the same test, which leaves four things worth having:

- a machine-readable result for each run, so the outcome is a record rather than scrollback
- the trajectory — what the agent did, as distinct from its account of what it did
- the exact harness and image the run executed against, so it can be repeated rather than merely described
- a baseline the agent cannot write, so forgiven findings cannot be forged

ynh gives you two of those directly. The trajectory is written by
[`--emit-jsonl`](tutorial/agent-loop.md#reading-a-trajectory), and the baseline
is unforgeable by construction: a run that alters what it is judged against
fails on that basis instead of passing. The other two are yours to supply, for
the same reason [containment
is](harness-engineering.md#ynh-does-not-own-containment) — ynh executes against
whatever harness and image you pin, and cannot attest that you pinned anything.
A factory whose runs are not pinned generates evidence for a run that nobody can
reproduce, which is a description rather than a record.

**Downstream disclosure has no settled answer, and inventing one here would be
worse than saying so.** If you publish a library or run a service, are consumers
told that a change was agent-authored? The case for is that it is material to
someone deciding how much to trust a dependency, and that discovering it later
costs more than disclosing it now. The case against is that it singles out one
authorship route among many when the obligation that actually matters —
the change is correct, reviewed and owned — is identical either way. The field
has not converged, and this page will not pretend it has. Decide, write down why
you decided it, and revisit it when the norm settles.

**The audit record is also a liability.** Trajectories can contain credentials,
customer data and source, which has two consequences. Redaction is a
precondition for retaining them at all, and ynh does not redact — whatever
writes and stores them is where that has to happen [4]. And the retention period
is a real trade rather than an administrative default: the longer they are kept,
the more can be answered later, and the larger the thing you are holding when
something goes wrong. Pick the period deliberately, and write it down alongside
your stop conditions rather than discovering it during an incident.

## Where ynh Stops

The [containment doctrine](harness-engineering.md#ynh-does-not-own-containment)
applies with full force here. ynh declares, executes and packages. It does not
enforce containment, and it never claims to.

For a factory this is not a caveat, it is a **prerequisite**. A loop that runs
unattended, proposes changes and touches credentials needs a container and an
egress policy that the operator owns [8]. That is not optional hardening to be
scheduled later — an unattended agent without it is an unattended agent with
your credentials on an open network. `--sandbox` may hand the request to a
sandbox you installed; where a backend cannot honour it, ynh fails rather than
proceeding unsandboxed. A containment control that cannot be applied is an
error, not a warning.

One property is worth relying on when you build the pipeline around it:
**environment parity**. There is no TTY-dependent behaviour, exactly one
CI-conditional branch, and every environment variable is a documented fallback
for an explicit flag. A run is fully described by its arguments — so a failed CI
run reproduces on a laptop by copying the command, and the local reproduction is
the same run, not an approximation of it.

## References

1. Martin Fowler — [Harness engineering for coding agent users](https://martinfowler.com/articles/harness-engineering.html)
2. Thoughtworks — [Technology Radar: techniques](https://www.thoughtworks.com/radar/techniques)
3. Cloud Security Alliance — [Slopsquatting and the AI supply chain](https://labs.cloudsecurityalliance.org/research/csa-research-note-slopsquatting-ai-supply-chain-20260419-csa/)
4. Cloud Security Alliance — [AI coding agent CI prompt injection](https://labs.cloudsecurityalliance.org/research/csa-research-note-ai-coding-agent-ci-prompt-injection-202608/)
5. GitHub — [Responsible use of Copilot Autofix for code scanning](https://docs.github.com/en/code-security/code-scanning/managing-code-scanning-alerts/responsible-use-autofix-code-scanning)
6. Google DeepMind — [CodeMender: an AI agent for code security](https://deepmind.google/blog/introducing-codemender-an-ai-agent-for-code-security/)
7. Oasis Security — [Cloudflare's outage and the need for safer rotations](https://www.oasis.security/blog/dont-look-back-in-anger-how-cloudflares-outage-highlights-the-need-for-safer-rotations)
8. Docker — [How the coding agent sandboxes team uses a fleet of agents](https://www.docker.com/blog/a-virtual-agent-team-at-docker-how-the-coding-agent-sandboxes-team-uses-a-fleet-of-agents-to-ship-faster/)
9. GitClear — [The AI code quality and maintainability gap](https://www.gitclear.com/the_ai_code_quality_maintainability_gap)
10. GitHub — [Agent pull requests are everywhere: how to review them](https://github.blog/ai-and-ml/generative-ai/agent-pull-requests-are-everywhere-heres-how-to-review-them/)
