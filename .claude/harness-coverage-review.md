# Harness coverage review — 2026-08-30

Does the harness ynh ships actually help someone adopt, implement and use ynh?

Short answer: **it helps with the first twenty minutes and then stops.** The
tutorials teach the product. The harness does not.

This is a snapshot with a method, not a verdict to be taken on trust. Re-run the
numbers before acting on them.

## Method

A command counts as "covered" if any shipped skill, agent, rule, or focus prompt
names it (`ynh <cmd>` / `ynd <cmd>`). That is a generous bar — a mention is not
guidance — and the results are still stark.

```bash
ynh help | ...   # 29 commands, excluding help/version
ynd help | ...   # 13 commands, excluding help/version
```

## The numbers

| Surface | ynh commands covered |
|---|---|
| `docs/tutorial/` (22 files) | **25 of 29** |
| Shipped harness (skills, agents, rules, focuses) | **2 of 29** |

The two: `ynh install` and `ynh vendors`.

Uncovered: `agent`, `backend`, `check`, `delegate`, `doctor`, `focus`, `fork`,
`hook`, `image`, `include`, `info`, `init`, `installed`, `ls`, `mcp`, `paths`,
`profile`, `prune`, `registry`, `run`, `schema`, `search`, `sensors`, `sources`,
`status`, `uninstall`, `update`.

ynd fares better — 7 of 13 (`compress`, `create`, `inspect`, `lint`, `migrate`,
`preview`, `validate`), missing `compose`, `diff`, `export`, `fmt`,
`marketplace`, `validate-output`.

Every feature added recently — sensors, check, the agent loop, images, focus,
profiles — has full tutorial coverage and zero harness coverage. The gap is
widening, because tutorials are written as part of shipping a feature and
harness artifacts are not.

## The sharper problem: the guide agent could not work

Worse than thin coverage was an agent that could not function in the case it
ships for.

`agents/ynh-guide.md` carried a table of doc paths (`docs/harnesses.md`,
`.github/CONTRIBUTING.md`, `internal/resolver/`, …) and the instruction *"never
from memory alone"*. `AGENTS.md` said to *"answer from the docs and code in this
harness"*.

An installed harness is 13 files:

```
.claude-plugin/plugin.json
.claude/agents/{ynd-artifact-reviewer,ynh-guide}.md
.claude/rules/artifact-authoring.md
.claude/skills/{ynd-compress,ynd-inspect,ynh-create-harness,ynh-team-setup}/…
CLAUDE.md
```

No docs. No Go source. The agent's tools were `Read, Grep, Glob` — scoped to
what is there. So the one agent whose job is answering "how does ynh work" was
told to read files that are not present, and forbidden to answer without them.

It worked for exactly one person: a contributor sitting in the ynh checkout,
where those paths resolve. The harness was authored for the repo and shipped as
if it were for users.

Its doc table had also gone stale: it referenced 8 doc files while `docs/` holds
20+, omitting `agent.md`, `focus.md`, `profiles.md`, `sensors.md`, `hooks.md`,
`mcp.md`, `marketplace.md`, `namespacing.md`, `migration.md` and five more.

**Fixed in this PR.** The agent now establishes which context it is in before
reading anything, and falls back to the CLI — which describes itself accurately
for the version actually installed — plus the published docs. `AGENTS.md` no
longer claims to carry docs it does not carry.

## What is genuinely good

Worth saying, because the list above is one-sided:

- **Focuses are the best adoption surface here.** Six named entry points, each
  pairing a prompt with a profile. `learn` walks a newcomer through the concepts;
  `contribute` and `release` switch to the dev profile. This is the right shape
  and it is under-advertised — `AGENTS.md` never mentioned it.
- **`ynh-create-harness` is a genuinely good wizard** — one question at a time,
  concrete scaffolding, real next steps.
- **The vendor-adapters reference set** (now four files) is unusually thorough:
  hand-tested findings with negative controls, not doc summaries.
- **Sensors** (`fmt`, `vet`, `check`) are declared and working.

## Recommendations, in order

1. **Cover `run`, `ls`, `info`, `doctor`, `update`.** The daily loop. A user who
   installed a harness and hit a problem has nothing. `doctor` especially — it
   exists precisely for the moment someone is stuck, and nothing points at it.
2. **A `ynh-troubleshoot` skill.** The commands exist (`doctor`, `validate`,
   `preview`, `info`, `status`, `paths`); what is missing is the decision tree
   connecting a symptom to the right one.
3. **Cover the composition features** — `include`, `profile`, `focus`,
   `delegate`. These are what distinguish ynh from copying a folder around, and
   they are exactly what a new user will not discover unaided.
4. **Make harness coverage part of shipping a feature.** The tutorial/harness
   gap widened because tutorials are part of the definition of done and harness
   artifacts are not. A checklist item in `CONTRIBUTING.md` costs little; the
   `scripts/vendor-parity.sh` precedent shows the mechanical half can be
   enforced.
5. **Ship `commands/`.** The format supports it, `ynd create` scaffolds it, and
   the shipped harness has none — so nothing demonstrates the artifact type to a
   user reading their own installed harness.

Items 1–3 are roughly one skill each. Item 4 is the one that stops the gap
reopening.

## Deliberately not done here

Writing five new skills inside a review PR would make it unreviewable, and the
content decisions above deserve their own discussion. This PR fixes what was
actively wrong — the guide agent and the front door — and records the rest.
