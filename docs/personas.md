---
title: Persona Reference
---

# Persona Reference

A persona is defined by a `.claude-plugin/plugin.json` manifest, an optional `metadata.json` sidecar, and artifact directories.

## Directory Structure

```
my-persona/
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

The name and version of the persona. Only fields from the Claude Code plugin schema belong here.

```json
{
  "name": "david",
  "version": "0.1.0",
  "description": "David's personal coding persona"
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

See [Private Repositories](getting-started.md#private-repositories) for authentication setup.

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

Other personas this one can invoke as subagents.

```json
{
  "ynh": {
    "delegates_to": [
      {"git": "github.com/eyelock/team-dev-persona"},
      {"git": "github.com/company/monorepo", "path": "personas/team-ops"}
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

**Embedded** artifacts live directly in the persona directory. They're always included.

**External** artifacts are pulled from Git repos via `includes`. They're fetched at runtime, cached locally.

Both are assembled into the same vendor config. Use embedded for persona-specific customizations, external for shared libraries.

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

### Personal with external skills

`.claude-plugin/plugin.json`:
```json
{
  "name": "david",
  "version": "0.1.0",
  "description": "David's coding persona"
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
      {"git": "github.com/eyelock/shared-skills"},
      {
        "git": "git@github.com:company/internal-tools.git",
        "path": "ai-config"
      }
    ],
    "delegates_to": [
      {"git": "git@github.com:company/team-ops-persona.git"}
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
      {"git": "github.com/company/team-backend-persona"},
      {"git": "github.com/company/team-design-persona"}
    ]
  }
}
```
