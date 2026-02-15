# Skill Authoring Reference

## Required reading

Before creating or modifying any skill, read the Agent Skills specification:

**https://agentskills.io/**

This is the canonical spec for how AI coding agent skills are structured. It covers:

- SKILL.md format and frontmatter requirements
- Directory layout conventions (references/, scripts/)
- How agents discover and invoke skills
- Progressive disclosure patterns
- Interaction models (interactive vs automated)

## ynh-specific notes

ynh distributes skills as persona artifacts. The assembler copies skill directories into the vendor's config dir at run time (e.g. `.claude/skills/`, `.codex/skills/`). This means:

- Skills must be self-contained within their directory
- References should be co-located in `references/` (not absolute paths)
- Scripts should be co-located in `scripts/` and be executable
- The SKILL.md is the entry point - agents read it first, then follow references as needed
