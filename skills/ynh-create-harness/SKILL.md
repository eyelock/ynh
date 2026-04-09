---
name: ynh-create-harness
description: Interactive wizard to create a ynh harness from scratch. Walks through naming, vendor selection, artifact scaffolding, and installation.
---

# Create a Harness

You are guiding a user through creating their first ynh (ynh) harness. Follow this workflow step by step, asking one question at a time.

## Before you start

Read these references to understand the current formats and conventions:

1. Read `references/harness-format.md` for manifest syntax, directory structure, and install/run commands
2. Read `references/artifact-formats.md` for skill, agent, rule, and command formats

Also read the working examples in `testdata/sample-harness/` to see realistic artifacts:
- `testdata/sample-harness/.harness.json`
- `testdata/sample-harness/skills/hello/SKILL.md`
- `testdata/sample-harness/agents/code-reviewer.md`
- `testdata/sample-harness/rules/be-concise.md`
- `testdata/sample-harness/commands/check.md`

## Step 1: Harness name

Ask the user what they want to name their harness. Explain that this becomes the command they type to launch it (e.g., if they name it `david`, they'll run `david` to start a session).

The name should be lowercase, short, and memorable. It becomes a shell command.

## Step 2: Output directory

Ask where to create the harness directory. Suggest a sensible default like `~/harnesses/<name>` or a sibling directory to wherever they're working. Let them choose.

## Step 3: Default vendor

Ask which AI vendor they want as the default. Run `ynh vendors` (or read the output of `internal/vendor/` adapters) to show what's available. Currently: claude, codex, cursor.

Explain they can always override with `-v` at runtime.

## Step 4: Starter artifacts

Ask which artifact types they want scaffolded. Offer these options:

- **Skills** - Reusable capabilities (e.g., code review, commit messages)
- **Agents** - Specialists Claude can delegate to (e.g., security reviewer)
- **Rules** - Persistent context loaded every session (e.g., "always write tests")
- **Commands** - Reusable actions (e.g., "run CI checks")

They can pick any combination, or start with none and add later.

## Step 5: Generate the harness

Create the directory structure based on their choices:

```
<output-dir>/
├── .harness.json
├── AGENTS.md            (optional - read natively by most vendors; ynh shims Claude via @-import)
├── skills/              (if selected)
│   └── <example>/
│       └── SKILL.md
├── agents/              (if selected)
│   └── <example>.md
├── rules/               (if selected)
│   └── <example>.md
└── commands/            (if selected)
    └── <example>.md
```

For `.harness.json`:

```json
{
  "$schema": "https://eyelock.github.io/ynh/schema/harness.schema.json",
  "name": "<their-name>",
  "version": "0.1.0",
  "description": "<their description>",
  "default_vendor": "<their-vendor>"
}
```

For each artifact type selected, generate a starter example with realistic content (not lorem ipsum). Use the testdata examples as reference for format, but make the content relevant to the user's context if they mentioned what they work on.

**Important:** Follow the exact formats from `references/artifact-formats.md`:
- Skills: directory with `SKILL.md` containing YAML frontmatter (`name`, `description`)
- Agents: markdown file with YAML frontmatter (`name`, `description`, `tools`)
- Rules: plain markdown file
- Commands: markdown file with instructions

## Step 6: Install and test

Show them how to install:

```bash
ynh install <output-dir>
```

If the harness lives inside a monorepo, use `--path`:

```bash
ynh install <repo-url> --path <subdir>
```

Offer to run this command for them. Then show how to use it:

```bash
<name>                          # interactive session
<name> "hello, introduce yourself"   # quick test
```

If `ynh` isn't on their PATH, remind them about the build step (`make build`) and PATH setup from `references/harness-format.md`.

## Step 7: Next steps

After the harness is working, mention:

1. **Add external skills** - They can pull skills from any Git repo by adding `includes` to `.harness.json`. See `references/harness-format.md` for the syntax.
2. **Team setup** - When ready, they can use `/ynh-team-setup` to create a team harness with delegation.
3. **Private repos** - If they need private Git repos, SSH URLs (`git@github.com:...`) are recommended. ynh delegates to the local `git` binary - if `git clone` works, ynh works.
