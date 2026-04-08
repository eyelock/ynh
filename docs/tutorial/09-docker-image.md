# Tutorial 9: Docker Images

Build self-contained Docker images with harnesses baked in. No bind-mounting `~/.ynh` — just mount your workspace and pass API keys.

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

## T9.2: Create and install a tutorial harness

Create a harness to use throughout this tutorial:

```bash
mkdir -p /tmp/ynh-tutorial/docker-harness/skills/greet
mkdir -p /tmp/ynh-tutorial/docker-harness/rules

cat > /tmp/ynh-tutorial/docker-harness/harness.json << 'EOF'
{
  "name": "docker-demo",
  "version": "0.1.0",
  "default_vendor": "claude"
}
EOF

cat > /tmp/ynh-tutorial/docker-harness/instructions.md << 'EOF'
You are a helpful coding assistant running inside a Docker container.
Always mention that you are running in Docker when greeting the user.
EOF

cat > /tmp/ynh-tutorial/docker-harness/skills/greet/SKILL.md << 'EOF'
---
name: greet
description: Greet the user with a friendly message
---

When asked to greet, respond with a warm welcome and mention the current environment.
EOF

cat > /tmp/ynh-tutorial/docker-harness/rules/be-concise.md << 'EOF'
---
name: be-concise
description: Keep responses brief
---

Keep all responses under 3 sentences unless asked for detail.
EOF
```

Install it:

```bash
ynh install /tmp/ynh-tutorial/docker-harness
```

Expected:

```
Installed harness "docker-demo"
  Location: ~/.ynh/harnesses/docker-demo
  Launcher: ~/.ynh/bin/docker-demo
  Vendor:   claude
```

## T9.3: Build a harness image

```bash
ynh image docker-demo --tag docker-demo:latest
```

Expected:

```
Building harness image docker-demo:latest...
...
Harness image built: docker-demo:latest
Run: docker run --rm -v $(pwd):/workspace -e ANTHROPIC_API_KEY docker-demo:latest -- "your prompt"
```

Verify the image exists:

```bash
docker images docker-demo
# Expected: docker-demo   latest   <id>   <size>
```

## T9.4: Run the harness image

The harness's instructions tell it to mention Docker when greeting, and the `be-concise` rule limits responses to 3 sentences. Try it:

```bash
docker run --rm \
  -v $(pwd):/workspace \
  -e ANTHROPIC_API_KEY \
  docker-demo:latest -- "greet me and tell me about your environment"
```

Expected: the response mentions Docker (from `instructions.md`) and stays brief (from `rules/be-concise`). This confirms the baked-in harness artifacts are active.

## T9.5: Switch vendors at runtime

The image defaults to the harness's `default_vendor` (claude). Override with `YNH_VENDOR`:

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

**Note:** Codex requires a Git working tree. If `/workspace` is not a Git repo, add `--skip-git-repo-check`:

```bash
docker run --rm \
  -v $(pwd):/workspace \
  -e OPENAI_API_KEY \
  docker-demo:latest -v codex --skip-git-repo-check -- "greet me and describe your setup"
```

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

# Pre-assembled vendor layouts (all three, ready to use)
COPY --link --chown=ynh:ynh vendors/claude/ /home/ynh/.ynh/run/docker-demo/claude/
COPY --link --chown=ynh:ynh vendors/codex/ /home/ynh/.ynh/run/docker-demo/codex/
COPY --link --chown=ynh:ynh vendors/cursor/ /home/ynh/.ynh/run/docker-demo/cursor/

# Harness source (metadata for ynh run)
COPY --link --chown=ynh:ynh harness/ /home/ynh/.ynh/harnesses/docker-demo/

# Default vendor (override: docker run -e YNH_VENDOR=codex)
ENV YNH_VENDOR=claude

# Baked entrypoint — just pass the prompt as CMD
ENTRYPOINT ["tini", "-s", "--", "ynh", "run", "docker-demo"]
CMD []

LABEL dev.ynh.harness="docker-demo" \
      dev.ynh.harness.default-vendor="claude" \
      dev.ynh.assembled-by="<version>"
```

## T9.8: Build from Git source

Build directly from a Git repo without installing the harness first:

```bash
ynh image tester \
  --from github.com/eyelock/assistants \
  --path ynh/tester \
  --tag tester:latest
```

This clones the repo (or uses the cached clone), loads the harness from `ynh/tester/`, resolves its includes, and builds the image — all without a prior `ynh install`.

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

Access the full ynh/ynd CLI inside the harness image:

```bash
# List installed harnesses inside the image
docker run --rm --entrypoint ynh docker-demo:latest ls

# Check ynd version
docker run --rm --entrypoint ynd docker-demo:latest version

# Shell access for debugging
docker run --rm -it --entrypoint sh docker-demo:latest
```

## T9.10: CI/CD matrix example

Run the same harness across all vendors in a CI pipeline:

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
        ghcr.io/org/harness-review:latest \
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
- `ynh image` builds harness appliance images with pre-assembled vendor layouts
- `--dry-run` previews the generated Dockerfile
- `--from` builds from Git sources without installing first
- `YNH_VENDOR` env var switches vendors at runtime
- Vendor flags pass through to the underlying CLI
- `--entrypoint` overrides give full ynh/ynd access
- Harness images are ready for CI/CD pipelines with no host setup
