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

## Still on `.ynh-plugin`? Migrate it

Before the manifest directory moved under `.agents/`, it was `.ynh-plugin/`.
That location is deprecated. ynh still reads it, second to
`.agents/harness/`, so a project that has not moved keeps working for now,
but every read warns and the fallback will be removed in a later release:

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
above. Each command also prints a warning on stderr that the harness keeps
its manifest in `.ynh-plugin/`, ending `To fix: ynd migrate
/tmp/ynh-tutorial/old-layout`. Nothing is written back: reading a
`.ynh-plugin` harness never creates `.agents/`.

```bash
ls -a /tmp/ynh-tutorial/old-layout
```

Expected: `.ynh-plugin` is still there and there is no `.agents`.

To move a project onto the documented layout, run the fix the warning names:

```bash
ynd migrate --dry-run /tmp/ynh-tutorial/old-layout
ynd migrate -y /tmp/ynh-tutorial/old-layout
ynd validate /tmp/ynh-tutorial/old-layout
```

Expected: the dry run lists `/tmp/ynh-tutorial/old-layout` with
`manifest dir: .ynh-plugin/ → .agents/harness/` and changes nothing. The
second command moves it. `validate` then reports `valid` with no warning,
and its `manifest` line points at `.agents/harness/plugin.json`. In a git
repository, commit the result; git records the rename. If both
directories exist, `ynd migrate` leaves the tree alone and says why,
`.agents/harness` wins, and `ynd validate` reports the shadowed
`.ynh-plugin` copy so it cannot be edited by mistake.

## Clean up

```bash
rm -rf /tmp/ynh-tutorial
```

## What You Learned

- `.agents/harness/plugin.json` in a project root provides zero-install AI configuration
- A project still on `.ynh-plugin/plugin.json` keeps working for now, with a deprecation warning: ynh reads `.agents/harness/` first and falls back to `.ynh-plugin/`. `ynd migrate` moves it
- `ynd validate`, `ynd preview`, and `ynd diff` work with project directories containing `.agents/harness/plugin.json`
- `ynh run` auto-discovers `.agents/harness/plugin.json` in the current working directory
- `ynh run <path>` (for example `ynh run ./my-project`) runs a project directory from anywhere, without installing it
- `ynh run --harness-file <path>` points to a specific manifest file by path. A legacy `.harness.json` is refused there too, with the `ynd migrate` fix
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
