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

`david` is now a command. It knows your skills, your team's rules, your coding standards. It works on Claude today. Switch to Codex with `-v codex`: same harness, but Codex does not yet get everything (see [Known gaps](#known-gaps-in-v0160)).

## What this gives you

**You become a command.** Not "claude with some config files." A named identity that carries your entire AI working environment — tools, standards, style — all behind your name.

```bash
david                              # your configured AI
david "review this PR"             # it knows how you review
david -v codex                     # same harness, different vendor
```

**Compose from anywhere.** Cherry-pick skills from community repos, your team's private config, open-source libraries — and mix them with your own. Like a package manager for AI capabilities, backed by Git.

## Known gaps in v0.16.0

ynh claims more than each vendor yet gets. In v0.16.0:

- **Codex** does not yet receive commands. Hooks and session context reach it under `ynh run` for an accepted harness only, not under `ynh agent run` ([Codex hook trust](docs/hooks.md#codex-hook-trust)). Instructions (written to `AGENTS.md`), rules (folded into them), MCP servers, skills, agents, focuses and profiles do. Under `ynh run`, Codex starts in the run directory without `-C`, so the project's own `AGENTS.md` is never read.
- **Rules** reach Claude Code, Codex and Copilot only by being folded into their instructions under `ynh run`: always on, with no path scoping.
- **OpenCode**: delegates cannot run as subagents (a harness's own agents can), `on_session_start` does not fire, `on_session_end` cannot fire (OpenCode has no such event, a vendor limit), and session context is not injected. `ynd export -v opencode` leaves delegates out and warns (`opencode: skipping N delegates (not supported)`).
- **Cursor**: print mode skips `before_prompt` and `on_stop`.
- **Delegates** carry only their instructions on Codex, Cursor and Copilot. On Claude Code a delegate also carries its MCP servers, not its hooks or workspace. A delegate does not run on OpenCode. "A full harness" holds for includes, not yet for delegates.
- **Resume**: `ynh run --resume` with a prompt is refused on Cursor and Copilot by ynh itself; both vendor CLIs accept a prompt on resume in print mode, so this is a ynh gap, not a vendor limit. Interactive resume through ynh is unverified on both. On Codex a resume with a prompt passes workspace folders as `writable_roots`, which takes effect only under the `workspace-write` sandbox (ynh warns on stderr otherwise; ynh does not change Codex's sandbox, and `codex exec` defaults to read-only).
- **Telemetry**: `ynh run` emits none on any vendor; only `ynh agent run` does.
- **Project instructions** (the project's own `AGENTS.md` or `CLAUDE.md` under `ynh run`) are unverified on every vendor except Codex, where they are not read.

These are fixed in v0.17.0 (parity) and later, except what a vendor cannot do. See [ynh #679](https://github.com/eyelock/ynh/issues/679).

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
mkdir -p david/.agents/harness && cat > david/.agents/harness/plugin.json << 'EOF'
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

ynh manages the **guide layer** of that harness: the proactive steering that happens *before* the agent acts. One harness definition, assembled for Claude, Codex, Cursor, Copilot or OpenCode. Read more in [Harness Engineering](https://eyelock.github.io/ynh/#/harness-engineering).

## What you can do

**Build**: create harnesses with skills, agents, rules, and commands. Pull artifacts from any Git repo with cherry-picking (`pick`). Declare hooks, MCP servers, environment-specific profiles, [focuses](docs/focus.md) (named prompt-and-profile bindings for repeatable runs) and [sensors](docs/sensors.md), the observation surfaces that loop drivers consume between agent turns. Let a harness request sibling [workspace folders](docs/workspace.md) that you grant with an allow-list or `ynh trust accept`.

**Refine** — Scaffold with `ynd create`, lint and validate with `ynd lint`/`ynd validate`, compress prompts with `ynd compress`, preview assembled output with `ynd preview`, diff across vendors with `ynd diff`. Edit focuses, profiles, hooks, MCP servers, includes, and delegates from the CLI with `ynh focus`/`profile`/`hook`/`mcp`/`include`/`delegate` — the same surface a GUI consumer drives.

**Share**: export as vendor-native plugins with `ynd export`, or as one portable [Agent Plugins](https://agent-plugins.org) package with `ynd export --format agent-plugin`. Build team marketplaces with `ynd marketplace build`. Publish to registries. Bake harnesses into Docker images for CI/CD.

## Documentation

Full docs at **[eyelock.github.io/ynh](https://eyelock.github.io/ynh)**:

- **[Getting Started](https://eyelock.github.io/ynh/#/getting-started)** — Create and run your first harness
- **[Harness Engineering](https://eyelock.github.io/ynh/#/harness-engineering)** — Why harness management matters
- **[Tutorials](https://eyelock.github.io/ynh/#/tutorial/)** — 18 progressive tutorials from first harness to sensors
- **[Artifacts Guide](https://eyelock.github.io/ynh/#/artifacts)** — Skills, agents, rules, commands
- **[CLI Reference](https://eyelock.github.io/ynh/#/reference)** — Full command reference for ynh and ynd
- **[Agent Skills Standard](https://eyelock.github.io/ynh/#/skills-standard)** — Cross-platform spec, discovery paths, catalog budget
- **[Vendor Support](https://eyelock.github.io/ynh/#/vendors)** — Claude, Codex, Cursor, Copilot and OpenCode capabilities

## License

[MIT](LICENSE)
