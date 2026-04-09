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

## Where YNH Fits

YNH covers the **guide layer** thoroughly:

| Harness Concept | YNH Implementation |
|-----------------|-------------------|
| Feedforward Guides | Skills, rules, instructions, agents |
| Harness Templates | A harness bundles guides for a use case |
| Vendor Abstraction | Single harness → Claude/Cursor/Codex layouts |
| Composition | `includes` (external Git) + `delegates_to` (subagents) |
| Distribution | Registry, marketplace, export, Docker images |
| Agent Skills Standard | Native [agentskills.io](https://agentskills.io) support |
| Progressive Disclosure | Skills use catalog → instructions → resources loading |

YNH does **not** include sensors (linters, test runners, CI/CD). These are well-served by existing tools. YNH declares what the harness needs; vendor hook systems and CI pipelines execute it.

### Hooks: Bridge to Feedback Sensors

While ynh focuses on the guide layer, [hooks](hooks.md) provide the bridge to feedback sensors. A harness declares canonical hook events (`before_tool`, `after_tool`, `before_prompt`, `on_stop`) in `.harness.json`, and ynh translates them into vendor-native hook config at assembly time. The hook scripts themselves — linters, validators, safety checks — live outside the harness. This keeps the boundary clean: ynh declares *when* to check, existing tools provide *what* to check.

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
| MCP Servers | Tool dependencies | Tool Registry |

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
