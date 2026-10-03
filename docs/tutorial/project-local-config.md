# Project-Local Config

Use a `.agents/harness/plugin.json` file in your project root for zero-install AI configuration. No `ynh install` needed — just drop the file and run.

## Prerequisites

```bash
# Clean up from any previous run
rm -rf /tmp/ynh-tutorial

mkdir -p /tmp/ynh-tutorial
```

## Create a project with .agents/harness/plugin.json

Create a project directory with a `.agents/harness/plugin.json` file:

```bash
mkdir -p /tmp/ynh-tutorial/my-project/rules

mkdir -p /tmp/ynh-tutorial/my-project/.agents/harness
cat > /tmp/ynh-tutorial/my-project/.agents/harness/plugin.json << 'EOF'
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "my-project",
  "version": "0.1.0",
  "default_vendor": "claude",
  "hooks": {
    "before_tool": [
      { "matcher": "Write", "command": "/usr/local/bin/lint.sh" }
    ]
  },
  "focuses": {
    "review": {
      "prompt": "Review staged changes for quality"
    }
  }
}
EOF

cat > /tmp/ynh-tutorial/my-project/rules/standards.md << 'EOF'
Follow the team coding standards. Use meaningful variable names.
EOF
```

Key points:
- `.agents/harness/plugin.json` in the project root — same format as an installed harness
- No `ynh install` needed — ynh can discover and use this file directly
- Rules, skills, agents, and commands sit alongside `.agents/harness/plugin.json` as usual

## Validate the project config

```bash
ynd validate /tmp/ynh-tutorial/my-project
```

Expected:
```
/tmp/ynh-tutorial/my-project: valid
  checked:
    manifest     /tmp/ynh-tutorial/my-project/.agents/harness/plugin.json against https://eyelock.github.io/ynh/schema/plugin.schema.json
    includes     none
    mcp_servers  none
    hooks        before_tool runs `/usr/local/bin/lint.sh`
    profiles     none
    focuses      review
    sensors      none
    delegates_to none
    skills       none
    agents       none
    rules        standards
    commands     none
    instructions none
```

## Preview the assembled output

```bash
ynd preview /tmp/ynh-tutorial/my-project -v claude
```

Expected output includes:
- `.claude/hooks/hooks.json` with the `before_tool` hook (PreToolUse with Write matcher)
- `.claude/rules/standards.md` with the rule content
- `.claude-plugin/plugin.json` with the project name

## Preview with --focus

```bash
ynd preview /tmp/ynh-tutorial/my-project -v claude --focus review
```

Expected: same as base preview — the `review` focus has no profile, so it uses the default configuration. The focus prompt is used by `ynh run`, not by `ynd preview`.

## Still on `.ynh-plugin`? It keeps working

Before the manifest directory moved under `.agents/`, it was `.ynh-plugin/`.
ynh reads `.agents/harness/` first and falls back to `.ynh-plugin/`, so a
project that has not moved needs no change and no migration step:

```bash
mkdir -p /tmp/ynh-tutorial/old-layout/rules

mkdir -p /tmp/ynh-tutorial/old-layout/.ynh-plugin
cat > /tmp/ynh-tutorial/old-layout/.ynh-plugin/plugin.json << 'EOF'
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "old-layout",
  "version": "0.1.0",
  "default_vendor": "claude"
}
EOF

cat > /tmp/ynh-tutorial/old-layout/rules/standards.md << 'EOF'
Follow the team coding standards.
EOF

ynd validate /tmp/ynh-tutorial/old-layout
ynd preview /tmp/ynh-tutorial/old-layout -v claude
```

Expected: `validate` reports `valid` (its `manifest` line points at `.ynh-plugin/plugin.json`) and `preview` lists
`.claude/rules/standards.md`, exactly as for the `.agents/harness` project
above. Nothing is written back: reading a `.ynh-plugin` harness never
creates `.agents/`.

```bash
ls -a /tmp/ynh-tutorial/old-layout
```

Expected: `.ynh-plugin` is still there and there is no `.agents`.

To move a project onto the documented layout, move the directory and commit:

```bash
mkdir -p /tmp/ynh-tutorial/old-layout/.agents
mv /tmp/ynh-tutorial/old-layout/.ynh-plugin /tmp/ynh-tutorial/old-layout/.agents/harness
ynd validate /tmp/ynh-tutorial/old-layout
```

Expected: `valid`, with the `manifest` line now pointing at `.agents/harness/plugin.json`. In a git repository use `git mv` so history follows the
file. If both directories exist, `.agents/harness` wins and `ynd validate`
reports the shadowed `.ynh-plugin` copy so it cannot be edited by mistake.

## Clean up

```bash
rm -rf /tmp/ynh-tutorial
```

## What You Learned

- `.agents/harness/plugin.json` in a project root provides zero-install AI configuration
- A project still on `.ynh-plugin/plugin.json` keeps working unchanged: ynh reads `.agents/harness/` first and falls back to `.ynh-plugin/`
- `ynd validate`, `ynd preview`, and `ynd diff` work with project directories containing `.agents/harness/plugin.json`
- `ynh run` auto-discovers `.agents/harness/plugin.json` in the current working directory
- `ynh run --harness-file <path>` points to a specific manifest file by path
- The file format is identical to installed harnesses — same hooks, MCP servers, profiles, and focus entries

## Composition with focus

The project-local config pattern works well with focus entries ([Focus](focus.md)) for CI automation:

```json
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "default_vendor": "claude",
  "focuses": {
    "review": { "prompt": "Review staged changes" },
    "security": { "profile": "ci", "prompt": "Audit for vulnerabilities" }
  }
}
```

## Next

[Developer Tools](developer-tools.md) — scaffold, lint, validate, format, compress, inspect with ynd.
