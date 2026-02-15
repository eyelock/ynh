# Persona Format Reference

## Directory structure

```
my-persona/
├── .claude-plugin/
│   └── plugin.json       # required - name, version
├── metadata.json          # optional - vendor, includes, delegates
├── instructions.md        # optional - becomes CLAUDE.md / codex.md / .cursorrules
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

## .claude-plugin/plugin.json

```json
{
  "name": "david",
  "version": "0.1.0",
  "description": "David's coding persona"
}
```

### name

Lowercase, hyphens and dots allowed. Regex: `^[a-zA-Z0-9][a-zA-Z0-9._-]*$`. This becomes the launcher command on PATH.

## metadata.json

```json
{
  "ynh": {
    "default_vendor": "claude",
    "includes": [],
    "delegates_to": []
  }
}
```

### includes

```json
{
  "ynh": {
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
}
```

### delegates_to

```json
{
  "ynh": {
    "delegates_to": [
      {
        "git": "github.com/user/other-persona",
        "ref": "main",
        "path": "personas/team-ops"
      }
    ]
  }
}
```

## Install and run

```bash
ynh install ./my-persona             # from local path
ynh install github.com/user/persona  # from Git

<name>                               # interactive session
<name> "review this PR"              # non-interactive
<name> -v codex                      # override vendor
<name> --model opus -- "fix this"    # pass flags to vendor CLI
<name> -v cursor --install           # install symlinks for vendor
<name> -v cursor --clean             # remove symlinks
```

## Vendor resolution order

CLI flag (`-v`) > persona `default_vendor` > global `~/.ynh/config.json`
