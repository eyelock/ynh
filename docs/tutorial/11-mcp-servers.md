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
mkdir -p /tmp/ynh-tutorial/mcp-harness

cat > /tmp/ynh-tutorial/mcp-harness/harness.json << 'EOF'
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

## T11.2: Preview for Claude

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

Cursor uses the same JSON structure as Claude but places the file at `.cursor/mcp.json` instead of `.claude/.mcp.json`.

## T11.4: Preview for Codex

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

## T11.5: Add an HTTP MCP server

Add a second server using HTTP transport:

```bash
cat > /tmp/ynh-tutorial/mcp-harness/harness.json << 'EOF'
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

Preview for Claude to see both servers:

```bash
ynd preview /tmp/ynh-tutorial/mcp-harness -v claude
```

Expected `.claude/.mcp.json` now includes both servers:

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

Preview for Codex to see the JSON translation with both server types:

```bash
ynd preview /tmp/ynh-tutorial/mcp-harness -v codex
```

Expected `.mcp.json` (at plugin root):

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

## T11.6: Compare MCP config across vendors

```bash
ynd diff /tmp/ynh-tutorial/mcp-harness claude cursor codex
```

Expected output shows:
- `.claude/.mcp.json` only in Claude
- `.cursor/mcp.json` only in Cursor
- `.mcp.json` only in Codex (at plugin root)
- The same two servers appear in all three, in the same JSON format but at different file locations

## Clean up

```bash
rm -rf /tmp/ynh-tutorial
```

## What You Learned

- MCP servers are declared in `harness.json` under `mcp_servers`
- Servers can use stdio transport (`command` + `args`) or HTTP transport (`url`)
- All three vendors use JSON with a `mcpServers` key, but in different file locations
- Claude places MCP config at `.claude/.mcp.json`, Cursor at `.cursor/mcp.json`, and Codex at `.mcp.json` (plugin root)
- `ynd preview` and `ynd diff` let you verify MCP config without installing

## Next

[Tutorial 6: Profiles](tutorial/13-profiles.md) — configure environment-specific overrides with profiles.
