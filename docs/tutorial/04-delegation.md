# Tutorial 4: Delegation

Chain harnesses together. A parent harness can invoke other harnesses as subagents — each with their own instructions, rules, and skills.

## Prerequisites

```bash
# Clean up from any previous run
rm -rf /tmp/ynh-tutorial
ynh uninstall team-lead 2>/dev/null

mkdir -p /tmp/ynh-tutorial
```

## T4.1: Create a delegate harness

Delegates must be Git repos (local or remote). Create a specialist harness and turn it into a Git repo:

```bash
mkdir -p /tmp/ynh-tutorial/specialist/.claude-plugin
mkdir -p /tmp/ynh-tutorial/specialist/skills/analyze

cat > /tmp/ynh-tutorial/specialist/.claude-plugin/plugin.json << 'EOF'
{
  "name": "specialist",
  "version": "0.1.0",
  "description": "Code analysis specialist"
}
EOF

cat > /tmp/ynh-tutorial/specialist/instructions.md << 'EOF'
You are a code analysis specialist. When delegated to, provide
detailed technical analysis. Always cite file paths and line numbers.
EOF

cat > /tmp/ynh-tutorial/specialist/skills/analyze/SKILL.md << 'EOF'
---
name: analyze
description: Deep code analysis with complexity metrics.
---

Analyze the specified code for:
1. Cyclomatic complexity
2. Dependency coupling
3. Test coverage gaps
Provide metrics and actionable recommendations.
EOF

# Make it a Git repo (required for delegation)
git -C /tmp/ynh-tutorial/specialist init
git -C /tmp/ynh-tutorial/specialist add .
git -C /tmp/ynh-tutorial/specialist commit -m "init"
```

## T4.2: Create a parent harness with delegates

```bash
mkdir -p /tmp/ynh-tutorial/team-lead/.claude-plugin

cat > /tmp/ynh-tutorial/team-lead/.claude-plugin/plugin.json << 'EOF'
{
  "name": "team-lead",
  "version": "0.1.0",
  "description": "Team lead harness with specialist delegates"
}
EOF

cat > /tmp/ynh-tutorial/team-lead/instructions.md << 'EOF'
You are a team lead. Delegate specialist tasks to your team members.
Use the specialist delegate for deep code analysis.
EOF

cat > /tmp/ynh-tutorial/team-lead/metadata.json << 'EOF'
{
  "ynh": {
    "default_vendor": "claude",
    "delegates_to": [
      {"git": "/tmp/ynh-tutorial/specialist"},
      {"git": "github.com/eyelock/ynh"}
    ]
  }
}
EOF
```

## T4.3: Install and verify

```bash
ynh install /tmp/ynh-tutorial/team-lead
```

Expected install output includes delegate fetching:
```
Fetching 0 include(s) and 2 delegate(s)...
  Fetched /tmp/ynh-tutorial/specialist
  Fetched eyelock/ynh
Installed harness "team-lead"
```

```bash
ynh ls
```

Expected:
```
NAME       VENDOR  SOURCE                          ARTIFACTS  INCLUDES  DELEGATES TO
team-lead  claude  /tmp/ynh-tutorial/team-lead      ...        0         /tmp/ynh-tutorial/specialist, eyelock/ynh
```

## T4.4: Inspect delegate agent files

Delegate repos are fetched at install time and cached locally. Agent files are generated at runtime from the cached repos. Run the harness to trigger assembly:

```bash
team-lead "list your available agents"
```

Check what was generated:

```bash
ls ~/.ynh/run/team-lead/.claude/agents/
```

Expected: `specialist.md` and `ynh.md` — generated agent files with the delegate's instructions, rules, and skill lists inlined.

```bash
cat ~/.ynh/run/team-lead/.claude/agents/specialist.md
```

Expected: frontmatter with name/description, then sections for Instructions, Rules, and Available Skills — all pulled from the specialist harness.

## T4.5: Test delegation

```bash
team-lead "delegate to the specialist agent and ask it to analyze this project's main.go"
```

The specialist's `instructions.md` says to provide detailed technical analysis with file paths. If you see that style of response, delegation is working.

```bash
team-lead "ask the ynh delegate what the /ynh-validate skill does"
```

This delegates to the ynh harness — a remote delegate from GitHub.

## Clean up

```bash
ynh uninstall team-lead
```

## What you learned

- `delegates_to` in metadata.json references other harnesses as subagents
- Delegates must be Git repos (local or remote)
- ynh generates vendor-native agent files from delegate harnesss at runtime
- Agent files inline the delegate's instructions, rules, and skill list
- Delegate repos are fetched at install time and cached — `ynh run` works offline
- Use `ynh update` to refresh cached delegate repos

## Next

[Tutorial 5: Export](tutorial/05-export.md) — produce vendor-native distributable plugins.
