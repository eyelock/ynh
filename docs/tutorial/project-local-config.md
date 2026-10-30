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
ynh no longer reads that location in a tree you own: a project that has not
moved is refused, with the command that fixes it:

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

Expected: both commands fail. `validate` reports that the harness keeps its
manifest in `.ynh-plugin/`, which ynh no longer reads, ending
`move it with: ynd migrate /tmp/ynh-tutorial/old-layout`. `preview` fails with
the same message. Nothing is written back: a refused read never creates
`.agents/` or touches `.ynh-plugin/`.

```bash
ls -a /tmp/ynh-tutorial/old-layout
```

Expected: `.ynh-plugin` is still there and there is no `.agents`.

`ynd migrate` is the one command that still understands the old layout. Run
the fix the error names:

```bash
ynd migrate --dry-run /tmp/ynh-tutorial/old-layout
ynd migrate -y /tmp/ynh-tutorial/old-layout
ynd validate /tmp/ynh-tutorial/old-layout
ynd preview /tmp/ynh-tutorial/old-layout -v claude
```

Expected: the dry run lists `/tmp/ynh-tutorial/old-layout` with
`manifest dir: .ynh-plugin/ → .agents/harness/` and changes nothing. The
second command moves it. `validate` then reports `valid`, and its `manifest`
line points at `.agents/harness/plugin.json`. `preview` lists
`.claude/rules/standards.md`, exactly as for the `.agents/harness` project
above. In a git repository, commit the result; git records the rename. If
both directories exist, `ynh` and `ynd` refuse the tree naming both,
`ynd migrate` leaves it alone and says why, and you choose which manifest to
keep.

## Clean up

```bash
rm -rf /tmp/ynh-tutorial
```

## What You Learned

- `.agents/harness/plugin.json` in a project root provides zero-install AI configuration
- A project still on `.ynh-plugin/plugin.json` is refused with the fix, because ynh no longer reads that directory: `ynd migrate` moves it
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

[Workspace Folders](workspace.md): request sibling folders and see how an allow-list or trust grants them.
