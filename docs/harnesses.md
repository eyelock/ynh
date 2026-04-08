# Harness Reference

A harness is a portable template that assembles the guide layer of a coding harness — skills, rules, agents, commands, and instructions — for any supported vendor. See [Harness Engineering](harness-engineering.md) for the broader context.

A harness is defined by a `harness.json` manifest and artifact directories.

> **Migration note:** Legacy format (`.claude-plugin/plugin.json` + `metadata.json`) is no longer supported. Consolidate into `harness.json`.

## Directory Structure

```
david/
├── harness.json              # required - name, version, vendor, includes, hooks, etc.
├── AGENTS.md                 # optional - project-level instructions (read natively by most vendors; ynh shims Claude via @-import)
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

## Harness Manifest (`harness.json`)

All harness configuration lives in a single `harness.json` file. Add `"$schema": "https://eyelock.github.io/ynh/schema/harness.schema.json"` for editor autocompletion and validation.

### Annotated Example

```json
{
  "$schema": "https://eyelock.github.io/ynh/schema/harness.schema.json",
  "name": "david",
  "version": "0.1.0",
  "description": "David's personal coding harness",
  "default_vendor": "claude",
  "includes": [
    {
      "git": "github.com/eyelock/claude-config-toolkit",
      "pick": ["skills/commit", "skills/tdd"]
    }
  ],
  "delegates_to": [
    {"git": "github.com/eyelock/team-dev-harness"}
  ],
  "hooks": {
    "before_tool": [
      {"matcher": "Bash", "command": "/usr/local/bin/check-dangerous-commands.sh"}
    ]
  },
  "mcp_servers": {
    "sqlite": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-sqlite", "/path/to/db.sqlite"]
    }
  },
  "profiles": {
    "strict": {
      "hooks": {
        "before_tool": [
          {"matcher": "Bash", "command": "/usr/local/bin/strict-check.sh"}
        ]
      }
    }
  }
}
```

After install, `david` becomes a command on your PATH.

### name (required)

The harness name. This becomes the launcher command on your PATH.

### version (required)

Semantic version string.

### description (optional)

Human-readable description of what the harness does.

### default_vendor (optional)

Which vendor CLI to launch by default. Overridable with `-v` flag.

If omitted, falls back to `~/.ynh/config.json` default.

### includes (optional)

External Git sources to pull artifacts from.

```json
{
  "includes": [
    {
      "git": "github.com/user/skills-repo",
      "ref": "v2.0.0",
      "pick": ["skills/commit", "agents/reviewer"]
    }
  ]
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
  "includes": [
    {
      "git": "github.com/company/monorepo",
      "path": "packages/ai-config",
      "pick": ["skills/deploy", "agents/ops"]
    }
  ]
}
```

**Include everything from a repo:**

```json
{
  "includes": [
    {"git": "github.com/user/skills-repo", "ref": "main"}
  ]
}
```

No `pick` means all recognized artifact directories (`skills/`, `agents/`, `rules/`, `commands/`) are included.

### delegates_to (optional)

Other harnesses this one can invoke as subagents.

```json
{
  "delegates_to": [
    {"git": "github.com/eyelock/team-dev-harness"},
    {"git": "github.com/company/monorepo", "path": "harnesses/team-ops"}
  ]
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

- **Description** from the delegate's `harness.json` (helps the AI route to the right delegate)
- **Instructions** from the delegate's `AGENTS.md` (gives the delegate its identity)
- **Rules** inlined from the delegate's `rules/` directory
- **Skills** listed from the delegate's `skills/` directory

## Embedded vs External Artifacts

**Embedded** artifacts live directly in the harness directory. They're always included.

**External** artifacts are pulled from Git repos via `includes`. They're fetched at install time and cached locally. At runtime, cached repos are used without network access (with a fallback fetch on cache miss).

Both are assembled into the same vendor config. Use embedded for harness-specific customizations, external for shared libraries.

### hooks (optional)

Lifecycle hooks. See [Hooks](hooks.md) for full reference.

### mcp_servers (optional)

MCP server declarations. See [MCP Servers](mcp.md) for full reference.

### profiles (optional)

Named configuration variants. A profile can override `hooks` and `mcp_servers`. It cannot override identity fields (`name`, `version`, `description`), composition (`includes`, `delegates_to`), or `default_vendor`. See [Profiles](profiles.md) for full scope reference.

## Profiles

Profiles let a single harness carry multiple configurations — e.g. a `strict` profile with extra hooks for CI, or a `lite` profile that skips MCP servers.

### Selection Precedence

| Priority | Source | Example |
|----------|--------|---------|
| 1 (highest) | `--profile` flag | `david --profile strict` |
| 2 | `YNH_PROFILE` env var | `export YNH_PROFILE=strict` |
| 3 (lowest) | Top-level config | Fields in `harness.json` root |

When a profile is selected, its `hooks` and `mcp_servers` **fully replace** the corresponding top-level values. No merge, no inheritance. If a profile defines `hooks`, the top-level hooks are discarded entirely.

### Example

```json
{
  "$schema": "https://eyelock.github.io/ynh/schema/harness.schema.json",
  "name": "ops-team",
  "version": "1.0.0",
  "default_vendor": "claude",
  "hooks": {
    "before_tool": [
      {"matcher": "Bash", "command": "/usr/local/bin/basic-check.sh"}
    ]
  },
  "profiles": {
    "strict": {
      "hooks": {
        "before_tool": [
          {"matcher": "Bash", "command": "/usr/local/bin/strict-check.sh"}
        ],
        "on_stop": [
          {"command": "/usr/local/bin/audit-log.sh"}
        ]
      }
    },
    "lite": {
      "mcp_servers": {}
    }
  }
}
```

Running `ops-team --profile strict` uses the strict hooks (replacing the top-level hooks entirely). Running `ops-team --profile lite` drops all MCP servers while keeping the base vendor and no hooks (both hooks and mcp_servers are replaced — lite defines empty mcp_servers and no hooks).

## Examples

### Minimal

`harness.json`:
```json
{
  "$schema": "https://eyelock.github.io/ynh/schema/harness.schema.json",
  "name": "scratch",
  "version": "0.1.0",
  "default_vendor": "claude"
}
```

Just a named launcher. Useful as a starting point.

### Harness with external skills

`harness.json`:
```json
{
  "$schema": "https://eyelock.github.io/ynh/schema/harness.schema.json",
  "name": "david",
  "version": "0.1.0",
  "description": "David's coding harness",
  "default_vendor": "claude",
  "includes": [
    {
      "git": "github.com/eyelock/claude-config-toolkit",
      "pick": ["skills/commit", "skills/tdd", "skills/create-pr"]
    }
  ]
}
```

### Team with private repos

`harness.json`:
```json
{
  "$schema": "https://eyelock.github.io/ynh/schema/harness.schema.json",
  "name": "team-dev",
  "version": "1.0.0",
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
```

### Multi-source composition

`harness.json`:
```json
{
  "$schema": "https://eyelock.github.io/ynh/schema/harness.schema.json",
  "name": "full-stack",
  "version": "1.0.0",
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
```
