# Delegation Reference

## delegates_to syntax

```yaml
delegates_to:
  - git: github.com/user/harness           # shorthand (expands to SSH)
    ref: main                               # optional - tag, branch, commit
    path: harnesses/team-ops                 # optional - monorepo subdirectory
  - git: git@github.com:co/private.git     # SSH for private repos
```

At runtime, ynh resolves each delegate harness, reads its manifest and artifacts, and generates a vendor-native agent file (e.g., `.claude/agents/<name>.md`) so the AI vendor can invoke it as a subagent.

## Git URL formats

| Format | Example | Auth |
|--------|---------|------|
| Shorthand | `github.com/user/repo` | SSH key |
| Full SSH | `git@github.com:co/repo.git` | SSH key |
| Full HTTPS | `https://github.com/user/repo.git` | Credential helper |

## Private repo authentication

ynh delegates to the local `git` binary. If `git clone <url>` works on the machine, ynh works too.

**SSH (default for shorthand):** Uses the user's SSH key. No extra config needed if `git clone git@...` works. Shorthand like `github.com/user/repo` expands to SSH automatically.

**HTTPS:** Requires a Git credential helper. `gh auth login` configures this automatically for GitHub.

**Quick test:** `git ls-remote <url>` verifies auth without cloning.

## Vendor support

| Vendor | CLI binary | Config dir | Instructions file |
|--------|-----------|------------|-------------------|
| Claude | `claude` | `.claude` | `CLAUDE.md` |
| Codex | `codex` | `.codex` | `codex.md` |
| Cursor | `agent` | `.cursor` | `.cursorrules` |

Setting `default_vendor` in the team harness standardizes the vendor across the team. Individual members can override with `-v` at runtime.

## Vendor resolution order

CLI flag (`-v`) > harness `default_vendor` > global `~/.ynh/config.json`

## Install flow for teams

```bash
# Creator pushes team harness to Git
cd team-harness && git init && git add . && git commit -m "Initial"
# Push to hosting...

# Team members install
ynh install github.com/org/team-harness
team-dev                    # interactive session with team config
team-dev "run deploy checklist"  # non-interactive
```
