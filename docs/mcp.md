# MCP Servers

MCP (Model Context Protocol) servers provide tools and resources that AI coding agents can use during a session. A harness can declare MCP server dependencies so that the correct servers are configured automatically when the harness is installed or previewed.

ynh treats MCP server declarations as part of the harness template. At assembly time, each vendor adapter translates the canonical format into the vendor's native MCP configuration.

## Why Harnesses Declare MCP Servers

Without harness-level MCP declarations, each developer must manually configure MCP servers per vendor per project. A harness that requires a database query tool or a documentation server can declare those dependencies once, and ynh handles vendor translation.

## metadata.json Format

MCP servers are declared under `ynh.mcp_servers` in `metadata.json`. Each key is the server name, and the value defines either a stdio server (with `command` + `args`) or an HTTP server (with `url`).

### Stdio Server

A stdio server runs as a subprocess:

```json
{
  "ynh": {
    "mcp_servers": {
      "sqlite": {
        "command": "npx",
        "args": ["-y", "@modelcontextprotocol/server-sqlite", "/path/to/db.sqlite"],
        "env": {
          "NODE_ENV": "production"
        }
      }
    }
  }
}
```

### HTTP Server

An HTTP server connects to a remote endpoint:

```json
{
  "ynh": {
    "mcp_servers": {
      "docs-api": {
        "url": "https://docs.example.com/mcp",
        "headers": {
          "Authorization": "Bearer ${DOCS_API_KEY}"
        }
      }
    }
  }
}
```

### Fields

| Field | Type | Description |
|-------|------|-------------|
| `command` | string | Executable to launch (stdio servers) |
| `args` | string[] | Arguments to pass to the command |
| `env` | map | Environment variables for the subprocess |
| `url` | string | Endpoint URL (HTTP servers) |
| `headers` | map | HTTP headers for the connection |

Each server must have either `command` or `url`, not both. Validation rejects servers with neither or both.

## Vendor Translation

### Config File Locations

| Vendor | File | Format |
|--------|------|--------|
| Claude Code | `.mcp.json` | JSON with `mcpServers` key |
| Cursor | `.cursor/mcp.json` | JSON with `mcpServers` key |
| Codex | `.codex/config.toml` | TOML with `[mcp_servers.<name>]` sections |

### Claude Code Format

Claude uses `.mcp.json` at the project root with direct passthrough of the server definition:

```json
{
  "mcpServers": {
    "sqlite": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-sqlite", "/path/to/db.sqlite"],
      "env": {
        "NODE_ENV": "production"
      }
    }
  }
}
```

### Cursor Format

Cursor uses `.cursor/mcp.json` with the same JSON structure as Claude:

```json
{
  "mcpServers": {
    "sqlite": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-sqlite", "/path/to/db.sqlite"],
      "env": {
        "NODE_ENV": "production"
      }
    }
  }
}
```

**Cursor env var limitation:** Cursor does not expand `${VAR}` references in env values at runtime. If your MCP server needs environment variables, set them in the shell environment before launching Cursor rather than relying on `${VAR}` syntax in the config.

### Codex TOML Format

Codex uses `.codex/config.toml` with TOML table syntax:

```toml
[mcp_servers.sqlite]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-sqlite", "/path/to/db.sqlite"]

[mcp_servers.sqlite.env]
NODE_ENV = "production"
```

For HTTP servers:

```toml
[mcp_servers.docs-api]
url = "https://docs.example.com/mcp"

[mcp_servers.docs-api.headers]
Authorization = "Bearer ${DOCS_API_KEY}"
```

## Root-Harness-Only Rule

MCP server declarations in **included harnesses** (via `includes`) are dropped during assembly. Only the root harness's MCP servers are configured. This prevents composed harnesses from silently adding tool dependencies.

If an included harness requires an MCP server, add the server declaration to the root harness's `metadata.json`.

## Future

The [Agentic AI Foundation (AAIF)](https://www.linuxfoundation.org/press/linux-foundation-announces-the-formation-of-the-agentic-ai-foundation) is working on standardizing MCP configuration across vendors. When a standard format emerges, ynh will adopt it as the canonical format and translate to any vendor that has not yet adopted the standard.

## See Also

- [Tutorial 11: MCP Servers](tutorial/11-mcp-servers.md) — step-by-step walkthrough
- [Hooks](hooks.md) — lifecycle hooks that bridge guides to sensors
- [Vendor Support](vendors.md) — vendor capabilities and differences
