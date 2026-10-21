# MCP Servers

MCP (Model Context Protocol) servers provide tools and resources that AI coding agents can use during a session. A harness can declare MCP server dependencies so that the correct servers are configured automatically when the harness is installed or previewed.

ynh treats MCP server declarations as part of the harness template. At assembly time, each vendor adapter translates the canonical format into the vendor's native MCP configuration.

> **Note:** MCP servers can vary by [profile](harnesses.md#profiles). When a profile is selected, its `mcp_servers` field is deep-merged with the top-level servers: profile keys win on collision, absent keys are inherited, a server's `env` map is merged key by key, and setting a server to `null` removes an inherited one. See [Profiles](profiles.md#merge-semantics).

## Why Harnesses Declare MCP Servers

Without harness-level MCP declarations, each developer must manually configure MCP servers per vendor per project. A harness that requires a database query tool or a documentation server can declare those dependencies once, and ynh handles vendor translation.

## Manifest Format

MCP servers are declared under the top-level `mcp_servers` key in `.agents/harness/plugin.json`. Each key is the server name, and the value defines either a stdio server (with `command` + `args`) or a remote server (with `url`).

The transport vocabulary is the one defined by the [Agent Plugins](https://agent-plugins.org/specification) specification: `stdio`, `streamable-http` and `sse`. A server may declare it with `type`; when it does not, `command` means `stdio` and `url` means `streamable-http`. The only case that needs the field is a remote server that still speaks the deprecated HTTP+SSE transport.

### Stdio Server

A stdio server runs as a subprocess:

```json
{
  "name": "my-harness",
  "version": "0.1.0",
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
```

### HTTP Server

An HTTP server connects to a remote endpoint:

```json
{
  "name": "my-harness",
  "version": "0.1.0",
  "mcp_servers": {
    "docs-api": {
      "url": "https://docs.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${DOCS_API_KEY}"
      }
    }
  }
}
```

### Legacy SSE Server

A remote server on the deprecated HTTP+SSE transport declares it, because nothing else in the entry can:

```json
{
  "mcp_servers": {
    "events": {
      "type": "sse",
      "url": "https://legacy.example.com/sse"
    }
  }
}
```

### Fields

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Transport: `stdio`, `streamable-http` or `sse`. Optional; inferred from `command` or `url` when absent |
| `command` | string | Executable to launch (stdio servers) |
| `args` | string[] | Arguments to pass to the command |
| `env` | map | Environment variables for the subprocess |
| `cwd` | string | Working directory for the subprocess (stdio servers) |
| `url` | string | Endpoint URL (remote servers) |
| `headers` | map | HTTP headers for the connection |

Each server must have either `command` or `url`, not both. Validation rejects servers with neither or both, and a declared `type` that disagrees with those fields (`stdio` without `command`, `streamable-http` or `sse` without `url`).

## Vendor Translation

### Config File Locations

| Vendor | File | Format |
|--------|------|--------|
| Claude Code | `.claude/.mcp.json` in a session, `mcp/claude.json` in a plugin | JSON with `mcpServers` key; remote servers carry `"type": "http"` or `"sse"` |
| Cursor | `.cursor/mcp.json` | JSON with `mcpServers` key; no transport field, Cursor infers it |
| Codex | `.mcp.json` | JSON with `mcpServers` key (at plugin root), same shape as Claude Code |
| Copilot | `.github/mcp.json` (project root) | JSON with `mcpServers` key, each server requires an explicit `"type": "local"|"http"` field |

Claude, Cursor and Copilot read MCP servers from a different file depending on how the harness reaches them, and ynh writes each file only where it is read:

| Vendor | `ynh run`, `ynd preview`, `ynh agent` (a session) | `ynd export`, `ynd marketplace build` (a plugin) |
|--------|------|------|
| Claude Code | `.claude/.mcp.json`: `.mcp.json` at the root of the `.claude/` plugin directory the session loads | `mcp/claude.json`, named by the `mcpServers` field of `.claude-plugin/plugin.json` ([plugin reference](https://code.claude.com/docs/en/plugins-reference)) |
| Codex | `.mcp.json` | `.mcp.json` at the plugin root, named by the `mcpServers` field of `.codex-plugin/plugin.json` |
| Cursor | `.cursor/mcp.json`, the only project-level path Cursor reads ([cursor.com/docs/context/mcp](https://cursor.com/docs/context/mcp)) | `mcp.json` (no dot) at the plugin root, which a Cursor plugin discovers automatically ([cursor.com/docs/reference/plugins](https://cursor.com/docs/reference/plugins)) |
| Copilot | `.copilot/.mcp.json` in the run directory, projected into the calling project's `.github/mcp.json` at launch (see [Copilot Format](#copilot-format)) | `.github/mcp.json` at the plugin root, a default MCP path for a Copilot plugin ([Copilot CLI plugin reference](https://docs.github.com/en/copilot/reference/cli-plugin-reference)) |

The document is identical in both places; only the path differs. A manifest names a file only when the plugin carries it.

Every vendor in a merged package gets a file of its own. A Claude plugin reads `.mcp.json` at its root and then what its manifest's `mcpServers` names, but Codex keeps its `.mcp.json` at the root, so Claude's servers go in `mcp/claude.json` instead. Claude still loads that root file first; it holds the same document, so the merge changes nothing. Copilot also accepts `.mcp.json` at a plugin root, which is why its file is `.github/mcp.json`.

Copilot reads `.claude-plugin/plugin.json`, the manifest Claude writes. In a Copilot-only export that manifest names no MCP file and Copilot reads `.github/mcp.json`, its default. In a package that also carries Claude, the manifest's `mcpServers` names `mcp/claude.json`, and Copilot's plugin reference lists that field beside `.mcp.json` and `.github/mcp.json` without saying how the three combine. `mcp/claude.json` is in Claude's format, which gives a stdio server no `"type"` where Copilot's own file says `"local"`; which file an installed Copilot plugin loads from such a package has not been tested by hand ([#499](https://github.com/eyelock/ynh/issues/499)).

Each adapter spells the canonical transport in the vendor's own words:

| Canonical `type` | Claude Code | Codex | Cursor | Copilot |
|---|---|---|---|---|
| `stdio` | _(omitted)_ | _(omitted)_ | _(omitted)_ | `local` |
| `streamable-http` | `http` | `http` | _(omitted)_ | `http` |
| `sse` | `sse` | `sse` | _(omitted)_ | `sse` |

Claude Code rejects a `url` entry that carries no `type` and reads an untyped entry as stdio, so the `http` on a remote server is not cosmetic.

> **Claude Code:** `ynh run` passes the assembled `.claude/` directory as `--plugin-dir`, and Claude activates its MCP servers (and hooks) from there with no `/plugin install` step. They load alongside your own MCP servers (user config, claude.ai connectors) unless you isolate them: see [Running with only the harness's servers](#running-with-only-the-harness-s-servers). Copilot is the exception: see [Copilot Format](#copilot-format).

### Claude Code Format

Claude uses the same document in `.claude/.mcp.json` (a session) and `mcp/claude.json` (a plugin), with the canonical fields as they are, plus Claude's spelling of the transport on a remote server:

```json
{
  "mcpServers": {
    "sqlite": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-sqlite", "/path/to/db.sqlite"],
      "env": {
        "NODE_ENV": "production"
      }
    },
    "docs-api": {
      "type": "http",
      "url": "https://docs.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${DOCS_API_KEY}"
      }
    }
  }
}
```

### Cursor Format

Cursor uses `.cursor/mcp.json` in a project, or `mcp.json` at the root of an exported plugin, with the same JSON structure as Claude:

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

### Codex Format

Codex uses `.mcp.json` at the plugin root with the same JSON format as Claude:

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

### Copilot Format

Copilot requires an explicit `"type"` field on every server (`"local"` for a `command`-based server, `"http"` for a `url`-based one, `"sse"` for the legacy transport). Claude Code and Codex carry `"type"` only on a remote server and treat an entry without one as stdio; Cursor has no `"type"` field and infers the transport from which fields are present:

```json
{
  "mcpServers": {
    "sqlite": {
      "type": "local",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-sqlite", "/path/to/db.sqlite"],
      "tools": ["*"]
    }
  }
}
```

**Delivery is project-root, not plugin-dir.** Copilot does not read a plugin-bundled `.mcp.json` via `--plugin-dir` (confirmed by hand-testing), so `ynh run` projects the generated config directly into the calling project's `.github/mcp.json` instead: a file fully owned by ynh, distinct from anything the user might hand-author. `ynd export` writes the same document to `.github/mcp.json` at the plugin root, where an installed Copilot plugin reads it (untested by hand: a plugin loaded with `--plugin-dir` ignores it, as above). The manifest sits beside it at `.claude-plugin/plugin.json` in the plugin root, next to the exported skills, whatever other files the export holds.

## Running with only the harness's servers

By default a harness's MCP servers load alongside the servers you have already configured: your user config and, on Claude Code, your claude.ai connectors. To run with only the harness's own servers, isolate them.

Two ways, which combine:

- `mcp_isolation` in the manifest, a boolean that defaults to `false`. A profile may set it too, and a profile's value replaces the harness's when set, either way; a profile that leaves it out inherits.
- `ynh run --isolated-mcp`, which turns isolation on for that run. It cannot turn isolation off: a harness that declares `"mcp_isolation": true` stays isolated.

```json
{
  "mcp_servers": { "docs-api": { "url": "https://example.com/mcp" } },
  "mcp_isolation": true,
  "profiles": {
    "open": { "mcp_isolation": false }
  }
}
```

Isolation is a launch setting. It changes how `ynh run` and `ynh agent run` start the vendor CLI; an export (`ynd export`, `ynd marketplace build`) and `ynd preview` are unchanged.

| Vendor | Isolation |
|--------|-----------|
| Claude Code | Full. ynh adds `--strict-mcp-config` and, when the harness has servers, `--mcp-config=<run dir>/.claude/.mcp.json`; `--plugin-dir` stays, so hooks still fire. A harness with no servers gets none at all. |
| Copilot | Partial. ynh adds `--disable-builtin-mcps` and warns: servers in `~/.copilot/mcp-config.json` still load. |
| Codex | Not supported. ynh warns that your servers will load, and launches normally. |
| Cursor | Not supported. ynh warns that your servers will load, and launches normally. |

The warning is one line on stderr, prefixed `warning:`, and never fails the run. `ynh agent run` applies isolation on the Claude backend only and warns on the others.

**Tool names change on Claude Code.** Without isolation a harness's servers are named `plugin:<harness>:<server>`; with it they are named `<server>`. A permission rule or hook matcher that names a tool by its server prefix must use the name for the mode the harness runs in.

## Servers from Included Harnesses

An include whose directory holds a harness manifest (`.agents/harness/plugin.json`, or an Agent Plugins package) is a harness, not just a folder of artifacts. Its `mcp_servers` are carried into the composed harness along with its skills, agents, rules and commands. An include with no manifest behaves as it always has: artifacts only.

```json
{
  "includes": [
    { "git": "github.com/eyelock/assistants", "path": "ynh/github" }
  ]
}
```

If `ynh/github` declares a `github` server, a run of this harness carries it. `ynd preview` lists each such server with where it came from:

```
MCP servers from included harnesses:
  github (from eyelock/assistants//ynh/github)
```

The rules:

- **Transitive.** An included harness's own `includes` are followed, so what it composes comes with it. Dependencies are assembled before the harness that includes them, and the root last, so later sources keep overriding earlier ones. Relative sources in an included harness resolve against that harness's directory, and the allow-list (`allowed_remote_sources`) applies to every include in the chain.
- **The root wins.** A server the root declares replaces an included server of the same name entirely: nothing is merged field by field.
- **`null` removes.** Set a top-level server to `null` in the root's `mcp_servers` to drop one inherited from an include. A profile can do the same with `null` in its own `mcp_servers`, which removes the server only while that profile is active. Removing a name that no include declares is not an error.
- **Conflicts are errors.** Two includes that declare the same server name must declare it identically, once expanded, or the run stops with an error naming the server and both includes. Declare the server in the root to choose one, or set it to `null` and declare nothing.
- **Each server expands in its own include's context.** `${PLUGIN_ROOT}` and a leading `./` resolve against the include's directory, and `${VAR}` references resolve against the include's own `env_passthrough`. An unset variable fails the run, exactly as it does for the root.
- **The root's `env_passthrough` is not widened.** An include's variables are visible to that include's servers only, and a variable only the root declares is not visible to an include's servers. The allow-list stays a containment boundary the root author controls.
- **`pick` brings the servers but not the includes.** An include that names `pick` contributes the picked artifacts and its own MCP servers, and its own `includes` are not followed. To pull in an include's dependencies, include it without `pick`.
- **Cycles are refused.** A harness that includes itself, directly or through others, fails with the chain: `include cycle: root -> eyelock/a -> eyelock/b -> root`. A harness reached by two routes contributes once.
- **Hooks, focuses, profiles and sensors of an included harness are not carried.** Only the root's are used (see [hooks](hooks.md#root-harness-only-rule) and [sensors](sensors.md#includes-root-only)).

### Exporting

`ynd export` and `ynd marketplace build` ship a plugin that does not contain an include's directory. An included server that points into it (a command or `cwd` starting with `./`, or a `${PLUGIN_ROOT}` reference in its command, args, env or cwd) cannot work there, so the export stops:

```
Error: included MCP server "db" from eyelock/assistants//ynh/db uses a path inside the include and cannot be exported; declare it in the root harness
```

Declare that server in the root harness, or set it to `null` in the root. An included server with no such reference, such as `npx` or a remote `url`, exports normally, and its `${VAR}` references stay literal as for any exported server.

## Plugin Placeholders

The Agent Plugins specification reserves two placeholders for a stdio
server's `args`, `env` values and `cwd`: `${PLUGIN_ROOT}`, the package's own
directory, and `${PLUGIN_DATA}`, a writable directory the client keeps for
that plugin across updates. A `command` or `cwd` beginning with `./` is
relative to the package. ynh does what the specification asks of a client
whenever it assembles a run (`ynh run`, `ynd preview`, `ynh agent run`):

- both placeholders are replaced, in one pass, with the harness directory and
  `~/.ynh/plugin-data/<id>/`, which `ynh run` creates before launching;
- a `./` command or working directory is made absolute against the harness
  directory, since the vendor CLI that launches the server has no idea where
  the package is;
- for a harness [installed from an Agent Plugin](harnesses.md#installing-an-agent-plugin),
  `PLUGIN_ROOT` and `PLUGIN_DATA` are also supplied in each stdio server's
  `env`, last, so a configured entry cannot override them.

The two names are never credentials: `env_passthrough` neither admits nor
refuses them, and `ynd validate` does not report them as undeclared. A ynh
harness that uses them assembles and exports the same way.

## The Portable Format

The [Agent Plugins](https://agent-plugins.org) specification defines a portable `mcp.json` (`$schema`, `mcpServers`, and a mandatory `type` per server) that Codex, Copilot, Cursor and others load directly. ynh's transport names are that specification's, so a harness declaration carries over without translation; the remaining differences (the `$schema` line, `${PLUGIN_ROOT}` and `${PLUGIN_DATA}` placeholders, one-token `command` values) are the export's job. `ynd validate` checks a directory holding a root `plugin.json` with the Agent Plugins schema against that specification.

## CLI Editing

MCP servers can be added, updated, and removed from the command line. The CLI distinguishes harness-level entries from profile-level overlays:

```bash
# Top-level harness MCP servers
ynh mcp add <harness> <name> --command <cmd> [--arg <v>...] [--env K=V...] [--cwd <dir>]
ynh mcp add <harness> <name> --url <url> [--header K=V...] [--type sse]
ynh mcp update <harness> <name> [flags] [--clear-args|--clear-env|--clear-headers]
ynh mcp remove <harness> <name>

# Profile-level MCP servers (override or extend the harness-level set when the profile is active)
ynh profile mcp add <harness> <profile> <name> [...flags] [--null]
ynh profile mcp update <harness> <profile> <name> [flags] [--clear-args|--clear-env|--clear-headers]
ynh profile mcp remove <harness> <profile> <name>
```

`--command` and `--url` are mutually exclusive; at least one is required at add time. `--arg` builds the args array in declaration order; `--env K=V` and `--header K=V` are repeatable. `--type` sets the transport and is refused when it disagrees with the fields; `--cwd` sets the subprocess working directory, and `--cwd ""` on update clears it.

**`--null` is profile-only.** A profile MCP entry can be a JSON null to suppress an inherited server when the profile is active. A harness can also null a server inherited from an [included harness](#servers-from-included-harnesses), but that is written in the manifest by hand: passing `--null` to `ynh mcp add` is rejected.

`mcp update` requires at least one flag. The `--clear-*` flags zero out a collection (args, env, headers) without supplying replacement content — useful for "remove all args" without writing the empty list out manually.

See [reference.md](reference.md) for the complete flag matrix and [profiles.md](profiles.md#cli-editing) for the surrounding profile-editor surface.

## See Also

- [MCP Servers](tutorial/mcp-servers.md) — step-by-step walkthrough
- [Hooks](hooks.md) — lifecycle hooks that bridge guides to sensors
- [Vendor Support](vendors.md) — vendor capabilities and differences

## Credentials — `${VAR}` and `env_passthrough`

An MCP server usually needs a token. Writing it literally in `plugin.json`
means committing it, so `env` values and `headers` support `${VAR}`
references resolved from the environment at assembly time:

```json
{
  "env_passthrough": ["GITHUB_TOKEN"],
  "mcp_servers": {
    "github": {
      "command": "gh-mcp",
      "env": { "TOKEN": "${GITHUB_TOKEN}" }
    }
  }
}
```

`env_passthrough` is the allowlist, and it governs two things deliberately:
which variables a `${VAR}` reference may resolve, and which variables reach an
agent worker's process under [`ynh agent run`](agent.md). One declaration, one
place to review.

Two rules keep the mechanism from becoming a hole of its own:

- **A reference to a variable outside the allowlist is an error.** Otherwise any
  manifest — including one pulled in from another repository — could name any
  variable in your environment and copy it into a config file.
- **A reference to an allowed but unset variable is an error**, not an empty
  string. An empty credential fails somewhere far from its cause, and a control
  that silently degrades is the failure this exists to avoid.

Bare `$VAR` is not expanded. It collides with ordinary shell and path content,
and a credential mechanism should not depend on guessing intent.

### Profiles can narrow it

A profile replaces the list rather than extending it, so a restrictive posture
can actually restrict:

```json
"profiles": {
  "untrusted": { "env_passthrough": [] }
}
```

Under that profile the example above fails to assemble, which is the intended
outcome: the server cannot run without a credential the profile has withheld.

### Export never expands

`ynd export` leaves `${VAR}` references literal. Assembly for a local run
resolves them because the config is about to be used by this operator on this
machine; an export is a distributable artifact, and resolving there would bake
whoever ran the export's credentials into a bundle meant to be shared.

### What this is not

ynh **declares** the scope. The process boundary that makes the declaration
meaningful is the container's — see
[ynh does not own containment](harness-engineering.md).
