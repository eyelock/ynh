---
name: new-skill
description: Scaffold a skill in the current harness, write it properly, and prove it lands
---

Add a skill to the harness in the current directory, then verify it reaches the
vendor rather than assuming it did.

## 1. Scaffold

```bash
ynd create skill <name>
```

That writes `skills/<name>/SKILL.md` with placeholder frontmatter. The `name`
must match the directory.

## 2. Write it

Ask what workflow this automates before writing anything. A skill is a workflow,
not a description — every step should be a command to run, a file to open, or a
pattern to look for.

Replace the placeholder `description` first. It is what the vendor routes on, so
it must say **what the skill does and when to use it**, not just what it is.

Follow `rules/artifact-authoring.md`. If the body grows past a screen, move
detail into `skills/<name>/references/` — those ship with the skill, so anything
the skill tells the reader to open must live there rather than elsewhere in the
repo.

## 3. Prove it

```bash
ynd validate .
ynd lint skills/<name>
ynd preview . -v claude
```

The preview is the step people skip. **Confirm the new file appears in it** — a
skill that validates but does not assemble is not installed, and nothing else
tells you.

If the harness is installed, reload it:

```bash
ynh install .
```
