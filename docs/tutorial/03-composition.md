# Tutorial 3: Composition

Pull skills from Git repos into your harness using includes. Cherry-pick specific artifacts from your own repos, third-party skill libraries, or local paths.

## Prerequisites

```bash
# Clean up from any previous run
rm -rf /tmp/ynh-tutorial
ynh uninstall my-dev with-anthropic with-vercel full-stack mixed local-ref pinned david 2>/dev/null

mkdir -p /tmp/ynh-tutorial
```

## T3.1: Own repo — pick skills from eyelock/assistants

The [eyelock/assistants](https://github.com/eyelock/assistants) repo is a skill library organized as plugins. It contains dev skills, language-specific skills, infrastructure skills, and more — all following the [Agent Skills](https://agentskills.io) standard.

Create a harness that cherry-picks specific skills from it:

```bash
mkdir -p /tmp/ynh-tutorial/my-dev

mkdir -p /tmp/ynh-tutorial/my-dev/.ynh-plugin
cat > /tmp/ynh-tutorial/my-dev/.ynh-plugin/plugin.json << 'EOF'
{
  "name": "my-dev",
  "version": "0.1.0",
  "description": "My development harness",
  "default_vendor": "claude",
  "includes": [
    {
      "git": "github.com/eyelock/assistants",
      "path": "skills/dev",
      "pick": ["skills/dev-project", "skills/dev-quality"]
    },
    {
      "git": "github.com/eyelock/assistants",
      "path": "skills/tech",
      "pick": ["skills/go-lang"]
    },
    {
      "git": "github.com/eyelock/assistants",
      "path": "skills/pause",
      "pick": ["skills/help-me-answer"]
    }
  ]
}
EOF

ynh install /tmp/ynh-tutorial/my-dev
```

This demonstrates:
- **`git`** — the repo to pull from
- **`path`** — scope into a subdirectory (the assistants repo organizes skills under `skills/<category>/skills/`)
- **`pick`** — cherry-pick specific skills (ynh's unique differentiator — no vendor supports this natively)
- **Multiple includes** — skills from different categories in the same repo

During install, ynh fetches all included repos and caches them locally. You should see output like:

```
Fetching 3 include(s) and 0 delegate(s)...
  Fetched eyelock/assistants
  Fetched eyelock/assistants
  Fetched eyelock/assistants
```

Run it once to trigger assembly, then verify only the picked skills are included:

```bash
my-dev "list your skills"
```

```bash
ls ~/.ynh/run/my-dev/.claude/skills/
# Expected: dev-project/ dev-quality/ go-lang/ help-me-answer/ (only the 4 picked skills)
# NOT: dev-review/ dev-backend/ dev-ui/ etc.
```

## T3.2: Own repo — local checkout (no clone)

If you have the assistants repo checked out locally, you can use a local path instead of a Git URL. This is faster (no clone) and useful during development:

```bash
mkdir -p /tmp/ynh-tutorial/my-dev/.ynh-plugin
cat > /tmp/ynh-tutorial/my-dev/.ynh-plugin/plugin.json << 'EOF'
{
  "name": "my-dev",
  "version": "0.1.0",
  "description": "My development harness",
  "default_vendor": "claude",
  "includes": [
    {
      "git": "/Users/david/Storage/Workspace/eyelock/assistants",
      "path": "skills/dev",
      "pick": ["skills/dev-project", "skills/dev-quality"]
    }
  ]
}
EOF
```

Local paths start with `/` or `.` — ynh uses them directly without cloning.

## T3.3: Third-party — Anthropic's official skills

Any GitHub repo that follows the [Agent Skills](https://agentskills.io) standard works with ynh. Community directories like [skills.sh](https://skills.sh) list thousands of them — but you can use any repo you find.

[anthropics/skills](https://github.com/anthropics/skills) has skills for frontend design, document handling, and more:

```bash
mkdir -p /tmp/ynh-tutorial/with-anthropic

mkdir -p /tmp/ynh-tutorial/with-anthropic/.ynh-plugin
cat > /tmp/ynh-tutorial/with-anthropic/.ynh-plugin/plugin.json << 'EOF'
{
  "name": "with-anthropic",
  "version": "0.1.0",
  "description": "Harness with Anthropic official skills",
  "default_vendor": "claude",
  "includes": [
    {
      "git": "github.com/anthropics/skills",
      "pick": ["skills/frontend-design"]
    }
  ]
}
EOF

ynh install /tmp/ynh-tutorial/with-anthropic
```

Verify it works:

```bash
with-anthropic "what skills do you have?"
```

## T3.4: Third-party — Vercel's skills

[vercel-labs/skills](https://github.com/vercel-labs/skills) has skills for Next.js and Vercel workflows:

```bash
mkdir -p /tmp/ynh-tutorial/with-vercel

mkdir -p /tmp/ynh-tutorial/with-vercel/.ynh-plugin
cat > /tmp/ynh-tutorial/with-vercel/.ynh-plugin/plugin.json << 'EOF'
{
  "name": "with-vercel",
  "version": "0.1.0",
  "description": "Harness with Vercel skills",
  "default_vendor": "claude",
  "includes": [
    {
      "git": "github.com/vercel-labs/skills",
      "pick": ["skills/find-skills"]
    }
  ]
}
EOF

ynh install /tmp/ynh-tutorial/with-vercel
```

Verify:

```bash
with-vercel "what skills do you have?"
```

## T3.5: Mixed sources — own + third-party

Combine skills from your own repos and third-party repos into one harness:

```bash
mkdir -p /tmp/ynh-tutorial/full-stack

mkdir -p /tmp/ynh-tutorial/full-stack/.ynh-plugin
cat > /tmp/ynh-tutorial/full-stack/.ynh-plugin/plugin.json << 'EOF'
{
  "name": "full-stack",
  "version": "0.1.0",
  "description": "Full-stack harness: own skills + third-party",
  "default_vendor": "claude",
  "includes": [
    {
      "git": "github.com/eyelock/assistants",
      "path": "skills/dev",
      "pick": ["skills/dev-project", "skills/dev-quality", "skills/dev-review"]
    },
    {
      "git": "github.com/eyelock/assistants",
      "path": "skills/tech",
      "pick": ["skills/go-lang"]
    },
    {
      "git": "github.com/anthropics/skills",
      "pick": ["skills/frontend-design"]
    }
  ]
}
EOF

ynh install /tmp/ynh-tutorial/full-stack
```

Run it to trigger assembly, then verify all 5 skills from 2 repos are present:

```bash
full-stack "list your skills"
```

```bash
ls ~/.ynh/run/full-stack/.claude/skills/
# Expected: dev-project/ dev-quality/ dev-review/ go-lang/ frontend-design/
```

## T3.6: Local — embedded skills in the harness

A harness can have its own skills alongside remote includes:

```bash
mkdir -p /tmp/ynh-tutorial/mixed/skills/my-custom-skill

cat > /tmp/ynh-tutorial/mixed/skills/my-custom-skill/SKILL.md << 'EOF'
---
name: my-custom-skill
description: A skill unique to this harness — not from any repo.
---

This skill lives directly in the harness directory.
It is not pulled from Git. It exists nowhere else.
EOF

mkdir -p /tmp/ynh-tutorial/mixed/.ynh-plugin
cat > /tmp/ynh-tutorial/mixed/.ynh-plugin/plugin.json << 'EOF'
{
  "name": "mixed",
  "version": "0.1.0",
  "includes": [
    {
      "git": "github.com/eyelock/assistants",
      "path": "skills/pause",
      "pick": ["skills/take-a-moment"]
    }
  ]
}
EOF

ynh install /tmp/ynh-tutorial/mixed
```

Run it to trigger assembly, then verify both local and remote skills are present:

```bash
mixed "what skills do you have?"
```

```bash
ls ~/.ynh/run/mixed/.claude/skills/
# Expected: my-custom-skill/ take-a-moment/ (local + remote)
```

For rapid iteration, keep the harness on disk and reinstall:

```bash
# Edit locally, install, test, repeat
ynh install /tmp/ynh-tutorial/mixed
mixed "what skills do you have?"
# Make changes to /tmp/ynh-tutorial/mixed/...
ynh install /tmp/ynh-tutorial/mixed   # reinstall picks up changes
```

## T3.7: Local — include from a local Git repo

If you have a skill library checked out locally and it's a Git repo:

```bash
# Create a local skill library
mkdir -p /tmp/ynh-tutorial/local-lib/skills/fast-deploy
cat > /tmp/ynh-tutorial/local-lib/skills/fast-deploy/SKILL.md << 'EOF'
---
name: fast-deploy
description: Quick deployment to staging.
---
Deploy the current branch to staging.
EOF
git -C /tmp/ynh-tutorial/local-lib init
git -C /tmp/ynh-tutorial/local-lib add .
git -C /tmp/ynh-tutorial/local-lib commit -m "init"

# Reference it in a harness
mkdir -p /tmp/ynh-tutorial/local-ref
mkdir -p /tmp/ynh-tutorial/local-ref/.ynh-plugin
cat > /tmp/ynh-tutorial/local-ref/.ynh-plugin/plugin.json << 'EOF'
{
  "name": "local-ref",
  "version": "0.1.0",
  "includes": [
    {
      "git": "/tmp/ynh-tutorial/local-lib",
      "pick": ["skills/fast-deploy"]
    }
  ]
}
EOF

ynh install /tmp/ynh-tutorial/local-ref
```

Run it, then verify:

```bash
local-ref "what skills do you have?"
```

```bash
ls ~/.ynh/run/local-ref/.claude/skills/
# Expected: fast-deploy/
```

## T3.7b: Local — bundled subdirectory (no Git repo)

When a harness ships its own artifact bundle inside the harness root — no Git, no cache, no clone — use a `local` include instead of `git`. The bundled directory is copied along with the harness at install time, so `ynh install` and `ynh run` both resolve it from the install location.

```bash
mkdir -p /tmp/ynh-tutorial/with-bundled/.ynh-plugin
mkdir -p /tmp/ynh-tutorial/with-bundled/extras/skills/team-standards

cat > /tmp/ynh-tutorial/with-bundled/extras/skills/team-standards/SKILL.md << 'EOF'
---
name: team-standards
description: Team coding standards and review checklist.
---
Apply our team's code review checklist to the diff.
EOF

cat > /tmp/ynh-tutorial/with-bundled/.ynh-plugin/plugin.json << 'EOF'
{
  "name": "with-bundled",
  "version": "0.1.0",
  "includes": [
    {"local": "extras"}
  ]
}
EOF

ynh install /tmp/ynh-tutorial/with-bundled
with-bundled "what skills do you have?"
```

```bash
ls ~/.ynh/run/with-bundled/.claude/skills/
# Expected: team-standards/
```

Key differences vs `git`:

- **`local`** expects a filesystem path. Relative paths resolve against the harness root; absolute paths are used as-is. No Git required — no clone, no cache, no ref.
- **`git`** expects a Git URL (or a local path that happens to be a Git repo). ynh clones/caches it.

Use `local` for artifact directories that travel with the harness source. Use `git` when the artifact source has its own version history and lifecycle.

> **Note on layout.** Relative `local` paths should stay inside the harness root so the bundle is copied along with the harness at install time. Sibling directories (e.g. `../shared`) work for `ynd preview` against a source tree but **won't survive `ynh install`** — the referenced dir isn't copied. Use an absolute path or fold the shared content into the harness if you need it after install.

`local` pairs well with profiles — see [Tutorial 13 → Profile-level includes](tutorial/13-profiles.md#t139-profile-level-includes--bundle-extra-artifacts-per-profile).

## T3.8: Pin a version with ref

```bash
mkdir -p /tmp/ynh-tutorial/pinned

mkdir -p /tmp/ynh-tutorial/pinned/.ynh-plugin
cat > /tmp/ynh-tutorial/pinned/.ynh-plugin/plugin.json << 'EOF'
{
  "name": "pinned",
  "version": "0.1.0",
  "includes": [
    {
      "git": "github.com/eyelock/assistants",
      "ref": "main",
      "path": "skills/pause",
      "pick": ["skills/help-me-answer"]
    }
  ]
}
EOF

ynh install /tmp/ynh-tutorial/pinned
```

Verify:

```bash
pinned "what skills do you have?"
```

```bash
ls ~/.ynh/run/pinned/.claude/skills/
# Expected: help-me-answer/
```

The `ref` field supports:
- Branch names: `"ref": "main"`
- Tags: `"ref": "v1.0.0"`
- Commit SHAs: `"ref": "abc1234"`

## T3.9: Update Git sources

After upstream repos change:

```bash
ynh update full-stack
```

Expected (one line per include — repos with multiple includes appear multiple times):
```
Checking github.com/eyelock/assistants...
  Already up to date.
Checking github.com/eyelock/assistants...
  Already up to date.
Checking github.com/anthropics/skills...
  Already up to date.
Checked 3 source(s) for harness "full-stack", 0 updated.
```

If upstream has changed, you'll see `Updated.` instead.

## T3.10: Install from a monorepo

The `eyelock/assistants` repo has pre-built harnesses under `ynh/`:

```bash
ynh install github.com/eyelock/assistants --path ynh/david
```

This installs the `david` harness, which already has includes configured to pull dev skills, Go skills, infrastructure skills, and pause skills.

The `--path` flag scopes into a subdirectory of the repo, installing only what's at that path.

## T3.11: Allow-list — deny a source

For security (especially in team environments), restrict which Git repos ynh can pull from.

First, back up your current config:

```bash
cp ~/.ynh/config.json ~/.ynh/config.json.bak
```

Restrict to only `eyelock` repos:

```bash
cat > ~/.ynh/config.json << 'EOF'
{
  "default_vendor": "claude",
  "allowed_remote_sources": [
    "github.com/eyelock/**"
  ]
}
EOF
```

Now try to run a harness that includes a non-eyelock source:

```bash
with-anthropic "hello" 2>&1
# Expected error: resolving includes: include "github.com/anthropics/skills": remote source "github.com/anthropics/skills" is not in the allowed sources list
```

The `anthropics/skills` source doesn't match `github.com/eyelock/**`, so it's rejected at run time when ynh tries to resolve the includes.

The `full-stack` harness also fails (it includes both eyelock and anthropic sources):

```bash
full-stack "hello" 2>&1
# Expected error: resolving includes: include "github.com/anthropics/skills": remote source "github.com/anthropics/skills" is not in the allowed sources list
```

> **Note:** `ynh install` now fetches all includes at install time, so the allow-list is enforced during install as well as at run time. If an include is blocked by the allow-list, `ynh install` will fail with an error.

## T3.12: Allow-list — allow a source

Add `anthropics` to the allow list:

```bash
cat > ~/.ynh/config.json << 'EOF'
{
  "default_vendor": "claude",
  "allowed_remote_sources": [
    "github.com/eyelock/**",
    "github.com/anthropics/**"
  ]
}
EOF
```

Now the same harness works:

```bash
full-stack "what skills do you have?"
# Expected: launches successfully with skills from both repos
```

Restore config:

```bash
mv ~/.ynh/config.json.bak ~/.ynh/config.json
```

**Pattern reference:**

| Pattern | Matches |
|---|---|
| `github.com/eyelock/**` | Any repo under the eyelock org |
| `github.com/eyelock/assistants` | Exactly that one repo |
| `github.com/*/public-*` | Any org, repos starting with `public-` |
| Not set (default) | All sources allowed |
| `[]` (empty array) | All sources denied |

## Clean up

```bash
ynh uninstall my-dev with-anthropic with-vercel full-stack mixed local-ref pinned david with-bundled 2>/dev/null
```

## What you learned

- **Your own repos:** Use `github.com/eyelock/assistants` (or any Git URL) with `path` and `pick`
- **Third-party repos:** Skills from [skills.sh](https://skills.sh), [anthropics/skills](https://github.com/anthropics/skills), [vercel-labs/skills](https://github.com/vercel-labs/skills) — any agentskills.io-compatible repo works
- **Local paths:** Start Git URLs with `/` or `.` to use a local Git checkout (faster, no clone)
- **Bundled local directories:** Use `"local": "path"` for a subdirectory that ships inside the harness — no Git required. The path is relative to the harness root (or absolute), and the directory is copied along with the harness at install time.
- **Embedded skills:** Put skills directly in the harness's `skills/` directory
- **`pick` is the differentiator:** No vendor natively supports cherry-picking individual skills from a larger repo. ynh does.
- **Mixing sources:** Combine your own skills, third-party skills, local sibling dirs, and embedded skills in one harness
- **Offline-ready:** All includes are fetched at install time — `ynh run` works offline
- `ref` pins to branches, tags, or commits
- `ynh update` refreshes cached repos
- `allowed_remote_sources` restricts which repos are permitted (enforced at both install and run time)

## Next

[Tutorial 4: Hooks](tutorial/10-hooks.md) — declare vendor-agnostic lifecycle hooks.
