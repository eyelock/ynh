---
name: ynd-compress
description: Workflow for compressing prompt and instruction files using LLM-powered techniques, with backup management and restore.
---

# Compress Artifacts

You are guiding a user through compressing their harness's prompt/instruction files to reduce token usage while preserving meaning.

## When to use

Use after authoring or updating skills, agents, rules, or instructions. Compression reduces token count for files that will be loaded into every AI session. Particularly valuable for verbose instructions or detailed skills.

## Step 1: Identify candidates

Help the user find files worth compressing. Good candidates are:

- Verbose `AGENTS.md` files
- Skills with lengthy step-by-step guides
- Rules that use more words than necessary
- Any markdown file over ~500 chars that loads every session

Files that should NOT be compressed:

- Reference documents (they're read on-demand, not loaded every session)
- Files that are already concise
- Config files (harness.json)

## Step 2: Review before compressing

Start with interactive mode so they can review the compression:

```bash
ynd compress skills/code-review/SKILL.md
```

This shows the original and compressed versions side by side with the reduction percentage. They can accept or skip.

## Step 3: Bulk compress with auto-apply

Once they trust the quality, compress multiple files at once:

```bash
ynd compress -y skills/*/SKILL.md agents/*.md rules/*.md
```

Or compress everything discovered automatically:

```bash
ynd compress -y
```

## Step 4: Validate after compression

Compression preserves YAML frontmatter structurally (it's never sent to the LLM), but always validate after:

```bash
ynd validate
ynd lint
```

Both should pass. If they don't, restore from backup and report the issue.

## Step 5: Backup management

Every compression creates an automatic backup in `~/.ynd/backups/`. Show the user how to manage them:

```bash
# See what backups exist for a file
ynd compress --list-backups skills/code-review/SKILL.md

# Restore the most recent backup
ynd compress --restore skills/code-review/SKILL.md

# Restore a specific older version
ynd compress --list-backups skills/code-review/SKILL.md  # note the number
ynd compress --restore --pick 3 skills/code-review/SKILL.md
```

## Tips

- Compression quality varies by LLM. Claude tends to produce tighter output than Codex.
- Run `ynd compress -v claude` to force a specific vendor if auto-detect picks the wrong one.
- Small files (under ~200 chars) rarely benefit from compression — the overhead of the LLM call isn't worth it.
- Re-compressing an already-compressed file rarely helps and can degrade quality. Check the reduction percentage — under 5% means stop.
