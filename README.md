# ynh

**Your name. Your AI.**

```bash
brew tap eyelock/tap && brew install ynh
ynh install github.com/eyelock/ynh
ynh-guide
```

That gets you a guided session that teaches you ynh — and helps you create your own harness. Once you have one:

```bash
ynh install github.com/myorg/david
david
```

`david` is now a command. It knows your skills, your team's rules, your coding standards. It works on Claude today. Switch to Codex with `-v codex`. Same harness, different engine.

## What this gives you

**You become a command.** Not "claude with some config files." A named identity that carries your entire AI working environment — tools, standards, style — all behind your name.

```bash
david                              # your configured AI
david "review this PR"             # it knows how you review
david -v codex                     # same harness, different vendor
```

**Compose from anywhere.** Cherry-pick skills from community repos, your team's private config, open-source libraries — and mix them with your own. Like a package manager for AI capabilities, backed by Git.

**Zero runtime.** ynh resolves your config, assembles it, launches the vendor CLI, and gets out of the way. No process sitting between you and the AI.

## The 60-second version

```bash
# Install ynh
brew tap eyelock/tap && brew install ynh

# Try the ynh harness — it teaches you ynh
ynh install github.com/eyelock/ynh
ynh-guide
```

Inside the session, use `/ynh-create-harness` to build your own. Or do it manually:

```bash
# Create a harness
mkdir -p david/.ynh-plugin && cat > david/.ynh-plugin/plugin.json << 'EOF'
{"name":"david","version":"0.1.0","default_vendor":"claude"}
EOF

# Install and run
ynh install ./david
david
```

Add skills, agents, rules, and commands to the harness directory. Pull from Git repos. Compose across sources. Switch vendors. Delegate to team harnesses.

## Why harness management?

> **Agent = Model + Harness.** Weak results are usually harness problems, not model problems.

The term "harness" was formalized by [Martin Fowler](https://martinfowler.com/articles/harness-engineering.html) and adopted by [OpenAI](https://openai.com/index/harness-engineering/) and [Anthropic](https://www.anthropic.com/engineering/harness-design-long-running-apps). A harness is everything in an AI coding agent except the model itself — the skills, rules, context, and constraints that shape its behavior.

ynh manages the **guide layer** of that harness: the proactive steering that happens *before* the agent acts. One harness definition, assembled for Claude, Codex, or Cursor. Read more in [Harness Engineering](https://eyelock.github.io/ynh/#/harness-engineering).

## What you can do

**Build** — Create harnesses with skills, agents, rules, and commands. Pull artifacts from any Git repo with cherry-picking (`pick`). Declare hooks, MCP servers, environment-specific profiles, and [sensors](docs/sensors.md) — observation surfaces that loop drivers consume between agent turns.

**Refine** — Scaffold with `ynd create`, lint and validate with `ynd lint`/`ynd validate`, compress prompts with `ynd compress`, preview assembled output with `ynd preview`, diff across vendors with `ynd diff`.

**Share** — Export as vendor-native plugins with `ynd export`. Build team marketplaces with `ynd marketplace build`. Publish to registries. Bake harnesses into Docker images for CI/CD.

## Documentation

Full docs at **[eyelock.github.io/ynh](https://eyelock.github.io/ynh)**:

- **[Getting Started](https://eyelock.github.io/ynh/#/getting-started)** — Create and run your first harness
- **[Harness Engineering](https://eyelock.github.io/ynh/#/harness-engineering)** — Why harness management matters
- **[Tutorials](https://eyelock.github.io/ynh/#/tutorial/)** — 13 progressive tutorials from first harness to Docker images
- **[Artifacts Guide](https://eyelock.github.io/ynh/#/artifacts)** — Skills, agents, rules, commands
- **[CLI Reference](https://eyelock.github.io/ynh/#/reference)** — Full command reference for ynh and ynd
- **[Agent Skills Standard](https://eyelock.github.io/ynh/#/skills-standard)** — Cross-platform spec, discovery paths, catalog budget
- **[Vendor Support](https://eyelock.github.io/ynh/#/vendors)** — Claude, Codex, Cursor capabilities

## License

MIT
