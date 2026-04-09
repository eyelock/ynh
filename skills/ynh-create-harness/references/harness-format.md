# Harness Format Reference

## Directory structure

```
my-harness/
├── .harness.json           # required - name, version, vendor, includes, delegates
├── AGENTS.md              # optional - read natively by most vendors; ynh shims Claude via @-import
├── skills/
│   └── <name>/
│       └── SKILL.md
├── agents/
│   └── <name>.md
├── rules/
│   └── <name>.md
└── commands/
    └── <name>.md
```

## .harness.json

```json
{
  "$schema": "https://eyelock.github.io/ynh/schema/harness.schema.json",
  "name": "david",
  "version": "0.1.0",
  "description": "David's coding harness",
  "default_vendor": "claude"
}
```

### name

Lowercase, hyphens and dots allowed. Regex: `^[a-zA-Z0-9][a-zA-Z0-9._-]*$`. This becomes the launcher command on PATH.

### includes

```json
{
  "name": "david",
  "version": "0.1.0",
  "default_vendor": "claude",
  "includes": [
    {
      "git": "github.com/user/repo",
      "ref": "v2.0.0",
      "path": "packages/ai-config",
      "pick": ["skills/commit", "agents/reviewer"]
    },
    {
      "git": "git@github.com:co/repo.git"
    }
  ]
}
```

### delegates_to

```json
{
  "name": "team-dev",
  "version": "0.1.0",
  "default_vendor": "claude",
  "delegates_to": [
    {
      "git": "github.com/user/other-harness",
      "ref": "main",
      "path": "harnesses/team-ops"
    }
  ]
}
```

## Install and run

```bash
ynh install ./my-harness             # from local path
ynh install github.com/user/harness  # from Git

<name>                               # interactive session
<name> "review this PR"              # non-interactive
<name> -v codex                      # override vendor
<name> --model opus -- "fix this"    # pass flags to vendor CLI
<name> -v cursor --install           # install symlinks for vendor
<name> -v cursor --clean             # remove symlinks
```

## Vendor resolution order

CLI flag (`-v`) > harness `default_vendor` > global `~/.ynh/config.json`
