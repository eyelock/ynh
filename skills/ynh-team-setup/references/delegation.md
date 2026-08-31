# Delegation Reference

## delegates_to syntax

The manifest is `.ynh-plugin/plugin.json` — **JSON**. There is no YAML form.

```json
{
  "delegates_to": [
    {
      "git": "github.com/user/harness",
      "ref": "main",
      "path": "harnesses/team-ops"
    },
    { "git": "git@github.com:co/private.git" }
  ]
}
```

`ref` and `path` are optional: a tag, branch or commit, and a subdirectory for a
monorepo holding several harnesses.

There is a CLI for this, so the manifest rarely needs hand-editing:

```bash
ynh delegate add <harness> github.com/user/harness --ref v1.2.0
ynh delegate update <harness> github.com/user/harness --ref v1.3.0
ynh delegate remove <harness> github.com/user/harness
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
| Copilot | `copilot` | `.copilot` | `AGENTS.md` |

Run `ynh vendors` for the live list — it reflects the adapters actually
compiled in, which this table cannot.

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
