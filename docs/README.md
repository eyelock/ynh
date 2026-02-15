---
title: ynh
description: Your name here. Your AI, your way.
---

# ynh

**Your name. Your AI.**

```bash
ynh install github.com/david/my-persona
david
```

That's it. `david` is now a command. It knows your skills, your team's rules, your coding standards. It works on Claude today. Switch to Codex with `-v codex`. Same persona, different engine.

## What this gives you

**You become a command.** Not "claude with some config files." Not "a dotfile repo." A named identity that carries your entire AI working environment. Your tools, your standards, your style - all behind your name.

```bash
david                              # your personalized AI
david "review this PR"             # it knows how you review
david -v codex --full-auto         # same persona, different vendor
```

**Compose from anywhere.** Cherry-pick skills from community repos, your team's private config, open-source libraries - and mix them with your own. Like a package manager for AI capabilities, backed by Git.

```json
{
  "ynh": {
    "includes": [
      {"git": "github.com/eyelock/shared-skills", "pick": ["skills/commit", "skills/tdd"]},
      {"git": "github.com/vercel-labs/skills", "pick": ["skills/next-app-router"]},
      {"git": "git@github.com:company/internal-tools.git", "path": "ai-config"}
    ]
  }
}
```

**Delegation is native.** Your personal persona can delegate to a team persona. Ask `david` to do a team task, and it hands off to `team-dev` using the vendor's native subagent system. No middleware, no proxy.

```bash
david                              # personal context
team-dev                           # full team context when you need it
```

**Zero runtime.** ynh resolves your config, assembles it, launches the vendor CLI, and gets out of the way. No process sitting between you and the AI. Each vendor gets the launch strategy that matches its capabilities - native plugin loading for Claude, symlinks for Cursor and Codex.

**Git is the package manager.** No registry accounts. No lock files. No build steps. Skills from [skills.sh](https://skills.sh), agents from your team repo, rules from a company monorepo - they all work as-is. Standard-format files, versioned with Git tags.

## The 60-second version

```bash
# Install ynh
brew tap eyelock/tap && brew install ynh

# Create a persona
mkdir -p my-persona/.claude-plugin
echo '{"name":"david","version":"0.1.0"}' > my-persona/.claude-plugin/plugin.json

# Install it
ynh install ./my-persona

# You're a command now
david
```

Add skills, agents, rules, and commands to the persona directory. Pull from Git repos. Compose across sources. Switch vendors. Delegate to team personas.

## Guides

- **[Getting Started](getting-started.md)** - Create your first persona and run it
- **[Persona Reference](personas.md)** - Plugin manifest, metadata, includes, delegates
- **[Artifacts Guide](artifacts.md)** - Skills, agents, rules, commands, and project instructions
- **[Vendor Support](vendors.md)** - Claude, Codex, Cursor - capabilities and launch strategies
- **[Full Walkthrough](walkthrough.md)** - Hands-on test of every feature, start to finish
