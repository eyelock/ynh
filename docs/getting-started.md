---
title: Getting Started
---

# Getting Started

## Install

```bash
brew tap eyelock/tap
brew install ynh
```

Add the launcher directory to your PATH (one-time):

```bash
echo 'export PATH="$HOME/.ynh/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

The `~/.ynh/` directory is created automatically on first use. To customize the location, set `YNH_HOME`:

```bash
export YNH_HOME="$HOME/.config/ynh"
```

## Create a Persona

A persona is a directory with a `.claude-plugin/plugin.json` and your artifacts:

`.claude-plugin/plugin.json`:
```json
{
  "name": "david",
  "version": "0.1.0"
}
```

`metadata.json` (optional):
```json
{
  "ynh": {
    "default_vendor": "claude"
  }
}
```

Add whatever you need - skills, agents, rules, commands:

```
my-persona/
├── .claude-plugin/
│   └── plugin.json
├── metadata.json
├── skills/greet/SKILL.md
├── agents/reviewer.md
└── rules/concise.md
```

## Install and Use

```bash
ynh install ./my-persona
david                              # interactive session
david "explain what this function does"   # one-shot
```

## Pull Skills From Git

Point your metadata at any Git repo. Skills are used as-is - no wrapping, no build step:

`metadata.json`:
```json
{
  "ynh": {
    "default_vendor": "claude",
    "includes": [
      {
        "git": "github.com/eyelock/claude-config-toolkit",
        "pick": ["skills/commit", "skills/tdd"]
      },
      {
        "git": "git@github.com:company/monorepo.git",
        "path": "packages/ai-config",
        "pick": ["skills/deploy"]
      }
    ]
  }
}
```

Reinstall to pick up changes:

```bash
ynh install ./my-persona
```

## Private Repositories

ynh uses your local `git` for all cloning. If `git clone` works on your machine, `ynh` works too - it inherits whatever authentication you already have configured.

**SSH (recommended for private repos):**

```json
{
  "ynh": {
    "includes": [
      {"git": "git@github.com:company/private-skills.git", "pick": ["skills/deploy"]}
    ]
  }
}
```

This uses your SSH key. If you can `git clone git@github.com:company/private-skills.git` from your terminal, ynh can too.

**HTTPS with credential helper:**

```json
{
  "ynh": {
    "includes": [
      {"git": "github.com/company/private-repo"}
    ]
  }
}
```

The shorthand form produces an HTTPS URL. For private repos over HTTPS, Git needs a credential helper configured.

**If cloning fails**, the issue is Git authentication, not ynh. Set up one of:

- **SSH keys** (most common) - [GitHub](https://docs.github.com/en/authentication/connecting-to-github-with-ssh), [GitLab](https://docs.gitlab.com/ee/user/ssh.html), [Bitbucket](https://support.atlassian.com/bitbucket-cloud/docs/set-up-personal-ssh-keys-on-macos/)
- **GitHub CLI** - `gh auth login` configures Git credentials automatically ([docs](https://cli.github.com/manual/gh_auth_login))
- **Git credential helpers** - [Git documentation](https://git-scm.com/doc/credential-helpers)

**Quick check:** if you're unsure whether auth is working, test the URL directly:

```bash
git ls-remote git@github.com:company/private-skills.git
```

## Switch Vendors

```bash
david -v codex
david -v cursor
```

Or set a global default in `~/.ynh/config.json`:

```json
{"default_vendor": "codex"}
```

### Symlink Installation (Codex/Cursor)

Vendors without native plugin support need symlinks installed into your project:

```bash
david -v cursor --install     # creates .cursor/ symlinks
david -v cursor               # launches normally
david -v cursor --clean       # removes symlinks
```

Use `ynh status` to see all symlink installations and `ynh prune` to clean up orphaned ones.

## Passing Vendor Flags

All flags except `-v`, `--install`, and `--clean` are passed through to the vendor CLI. Use `--` to separate vendor flags from the prompt:

```bash
david --model opus -- "fix this bug"
david -v codex --full-auto -- "refactor auth"
```

When there are no vendor flags, the prompt can be a plain argument:

```bash
david "explain this function"
```

## Build From Source

If you prefer not to use Homebrew:

```bash
brew install go
git clone https://github.com/eyelock/ynh.git
cd ynh
make install
export PATH="$HOME/.ynh/bin:$PATH"
ynh install ./my-persona
```

## Next Steps

- [Persona Reference](personas.md) - full manifest syntax
- [Artifacts Guide](artifacts.md) - skills, agents, rules, commands
- [Vendor Support](vendors.md) - supported vendors and switching between them
