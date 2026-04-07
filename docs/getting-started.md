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

See [CLI Reference](reference.md) for the full list of environment variables and CLI flags.

## Create a Harness

A harness is a directory with a `harness.json` and your artifacts:

`harness.json`:
```json
{
  "$schema": "https://eyelock.github.io/ynh/schema/harness.schema.json",
  "name": "david",
  "version": "0.1.0",
  "default_vendor": "claude"
}
```

Add whatever you need - skills, agents, rules, commands:

```
david/
├── harness.json
├── skills/greet/SKILL.md
├── agents/reviewer.md
└── rules/concise.md
```

## Install and Use

```bash
ynh install ./david
david                              # interactive session
david "explain what this function does"   # one-shot
```

### Install from a monorepo

Use `--path` to install a harness from a subdirectory:

```bash
ynh install github.com/org/assistants --path harnesses/david
ynh install ./local-monorepo --path plugins/my-plugin
```

## Pull Skills From Git

Point your harness at any Git repo. Skills are used as-is - no wrapping, no build step:

`harness.json`:
```json
{
  "$schema": "https://eyelock.github.io/ynh/schema/harness.schema.json",
  "name": "david",
  "version": "0.1.0",
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
```

All included repos are fetched at install time and cached locally. This means `ynh run` works offline - no network access needed unless the cache is cleared.

Reinstall to pick up changes:

```bash
ynh install ./david
```

## Private Repositories

ynh uses your local `git` for all cloning. If `git clone` works on your machine, `ynh` works too - it inherits whatever authentication you already have configured.

**SSH (recommended for private repos):**

```json
{
  "includes": [
    {"git": "git@github.com:company/private-skills.git", "pick": ["skills/deploy"]}
  ]
}
```

This uses your SSH key. If you can `git clone git@github.com:company/private-skills.git` from your terminal, ynh can too.

**Shorthand (also uses SSH):**

```json
{
  "includes": [
    {"git": "github.com/company/private-repo"}
  ]
}
```

The shorthand form expands to an SSH URL (`git@github.com:company/private-repo.git`). If your SSH key has access, no extra config is needed.

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

## Restrict Remote Sources

By default, harnesses can pull skills and agents from any Git repo via `includes` or `delegates_to`. You can restrict which remote sources are allowed by adding an `allowed_remote_sources` list to `~/.ynh/config.json`:

```json
{
  "default_vendor": "claude",
  "allowed_remote_sources": [
    "github.com/eyelock/*",
    "github.com/acme-corp/assistants",
    "github.com/acme-corp/monorepo/**/ai-config/*"
  ]
}
```

**Behaviour:**

| Config state | Effect |
|---|---|
| Field absent | All remote sources allowed (default) |
| Empty array `[]` | All remote sources blocked |
| Patterns present | Only matching sources allowed |

Local paths (e.g. `ynh install ./david`) are always trusted and bypass this check.

**Pattern syntax:**

| Pattern | Meaning |
|---|---|
| `*` | Matches a single path segment (does not cross `/`) |
| `**` | Matches zero or more path segments |
| Everything else | Literal, case-sensitive match |

Patterns match against the normalized URL path. All Git URL formats (shorthand, SSH, HTTPS) are normalized before matching:

```
github.com/user/repo              →  github.com/user/repo
git@github.com:user/repo.git      →  github.com/user/repo
https://github.com/user/repo.git  →  github.com/user/repo
```

**Enforcement points:** The allow-list is checked on `ynh install`, `ynh run`, and `ynh update` — before any Git clone or fetch occurs.

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
david -v codex -- "refactor auth"
```

When there are no vendor flags, the prompt can be a plain argument:

```bash
david "explain this function"
```

## Discover and Install From Registries

A registry is a Git repository containing a `registry.json` that indexes available harnesses. You can add registries to discover and install harnesses by name instead of URL.

```bash
# Add a registry (a Git repo with registry.json)
ynh registry add github.com/your-org/ynh-registry

# Search across all registries
ynh search "go development"

# Install by name
ynh install go-dev

# If the name exists in multiple registries, disambiguate
ynh install go-dev@eyelock
```

### Registry management

```bash
ynh registry list              # show configured registries
ynh registry remove <url>      # remove a registry
ynh registry update            # refresh all cached registries
```

### Install disambiguation

ynh resolves install arguments in this order:

| Argument form | Resolution |
|---|---|
| Starts with `.` or `/` | Local path |
| Starts with `git@` | Git SSH URL |
| Starts with `https://` or `http://` | Git HTTPS URL |
| Contains `@` (not SSH) | Registry lookup as `name@registry-name` |
| Contains `/` | Git URL shorthand (e.g., `github.com/user/repo`) |
| Plain word | Exact name search across all registries |

Plain word matches: single match installs directly; multiple matches errors with disambiguation guidance; no exact match searches descriptions and suggests similar results.

See [Tutorial 7: Registry & Discovery](tutorial/07-registry-and-discovery.md) for a guided walkthrough.

## Build From Source

If you prefer not to use Homebrew:

```bash
brew install go
git clone https://github.com/eyelock/ynh.git
cd ynh
make install
export PATH="$HOME/.ynh/bin:$PATH"
ynh install ./david
```

## Next Steps

- [Harness Reference](harnesses.md) - full manifest syntax
- [Artifacts Guide](artifacts.md) - skills, agents, rules, commands
- [Vendor Support](vendors.md) - supported vendors and switching between them
- [Docker](docker.md) - run harnesses in containers, build harness appliance images for CI/CD
- [ynd Developer Tools](ynd.md) - authoring, exporting, and marketplace building
- [Tutorials](tutorial/README.md) - progressive walkthroughs from first harness to Docker images
