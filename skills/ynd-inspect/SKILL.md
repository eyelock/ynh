---
name: ynd-inspect
description: Guided walkthrough for using ynd inspect to bootstrap a project's AI skills and agents from its codebase signals.
---

# Inspect a Project

You are guiding a user through using `ynd inspect` to analyze their codebase and generate tailored skills and agents.

## Before you start

Read these references to understand what inspect does and the artifact formats it generates:

- `references/inspect-workflow.md` - How inspect works, output directories, vendor-specific paths
- `references/beyond-artifacts.md` - Turning the same signals into profiles, focuses and sensors, which inspect does not propose

## Step 1: Check prerequisites

Confirm the user has:

1. `ynd` built and on PATH (or they know the path to the binary)
2. An LLM CLI installed — `claude`, `codex`, `copilot`, or Cursor's `agent` (run `ynh vendors` to see which are on PATH)
3. A project directory with recognizable files (go.mod, package.json, Makefile, etc.)

If they're missing the LLM CLI, explain that inspect needs one to analyze the codebase. Point them to installation docs.

## Step 2: Choose the target project

Ask which project they want to inspect. They should `cd` into the project root. Explain that inspect looks for signal files (build configs, test configs, CI files, etc.) to understand the project.

## Step 3: First run — interactive mode

Suggest starting with interactive mode to review the analysis:

```bash
ynd inspect
```

Walk them through each step:

1. **Project Understanding** — the LLM characterizes the project. They can refine if it got something wrong.
2. **Existing Artifacts** — if skills/agents already exist, they can update or skip each one.
3. **New Proposals** — suggested skills and agents. They can walk through each one individually.

## Step 4: Review the proposals

Help them evaluate each proposal:

- Is it specific to this project's stack, or too generic?
- Does it overlap with something they already have?
- Would they actually use this workflow regularly?

Encourage them to skip proposals that feel generic. The best skills automate workflows they already do manually.

## Step 5: Choose output location

Explain the output options:

- **Default**: artifacts go to `.{vendor}/` (e.g., `.claude/skills/`, `.claude/agents/`). This is where the vendor CLI looks for them.
- **`-o .`**: write to project root (`skills/`, `agents/`). Use this if the project itself is a harness/plugin.
- **`-o /path`**: write to any custom directory.

## Step 6: Generate

Once they're happy with the proposals, generate:

```bash
# Interactive — review each one
ynd inspect

# Or auto-generate all at once
ynd inspect -y
```

## Step 7: Validate the output

After generation, validate and lint the generated artifacts:

```bash
ynd lint .claude/skills/ .claude/agents/
```

Check that frontmatter is correct and the content is specific to their project.

## Step 8: Go beyond skills and agents

**`ynd inspect` proposes skills and agents only.** That answers what the
assistant can *do*, not how the harness is configured or what the project should
*observe*. The same signals inspect just found imply the rest.

Read `references/beyond-artifacts.md` and work through it with them:

- **Focuses** first — cheapest and highest value. What did they do three times
  last week? That is a focus. `ynh focus add <harness> <name> "<prompt>"`.
- **Profiles** only where they genuinely work differently. Do not invent one per
  environment because environments exist; an unused profile is a cost with no
  reader.
- **Sensors** last, because they change what gates a merge. The strongest move
  is that **whatever CI already runs is a sensor waiting to be declared** — a
  command the team already trusts. Open the CI workflow and read the steps.

Hand off to the `ynh-sensors` skill for the sensor work itself: the vocabulary
is a closed enum, a dirty repo needs a baseline, `files` sensors need
`observes`, and blocking sensors need calibrating. Nothing should be `blocking`
on the first pass.

## Step 9: Iterate

Explain that inspect is meant to be run periodically as the project evolves. New dependencies, new test frameworks, or new CI pipelines will produce different suggestions. They can re-run and update existing artifacts.
