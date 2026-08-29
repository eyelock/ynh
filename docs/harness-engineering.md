# Harness Engineering

YNH is a **harness template manager** — it creates, composes, distributes, and installs the guide layer of coding harnesses across multiple AI coding vendors.

## What Is a Coding Harness?

A coding harness is everything in an AI coding agent except the model itself. The term was formalized by Martin Fowler [1] and adopted by OpenAI [2] and Anthropic [3][4]. The core equation:

> **Agent = Model + Harness**

Fowler describes two control types:

- **Feedforward Controls (Guides)**: Proactive steering *before* the agent acts — architecture docs, coding conventions, skills, rules, agent definitions
- **Feedback Controls (Sensors)**: Post-generation observation and correction — linters, tests, review agents, structural validation

And three regulation categories:

1. **Maintainability** — internal code quality (linters, coverage, style)
2. **Architecture Fitness** — structural constraints (dependency boundaries, performance budgets)
3. **Behaviour** — functional correctness (tests, mutation testing, specs)

## Sensor, Gate, Ratchet, Loop

Four terms carry most of the weight in the rest of these docs, and they collapse
into one another easily. A reader who has finished [Sensors](sensors.md) and
[Agent Loop](agent.md) can still be unsure how a gate differs from a ratchet.

| Term | Definition | Implementation |
|------|------------|----------------|
| **Sensor** | One declared observation — a linter, a test suite, a build. Emits raw signal, passes no judgement | `Sensor{Category, Role, Tolerance, Source, Output}` |
| **Gate** | Runs the sensors and decides pass or fail. What a pipeline blocks on | `ynh check` |
| **Ratchet** | The gate's record of debt that already existed, so it fails only on *new* findings | `internal/baseline/` |
| **Loop** | Prompts a model, lets it act, re-observes, halts on convergence or budget. **Consumes** a ratchet; it is not one | `internal/agent/` |

<svg viewBox="0 0 720 296" role="img" aria-label="Layer diagram showing model plus harness, with guides, sensors and loop owned by ynh and containment owned by the runtime">
<defs><marker id="ar_B" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse">
<polygon points="0,1 9,5 0,9" fill="currentColor"/></marker></defs>
<g font-family="IBM Plex Mono, monospace" font-size="11" fill="currentColor">
<text x="16" y="20" font-size="10" opacity=".6" letter-spacing="1">AGENT  =  MODEL  +  HARNESS</text>
<rect x="16" y="34" width="140" height="222" fill="none" stroke="currentColor" opacity=".45" stroke-dasharray="3 3"/>
<text x="86" y="58" text-anchor="middle" font-size="12" font-weight="600">MODEL</text>
<text x="86" y="76" text-anchor="middle" font-size="10" opacity=".6">vendor supplied</text>
<rect x="196" y="34" width="508" height="222" fill="none" stroke="currentColor" stroke-width="1.5"/>
<text x="450" y="58" text-anchor="middle" font-size="12" font-weight="600">HARNESS</text>
<rect x="216" y="74" width="228" height="70" fill="none" stroke="currentColor" opacity=".55"/>
<text x="330" y="94" text-anchor="middle" font-size="11" font-weight="600">GUIDES &middot; feedforward</text>
<text x="330" y="112" text-anchor="middle" font-size="10" opacity=".75">skills &middot; rules &middot; agents &middot; MCP</text>
<text x="330" y="132" text-anchor="middle" font-size="9.5" fill="var(--ynh-owns, #1d5a66)" font-weight="600">ynh OWNS</text>
<rect x="460" y="74" width="228" height="70" fill="none" stroke="currentColor" opacity=".55"/>
<text x="574" y="94" text-anchor="middle" font-size="11" font-weight="600">SENSORS &middot; feedback</text>
<text x="574" y="112" text-anchor="middle" font-size="10" opacity=".75">build &middot; lint &middot; test &middot; scan</text>
<text x="574" y="132" text-anchor="middle" font-size="9.5" fill="var(--ynh-owns, #1d5a66)" font-weight="600">ynh DECLARES + GATES</text>
<line x1="330" y1="144" x2="330" y2="170" stroke="currentColor" marker-end="url(#ar_B)"/>
<line x1="574" y1="170" x2="574" y2="144" stroke="currentColor" marker-end="url(#ar_B)"/>
<text x="339" y="162" font-size="9.5" opacity=".7">steers</text>
<text x="583" y="162" font-size="9.5" opacity=".7">observes</text>
<rect x="216" y="174" width="472" height="40" fill="none" stroke="currentColor" opacity=".55"/>
<text x="452" y="192" text-anchor="middle" font-size="11" font-weight="600">LOOP &middot; re-prompt, converge, halt</text>
<text x="452" y="207" text-anchor="middle" font-size="9.5" fill="var(--ynh-owns, #1d5a66)" font-weight="600">ynh DRIVES</text>
<rect x="216" y="224" width="472" height="22" fill="none" stroke="var(--ynh-external, #93372c)" stroke-dasharray="4 3"/>
<text x="452" y="239" text-anchor="middle" font-size="10" fill="var(--ynh-external, #93372c)">CONTAINMENT &middot; sandbox, egress, credentials &mdash; <tspan font-weight="600">the runtime&rsquo;s, never ynh&rsquo;s</tspan></text>
<line x1="156" y1="144" x2="194" y2="144" stroke="currentColor" marker-end="url(#ar_B)"/>
</g></svg>

**How the ratchet works.** Each line of sensor output is reduced to a
fingerprint: paths are made relative to the repository root, line and column
positions are collapsed to a placeholder, and what remains is hashed. Comparing
a run against the record sorts findings into *new* (fails the build), *known*
(forgiven) and *fixed* (debt paid off). Collapsing the positions is what makes
it a ratchet rather than a tripwire — a finding does not become a different
finding because someone inserted a line above it.

**Why it is load-bearing.** Without a ratchet, "done" means zero findings. Every
repository worth gating already carries thousands, so every run fails on
arrival. The ratchet converts an unreachable absolute into a reachable relative
one: *no findings that were not already there.*

None of this is a ynh invention — ESLint and Checkstyle keep baseline files,
SonarQube gates on "new code". What ynh adds is that a single recorded set is
consumed by both the gate and the loop, so an agent is held to exactly the
standard the pipeline enforces.

## Where YNH Fits

YNH covers the **guide layer** thoroughly and **declares** the sensor layer:

| Harness Concept | YNH Implementation |
|-----------------|-------------------|
| Feedforward Guides | Skills, rules, instructions, agents |
| Feedback Sensors (declared) | `sensors` block — observation surfaces a loop driver consumes |
| Harness Templates | A harness bundles guides for a use case |
| Vendor Abstraction | Single harness → Claude/Cursor/Codex layouts |
| Composition | `includes` (external Git) + `delegates_to` (subagents) |
| Distribution | Registry, marketplace, export, Docker images |
| Agent Skills Standard | Native [agentskills.io](https://agentskills.io) support |
| Progressive Disclosure | Skills use catalog → instructions → resources loading |

YNH declares both guides (artifacts) and observation surfaces (sensors), executes an individual sensor via `ynh sensors run`, and runs the whole declared set as a gate via `ynh check`. What it does **not** own is iteration: deciding when to re-prompt an agent, what counts as convergence, and when to stop belongs to a loop driver (CI, an orchestrator, a custom tool). See [Sensors](sensors.md) for the full contract.

### Hooks: Bridge to Feedback Sensors

While ynh focuses on the guide layer, [hooks](hooks.md) provide the bridge to feedback sensors. A harness declares canonical hook events (`before_tool`, `after_tool`, `before_prompt`, `on_stop`, `on_session_start`) in `.ynh-plugin/plugin.json`, and ynh translates them into vendor-native hook config at assembly time. The hook scripts themselves — linters, validators, safety checks — live outside the harness. This keeps the boundary clean: ynh declares *when* to check, existing tools provide *what* to check.

### MCP Servers: Tool Registry

[MCP server declarations](mcp.md) let a harness specify the tools an agent needs — databases, APIs, documentation servers. Rather than requiring each developer to manually configure MCP per vendor, the harness declares its tool dependencies once and ynh generates the correct config for Claude (`.mcp.json`), Cursor (`.cursor/mcp.json`), or Codex (`.codex/config.toml`).

### Developer Preview and Diff

The `ynd preview` and `ynd diff` commands support rapid harness iteration. Preview assembles a harness for a specific vendor and shows the output without installing. Diff compares the assembled output across two or more vendors, highlighting structural differences in hook config, MCP config, and artifact layout. This implements the principle that "every harness component encodes an assumption about what the model can't do" — preview and diff make it easy to verify and evolve those assumptions.

## Key Industry Principles

**"Give agents a map, not a 1,000-page manual"** (OpenAI [2]) — AGENTS.md works best as a short entry point with pointers to structured docs. YNH's multi-artifact architecture (skills + rules + agents + commands + instructions) implements progressive disclosure by design.

**"Enforce invariants, not implementations"** (OpenAI [2]) — Define boundaries, allow autonomy. Harness rules should constrain *what matters*, not micromanage *how*.

**"Anything the agent can't access in-context doesn't exist"** (OpenAI [2]) — Validates bundling everything into the harness template. If it's not in the assembled output, the agent won't use it.

**"Weak results are usually harness problems, not model problems"** (HumanLayer [5]) — Same model, different harness, wildly different outcomes. Anthropic [4] demonstrated this with a three-agent system where harness design was the differentiator.

**"Every harness component encodes an assumption about what the model can't do"** (Anthropic [4]) — As models improve, harness components become unnecessary. This justifies rapid iteration tools like `ynd preview` and `ynd diff`.

### Lineage: this is manufacturing quality control, rebuilt

The mechanisms are not new, and the names for them are older than software.

- **Jidoka** — a machine that halts itself the moment it produces a defect,
  rather than making a thousand more. That is the gate stopping the pipeline on
  a *new* finding, and the reason a ratchet distinguishes new from known: a line
  that halts on pre-existing debt halts permanently and gets switched off.
- **Poka-yoke** — mistake-proofing, so the wrong action is impossible rather
  than merely discouraged. That is a baseline the agent cannot write.
  `--update-baseline` is refused inside an agent run, so the standard cannot be
  lowered by the thing being measured.
- **Statistical process control** — measure the variation before trying to
  automate it away. That is measuring yield against known-good outcomes before
  scaling a factory, rather than after.

The sequencing is the honest part of the analogy. Lights-out plants arrived
**last** — decades after the measurement disciplines they depend on, not
alongside them. Mistake-proofing handles defects one at a time; process
variation is a statistical property and needs measurement before it can be
managed. Any programme that reaches for the autonomous end first is repeating an
experiment whose result is already documented, in another industry, at
considerable expense.

## Harness Components

A YNH harness template can include:

| Component | Purpose | Fowler Category |
|-----------|---------|----------------|
| Skills | Reusable capabilities (Agent Skills format) | Feedforward Guide |
| Rules | Constraints and conventions | Feedforward Guide |
| Agents | Specialist sub-agents | Feedforward Guide |
| Commands | Slash commands / workflows | Feedforward Guide |
| Instructions | Project-level context (AGENTS.md) | Feedforward Guide |
| Hooks | Vendor hook declarations | Bridge to Sensors |
| Sensors | Observation surfaces (declared, not orchestrated) | Feedback Sensor (declared) |
| MCP Servers | Tool dependencies | Tool Registry |

## Design Stance — Declarative-First, Vendor-Neutral

Two architectural choices constrain everything else and explain why ynh's surface looks the way it does.

**Declarative-first.** A ynh harness is configuration, not code. The manifest emits artifacts and declarations; it never executes the agent loop, mutates session state at runtime, or registers behaviour dynamically. Anything that requires a live runtime — dynamic tool registration, in-process event subscription, mid-session mutation, custom UI — belongs to the runtime that runs the agent, not to the harness.

**Vendor-neutral.** The feature surface is bounded by what is portable across every supported vendor. Hook events, MCP semantics, assembly behaviour, and sensor contracts are all chosen to translate cleanly into each target. Restraint here — a small canonical hook vocabulary instead of a rich one, declared sensors instead of executable observers — is the cost of working everywhere.

**Corollary: structure where prose would suffice in a single runtime.** A runtime that owns its agent loop can drive feedback through prose instructions alone — the runtime obeys its own conventions. ynh cannot. The loop is run by an external consumer (CI, an orchestrator, another tool) that has no way to discover what the harness exposes unless it is declared. Sensors formalise what would otherwise live as informal prose; that formality is the price of portability.

**What this rules out.** Programmatic extensions in the manifest. Runtime hooks beyond the canonical four. Live mutation of artifacts after assembly. These are not gaps to be filled; they are deliberate refusals that protect the portability guarantee.

**What changed, and why.** This section previously also ruled out *any* pass/fail policy in ynh, on the grounds that a verdict is loop-driver business. That was too strict, and the cost was that nothing consumed the sensor contract at all. `ynh check` now owns the thinnest possible policy — a command sensor passes when it exits 0, and `tolerance` says whether a failure gates — because a declared observation surface that cannot answer "did it pass" is not usable by any consumer without every consumer reinventing the same answer. Everything above that line (thresholds, severity, convergence, when to iterate) is still refused. Declarative-first survives: ynh executes declarations, it does not become a runtime.

**What this rules in.** A small, stable, machine-readable surface that any runtime — interactive vendor CLI, headless CI job, custom orchestrator — can consume in the same way. The harness encodes intent; runtimes provide capability.

## ynh does not own containment

A line worth stating plainly, because every request of a particular shape
pushes on it:

> ynh executes declarations and reports verdicts. **ynh does not own
> containment.** Its answer to "is this safe" is *"run me inside a container you
> configured"* — never *"ynh provides isolation."*

This decides a whole class of question the same way each time. `--sandbox` may
hand a request to a sandbox the operator installed; it may not claim to *be*
one, and where a backend cannot honour it, ynh must fail rather than proceed
unsandboxed. Credential scoping is **declared** in the manifest — which
variables reach the agent — while the process boundary that makes the
declaration meaningful is the container's.

The reason to write it down is that the alternative is attractive and wrong.
A harness manager that ships isolation has to be correct on every platform and
every vendor simultaneously, and the failure mode is silent: a control that is
declared, believed, and absent. Refusing the responsibility means ynh can be
honest about what it does not do, which is what makes the parts it *does* do
trustworthy.

The corollary is that ynh must never degrade quietly. A containment control
that cannot be applied is an error, not a warning.

## Standards Alignment

| Standard | YNH Status |
|----------|-----------|
| [Agent Skills](https://agentskills.io) (SKILL.md) | Native format |
| [AGENTS.md](https://www.linuxfoundation.org/press/linux-foundation-announces-the-formation-of-the-agentic-ai-foundation) (Linux Foundation) | Input + export |
| CLAUDE.md (Anthropic) | Generated on assembly |
| .cursorrules (Cursor) | Generated on assembly |
| MCP (Model Context Protocol) | Declared in harness, assembled per vendor |

## References

1. Martin Fowler — [Harness engineering for coding agent users](https://martinfowler.com/articles/harness-engineering.html)
2. OpenAI — [Harness engineering: leveraging Codex in an agent-first world](https://openai.com/index/harness-engineering/)
3. Anthropic — [Effective harnesses for long-running agents](https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents) (Nov 2025)
4. Anthropic — [Harness design for long-running application development](https://www.anthropic.com/engineering/harness-design-long-running-apps) (Mar 2026)
5. HumanLayer — [Skill Issue: Harness Engineering for Coding Agents](https://www.humanlayer.dev/blog/skill-issue-harness-engineering-for-coding-agents)
