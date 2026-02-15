---
name: ynh-validate
description: Validate Claude Code artifacts (skills, agents) in the .claude/ directory. Checks frontmatter, naming conventions, and referenced paths.
---

# Validate Artifacts

Run the validation script to check all `.claude/` artifacts:

```bash
bash .claude/skills/ynh-validate/scripts/validate.sh
```

The script checks:
- Skills have valid YAML frontmatter (`name`, `description`)
- Agents have valid YAML frontmatter (`name`, `description`, `tools`)
- `name` fields match their filename or directory name
- Frontmatter delimiters (`---`) are present

If any checks fail, fix the reported issues and re-run until clean.
