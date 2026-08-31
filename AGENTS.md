# ynh — Your Name, Your AI

You are running inside a ynh-managed harness. ynh assembles skills, agents,
rules, and commands from any source into a vendor-native layout for Claude Code,
OpenAI Codex, Cursor, or GitHub Copilot CLI. Run `ynh vendors` for the live
list — it reports the adapters compiled into the binary actually installed.

## What you can help with

Eight skills ship with this harness. Reach for them in roughly this order — it
is the order someone actually meets these problems.

| Skill | When |
|---|---|
| `ynh-adopt` | They already have a `.claude/` folder, a `.cursorrules` or a grown `CLAUDE.md` and want it under ynh without losing it. **Most people start here, not from nothing.** |
| `ynh-create-harness` | Building a harness from scratch — naming, vendor, artifacts, install |
| `ynd-inspect` | Reading a codebase's signals to propose the skills, agents, profiles and sensors it needs |
| `ynh-compose` | Reusing artifacts across projects — includes, profiles, focuses, delegates, fork |
| `ynh-sensors` | Deciding what a project should observe, and making `ynh check` a gate worth trusting |
| `ynh-team-setup` | Graduating a personal harness to team delegation |
| `ynh-troubleshoot` | Something is broken and it is not obvious why — routes a symptom to the command that diagnoses it |
| `ynd-compress` | Shrinking verbose instruction files without losing meaning |

Two agents to delegate to rather than doing the work inline:

- **`ynh-guide`** — questions about how ynh works. It establishes whether the
  ynh repository is on disk before answering, and falls back to the installed
  CLI when it is not.
- **`ynd-artifact-reviewer`** — reviewing one skill or agent for specificity,
  frontmatter correctness and reference integrity.
- **`ynh-harness-reviewer`** — reviewing a *whole* harness before it ships:
  validates it, assembles it for all four vendors, and checks that every file
  the artifacts promise actually reaches a user. Delegate before installing,
  publishing or tagging.

## Answer from what you can check

This harness ships skills, agents and rules. It does **not** ship ynh's
documentation or Go source — those live in the ynh repository, which is usually
not present in a user's project.

So do not answer questions about ynh from a document you have not opened. The
installed CLI describes itself and is always available:

```bash
ynh help                        # every command
ynh vendors                     # vendors, and which are on PATH
ynh info <name>                 # a harness's resolved config
ynh doctor                      # diagnose the setup and report what is wrong
ynd validate <dir>              # is this harness valid, and why not
ynd preview <dir> -v <vendor>   # exactly what a vendor will receive
```

`ynh <command> --help` describes that command and runs nothing, so it is safe
to probe with. `ynh help` and `ynd help` give the command lists.

One exception worth knowing: `ynh run` forwards unrecognised flags to the vendor
CLI, so `ynh run <name> -- --help` sends `--help` onward rather than describing
`ynh run`. The `--` is what makes the difference.

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
  profile. `ynh profile ls <name>` and `ynh focus ls <name>` list them.
- Switch vendors at any time: `-v claude`, `-v codex`, `-v cursor`, `-v copilot`

## Starting points

This harness ships focuses — named entry points that set a prompt and, where it
matters, a profile. `ynh focus ls <name>` lists them, `ynh info <name>` shows
them alongside the rest of the resolved config, and `ynh run <name> --focus <f>`
launches one.

| Focus | For |
|-------|-----|
| `learn` | First time with ynh — the concepts, grounded in what the installed CLI reports |
| `adopt` | You already have a `.claude/` folder or a `CLAUDE.md` and want it under ynh |
| `create` | Build a harness from scratch |
| `sensors` | Decide what this project should observe, and make `ynh check` a gate worth trusting |
| `team` | Set up team delegation |
| `validate` | Validate the harness in the current directory and fix what is wrong |
| `fix` | Something is broken and it is not obvious why |
| `contribute` | Work on ynh itself — routes to the right developer skill (contributor profile) |
| `release` | Prepare a ynh release (contributor profile) |

**Contributors:** the developer skills — `ynh-dev`, `add-vendor-adapter`,
`add-ynd-command`, `capability-bump`, `vendor-adapters` — and the `evals`,
`ynh-contributor` and `ynh-releaser` agents only load under the `ynh-dev`
profile. `--focus contribute` and `--focus release` set it for you; otherwise
pass `--profile ynh-dev`.
