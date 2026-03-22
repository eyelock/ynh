# Tutorial 9: Docker Images

Build self-contained Docker images with personas baked in. No bind-mounting `~/.ynh` — just mount your workspace and pass API keys.

## Prerequisites

```bash
# Clean up from any previous run
rm -rf /tmp/ynh-tutorial
docker rmi docker-demo:latest 2>/dev/null
ynh uninstall docker-demo 2>/dev/null

# Verify Docker is available
docker version
```

## T9.1: Pull the base image

The base image ships with all vendor CLIs pre-installed (Claude Code, Codex, Cursor):

```bash
docker pull ghcr.io/eyelock/ynh:latest
```

> **Tip:** If the base image isn't published yet, you can build it locally from the ynh repo: `make docker-build`

Verify:

```bash
docker run --rm ghcr.io/eyelock/ynh:latest version
# Expected: ynh <version>
```

## T9.2: Create and install a tutorial persona

Create a persona to use throughout this tutorial:

```bash
mkdir -p /tmp/ynh-tutorial/docker-persona/.claude-plugin
mkdir -p /tmp/ynh-tutorial/docker-persona/skills/greet
mkdir -p /tmp/ynh-tutorial/docker-persona/rules

cat > /tmp/ynh-tutorial/docker-persona/.claude-plugin/plugin.json << 'EOF'
{"name": "docker-demo", "version": "0.1.0"}
EOF

cat > /tmp/ynh-tutorial/docker-persona/metadata.json << 'EOF'
{
  "ynh": {
    "default_vendor": "claude"
  }
}
EOF

cat > /tmp/ynh-tutorial/docker-persona/instructions.md << 'EOF'
You are a helpful coding assistant running inside a Docker container.
Always mention that you are running in Docker when greeting the user.
EOF

cat > /tmp/ynh-tutorial/docker-persona/skills/greet/SKILL.md << 'EOF'
---
name: greet
description: Greet the user with a friendly message
---

When asked to greet, respond with a warm welcome and mention the current environment.
EOF

cat > /tmp/ynh-tutorial/docker-persona/rules/be-concise.md << 'EOF'
---
name: be-concise
description: Keep responses brief
---

Keep all responses under 3 sentences unless asked for detail.
EOF
```

Install it:

```bash
ynh install /tmp/ynh-tutorial/docker-persona
```

Expected:

```
Installed persona "docker-demo"
  Location: ~/.ynh/personas/docker-demo
  Launcher: ~/.ynh/bin/docker-demo
  Vendor:   claude
```

## T9.3: Build a persona image

```bash
ynh image docker-demo --tag docker-demo:latest
```

Expected:

```
Building persona image docker-demo:latest...
...
Persona image built: docker-demo:latest
Run: docker run --rm -v $(pwd):/workspace -e ANTHROPIC_API_KEY docker-demo:latest -- "your prompt"
```

Verify the image exists:

```bash
docker images docker-demo
# Expected: docker-demo   latest   <id>   <size>
```

## T9.4: Run the persona image

The persona's instructions tell it to mention Docker when greeting, and the `be-concise` rule limits responses to 3 sentences. Try it:

```bash
docker run --rm \
  -v $(pwd):/workspace \
  -e ANTHROPIC_API_KEY \
  docker-demo:latest -- "greet me and tell me about your environment"
```

Expected: the response mentions Docker (from `instructions.md`) and stays brief (from `rules/be-concise`). This confirms the baked-in persona artifacts are active.

## T9.5: Switch vendors at runtime

The image defaults to the persona's `default_vendor` (claude). Override with `YNH_VENDOR`:

```bash
docker run --rm \
  -v $(pwd):/workspace \
  -e OPENAI_API_KEY \
  -e YNH_VENDOR=codex \
  docker-demo:latest -- "greet me and describe your setup"
```

Or use the `-v` flag directly:

```bash
docker run --rm \
  -v $(pwd):/workspace \
  -e OPENAI_API_KEY \
  docker-demo:latest -v codex -- "greet me and describe your setup"
```

Both do the same thing. `YNH_VENDOR` is more CI-friendly (see T9.10).

## T9.6: Pass vendor flags

Everything after the image name becomes arguments to `ynh run docker-demo`. Unrecognised flags pass through to the vendor CLI:

```bash
# Model override
docker run --rm -v $(pwd):/workspace -e ANTHROPIC_API_KEY \
  docker-demo:latest --model claude-sonnet-4-5-20250514 -- "greet me and tell me which model you are"

# Skip permissions (headless CI)
docker run --rm -v $(pwd):/workspace -e ANTHROPIC_API_KEY \
  docker-demo:latest --dangerously-skip-permissions -- "greet me and tell me about your environment"

# Multiple flags + vendor switch
docker run --rm -v $(pwd):/workspace -e OPENAI_API_KEY \
  docker-demo:latest -v codex --model gpt-4.1 --full-auto -- "greet me and describe your setup"
```

## T9.7: Inspect with --dry-run

Preview the generated Dockerfile without building:

```bash
ynh image docker-demo --dry-run
```

Expected output (Dockerfile printed to stdout):

```dockerfile
FROM ghcr.io/eyelock/ynh:latest

COPY --link --chown=ynh:ynh vendors/claude/ /home/ynh/.ynh/run/docker-demo/claude/
COPY --link --chown=ynh:ynh vendors/codex/ /home/ynh/.ynh/run/docker-demo/codex/
COPY --link --chown=ynh:ynh vendors/cursor/ /home/ynh/.ynh/run/docker-demo/cursor/

COPY --link --chown=ynh:ynh persona/ /home/ynh/.ynh/personas/docker-demo/

ENV YNH_VENDOR=claude

ENTRYPOINT ["tini", "-s", "--", "ynh", "run", "docker-demo"]
CMD []

LABEL dev.ynh.persona="docker-demo" \
      dev.ynh.persona.default-vendor="claude"
```

## T9.8: Build from Git source

Build directly from a Git repo without installing the persona first:

```bash
ynh image tester \
  --from github.com/eyelock/assistants \
  --path ynh/tester \
  --tag tester:latest
```

This clones the repo (or uses the cached clone), loads the persona from `ynh/tester/`, resolves its includes, and builds the image — all without a prior `ynh install`.

Verify the remote includes were resolved and baked in:

```bash
docker run --rm --entrypoint sh tester:latest -c \
  "find /home/ynh/.ynh/run/tester/claude -type f | sort"
# Expected: skills from github.com/eyelock/assistants assembled into the image:
#   /home/ynh/.ynh/run/tester/claude/.claude/skills/dev-quality/SKILL.md
#   /home/ynh/.ynh/run/tester/claude/.claude/skills/go-lang/SKILL.md
#   /home/ynh/.ynh/run/tester/claude/.claude/skills/go-lang/assets/...
```

## T9.9: Override entrypoint

Access the full ynh/ynd CLI inside the persona image:

```bash
# List installed personas inside the image
docker run --rm --entrypoint ynh docker-demo:latest ls

# Check ynd version
docker run --rm --entrypoint ynd docker-demo:latest version

# Shell access for debugging
docker run --rm -it --entrypoint sh docker-demo:latest
```

## T9.10: CI/CD matrix example

Run the same persona across all vendors in a CI pipeline:

```yaml
# .github/workflows/ai-review.yml
strategy:
  matrix:
    vendor: [claude, codex, cursor]
    include:
      - vendor: claude
        api_key_secret: ANTHROPIC_API_KEY
      - vendor: codex
        api_key_secret: OPENAI_API_KEY
      - vendor: cursor
        api_key_secret: CURSOR_API_KEY

steps:
  - uses: actions/checkout@v4
  - run: |
      docker run --rm \
        -v ${{ github.workspace }}:/workspace \
        -e ${{ matrix.api_key_secret }}=${{ secrets[matrix.api_key_secret] }} \
        -e YNH_VENDOR=${{ matrix.vendor }} \
        ghcr.io/org/persona-review:latest \
        --dangerously-skip-permissions -- "review the changes in this PR"
```

## Cleanup

```bash
docker rmi docker-demo:latest 2>/dev/null
docker rmi tester:latest 2>/dev/null
ynh uninstall docker-demo 2>/dev/null
rm -rf /tmp/ynh-tutorial
```

## What you learned

- The base image `ghcr.io/eyelock/ynh:latest` provides the runtime with all vendor CLIs
- `ynh image` builds persona appliance images with pre-assembled vendor layouts
- `--dry-run` previews the generated Dockerfile
- `--from` builds from Git sources without installing first
- `YNH_VENDOR` env var switches vendors at runtime
- Vendor flags pass through to the underlying CLI
- `--entrypoint` overrides give full ynh/ynd access
- Persona images are ready for CI/CD pipelines with no host setup
