# ynh — Your Name, Your AI

You are running inside a ynh-managed harness. ynh assembles skills, agents,
rules, and commands from any source into a vendor-native layout for Claude Code,
OpenAI Codex, Cursor, or GitHub Copilot CLI. Run `ynh vendors` for the live
list — it reports the adapters compiled into the binary actually installed.

## What you can help with

- **Creating harnesses** — the `ynh-create-harness` skill walks a user through
  building their first one
- **Team setup** — `ynh-team-setup` graduates a personal harness to team
  delegation
- **Bootstrapping from a codebase** — `ynd-inspect` reads a project's signals
  and proposes skills and agents for it
- **Tightening prompts** — `ynd-compress` shrinks instruction files without
  losing meaning
- **Questions about ynh** — hand these to the `ynh-guide` agent. It establishes
  whether the ynh repository is on disk before answering, and falls back to the
  installed CLI when it is not.

## Answer from what you can check

This harness ships skills, agents and rules. It does **not** ship ynh's
documentation or Go source — those live in the ynh repository, which is usually
not present in a user's project.

So do not answer questions about ynh from a document you have not opened. The
installed CLI describes itself and is always available:

```bash
ynh help                        # every command
ynh <command> --help            # one command in detail
ynh vendors                     # vendors, and which are on PATH
ynh info <name>                 # a harness's resolved config
ynh doctor                      # diagnose a broken setup
ynd validate <dir>              # is this harness valid, and why not
ynd preview <dir> -v <vendor>   # exactly what a vendor will receive
```

Prose docs are published at **https://eyelock.github.io/ynh**. Point users there
rather than paraphrasing a page you cannot read.

## Key concepts

- A **harness** is a directory with `.ynh-plugin/plugin.json` plus `skills/`,
  `agents/`, `rules/`, `commands/`. (`.harness.json` is the legacy form; `ynd
  migrate` converts it, and validation does so transparently.)
- `ynh install` copies a harness (local or Git) into `~/.ynh/harnesses/` and
  assembles it for the target vendor
- `ynh run <name>` launches the vendor CLI with the assembled config
- Harnesses compose via `includes` (pull artifacts from other repos) and
  `delegates_to` (subagent delegation)
- **Profiles** layer config variants; **focuses** pair a starting prompt with a
  profile. `ynh profile` and `ynh focus` list what a harness offers.
- Switch vendors at any time: `-v claude`, `-v codex`, `-v cursor`, `-v copilot`

## Starting points

This harness ships focuses — named entry points that set both a prompt and a
profile. `ynh focus` lists them:

| Focus | For |
|-------|-----|
| `learn` | First time with ynh; walks the concepts from the README |
| `create` | Build a harness with `ynh-create-harness` |
| `team` | Set up team delegation |
| `validate` | Validate the harness in the current directory and fix what's wrong |
| `contribute` | Work on ynh itself (contributor profile) |
| `release` | Prepare a ynh release (contributor profile) |
