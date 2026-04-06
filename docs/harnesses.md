# Harness Reference

A harness is a portable template that assembles the guide layer of a coding harness — skills, rules, agents, commands, and instructions — for any supported vendor. See [Harness Engineering](harness-engineering.md) for the broader context.

A harness is defined by a `.claude-plugin/plugin.json` manifest, an optional `metadata.json` sidecar, and artifact directories.

## Directory Structure

```
david/
├── .claude-plugin/
│   └── plugin.json           # required - name, version
├── metadata.json             # optional - vendor, includes, delegates
├── instructions.md           # optional - project-level instructions
├── skills/                   # optional - embedded skills
│   └── review/
│       └── SKILL.md
├── agents/                   # optional - embedded agents
│   └── code-reviewer.md
├── rules/                    # optional - embedded rules
│   └── always-test.md
└── commands/                 # optional - embedded commands
    └── check.md
```

## Plugin Manifest (`.claude-plugin/plugin.json`)

The name and version of the harness. Only fields from the Claude Code plugin schema belong here.

```json
{
  "name": "david",
  "version": "0.1.0",
  "description": "David's personal coding harness"
}
```

After install, `david` becomes a command on your PATH.

## Metadata Sidecar (`metadata.json`)

ynh-specific configuration lives under the `"ynh"` key. The file is extensible - other tools can add their own keys without conflict.

### default_vendor (optional)

Which vendor CLI to launch by default. Overridable with `-v` flag.

```json
{
  "ynh": {
    "default_vendor": "claude"
  }
}
```

If omitted, falls back to `~/.ynh/config.json` default.

### includes (optional)

External Git sources to pull artifacts from.

```json
{
  "ynh": {
    "includes": [
      {
        "git": "github.com/user/skills-repo",
        "ref": "v2.0.0",
        "pick": ["skills/commit", "agents/reviewer"]
      }
    ]
  }
}
```

**Fields:**

| Field | Required | Description |
|-------|----------|-------------|
| `git` | yes | Git URL (see formats below) |
| `ref` | no | Git tag, branch, or commit |
| `path` | no | Subdirectory within the repo (monorepo support) |
| `pick` | no | Specific artifact paths to include. If omitted, includes all. |

**Git URL formats:**

```
# Shorthand - expands to git@github.com:user/repo.git (SSH)
github.com/user/repo

# Full SSH
git@github.com:company/private-repo.git

# Full HTTPS - authenticates via Git credential helper
https://github.com/user/repo.git
```

See [Private Repositories](getting-started.md#private-repositories) for authentication setup and [Restrict Remote Sources](getting-started.md#restrict-remote-sources) to control which Git repos are allowed.

**Monorepo example:**

```json
{
  "ynh": {
    "includes": [
      {
        "git": "github.com/company/monorepo",
        "path": "packages/ai-config",
        "pick": ["skills/deploy", "agents/ops"]
      }
    ]
  }
}
```

**Include everything from a repo:**

```json
{
  "ynh": {
    "includes": [
      {"git": "github.com/user/skills-repo", "ref": "main"}
    ]
  }
}
```

No `pick` means all recognized artifact directories (`skills/`, `agents/`, `rules/`, `commands/`) are included.

### delegates_to (optional)

Other harnesses this one can invoke as subagents.

```json
{
  "ynh": {
    "delegates_to": [
      {"git": "github.com/eyelock/team-dev-harness"},
      {"git": "github.com/company/monorepo", "path": "harnesses/team-ops"}
    ]
  }
}
```

**Fields:**

| Field | Required | Description |
|-------|----------|-------------|
| `git` | yes | Git URL (same formats as includes) |
| `ref` | no | Git tag/branch |
| `path` | no | Subdirectory within the repo (monorepo support) |

When running as `david`, you can ask it to delegate a task to `team-dev`. The delegation happens through the vendor's native subagent system.

At runtime, ynh generates a vendor-native agent file for each delegate containing:

- **Description** from the delegate's `plugin.json` (helps the AI route to the right delegate)
- **Instructions** from the delegate's `instructions.md` (gives the delegate its identity)
- **Rules** inlined from the delegate's `rules/` directory
- **Skills** listed from the delegate's `skills/` directory

## Embedded vs External Artifacts

**Embedded** artifacts live directly in the harness directory. They're always included.

**External** artifacts are pulled from Git repos via `includes`. They're fetched at install time and cached locally. At runtime, cached repos are used without network access (with a fallback fetch on cache miss).

Both are assembled into the same vendor config. Use embedded for harness-specific customizations, external for shared libraries.

## Examples

### Minimal

`.claude-plugin/plugin.json`:
```json
{
  "name": "scratch",
  "version": "0.1.0"
}
```

`metadata.json`:
```json
{
  "ynh": {
    "default_vendor": "claude"
  }
}
```

Just a named launcher. Useful as a starting point.

### Harnessl with external skills

`.claude-plugin/plugin.json`:
```json
{
  "name": "david",
  "version": "0.1.0",
  "description": "David's coding harness"
}
```

`metadata.json`:
```json
{
  "ynh": {
    "default_vendor": "claude",
    "includes": [
      {
        "git": "github.com/eyelock/claude-config-toolkit",
        "pick": ["skills/commit", "skills/tdd", "skills/create-pr"]
      }
    ]
  }
}
```

### Team with private repos

`metadata.json`:
```json
{
  "ynh": {
    "default_vendor": "claude",
    "includes": [
      {"git": "github.com/eyelock/assistants", "path": "skills/dev"},
      {
        "git": "git@github.com:company/internal-tools.git",
        "path": "ai-config"
      }
    ],
    "delegates_to": [
      {"git": "git@github.com:company/team-ops-harness.git"}
    ]
  }
}
```

### Multi-source composition

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
        "git": "github.com/vercel-labs/skills",
        "pick": ["skills/next-app-router"]
      },
      {
        "git": "github.com/company/design-system",
        "path": "ai",
        "pick": ["rules/component-standards"]
      }
    ],
    "delegates_to": [
      {"git": "github.com/company/team-backend-harness"},
      {"git": "github.com/company/team-design-harness"}
    ]
  }
}
```
