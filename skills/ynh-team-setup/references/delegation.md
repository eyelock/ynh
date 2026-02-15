# Delegation Reference

## delegates_to syntax

```yaml
delegates_to:
  - git: github.com/user/persona           # shorthand HTTPS
    ref: main                               # optional - tag, branch, commit
    path: personas/team-ops                 # optional - monorepo subdirectory
  - git: git@github.com:co/private.git     # SSH for private repos
```

At runtime, ynh resolves each delegate persona, reads its manifest and artifacts, and generates a vendor-native agent file (e.g., `.claude/agents/<name>.md`) so the AI vendor can invoke it as a subagent.

## Git URL formats

| Format | Example | Auth |
|--------|---------|------|
| Shorthand | `github.com/user/repo` | HTTPS (public) |
| SSH | `git@github.com:co/repo.git` | SSH key (private) |
| Full HTTPS | `https://github.com/user/repo.git` | Credential helper |

## Private repo authentication

ynh delegates to the local `git` binary. If `git clone <url>` works on the machine, ynh works too.

**SSH (recommended for private repos):** Uses the user's SSH key. No extra config needed if `git clone git@...` works.

**HTTPS:** Requires a Git credential helper. `gh auth login` configures this automatically for GitHub.

**Quick test:** `git ls-remote <url>` verifies auth without cloning.

## Vendor support

| Vendor | CLI binary | Config dir | Instructions file |
|--------|-----------|------------|-------------------|
| Claude | `claude` | `.claude` | `CLAUDE.md` |
| Codex | `codex` | `.codex` | `codex.md` |
| Cursor | `agent` | `.cursor` | `.cursorrules` |

Setting `default_vendor` in the team persona standardizes the vendor across the team. Individual members can override with `-v` at runtime.

## Vendor resolution order

CLI flag (`-v`) > persona `default_vendor` > global `~/.ynh/config.json`

## Install flow for teams

```bash
# Creator pushes team persona to Git
cd team-persona && git init && git add . && git commit -m "Initial"
# Push to hosting...

# Team members install
ynh install github.com/org/team-persona
team-dev                    # interactive session with team config
team-dev "run deploy checklist"  # non-interactive
```
