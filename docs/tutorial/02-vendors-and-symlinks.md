# Tutorial 2: Vendors & Symlinks

Run the same harness with different AI coding assistants. Understand how ynh adapts to each vendor's capabilities.

## Setup

Make sure `ynh` and `ynd` are installed and on your PATH. See the [install instructions](tutorial/README.md) if you haven't set up yet.

```bash
# Clean up from any previous run
rm -rf /tmp/ynh-tutorial
ynh uninstall my-harness 2>/dev/null

mkdir -p /tmp/ynh-tutorial
```

## T2.1: Create and install a test harness

```bash
mkdir -p /tmp/ynh-tutorial/my-harness/skills/ping

cat > /tmp/ynh-tutorial/my-harness/.harness.json << 'EOF'
{
  "name": "my-harness",
  "version": "0.1.0",
  "description": "Vendor test harness",
  "default_vendor": "claude"
}
EOF

cat > /tmp/ynh-tutorial/my-harness/skills/ping/SKILL.md << 'EOF'
---
name: ping
description: Reply with pong.
---
Reply with "pong" when invoked.
EOF

ynh install /tmp/ynh-tutorial/my-harness
# Expected: Installed harness "my-harness"
```

## T2.2: List available vendors

```bash
ynh vendors
```

Expected:
```
NAME    DISPLAY NAME  CLI     CONFIG DIR  AVAILABLE
claude  Claude Code   claude  .claude     true
codex   OpenAI Codex  codex   .codex      true
cursor  Cursor        agent   .cursor     true
```

## How vendor resolution works

ynh picks the vendor in this order:
1. CLI flag `-v` (highest priority)
2. Harness's `default_vendor` in `.harness.json`
3. Global `~/.ynh/config.json` default (fallback: "claude")

## T2.3: Switch vendors

```bash
my-harness -v codex
my-harness -v cursor
```

Each launches the same harness through a different vendor CLI. Artifacts are reassembled into the vendor's expected layout.

**Note:** Codex and Cursor require their CLIs installed separately. If missing, you'll see: `exec: "codex": executable file not found in $PATH`.

## T2.4: Symlinks — automatic prompt

When you run a harness with a symlink vendor (`-v codex` or `-v cursor`), ynh checks if symlinks are already installed in the current project directory. If not, it **automatically prompts** you:

```bash
mkdir -p /tmp/ynh-tutorial/project
cd /tmp/ynh-tutorial/project

my-harness -v cursor
```

Expected:
```
cursor requires symlinks in your project directory.
The following symlinks will be created in /tmp/ynh-tutorial/project:

  .cursor/skills/ping -> /Users/<you>/.ynh/run/my-harness/.cursor/skills/ping

Install 1 symlinks? [Y/n]
```

Press `Y` — ynh creates the symlinks and then launches the vendor CLI.

On subsequent runs from the same directory, ynh sees the symlinks are already in place and launches immediately without prompting.

## T2.5: Symlinks — explicit install and clean

You can also manage symlinks explicitly without launching a session:

```bash
# Install symlinks without launching
cd /tmp/ynh-tutorial/project
my-harness -v cursor --install
```

Expected:
```
Installed 1 symlinks for my-harness (cursor) in /tmp/ynh-tutorial/project:

  .cursor/skills/ping -> /Users/<you>/.ynh/run/my-harness/.cursor/skills/ping
```

### Verify symlinks

```bash
ls -la .cursor/skills/
```

Symlinks point back to `~/.ynh/run/my-harness/`.

### Check installation status

```bash
ynh status
```

Expected:
```
HARNESS     VENDOR  PROJECT                    SYMLINKS
my-harness  cursor  /tmp/ynh-tutorial/project  1
```

### Clean symlinks

```bash
my-harness -v cursor --clean
```

Expected:
```
Cleaned cursor symlinks for harness "my-harness" in /tmp/ynh-tutorial/project
```

Verify:
```bash
ynh status
# Expected: No symlink installations found.
```

## T2.6: Symlinks — Claude doesn't need them

```bash
my-harness -v claude --install
# Expected: "claude uses native plugin loading - no symlink installation needed."

my-harness -v claude --clean
# Expected: "claude uses native plugin loading - no symlinks to clean."
```

## T2.7: Prune orphaned installations

If a project directory is deleted while symlinks are still registered, `ynh prune` cleans up the stale entries. It also removes stale launcher scripts from `~/.ynh/bin/` when their harness no longer exists.

### Prune orphaned symlinks

Re-create the project and install symlinks:

```bash
mkdir -p /tmp/ynh-tutorial/project
cd /tmp/ynh-tutorial/project
my-harness -v cursor --install
```

Verify it's tracked:

```bash
ynh status
# Expected: shows my-harness / cursor / /tmp/ynh-tutorial/project
```

Simulate an orphan by deleting the project directory:

```bash
rm -rf /tmp/ynh-tutorial/project
```

Prune finds and removes the orphaned entry:

```bash
ynh prune
```

Expected:
```
Removing orphaned installation: my-harness (cursor) in /tmp/ynh-tutorial/project
```

Verify the orphan was removed:

```bash
ynh status
# Expected: no cursor installation for my-harness in /tmp/ynh-tutorial/project
```

### Prune stale launchers

Simulate a stale launcher by removing the harness directory but leaving its launcher script:

```bash
rm -rf ~/.ynh/harnesses/my-harness
ls ~/.ynh/bin/my-harness
# Expected: file exists (stale launcher)
```

Prune detects and removes the stale launcher:

```bash
ynh prune
```

Expected:
```
Removed stale launcher: /Users/<you>/.ynh/bin/my-harness
Removed stale run dir: /Users/<you>/.ynh/run/my-harness
```

Verify the launcher was removed:

```bash
ls ~/.ynh/bin/my-harness 2>/dev/null
# Expected: no such file
```

Verify ynh and ynd binaries are untouched:

```bash
ls ~/.ynh/bin/ynh ~/.ynh/bin/ynd
# Expected: both still exist
```

## What you learned

- ynh supports three vendors: Claude, Codex, Cursor
- Claude uses `--plugin-dir` (no symlinks needed)
- Codex and Cursor need symlinks from the project directory to ynh's staging area
- ynh **automatically prompts** to install symlinks on first run in a project
- `--install` and `--clean` manage symlinks explicitly without launching
- `ynh status` shows all symlink installations across projects
- `ynh prune` cleans orphaned symlink entries and stale launcher scripts

## Next

[Tutorial 3: Composition](tutorial/03-composition.md) — pull skills from Git repos.
