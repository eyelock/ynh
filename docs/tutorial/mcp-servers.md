# MCP Servers

Declare MCP server dependencies in a harness and preview how each vendor configures them. MCP servers give agents access to tools like databases, APIs, and documentation.

## Prerequisites

```bash
# Clean up from any previous run
rm -rf /tmp/ynh-tutorial
```

## Add a stdio MCP server to a harness

Create a harness with an MCP server declaration:

```bash
mkdir -p /tmp/ynh-tutorial/mcp-harness

mkdir -p /tmp/ynh-tutorial/mcp-harness/.agents/harness
cat > /tmp/ynh-tutorial/mcp-harness/.agents/harness/plugin.json << 'EOF'
{
  "name": "mcp-demo",
  "version": "0.1.0",
  "default_vendor": "claude",
  "mcp_servers": {
    "sqlite": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-sqlite", "/tmp/demo.db"],
      "env": {
        "NODE_ENV": "production"
      }
    }
  }
}
EOF

cat > /tmp/ynh-tutorial/mcp-harness/instructions.md << 'EOF'
You are a data analyst assistant with access to a SQLite database via MCP.
EOF
```

## Preview for Claude

```bash
ynd preview /tmp/ynh-tutorial/mcp-harness -v claude
```

Expected output includes `.claude/.mcp.json`:

```json
{
  "mcpServers": {
    "sqlite": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-sqlite", "/tmp/demo.db"],
      "env": {
        "NODE_ENV": "production"
      }
    }
  }
}
```

Claude uses `.claude/.mcp.json` with a `mcpServers` key — the server definition passes through directly.

## Preview for Cursor

```bash
ynd preview /tmp/ynh-tutorial/mcp-harness -v cursor
```

Expected output includes `.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "sqlite": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-sqlite", "/tmp/demo.db"],
      "env": {
        "NODE_ENV": "production"
      }
    }
  }
}
```

Cursor uses the same JSON structure as Claude but places the file at `.cursor/mcp.json` instead of `.claude/.mcp.json`.

`.cursor/mcp.json` is the only MCP file in the Cursor preview, because it is the one project path Cursor reads MCP servers from. There is no root `mcp.json`: that path belongs to a Cursor plugin, and `ynd export -v cursor` writes it there instead (see [MCP Servers: Config File Locations](../mcp.md#config-file-locations)). Check it:

```bash
ynd preview /tmp/ynh-tutorial/mcp-harness -v cursor -o /tmp/ynh-tutorial/mcp-harness-cursor
find /tmp/ynh-tutorial/mcp-harness-cursor -name mcp.json
```

Expected: exactly one line, ending in `.cursor/mcp.json`.

## Preview for Codex

```bash
ynd preview /tmp/ynh-tutorial/mcp-harness -v codex
```

Expected output includes `.mcp.json` with JSON format (same structure as Claude, at plugin root):

```json
{
  "mcpServers": {
    "sqlite": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-sqlite", "/tmp/demo.db"],
      "env": {
        "NODE_ENV": "production"
      }
    }
  }
}
```

Codex uses the same JSON format as Claude with a `mcpServers` key, placed at the plugin root as `.mcp.json`.

## Export as a plugin

An export is a plugin, and a plugin reads its MCP config from a different file than a project session does:

```bash
ynd export /tmp/ynh-tutorial/mcp-harness -v claude,codex,cursor,copilot -o /tmp/ynh-tutorial/mcp-export
cd /tmp/ynh-tutorial/mcp-export && find . -type f | sort && cd - > /dev/null
```

Expected:

```
Exported for claude → /tmp/ynh-tutorial/mcp-export/claude (0 skills, 0 agents)
Exported for codex → /tmp/ynh-tutorial/mcp-export/codex (0 skills, 0 agents)
Exported for cursor → /tmp/ynh-tutorial/mcp-export/cursor (0 skills, 0 agents)
Exported for copilot → /tmp/ynh-tutorial/mcp-export/copilot (0 skills, 0 agents)
./claude/.claude-plugin/plugin.json
./claude/AGENTS.md
./claude/CLAUDE.md
./claude/mcp/claude.json
./codex/.codex-plugin/plugin.json
./codex/.mcp.json
./codex/AGENTS.md
./copilot/.claude-plugin/plugin.json
./copilot/.github/mcp.json
./copilot/AGENTS.md
./cursor/.cursor-plugin/plugin.json
./cursor/.cursorrules
./cursor/AGENTS.md
./cursor/mcp.json
```

- Claude's plugin carries `mcp/claude.json`, and its manifest names it. There is no `.claude/.mcp.json`: that is the session path, and inside a plugin nothing reads it.
- Codex's plugin carries `.mcp.json` at its root, named by its manifest.
- Cursor's plugin carries `mcp.json` (no dot) at its root, which a Cursor plugin discovers automatically. There is no `.cursor/mcp.json`: inside a plugin nothing reads it.
- Copilot's plugin carries `.github/mcp.json` at its root, in Copilot's format with `"type"` and `"tools"` on each server, and its manifest at `.claude-plugin/plugin.json` beside it. Nothing is nested under `.copilot/`, which is only the `ynh run` layout.

Check the manifest pointers:

```bash
grep mcpServers /tmp/ynh-tutorial/mcp-export/*/.*-plugin/plugin.json
```

Expected:

```
/tmp/ynh-tutorial/mcp-export/claude/.claude-plugin/plugin.json:  "mcpServers": "./mcp/claude.json"
/tmp/ynh-tutorial/mcp-export/codex/.codex-plugin/plugin.json:  "mcpServers": "./.mcp.json"
```

Copilot's manifest names nothing, because `.github/mcp.json` is a file it reads by default. A merged package puts every vendor's file in one directory, and none of them shares a path:

```bash
ynd export /tmp/ynh-tutorial/mcp-harness --merged -o /tmp/ynh-tutorial/mcp-merged
cd /tmp/ynh-tutorial/mcp-merged && find . -type f | sort && cd - > /dev/null
```

Expected:

```
Exported for merged → /tmp/ynh-tutorial/mcp-merged (0 skills, 0 agents)
./.claude-plugin/plugin.json
./.codex-plugin/plugin.json
./.cursor-plugin/plugin.json
./.cursorrules
./.github/mcp.json
./.mcp.json
./AGENTS.md
./CLAUDE.md
./mcp.json
./mcp/claude.json
```

`.claude-plugin/plugin.json` names `./mcp/claude.json` and `.codex-plugin/plugin.json` names `./.mcp.json`, as in the per-vendor export. Copilot reads the same `.claude-plugin/plugin.json` as Claude, so here it is also offered `mcp/claude.json`; see [MCP Servers: Config File Locations](../mcp.md#config-file-locations) for what that means.

## Add an HTTP MCP server

Add a second server using HTTP transport:

```bash
mkdir -p /tmp/ynh-tutorial/mcp-harness/.agents/harness
cat > /tmp/ynh-tutorial/mcp-harness/.agents/harness/plugin.json << 'EOF'
{
  "name": "mcp-demo",
  "version": "0.1.0",
  "default_vendor": "claude",
  "env_passthrough": ["DOCS_API_KEY"],
  "mcp_servers": {
    "sqlite": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-sqlite", "/tmp/demo.db"],
      "env": {
        "NODE_ENV": "production"
      }
    },
    "docs-api": {
      "url": "https://docs.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${DOCS_API_KEY}"
      }
    }
  }
}
EOF
```

`${DOCS_API_KEY}` is a reference, not a value — the credential stays out of the
manifest, and therefore out of the repository. It only resolves because the
variable is named in `env_passthrough`.

That allowlist is load-bearing, and two rules keep it from becoming a hole of
its own.

Drop `DOCS_API_KEY` from `env_passthrough` and the reference is refused rather
than resolved, so a manifest cannot name an arbitrary variable in your
environment and quietly copy it into a config file:

```
Error: mcp server "docs-api": headers.Authorization references ${DOCS_API_KEY}, which is not in env_passthrough
```

Keep it declared but leave it unset and that is an error too — an empty
credential would fail somewhere far from its cause. Preview now and you get:

```bash
ynd preview /tmp/ynh-tutorial/mcp-harness -v claude
```

```
Error: mcp server "docs-api": headers.Authorization references ${DOCS_API_KEY}, which is not set
```

So set it, then preview:

```bash
export DOCS_API_KEY=sk-demo-123
ynd preview /tmp/ynh-tutorial/mcp-harness -v claude
```

Expected `.claude/.mcp.json` now includes both servers, with the reference
resolved:

```json
{
  "mcpServers": {
    "docs-api": {
      "type": "http",
      "url": "https://docs.example.com/mcp",
      "headers": {
        "Authorization": "Bearer sk-demo-123"
      }
    },
    "sqlite": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-sqlite", "/tmp/demo.db"],
      "env": {
        "NODE_ENV": "production"
      }
    }
  }
}
```

Preview for Codex to see the JSON translation with both server types:

```bash
ynd preview /tmp/ynh-tutorial/mcp-harness -v codex
```

Expected `.mcp.json` (at plugin root):

```json
{
  "mcpServers": {
    "docs-api": {
      "type": "http",
      "url": "https://docs.example.com/mcp",
      "headers": {
        "Authorization": "Bearer sk-demo-123"
      }
    },
    "sqlite": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-sqlite", "/tmp/demo.db"],
      "env": {
        "NODE_ENV": "production"
      }
    }
  }
}
```

Note what that means: the assembled config holds the real credential, expanded.
The manifest is safe to commit; the assembled output is not. It belongs in the
run directory and in `.gitignore`, never in a harness you publish.

## Compare MCP config across vendors

```bash
ynd diff /tmp/ynh-tutorial/mcp-harness claude cursor codex
```

Expected output shows:
- `.claude/.mcp.json` only in Claude
- `.cursor/mcp.json` only in Cursor
- `.mcp.json` only in Codex (at plugin root)
- The same two servers appear in all three, in the same JSON format but at different file locations

## Edit MCP servers from the command line

MCP servers can be authored from the CLI as well as in the manifest:

```bash
# Top-level (default) MCP server using stdio
ynh mcp add /tmp/ynh-tutorial/mcp-harness github \
    --command npx --arg -y --arg @modelcontextprotocol/server-github \
    --env GITHUB_TOKEN=ghp_xxx
# Added mcp server "github"

# HTTP transport
ynh mcp add /tmp/ynh-tutorial/mcp-harness api \
    --url https://mcp.example.com --header "Authorization=Bearer xyz"
# Added mcp server "api"

# Update an existing entry
ynh mcp update /tmp/ynh-tutorial/mcp-harness github --env GITHUB_TOKEN=ghp_new
# Updated mcp server "github"

# Remove an entry
ynh mcp remove /tmp/ynh-tutorial/mcp-harness api
# Removed mcp server "api"

# Profile-level overlay (with optional --null to suppress an inherited entry).
# The harness has no profile yet, so add one first.
ynh profile add /tmp/ynh-tutorial/mcp-harness ci
# Added profile "ci"
ynh profile mcp add /tmp/ynh-tutorial/mcp-harness ci postgres --null
# Added mcp server "postgres" to profile "ci"
ynh profile remove /tmp/ynh-tutorial/mcp-harness ci
# Removed profile "ci"
```

Quote any `--header` or `--env` value that contains a space: unquoted, `Bearer xyz` is two arguments and `ynh mcp add` prints its usage line instead.

`--command` and `--url` are mutually exclusive; at least one is required at add time. `--null` is profile-only (harness-level entries cannot be null — see [mcp.md §"CLI Editing"](../mcp.md#cli-editing)).

The first positional argument accepts either a filesystem path (during authoring) or a canonical harness id (`local/<name>`, `github.com/<org>/<repo>/<name>`) once installed.

## Declare the transport

A server's transport is normally implied: `command` means stdio and `url`
means Streamable HTTP. The one case the fields cannot express is a remote
server on the deprecated HTTP+SSE transport, so `--type` exists for it. The
names are the [Agent Plugins](https://agent-plugins.org) vocabulary
(`stdio`, `streamable-http`, `sse`), and each vendor gets its own spelling:

```bash
ynh mcp add /tmp/ynh-tutorial/mcp-harness legacy --url https://legacy.example.com/sse --type sse
ynd preview /tmp/ynh-tutorial/mcp-harness -v claude
```

Expected `.claude/.mcp.json` now carries Claude Code's spelling on every
remote server, and none on the stdio ones:
```json
    "docs-api": {
      "type": "http",
      "url": "https://docs.example.com/mcp",
      ...
    },
    "legacy": {
      "type": "sse",
      "url": "https://legacy.example.com/sse"
    },
```

Claude Code rejects a `url` entry with no `type`, so the `http` is not
cosmetic. Copilot's file says `local`, `http` and `sse` for the same three
servers; Cursor's has no transport field at all.

A type that disagrees with the fields is refused rather than written:

```bash
ynh mcp add /tmp/ynh-tutorial/mcp-harness broken --command x --type sse
```

Expected:
```
Error: mcp_servers.broken: type sse requires url
```

## Clean up

```bash
rm -rf /tmp/ynh-tutorial
```

## What You Learned

- MCP servers are declared in `.agents/harness/plugin.json` under `mcp_servers`
- Servers can use stdio transport (`command` + `args`) or HTTP transport (`url`)
- All three vendors use JSON with a `mcpServers` key, but in different file locations
- Claude places MCP config at `.claude/.mcp.json`, Cursor at `.cursor/mcp.json`, and Codex at `.mcp.json` (plugin root)
- An export writes each vendor's plugin MCP file instead: Claude's `mcp/claude.json` (named by its manifest), Codex's `.mcp.json`, Cursor's root `mcp.json`, Copilot's `.github/mcp.json`. No two share a path, so a merged package carries all four
- `ynd preview` and `ynd diff` let you verify MCP config without installing
- MCP servers can be edited from the CLI with `ynh mcp add/update/remove` (top-level) and `ynh profile mcp add/update/remove` (profile-level), with `--null` available on profile-level to suppress an inherited entry
- The transport is inferred from `command` or `url`; `--type sse` declares the one case that cannot be, and each vendor's config carries its own spelling

## Next

[Profiles](profiles.md) — configure environment-specific overrides with profiles.
