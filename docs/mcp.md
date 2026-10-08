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

**Tool names change on Claude Code.** Without isolation a harness's servers are named `plugin:.claude:<server>` (ynh run loads the assembled `.claude/` directory as the plugin, and Claude names a plugin after its directory); with it they are named `<server>`. A permission rule or hook matcher that names a tool by its server prefix must use the name for the mode the harness runs in.

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
- **An included harness's own `null` removes from its own dependencies.** A `null` in the `mcp_servers` of an included harness, or of the profile selected for it, drops that server from the harnesses it includes, as the root's `null` does for everything. The root's `null` still removes a server whichever harness declared it.
- **Sensors of an included harness are not carried, and its hooks only with consent.** Only the root's sensors are used (see [sensors](sensors.md#includes-root-only)). An included harness's hooks run only when its include says `"hooks": true` (see [hooks](hooks.md#hooks-from-included-harnesses)).
- **Profiles and focuses of an included harness are carried only when selected.** `--profile github:ci` applies the included harness's `ci` profile to that harness alone, and its `mcp_servers` and `includes` change accordingly. See [Profiles and focuses of included harnesses](profiles.md#profiles-and-focuses-of-included-harnesses).

### Exporting

`ynd export` and `ynd marketplace build` ship a plugin that does not contain an include's directory. An included server that points into it (a command or `cwd` starting with `./`, or a `${PLUGIN_ROOT}` reference in its command, args, env or cwd) cannot work there, so the export stops:

```
Error: included MCP server "db" from eyelock/assistants//ynh/db uses a path inside the include and cannot be exported; declare it in the root harness
```

Declare that server in the root harness, or set it to `null` in the root. An included server with no such reference, such as `npx` or a remote `url`, exports normally, and its `${VAR}` references stay literal as for any exported server.

## MCP Servers of Delegates

A delegate (`delegates_to`) is a harness, and the MCP servers it declares are its own. They are not the session's: the parent harness's run does not list them, and the delegate's subagent is the only one that connects to them. ynh resolves the delegate the way it resolves any harness. Its `includes` are followed (transitively, with `pick`, the cycle guard and the allow-list, against the delegate's own directory), and its servers are composed from its included harnesses' and its own under the rules of [Servers from Included Harnesses](#servers-from-included-harnesses). Each server expands in its own harness's context, so `${VAR}` references resolve against the delegate's `env_passthrough`, not the parent's. The skills of the delegate's includes are listed in its agent file with its own. A delegate's own delegates are not followed.

```
$ ynd preview ./team-lead
...
MCP servers of delegates:
  probe: probe-srv (its own)
  probe: db (from eyelock/assistants//ynh/db)
```

| Vendor | Delegate servers |
|--------|------------------|
| Claude Code | Carried. The generated agent has an `mcpServers` frontmatter field and a run hands the agent to Claude with `--agents`; see below. |
| Cursor, Copilot, Codex | Not carried. ynh warns once per delegate on stderr and writes the agent without them. |

The warning reads:

```
  warning: delegate probe declares MCP servers (db, probe-srv) that Cursor subagents cannot carry; they are not available to it
```

No documented per-agent MCP field exists for the other vendors. Copilot's references document MCP only at the project and user level (`.github/mcp.json`, `~/.copilot/mcp-config.json`), so ynh does not guess at a custom-agent field it has not verified. Codex has no agents directory in a run, so its delegates are not assembled at all.

### How Claude Code gets them

Claude Code subagents accept an `mcpServers` frontmatter field: a list whose entries are a one-key map from the server name to its definition, in the same per-server shape as `.mcp.json`. The servers connect when the subagent starts and disconnect when it finishes. ynh writes the field into the agent file:

```markdown
---
name: probe
description: Probe agent with a ping tool
mcpServers: [{"probe-srv":{"command":"python3","args":["/opt/probe/server.py"]}}]
---
```

The tools appear to the subagent as `mcp__<server>__<tool>`.

**Claude ignores that field in a plugin's agents** ("For security reasons, plugin subagents don't support the `hooks`, `mcpServers`, or `permissionMode` frontmatter fields", code.claude.com/docs/en/sub-agents), and `ynh run` loads the assembled directory as a plugin with `--plugin-dir`. Checked against Claude Code 2.1.293: with the field alone, the subagent had no `probe_ping` tool. So for each delegate that declares servers, ynh also writes `<run dir>/.ynh-delegate-agents.json` (mode 0600) and launches Claude with `--agents <that JSON>` (in `ynh run` and `ynh agent run`). An agent passed that way is a session-level definition, which outranks the plugin's agent of the same name and which Claude honours `mcpServers` for. With that, the subagent called `mcp__probe-srv__probe_ping` and got its answer, and the main session's `init` event did not list the server.

### Secrets: `.env.ynh` and `ynh mcp-exec`

A server's `${VAR}` references are resolved from the delegate's own `env_passthrough`, as for any harness: an undeclared reference, or a declared variable that is not set, fails the run with the usual error naming the server and the field. But the `--agents` JSON is a command-line argument, and any local process can read a command line (`ps -axww`). Claude does not expand `${VAR}` inside `--agents` either (the server received the text `${PROBE_TOKEN}`). So ynh does not put the values there. Instead:

- For each delegate with servers that reference a variable, ynh writes `<run dir>/delegates/<delegate>/.env.ynh`, mode 0600 in a 0700 directory, regenerated on every run. It holds exactly the variables that delegate's servers reference, as `NAME="value"` lines. The value is a double-quoted string with Go escapes (`\n`, `\"`, `\\`, `\xNN`), so any value, including `=`, quotes, newlines and arbitrary bytes, stays on one line and reads back exactly. Blank lines and `#` comments are skipped.
- A **stdio** server that references a variable is passed to Claude as `ynh mcp-exec --env-file <abs path to .env.ynh> -- <command> <args...>`. The `args` and `env` keep `${VAR}` literal. `ynh mcp-exec` is an internal command (it is not in `ynh help`): it reads the file, expands `${VAR}` in the arguments and in its own environment values using only the file's variables (an unknown reference is an error naming it, on stderr, with a non-zero exit and nothing on stdout, which is the MCP channel), and then replaces itself with the command, so stdin and stdout are the server's. `ynh` is found through `PATH`, like every other tool of the family.
- A **remote** (`http`, `sse`) server whose `headers` reference a variable keeps its `url` and its other headers as they are, and gets a `headersHelper`: `ynh mcp-headers --env-file <path> -- 'Authorization: Bearer ${TOKEN}'`. Claude runs that command and reads a JSON object of headers from its stdout (checked against Claude Code 2.1.294: a `headersHelper` in a server passed with `--agents` is honoured, and its headers arrive alongside the static ones). The command line carries the template, never the value. A `${VAR}` in the `url` itself is an error that tells you to move the secret to a header.
- A server with no `${VAR}` reference is passed unchanged, with no launcher.
- `${PLUGIN_ROOT}`, `${PLUGIN_DATA}` and plugin-relative `./` paths are not secrets; ynh expands them as it always has. A `${VAR}` in a server's `cwd` is not expanded by anything.

Checked against Claude Code 2.1.294 with `PROBE_TOKEN=s3cret-xyz`: the subagent's tool returned `pong:s3cret-xyz`, and sampling `ps -axww -o args` through the whole run never showed the value in the `claude` process's arguments.

`.env.ynh` matches common `.env.*` ignore patterns, which is the intent, but ynh only ever writes it under its own run directory. It is never written by `ynd export`, `ynd preview` or `ynh image`. In `ynh agent run` the run directory is the loop's temporary config directory, which is removed when the run ends; the launcher reads the file, so the worker's environment (built from the root harness's `env_passthrough` only) does not need the delegate's variables. A baked image (`ynh image`) is assembled without secrets, so its delegates' servers are not carried on Claude; run the harness from source to get them.

The field stays in the agent file as well: it is what `ynd export` ships, and it works once the file is copied into `.claude/agents/`.

### Isolation

A delegate's servers are the delegate's own declaration, so isolation (`mcp_isolation`, `--isolated-mcp`) keeps them. `--strict-mcp-config` restricts the session's servers and the servers of subagent frontmatter, but not servers passed inline with `--agents`, which Claude treats as explicit caller input. Checked: an isolated run's `init` event listed no MCP servers, and the delegate's subagent still called `mcp__probe-srv__probe_ping` and got `pong`.

### Exporting delegates

`ynd export` writes delegate agents too, with the servers unexpanded: `${VAR}` is left literal for the consumer, as for any exported server. Claude Code does not honour `mcpServers` in a plugin's subagents, so in an installed plugin the field is there for whoever copies the agent into `.claude/agents/`. The export says so, with a warning in its result: `delegate probe declares MCP servers (probe-srv); Claude Code ignores mcpServers in plugin agents, so they will not load when this plugin is installed`. A delegate server that points into the delegate's own directory (a `./` command or `cwd`, or a `${PLUGIN_ROOT}` reference), or into one of its includes, cannot be exported, because the plugin carries the agent file and not the delegate's directory. The export stops with an error naming the delegate and the server. Other vendors get a warning in the export result, as above.

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
- [Composing Tools Across Harnesses](tutorial/composing-tools.md): servers, profiles, focuses and hooks across includes
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
