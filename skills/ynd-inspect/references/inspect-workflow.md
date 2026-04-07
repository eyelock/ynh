# ynd inspect Workflow

## What it does

`ynd inspect` scans a project for signal files (build configs, test configs, CI pipelines, linter configs, etc.), sends them to an LLM for analysis, and proposes skills and agents tailored to the project's stack.

## Three-step flow

1. **Project Understanding** — LLM characterizes the project: languages, frameworks, build system, testing, CI/CD, conventions
2. **Review Existing Artifacts** — if skills/agents already exist (in project root or vendor dirs), offers to update them
3. **New Proposals** — suggests new skills and agents based on gaps, with generate/walkthrough/skip options

## Output directories

By default, artifacts are written into the vendor-specific config directory:

| Vendor | Output path |
|--------|-------------|
| claude | `.claude/skills/`, `.claude/agents/` |
| cursor | `.cursor/skills/`, `.cursor/agents/` |
| codex  | `.codex/skills/`, `.codex/agents/` |

Override with `-o`:

- `ynd inspect -o .` — write to project root (for plugin/harness development)
- `ynd inspect -o /tmp/out` — write to any custom directory

## Discovery

Inspect searches for existing artifacts in both the project root and all vendor dirs to avoid proposing duplicates. It checks:

- `skills/<name>/SKILL.md`
- `agents/<name>.md`
- `.claude/skills/`, `.cursor/skills/`, `.codex/skills/`
- `.claude/agents/`, `.cursor/agents/`, `.codex/agents/`

## Flags

| Flag | Description |
|------|-------------|
| `-v, --vendor <name>` | LLM CLI to use (default: auto-detect) |
| `-y, --yes` | Skip all confirmation prompts |
| `-o, --output-dir <path>` | Override output directory |

## Signal files

Inspect recognizes files in these categories: Build (go.mod, package.json, Makefile, etc.), Test (jest.config, pytest.ini, etc.), CI/CD (.github/workflows/, .gitlab-ci.yml, etc.), Lint (.eslintrc, .golangci.yml, etc.), Config (tsconfig.json, .env.example, etc.).
