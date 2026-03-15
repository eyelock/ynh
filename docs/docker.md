# Docker

Run ynh in a container with all vendor CLIs pre-installed. Designed for non-interactive use — CI pipelines, automation, and scripted runs.

## Quick Start

```bash
# Build the image
docker compose build

# Run a persona non-interactively
docker compose run --rm ynh run david "fix this bug"

# List installed personas
docker compose run --rm ynh ls

# Install a persona from Git
docker compose run --rm ynh install github.com/user/my-persona
```

## API Keys

Set vendor API keys as environment variables before running. Create a `.env` file or export them:

```bash
# .env (git-ignored)
ANTHROPIC_API_KEY=sk-ant-...
OPENAI_API_KEY=sk-...
CURSOR_API_KEY=...
```

| Variable | Vendor |
|----------|--------|
| `ANTHROPIC_API_KEY` | Claude Code |
| `OPENAI_API_KEY` | Codex |
| `CURSOR_API_KEY` | Cursor |

## Volumes

### Simple: bind-mount ~/.ynh

The default `docker-compose.yml` mounts your host `~/.ynh` into the container. Personas, cache, and config are shared between host and container:

```bash
docker compose run --rm ynh run david "review this PR"
```

Git cache reuse works automatically — cloned repos persist in `~/.ynh/cache/` across runs.

### Granular: named volumes

Use the `ynh-granular` service to mount subdirectories individually. Useful in CI where you want shared cache but isolated config:

```bash
docker compose run --rm ynh-granular run david "deploy"
```

### Project directory

The current directory is mounted at `/workspace` by default. Override with `PROJECT_DIR`:

```bash
PROJECT_DIR=/path/to/project docker compose run --rm ynh run david "fix tests"
```

### Direct docker run

```bash
docker run --rm \
  -e ANTHROPIC_API_KEY \
  -v ~/.ynh:/home/ynh/.ynh \
  -v $(pwd):/workspace \
  ynh:latest run david "fix this bug"
```

## Vendor Override

```bash
# Use Codex
docker compose run --rm ynh run david -v codex "refactor auth"

# Use Cursor
docker compose run --rm ynh run david -v cursor "review changes"

# Pass vendor-specific flags
docker compose run --rm ynh run david --model opus -- "explain this function"
```

## Claude Code Permissions

Claude Code has a permission system that prompts for approval on tool use. In a headless container there is no TTY to approve prompts.

Pass `--dangerously-skip-permissions` as a vendor flag when running non-interactively:

```bash
docker compose run --rm ynh run david \
  --dangerously-skip-permissions -- "fix the failing test"
```

> **Security note:** This flag disables all permission checks. Only use it in trusted environments where the prompt and codebase are controlled.

## ynd in Docker

Override the entrypoint to use ynd:

```bash
docker compose run --rm --entrypoint ynd ynh lint .
docker compose run --rm --entrypoint ynd ynh validate
docker compose run --rm --entrypoint ynd ynh fmt
```

## Private Git Repos

If your personas reference private repos via `includes` or `delegates_to`, mount your SSH keys (read-only):

Uncomment the SSH volume in `docker-compose.yml`:

```yaml
volumes:
  - ~/.ssh:/home/ynh/.ssh:ro
```

Or pass with `docker run`:

```bash
docker run --rm \
  -e ANTHROPIC_API_KEY \
  -v ~/.ynh:/home/ynh/.ynh \
  -v ~/.ssh:/home/ynh/.ssh:ro \
  -v $(pwd):/workspace \
  ynh:latest run david "deploy"
```

## UID/GID Mapping

When bind-mounting `~/.ynh` from the host, file ownership can mismatch between host and container. Set `USER_UID` and `USER_GID` at build time to match your host user:

```bash
# macOS default UID is 501
docker compose build --build-arg USER_UID=$(id -u) --build-arg USER_GID=$(id -g)
```

Or set in `.env`:

```bash
USER_UID=501
USER_GID=20
```

## Building with a Specific Version

```bash
VERSION=0.1.0 docker compose build
```

The version is injected into the ynh/ynd binaries via ldflags.

Vendor CLI versions are pinned in the Dockerfile via build args (`CLAUDE_CODE_VERSION`, `CODEX_VERSION`). Update these periodically:

```bash
docker compose build --build-arg CLAUDE_CODE_VERSION=2.2.0 --build-arg CODEX_VERSION=0.115.0
```

## Image Metadata

The image includes OCI and ynh-specific labels with version information:

```bash
docker inspect ynh-ynh:latest --format '{{json .Config.Labels}}' | jq .
```

| Label | Description |
|-------|-------------|
| `dev.ynh.version` | ynh/ynd binary version |
| `dev.ynh.claude-code.version` | Claude Code CLI version |
| `dev.ynh.codex.version` | Codex CLI version |
| `dev.ynh.cursor-agent.version` | Cursor Agent CLI version |

## Image Contents

| Component | Source |
|-----------|--------|
| ynh, ynd | Built from source (Go, static binary) |
| Claude Code (`claude`) | `npm install -g @anthropic-ai/claude-code` (pinned) |
| Codex (`codex`) | `npm install -g @openai/codex` (pinned) |
| Cursor (`agent`) | `curl https://cursor.com/install` (latest) |
| Git, SSH, bash | Alpine packages |
| tini | PID 1 signal handler |
