# Tutorial 15: Project-Local Config

Use a `.ynh-plugin/plugin.json` file in your project root for zero-install AI configuration. No `ynh install` needed — just drop the file and run.

## Prerequisites

```bash
# Clean up from any previous run
rm -rf /tmp/ynh-tutorial

mkdir -p /tmp/ynh-tutorial
```

## T15.1: Create a project with .ynh-plugin/plugin.json

Create a project directory with a `.ynh-plugin/plugin.json` file:

```bash
mkdir -p /tmp/ynh-tutorial/my-project/rules

mkdir -p /tmp/ynh-tutorial/my-project/.ynh-plugin
cat > /tmp/ynh-tutorial/my-project/.ynh-plugin/plugin.json << 'EOF'
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
  "focus": {
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
- `.ynh-plugin/plugin.json` in the project root — same format as an installed harness
- No `ynh install` needed — ynh can discover and use this file directly
- Rules, skills, agents, and commands sit alongside `.ynh-plugin/plugin.json` as usual

## T15.2: Validate the project config

```bash
ynd validate /tmp/ynh-tutorial/my-project
```

Expected:
```
/tmp/ynh-tutorial/my-project: valid
```

## T15.3: Preview the assembled output

```bash
ynd preview /tmp/ynh-tutorial/my-project -v claude
```

Expected output includes:
- `.claude/hooks/hooks.json` with the `before_tool` hook (PreToolUse with Write matcher)
- `.claude/rules/standards.md` with the rule content
- `.claude-plugin/plugin.json` with the project name

## T15.4: Preview with --focus

```bash
ynd preview /tmp/ynh-tutorial/my-project -v claude --focus review
```

Expected: same as base preview — the `review` focus has no profile, so it uses the default configuration. The focus prompt is used by `ynh run`, not by `ynd preview`.

## Clean up

```bash
rm -rf /tmp/ynh-tutorial
```

## What You Learned

- `.ynh-plugin/plugin.json` in a project root provides zero-install AI configuration
- `ynd validate`, `ynd preview`, and `ynd diff` work with project directories containing `.ynh-plugin/plugin.json`
- `ynh run` auto-discovers `.ynh-plugin/plugin.json` in the current working directory
- `ynh run --harness-file <path>` points to a specific `.ynh-plugin/plugin.json` file
- The file format is identical to installed harnesses — same hooks, MCP servers, profiles, and focus entries

## Next

The project-local config pattern works well with focus entries (Tutorial 14) for CI automation:

```json
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "default_vendor": "claude",
  "focus": {
    "review": { "prompt": "Review staged changes" },
    "security": { "profile": "ci", "prompt": "Audit for vulnerabilities" }
  }
}
```
