# Export

Produce vendor-native distributable plugins from harnesses. The exported output passes strict vendor validation and can be loaded directly by Claude Code, Cursor, or Codex.

## Prerequisites

```bash
# Clean up from any previous run
rm -rf /tmp/ynh-tutorial

mkdir -p /tmp/ynh-tutorial
```

## Create a harness to export

```bash
mkdir -p /tmp/ynh-tutorial/exportable/skills/review
mkdir -p /tmp/ynh-tutorial/exportable/agents

cat > /tmp/ynh-tutorial/exportable/skills/review/SKILL.md << 'EOF'
---
name: review
description: Code review with security and performance focus.
---

Review code for:
1. Security vulnerabilities (OWASP top 10)
2. Performance bottlenecks
3. Error handling gaps
EOF

cat > /tmp/ynh-tutorial/exportable/agents/checker.md << 'EOF'
---
name: checker
description: Automated checks subagent.
---

Run automated checks on the codebase and report results.
EOF

cat > /tmp/ynh-tutorial/exportable/instructions.md << 'EOF'
You are a code quality harness. Focus on correctness and security.
EOF

mkdir -p /tmp/ynh-tutorial/exportable/.ynh-plugin
cat > /tmp/ynh-tutorial/exportable/.ynh-plugin/plugin.json << 'EOF'
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "exportable",
  "version": "1.0.0",
  "description": "A harness designed for cross-vendor export",
  "default_vendor": "claude",
  "includes": [
    {
      "git": "github.com/eyelock/assistants",
      "path": "skills/pause",
      "pick": ["skills/take-a-moment"]
    }
  ]
}
EOF
```

## Export for all vendors

```bash
ynd export /tmp/ynh-tutorial/exportable -o /tmp/ynh-tutorial/export-output
```

Expected output:
```
Exported for claude → /tmp/ynh-tutorial/export-output/claude (2 skills, 1 agents)
Exported for codex → /tmp/ynh-tutorial/export-output/codex (2 skills, 0 agents)
  warning: codex: skipping 1 agents (not supported)
Exported for copilot → /tmp/ynh-tutorial/export-output/copilot (2 skills, 1 agents)
Exported for cursor → /tmp/ynh-tutorial/export-output/cursor (2 skills, 1 agents)
```

## Verify Claude export

```bash
ls -Ra /tmp/ynh-tutorial/export-output/claude/
```

Expected:
```
.claude-plugin/               # plugin manifest directory
  plugin.json                 # generated from .ynh-plugin/plugin.json
agents/
  checker.md                  # local agent
skills/
  review/SKILL.md             # local skill
  take-a-moment/SKILL.md      # picked from remote include
AGENTS.md                     # instructions (cross-vendor format)
CLAUDE.md                     # @AGENTS.md import (Claude reads this)
```

Key points:
- `AGENTS.md` contains the actual instructions (read by Codex, Cursor, Copilot, etc.)
- `CLAUDE.md` contains just `@AGENTS.md` — Claude Code reads this and imports the instructions. No duplication, no conflict with the project's own `CLAUDE.md` (the plugin lives in its own directory).
- No `.claude/` wrapper — artifacts are at the plugin root
- Remote includes are resolved and flattened

### Verify Claude reads the instructions

```bash
cat /tmp/ynh-tutorial/export-output/claude/CLAUDE.md
# Expected: @AGENTS.md

cat /tmp/ynh-tutorial/export-output/claude/AGENTS.md
# Expected: You are a code quality harness. Focus on correctness and security.
```

### Validate the Claude export

```bash
claude plugin validate /tmp/ynh-tutorial/export-output/claude
# Expected: validation passes
```

### Verify the @-import works

The exported `CLAUDE.md` contains `@AGENTS.md` — Claude's import syntax. Verify Claude reads the instructions by launching from the export directory:

```bash
cd /tmp/ynh-tutorial/export-output/claude
git init && git add -A && git commit -m "init"
claude -p "What should you focus on? Answer in one sentence."
```

Expected: Claude should respond mentioning **code quality**, **correctness**, and **security** — the instructions from `AGENTS.md` imported via `CLAUDE.md`'s `@AGENTS.md` reference.

Return to your previous directory before continuing:

```bash
cd -
```

## Verify Cursor export

```bash
ls -Ra /tmp/ynh-tutorial/export-output/cursor/
```

Expected (note: `.cursor-plugin` and `.cursorrules` are hidden — `ls -Ra` shows them):
```
.cursor-plugin/
  plugin.json

agents/
  checker.md

skills/
  review/SKILL.md
  take-a-moment/SKILL.md

.cursorrules                  # Cursor-native instructions
AGENTS.md                     # universal instructions
```

Cursor gets both `.cursorrules` (Cursor-native) and `AGENTS.md` (universal). Without `-a`, you'd only see `agents/`, `AGENTS.md`, and `skills/`.

## Verify Codex export

```bash
ls -Ra /tmp/ynh-tutorial/export-output/codex/
```

Expected:
```
.codex-plugin/
  plugin.json
skills/
  review/SKILL.md
  take-a-moment/SKILL.md
AGENTS.md
```

Key points:
- `.codex-plugin/plugin.json` — plugin manifest with path pointers for skills and MCP
- Skills go to `skills/` at the plugin root (same as Claude and Cursor)
- Agents, rules, and commands are **excluded** (Codex doesn't support them in plugins)
- `AGENTS.md` only (Codex natively consumes it)

## Export for specific vendors

```bash
ynd export /tmp/ynh-tutorial/exportable -o /tmp/ynh-tutorial/export-claude -v claude
ls /tmp/ynh-tutorial/export-claude/
# Expected: only claude/ directory
```

## Export in merged mode

Merged mode produces one directory with both Claude and Cursor manifests — useful for CI pipelines and marketplace-ready plugins:

```bash
ynd export /tmp/ynh-tutorial/exportable -o /tmp/ynh-tutorial/export-merged --merged -v claude,cursor
ls -Ra /tmp/ynh-tutorial/export-merged/
```

Expected (note: hidden directories and files shown with `-a`):
```
.claude-plugin/
  plugin.json

.cursor-plugin/
  plugin.json

agents/
  checker.md

skills/
  review/SKILL.md
  take-a-moment/SKILL.md

.cursorrules
AGENTS.md
CLAUDE.md
```

One physical directory with both vendor manifests — serves Claude and Cursor from the same files.

## Export as an Agent Plugin

[Agent Plugins](https://agent-plugins.org) is the open package format that Codex, Copilot, VS Code and Cursor load directly: a root `plugin.json`, skills under `skills/`, MCP servers in `mcp.json`. Everything else is client-specific and travels in a namespace the client has published. `--format agent-plugin` writes one such package, and `-v` picks which clients' own files join it:

```bash
ynd export /tmp/ynh-tutorial/exportable -o /tmp/ynh-tutorial/agent-plugin --format agent-plugin
```

Expected output:
```
Exported Agent Plugin → /tmp/ynh-tutorial/agent-plugin (2 skills, 2 agents)
```

```bash
find /tmp/ynh-tutorial/agent-plugin -type f | LC_ALL=C sort
```

Expected (`LC_ALL=C` pins the order, which otherwise depends on the locale):
```
/tmp/ynh-tutorial/agent-plugin/.claude-plugin/plugin.json
/tmp/ynh-tutorial/agent-plugin/AGENTS.md
/tmp/ynh-tutorial/agent-plugin/CLAUDE.md
/tmp/ynh-tutorial/agent-plugin/agents/checker.md
/tmp/ynh-tutorial/agent-plugin/com.github.copilot/agents/checker.md
/tmp/ynh-tutorial/agent-plugin/plugin.json
/tmp/ynh-tutorial/agent-plugin/skills/review/SKILL.md
/tmp/ynh-tutorial/agent-plugin/skills/take-a-moment/SKILL.md
```

Three layers in one directory. `plugin.json` and `skills/` are the portable core every client reads. `com.github.copilot/agents/` is Copilot's namespace, which other clients ignore. `.claude-plugin/plugin.json`, `agents/` and `CLAUDE.md` are there because Claude Code has not adopted the format and still reads its own layout; the spec calls this a compatibility package.

```bash
cat /tmp/ynh-tutorial/agent-plugin/plugin.json
```

Expected:
```json
{
  "$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json",
  "name": "exportable",
  "version": "1.0.0",
  "description": "A harness designed for cross-vendor export"
}
```

The package was checked against the specification as it was written; the same check is available on demand:

```bash
ynd validate /tmp/ynh-tutorial/agent-plugin
# Expected: /tmp/ynh-tutorial/agent-plugin: valid (Agent Plugin 1.0.0)
```

Select only Cursor, which has published no namespace, and the agent has nowhere to go. The export says so rather than dropping it quietly:

```bash
ynd export /tmp/ynh-tutorial/exportable -o /tmp/ynh-tutorial/agent-plugin-cursor --format agent-plugin -v cursor
```

Expected output:
```
Exported Agent Plugin → /tmp/ynh-tutorial/agent-plugin-cursor (2 skills, 0 agents)
  warning: 1 agents not portable: no selected vendor (cursor) carries them in an Agent Plugin
```

### Install the package back

The package is also a harness source. `ynh install` reads it as an Agent
Plugin, derives the harness in memory, and never writes into the directory:

```bash
ynh install /tmp/ynh-tutorial/agent-plugin
```

Expected:
```
Fetching 1 include(s) and 0 delegate(s)...
  Local  com.github.copilot
Installed harness "exportable"
  Location: /tmp/ynh-tutorial/agent-plugin
  Launcher: /Users/<you>/.ynh/bin/exportable
```

The Copilot namespace directory became a local include, which is how its
agent reaches the assembled harness with source attribution. The install
record says what the source was:

```bash
ynh installed local/exportable
# Expected: a record with "source_type": "local" and "format": "agent-plugin"
ls -a /tmp/ynh-tutorial/agent-plugin
# Expected: no .ynh-plugin directory; the package is as exported
ynh uninstall local/exportable
```

### Validate and install a package you did not write

The same loader handles a package from anyone. The specification project
publishes a reference package; validate it and install it exactly as above:

```bash
git clone https://github.com/agentplugins/agent-plugins-example /tmp/ynh-tutorial/agent-plugins-example
ynd validate /tmp/ynh-tutorial/agent-plugins-example
```

Expected:
```
/tmp/ynh-tutorial/agent-plugins-example: valid (Agent Plugin 1.0.0)
```

```bash
ynh install /tmp/ynh-tutorial/agent-plugins-example
```

Expected:
```
Installed harness "agent-plugins-example"
  Location: /tmp/ynh-tutorial/agent-plugins-example
  Launcher: /Users/<you>/.ynh/bin/agent-plugins-example
```

```bash
ynh info local/agent-plugins-example
```

Expected output includes the package's one skill and no includes, since this
package has no vendor namespace directory:
```
Artifacts:
  skills:    migrate-agent-plugin

Includes:
  (none)
```

```bash
ls -a /tmp/ynh-tutorial/agent-plugins-example
# Expected: .git LICENSE README.md plugin.json skills; no .ynh-plugin
ynh uninstall local/agent-plugins-example
```

### Test with GitHub Copilot

Copilot CLI loads the Agent Plugins format natively. Its `--plugin-dir` flag
mounts a local plugin for one invocation without changing your Copilot
configuration, so this is a safe check that a real client accepts what ynh
wrote:

```bash
copilot --plugin-dir /tmp/ynh-tutorial/agent-plugin plugin list
```

Expected:
```
External Plugins (via --plugin-dir):
  • exportable
```

Copilot found the root `plugin.json`, read it as an Agent Plugins manifest,
and took the package's name from it. The marketplace tutorial goes further and
installs a ynh-built package through Copilot's marketplace flow.

## Export with --clean

```bash
# First export
ynd export /tmp/ynh-tutorial/exportable -o /tmp/ynh-tutorial/clean-test

# Second export adds a new vendor dir from a different run
ynd export /tmp/ynh-tutorial/exportable -o /tmp/ynh-tutorial/clean-test -v claude

# Old codex/, copilot/, cursor/ dirs still exist from first run
ls /tmp/ynh-tutorial/clean-test/
# Expected: claude/ codex/ copilot/ cursor/

# --clean removes entire output first
ynd export /tmp/ynh-tutorial/exportable -o /tmp/ynh-tutorial/clean-test -v claude --clean
ls /tmp/ynh-tutorial/clean-test/
# Expected: claude/ only
```

## Export from a Git URL

```bash
ynd export github.com/eyelock/assistants --path ynh/david -o /tmp/ynh-tutorial/remote-export -v claude
```

Clones the repo, applies `--path` scoping, exports. Same as exporting a local directory.

## Export with no instructions

```bash
mkdir -p /tmp/ynh-tutorial/no-instructions
mkdir -p /tmp/ynh-tutorial/no-instructions/.ynh-plugin
cat > /tmp/ynh-tutorial/no-instructions/.ynh-plugin/plugin.json << 'EOF'
{"$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json", "name": "no-instructions", "version": "0.1.0"}
EOF

ynd export /tmp/ynh-tutorial/no-instructions -o /tmp/ynh-tutorial/no-inst-out -v claude
# Expected: succeeds (no warning)

ls -a /tmp/ynh-tutorial/no-inst-out/claude/
# Expected: .claude-plugin/ only (generated from .ynh-plugin/plugin.json, no AGENTS.md)
```

## Clean up

```bash
rm -rf /tmp/ynh-tutorial/export-*
rm -rf /tmp/ynh-tutorial/agent-plugin /tmp/ynh-tutorial/agent-plugin-cursor
rm -rf /tmp/ynh-tutorial/agent-plugins-example
rm -rf /tmp/ynh-tutorial/clean-test
rm -rf /tmp/ynh-tutorial/remote-export
rm -rf /tmp/ynh-tutorial/no-inst-out
```

## What you learned

- `ynd export` produces vendor-native distributable plugins
- Each vendor gets its own layout:
  - Claude: `.claude-plugin/plugin.json` + artifacts at root
  - Cursor: `.cursor-plugin/plugin.json` + `.cursorrules`
  - Codex: `.codex-plugin/plugin.json` + `skills/` (agents, rules, commands excluded)
- `--merged` produces a single dir with dual manifests (marketplace-ready)
- `--format agent-plugin` produces one portable Agent Plugins package; `-v` picks which clients' namespaces and compatibility files join the portable core, and whatever cannot travel is warned about
- An Agent Plugins package is itself a harness source: `ynh install` derives the harness at load time and never writes into the package, whether ynh wrote the package or someone else did
- Copilot CLI mounts an exported package with `--plugin-dir`, which confirms a real client reads it
- Remote includes are resolved and flattened into the export
- Pick filtering carries through to the export
- `AGENTS.md` is the universal instruction format (read by Codex, Cursor, Copilot, etc.)
- `CLAUDE.md` is generated with `@AGENTS.md` import — Claude reads this, no duplication

## Next

[Marketplace](marketplace.md) — generate marketplace indexes for team distribution.
