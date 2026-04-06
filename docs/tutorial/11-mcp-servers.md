# Tutorial 11: MCP Servers

Declare MCP server dependencies in a harness and preview how each vendor configures them. MCP servers give agents access to tools like databases, APIs, and documentation.

## Prerequisites

```bash
# Clean up from any previous run
rm -rf /tmp/ynh-tutorial
```

## T11.1: Add a stdio MCP server to a harness

Create a harness with an MCP server declaration:

```bash
mkdir -p /tmp/ynh-tutorial/mcp-harness/.claude-plugin

cat > /tmp/ynh-tutorial/mcp-harness/.claude-plugin/plugin.json << 'EOF'
{"name": "mcp-demo", "version": "0.1.0"}
EOF

cat > /tmp/ynh-tutorial/mcp-harness/instructions.md << 'EOF'
You are a data analyst assistant with access to a SQLite database via MCP.
EOF

cat > /tmp/ynh-tutorial/mcp-harness/metadata.json << 'EOF'
{
  "ynh": {
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
}
EOF
```

## T11.2: Preview for Claude

```bash
ynd preview /tmp/ynh-tutorial/mcp-harness -v claude
```

Expected output includes `.mcp.json` at the project root:

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

Claude uses `.mcp.json` with a `mcpServers` key — the server definition passes through directly.

## T11.3: Preview for Cursor

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

Cursor uses the same JSON structure as Claude but places the file at `.cursor/mcp.json` instead of the project root.

## T11.4: Preview for Codex

```bash
ynd preview /tmp/ynh-tutorial/mcp-harness -v codex
```

Expected output includes `.codex/config.toml` with TOML format:

```toml
[mcp_servers.sqlite]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-sqlite", "/tmp/demo.db"]

[mcp_servers.sqlite.env]
NODE_ENV = "production"
```

Codex uses TOML instead of JSON, with `[mcp_servers.<name>]` table headers and a separate `[mcp_servers.<name>.env]` sub-table for environment variables.

## T11.5: Add an HTTP MCP server

Add a second server using HTTP transport:

```bash
cat > /tmp/ynh-tutorial/mcp-harness/metadata.json << 'EOF'
{
  "ynh": {
    "default_vendor": "claude",
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
}
EOF
```

Preview for Claude to see both servers:

```bash
ynd preview /tmp/ynh-tutorial/mcp-harness -v claude
```

Expected `.mcp.json` now includes both servers:

```json
{
  "mcpServers": {
    "docs-api": {
      "url": "https://docs.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${DOCS_API_KEY}"
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

Preview for Codex to see the TOML translation with both server types:

```bash
ynd preview /tmp/ynh-tutorial/mcp-harness -v codex
```

Expected `.codex/config.toml`:

```toml
[mcp_servers.docs-api]
url = "https://docs.example.com/mcp"

[mcp_servers.docs-api.headers]
Authorization = "Bearer ${DOCS_API_KEY}"

[mcp_servers.sqlite]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-sqlite", "/tmp/demo.db"]

[mcp_servers.sqlite.env]
NODE_ENV = "production"
```

## T11.6: Compare MCP config across vendors

```bash
ynd diff /tmp/ynh-tutorial/mcp-harness claude cursor codex
```

Expected output shows:
- `.mcp.json` only in Claude (project-root MCP config)
- `.cursor/mcp.json` only in Cursor
- `.codex/config.toml` only in Codex
- The same two servers appear in all three, but in structurally different formats

## Clean up

```bash
rm -rf /tmp/ynh-tutorial
```

## What You Learned

- MCP servers are declared in `metadata.json` under `ynh.mcp_servers`
- Servers can use stdio transport (`command` + `args`) or HTTP transport (`url`)
- Claude and Cursor both use JSON with `mcpServers` key, but in different file locations
- Codex uses TOML format with `[mcp_servers.<name>]` table sections
- `ynd preview` and `ynd diff` let you verify MCP config without installing

## Next

[Tutorial 12: Developer Preview](12-developer-preview.md) — use preview and diff to iterate on harness design.
