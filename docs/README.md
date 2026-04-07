# ynh

**Your name. Your AI.**

```bash
ynh install github.com/myorg/david
david
```

That's it. `david` is now a command. It knows your skills, your team's rules, your coding standards. It works on Claude today. Switch to Codex with `-v codex`. Same harness, different engine.

## What this gives you

**You become a command.** Not "claude with some config files." Not "a dotfile repo." A named identity that carries your entire AI working environment. Your tools, your standards, your style - all behind your name.

```bash
david                              # your configured AI
david "review this PR"             # it knows how you review
david -v codex                     # same harness, different vendor
```

**Compose from anywhere.** Cherry-pick skills from community repos, your team's private config, open-source libraries - and mix them with your own. Like a package manager for AI capabilities, backed by Git.

```json
{
  "name": "david",
  "version": "0.1.0",
  "includes": [
    {"git": "github.com/eyelock/assistants", "path": "skills/dev", "pick": ["skills/dev-project", "skills/dev-quality"]},
    {"git": "github.com/vercel-labs/skills", "pick": ["skills/next-app-router"]},
    {"git": "git@github.com:company/internal-tools.git", "path": "ai-config"}
  ]
}
```

**Delegation is native.** Your personal harness can delegate to a team harness. Ask `david` to do a team task, and it hands off to `team-dev` using the vendor's native subagent system. No middleware, no proxy.

```bash
david                              # personal context
team-dev                           # full team context when you need it
```

**Zero runtime.** ynh resolves your config, assembles it, launches the vendor CLI, and gets out of the way. No process sitting between you and the AI. Each vendor gets the launch strategy that matches its capabilities - native plugin loading for Claude, symlinks for Cursor and Codex.

**Discover and share.** Search registries for harnesses by name or keyword, install with a single command. Export your harness as vendor-native plugins, or build a marketplace indexing multiple harnesses for your team or community.

```bash
ynh search "go development"       # find harnesses across registries
ynh install go-dev                 # install by name
ynd export ./david                 # vendor-native plugins
ynd marketplace build              # build a shareable marketplace
```

**Git is the package manager.** No lock files. No build steps. Skills from [skills.sh](https://skills.sh), agents from your team repo, rules from a company monorepo - they all work as-is. Standard-format files, versioned with Git tags. Registries are just Git repos with a `registry.json`.

## The 60-second version

```bash
# Install ynh
brew tap eyelock/tap && brew install ynh

# Create a harness
mkdir david
echo '{"name":"david","version":"0.1.0","default_vendor":"claude"}' > david/harness.json

# Install it
ynh install ./david

# It's a command now
david
```

Add skills, agents, rules, and commands to the harness directory. Pull from Git repos. Compose across sources. Switch vendors. Delegate to team harnesses.

## Guides

- **[Getting Started](getting-started.md)** - Create your first harness and run it
- **[Harness Reference](harnesses.md)** - Harness manifest, includes, delegates, profiles
- **[Artifacts Guide](artifacts.md)** - Skills, agents, rules, commands, and project instructions
- **[Vendor Support](vendors.md)** - Claude, Codex, Cursor - capabilities and launch strategies
- **[Agent Skills Standard](skills-standard.md)** - Cross-platform spec, frontmatter fields, catalog budget, discovery paths
- **[Marketplace & Distribution](marketplace.md)** - Cross-vendor marketplace systems, distribution formats, and ynh's marketplace builder
- **[Docker](docker.md)** - Run harnesses in containers, build harness appliance images for CI/CD
- **[Tutorials](tutorial/README.md)** - Progressive tutorials from first harness to Docker images
- **[Manual Test Plan](tutorial/manual-test-plan.md)** - Tests covering every feature
- **[ynd Developer Tools](ynd.md)** - CLI for scaffolding, linting, formatting, compressing, inspecting, exporting, and marketplace building
