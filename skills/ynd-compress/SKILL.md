---
name: ynd-compress
description: Workflow for compressing prompt and instruction files using LLM-powered techniques, with backup management and restore.
---

# Compress Artifacts

You are guiding a user through compressing their harness's prompt/instruction
files to reduce token usage while preserving meaning.

**This is a lossy transform, performed by an LLM, on the instructions that steer
an agent.** Treat it with the caution that deserves. The failure is not a
mangled file — `ynd lint` catches those — it is a file that still reads well and
has quietly lost the specific detail that made it work: `run \`make check\``
becoming "run the checks". Nothing downstream detects that.

## When to use

Use after authoring or updating skills, agents, rules, or instructions. Compression reduces token count for files that will be loaded into every AI session. Particularly valuable for verbose instructions or detailed skills.

## Step 1: Identify candidates

Help the user find files worth compressing. Good candidates are:

- Verbose `AGENTS.md` files
- Skills with lengthy step-by-step guides
- Rules that use more words than necessary
- Any markdown file over ~500 chars that loads every session

Files that should NOT be compressed:

- **Anything whose value is its specificity.** `rules/artifact-authoring.md`
  asks artifacts to name actual files, commands and patterns rather than give
  generic advice. Compression optimises for fewer tokens, and those two goals
  pull against each other. Where they conflict, specificity wins — a shorter
  skill that no longer says which command to run is worse than a long one.
- Files that are already concise
- Config files (`.ynh-plugin/plugin.json`) — not prose, and compression has
  nothing to do with them
- Reference documents, usually. They load only when the agent reads them, so
  they cost nothing until used, and they are where the specific detail lives.

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

Frontmatter is stripped before the LLM sees it and reattached after, so `name`
and `description` survive structurally. That is the only guarantee.

```bash
git diff
ynd validate <dir>
ynd lint <dir>
```

**Read the diff, not just the exit code.** Validation and lint prove the file is
still well-formed; they cannot tell you it still means what it did. Look for
commands, paths and flags that turned into descriptions of themselves.

If the meaning moved, restore from backup rather than editing the compressed
version — the original is the thing that worked.

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

- Quality varies by model and by file. Judge it per file from the diff rather
  than assuming any vendor is better — run `ynd compress -v <vendor>` to force
  one if auto-detect picks a CLI you did not intend.
- Small files (under ~200 chars) rarely benefit from compression — the overhead of the LLM call isn't worth it.
- Re-compressing an already-compressed file rarely helps and can degrade
  quality. Check the reduction percentage — under 5% means stop.
- Delegate to the `ynd-artifact-reviewer` agent afterwards for anything
  important. It checks the criterion compression is most likely to have cost:
  whether the artifact still references actual files, commands and patterns.
