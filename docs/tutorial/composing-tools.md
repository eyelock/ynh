# Composing Tools Across Harnesses

Build a small team harness out of two harnesses it includes, and watch what comes along: MCP servers, profiles, focuses and hooks. Each travels by its own rule. The servers come with the include. The profiles and focuses come only when you ask for them by name. The hooks come only when you say they may.

You will build three harnesses, each shipping a copy of one tiny MCP server, and later a fourth as a delegate:

```
my-team          the root: declares its own server, drops one it does not want
  github-lite    includes notes; a server with a secret, a profile, a focus, a hook
    notes        one server and one skill
```

Everything here runs offline. No model is called until the steps marked as real sessions.

## Prerequisites

```bash
# Clean up from any previous run
rm -rf /tmp/ynh-tutorial/composing-tools
unset TRACKER_TOKEN PAGER_TOKEN

mkdir -p /tmp/ynh-tutorial/composing-tools
cd /tmp/ynh-tutorial/composing-tools
```

## A tiny MCP server

Every harness will ship the same server, so the tutorial can show which copy answered. It speaks newline-delimited JSON-RPC on stdin and stdout, has one tool, `whoami`, and reads its name and a token from the environment. Python 3 and its standard library are all it needs:

```bash
cat > server.py << 'EOF'
#!/usr/bin/env python3
"""A tiny MCP server over stdio: newline-delimited JSON-RPC, one tool."""
import json
import os
import sys

NAME = os.environ.get("SERVER_NAME", "unnamed")
TOKEN = "yes" if os.environ.get("TOKEN") else "no"


def reply(msg_id, result):
    print(json.dumps({"jsonrpc": "2.0", "id": msg_id, "result": result}), flush=True)


for line in sys.stdin:
    msg = json.loads(line)
    method, msg_id = msg.get("method"), msg.get("id")
    if msg_id is None:
        continue  # a notification: nothing to answer
    if method == "initialize":
        reply(msg_id, {
            "protocolVersion": "2025-06-18",
            "capabilities": {"tools": {}},
            "serverInfo": {"name": NAME, "version": "0.1.0"},
        })
    elif method == "tools/list":
        reply(msg_id, {"tools": [{
            "name": "whoami",
            "description": "Say which server is answering",
            "inputSchema": {"type": "object", "properties": {}},
        }]})
    elif method == "tools/call":
        text = f"{NAME} answering; token present: {TOKEN}"
        reply(msg_id, {"content": [{"type": "text", "text": text}]})
    else:
        reply(msg_id, {})
EOF
```

Ask it who it is:

```bash
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize"}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"whoami"}}' \
  | SERVER_NAME=demo TOKEN=secret python3 server.py
```

Expected output:

```
{"jsonrpc": "2.0", "id": 1, "result": {"protocolVersion": "2025-06-18", "capabilities": {"tools": {}}, "serverInfo": {"name": "demo", "version": "0.1.0"}}}
{"jsonrpc": "2.0", "id": 2, "result": {"content": [{"type": "text", "text": "demo answering; token present: yes"}]}}
```

The notification in the middle got no answer, as the protocol asks. One more small helper, which later steps use to print the server names in a generated MCP config without the noise:

```bash
cat > servers.py << 'EOF'
import json
import sys

for name, server in json.load(open(sys.argv[1]))["mcpServers"].items():
    print(name, "->", server["env"]["SERVER_NAME"])
EOF
```

## The bottom harness: notes

`notes` has one server and one skill. The server is declared with `${PLUGIN_ROOT}`, which ynh replaces with the directory of the harness that declares it:

```bash
mkdir -p notes/.agents/harness notes/skills/take-note
cp server.py notes/

cat > notes/.agents/harness/plugin.json << 'EOF'
{
  "name": "notes",
  "version": "0.1.0",
  "default_vendor": "claude",
  "mcp_servers": {
    "notes": {
      "command": "python3",
      "args": ["${PLUGIN_ROOT}/server.py"],
      "env": { "SERVER_NAME": "notes" }
    }
  }
}
EOF

cat > notes/skills/take-note/SKILL.md << 'EOF'
---
name: take-note
description: Capture a short note about what was just decided.
---

Write the note as one sentence, then say where you saved it.
EOF
```

## The middle harness: github-lite

`github-lite` includes `notes`, and has more to offer:

- a server `tracker` that needs a secret, passed as `${TRACKER_TOKEN}` and admitted by `env_passthrough`
- a server `noisy` that the root will not want
- a profile `readonly` that swaps `tracker` for a different one
- a focus `triage` that runs under that profile with a prompt of its own
- a hook that runs `./scripts/mark.sh` when a session starts

A local include inside a harness is part of that harness, so these includes name each other by absolute path:

```bash
mkdir -p github-lite/.agents/harness github-lite/skills/triage-issue github-lite/scripts
cp server.py github-lite/

cat > github-lite/.agents/harness/plugin.json << 'EOF'
{
  "name": "github-lite",
  "version": "0.1.0",
  "default_vendor": "claude",
  "includes": [
    { "local": "/tmp/ynh-tutorial/composing-tools/notes" }
  ],
  "mcp_servers": {
    "tracker": {
      "command": "python3",
      "args": ["${PLUGIN_ROOT}/server.py"],
      "env": { "SERVER_NAME": "tracker", "TOKEN": "${TRACKER_TOKEN}" }
    },
    "noisy": {
      "command": "python3",
      "args": ["${PLUGIN_ROOT}/server.py"],
      "env": { "SERVER_NAME": "noisy" }
    }
  },
  "env_passthrough": ["TRACKER_TOKEN"],
  "hooks": {
    "on_session_start": [
      { "command": "./scripts/mark.sh" }
    ]
  },
  "profiles": {
    "readonly": {
      "mcp_servers": {
        "tracker": {
          "command": "python3",
          "args": ["${PLUGIN_ROOT}/server.py"],
          "env": { "SERVER_NAME": "tracker-readonly", "TOKEN": "${TRACKER_TOKEN}" }
        }
      }
    }
  },
  "focuses": {
    "triage": {
      "profile": "readonly",
      "prompt": "Triage the open issues using the tracker server. Do not change anything."
    }
  }
}
EOF

cat > github-lite/skills/triage-issue/SKILL.md << 'EOF'
---
name: triage-issue
description: Sort an incoming issue into a label and an owner.
---

Read the issue, pick one label and one owner, and explain the choice in a sentence.
EOF

cat > github-lite/scripts/mark.sh << 'EOF'
#!/bin/sh
# Leave a marker so we can see the hook ran.
mkdir -p /tmp/ynh-tutorial/composing-tools/markers
date > /tmp/ynh-tutorial/composing-tools/markers/github-lite.started
EOF
chmod +x github-lite/scripts/mark.sh
```

## The root harness: my-team

`my-team` includes `github-lite`, declares one server of its own, and sets `noisy` to `null`. A `null` in the root removes a server it inherited:

```bash
mkdir -p my-team/.agents/harness my-team/skills/team-brief
cp server.py my-team/

cat > my-team/.agents/harness/plugin.json << 'EOF'
{
  "name": "my-team",
  "version": "0.1.0",
  "default_vendor": "claude",
  "includes": [
    { "local": "/tmp/ynh-tutorial/composing-tools/github-lite" }
  ],
  "mcp_servers": {
    "team": {
      "command": "python3",
      "args": ["${PLUGIN_ROOT}/server.py"],
      "env": { "SERVER_NAME": "team" }
    },
    "noisy": null
  }
}
EOF

cat > my-team/instructions.md << 'EOF'
You are a team assistant. Use your MCP servers to answer questions about the team's work.
EOF

cat > my-team/skills/team-brief/SKILL.md << 'EOF'
---
name: team-brief
description: Summarise the team's open work in five lines.
---

Collect the open work from your tools and summarise it in five lines.
EOF
```

## Preview what the root gets

`tracker` reads `${TRACKER_TOKEN}` from the environment, and `github-lite` admits that variable with `env_passthrough`. Nothing sets it yet, so the preview stops before it writes anything:

```bash
ynd preview my-team -v claude -o out/claude 2>&1
```

Expected output:

```
  warning: included harness /tmp/ynh-tutorial/composing-tools/github-lite declares hooks (on_session_start) that are not active; add "hooks": true to its include to run them
Error: MCP servers of included harness /tmp/ynh-tutorial/composing-tools/github-lite: mcp server "tracker": env.TOKEN references ${TRACKER_TOKEN}, which is not set
```

An unset variable is an error for an included server exactly as it is for the root's own. Set it and preview again:

```bash
export TRACKER_TOKEN=demo-token
ynd preview my-team -v claude -o out/claude 2>&1
```

Expected output:

```
  warning: included harness /tmp/ynh-tutorial/composing-tools/github-lite declares hooks (on_session_start) that are not active; add "hooks": true to its include to run them
Preview written to out/claude

MCP servers from included harnesses:
  notes (from /tmp/ynh-tutorial/composing-tools/github-lite > /tmp/ynh-tutorial/composing-tools/notes)
  tracker (from /tmp/ynh-tutorial/composing-tools/github-lite)

Focuses from included harnesses:
  github-lite:triage

Profiles from included harnesses:
  github-lite:readonly

Hooks from included harnesses, not active (add "hooks": true to the include):
  on_session_start (from /tmp/ynh-tutorial/composing-tools/github-lite)
```

Four things to read here:

- **Servers carry their provenance.** `tracker` came from `github-lite`; `notes` came from `notes`, through `github-lite`. `team` is the root's own, so it is not listed. `noisy` is gone: the root's `null` removed it.
- **Focuses and profiles are listed, not applied.** They are offered under the namespace `github-lite:`, and nothing uses them until you select one.
- **The hook is declared, not active.** The warning on the first line, and the last section, say so. Hooks are command execution, so the root has to consent. That is a later step.
- **Skills from every level arrived.** List the files:

```bash
find out/claude -type f | sort
```

Expected output:

```
out/claude/.claude-plugin/plugin.json
out/claude/.claude/.mcp.json
out/claude/.claude/skills/take-note/SKILL.md
out/claude/.claude/skills/team-brief/SKILL.md
out/claude/.claude/skills/triage-issue/SKILL.md
out/claude/CLAUDE.md
```

`take-note` is from `notes`, `triage-issue` from `github-lite`, `team-brief` from the root. There is no hooks file, because no hook is active. Now the MCP config itself:

```bash
cat out/claude/.claude/.mcp.json
```

Expected output:

```json
{
  "mcpServers": {
    "notes": {
      "command": "python3",
      "args": [
        "/tmp/ynh-tutorial/composing-tools/notes/server.py"
      ],
      "env": {
        "SERVER_NAME": "notes"
      }
    },
    "team": {
      "command": "python3",
      "args": [
        "/tmp/ynh-tutorial/composing-tools/my-team/server.py"
      ],
      "env": {
        "SERVER_NAME": "team"
      }
    },
    "tracker": {
      "command": "python3",
      "args": [
        "/tmp/ynh-tutorial/composing-tools/github-lite/server.py"
      ],
      "env": {
        "SERVER_NAME": "tracker",
        "TOKEN": "demo-token"
      }
    }
  }
}
```

Each server expanded in its own harness's context. `${PLUGIN_ROOT}` became the directory of the harness that declared the server, so `notes` runs its own copy of `server.py` and so do the others. `TOKEN` was filled from `TRACKER_TOKEN`, and only `github-lite` could see that variable: `my-team` does not declare it, so the root's `env_passthrough` is not widened.

## The root wins, conflicts are errors, cycles are refused

A server the root declares replaces an included one of the same name, whole. Redeclare `tracker` in `my-team`:

```bash
cat > my-team/.agents/harness/plugin.json << 'EOF'
{
  "name": "my-team",
  "version": "0.1.0",
  "default_vendor": "claude",
  "includes": [
    { "local": "/tmp/ynh-tutorial/composing-tools/github-lite" }
  ],
  "mcp_servers": {
    "team": {
      "command": "python3",
      "args": ["${PLUGIN_ROOT}/server.py"],
      "env": { "SERVER_NAME": "team" }
    },
    "tracker": {
      "command": "python3",
      "args": ["${PLUGIN_ROOT}/server.py"],
      "env": { "SERVER_NAME": "tracker-team" }
    },
    "noisy": null
  }
}
EOF

ynd preview my-team -v claude -o out/override > /dev/null 2>&1
python3 servers.py out/override/.claude/.mcp.json
```

Expected output:

```
notes -> notes
team -> team
tracker -> tracker-team
```

The root's `tracker` won, and nothing was merged field by field: it has no `TOKEN` at all. Now put the manifest back, and make a conflict. A second small harness, `other`, declares a `tracker` of its own, and `my-team` includes both:

```bash
mkdir -p other/.agents/harness
cp server.py other/

cat > other/.agents/harness/plugin.json << 'EOF'
{
  "name": "other",
  "version": "0.1.0",
  "mcp_servers": {
    "tracker": {
      "command": "python3",
      "args": ["${PLUGIN_ROOT}/server.py"],
      "env": { "SERVER_NAME": "other-tracker" }
    }
  }
}
EOF

cat > my-team/.agents/harness/plugin.json << 'EOF'
{
  "name": "my-team",
  "version": "0.1.0",
  "default_vendor": "claude",
  "includes": [
    { "local": "/tmp/ynh-tutorial/composing-tools/github-lite" },
    { "local": "/tmp/ynh-tutorial/composing-tools/other" }
  ],
  "mcp_servers": {
    "team": {
      "command": "python3",
      "args": ["${PLUGIN_ROOT}/server.py"],
      "env": { "SERVER_NAME": "team" }
    },
    "noisy": null
  }
}
EOF

ynd preview my-team -v claude -o out/conflict 2>&1
```

Expected output:

```
  warning: included harness /tmp/ynh-tutorial/composing-tools/github-lite declares hooks (on_session_start) that are not active; add "hooks": true to its include to run them
Error: MCP server "tracker" is defined differently by /tmp/ynh-tutorial/composing-tools/github-lite and /tmp/ynh-tutorial/composing-tools/other; declare it in the root harness to choose one
```

Two includes may declare the same server only if they declare it identically. Different declarations are a conflict, and ynh will not pick one for you. Declaring the server in the root is the choice, as the override above showed; setting it to `null` and declaring nothing is the other.

Last, a cycle. Restore the working `my-team` (one include, `noisy` dropped), then make `notes` include `my-team`, which includes `github-lite`, which includes `notes`:

```bash
cat > my-team/.agents/harness/plugin.json << 'EOF'
{
  "name": "my-team",
  "version": "0.1.0",
  "default_vendor": "claude",
  "includes": [
    { "local": "/tmp/ynh-tutorial/composing-tools/github-lite" }
  ],
  "mcp_servers": {
    "team": {
      "command": "python3",
      "args": ["${PLUGIN_ROOT}/server.py"],
      "env": { "SERVER_NAME": "team" }
    },
    "noisy": null
  }
}
EOF

cp notes/.agents/harness/plugin.json notes-plugin.json.bak
cat > notes/.agents/harness/plugin.json << 'EOF'
{
  "name": "notes",
  "version": "0.1.0",
  "default_vendor": "claude",
  "includes": [
    { "local": "/tmp/ynh-tutorial/composing-tools/my-team" }
  ]
}
EOF

ynd preview my-team -v claude -o out/cycle 2>&1
```

Expected output:

```
Error: resolving includes: include cycle: root -> /tmp/ynh-tutorial/composing-tools/github-lite -> /tmp/ynh-tutorial/composing-tools/notes -> root
```

The error names the whole chain. Put `notes` back:

```bash
mv notes-plugin.json.bak notes/.agents/harness/plugin.json
```

## Run with only the harness's servers

By default a harness's servers load alongside everything you have already configured: your user MCP servers and, on Claude Code, your claude.ai connectors. `--isolated-mcp` runs with only the harness's own. The harness can also ask for it with `"mcp_isolation": true`.

Without the flag, this is what ynh hands Claude Code:

*This launches:* `claude --plugin-dir ~/.ynh/run/_inline-.../.claude --add-dir ~/.ynh/run/_inline-... --append-system-prompt You are a team assistant. ... -p list your MCP servers`
```bash
ynh run ./my-team "list your MCP servers"
```

With it, two more arguments appear, `--strict-mcp-config` and `--mcp-config=` pointing at the harness's own generated file:

*This launches:* `claude --plugin-dir ~/.ynh/run/_inline-.../.claude --add-dir ~/.ynh/run/_inline-... --append-system-prompt You are a team assistant. ... -p list your MCP servers --strict-mcp-config --mcp-config=~/.ynh/run/_inline-.../.claude/.mcp.json`
```bash
ynh run ./my-team --isolated-mcp "list your MCP servers"
```

`--plugin-dir` stays in both, so the harness's hooks and skills still load. Isolation is a launch setting: `ynd preview` and `ynd export` are unchanged by it. Other vendors differ; see [Running with only the harness's servers](../mcp.md#running-with-only-the-harness-s-servers).

With a real model, ask every server to identify itself. `-p` is added for you when you pass a prompt, so it does not belong on the command line. A `-p` session cannot stop to ask you before it calls a tool, so name the tools it may call with `--allowedTools`, which `ynh run` passes through to Claude Code. Without it, every call is refused for want of permission:

*Your output will differ: it shows what the model did.*
```bash
ynh run ./my-team --isolated-mcp "Call the whoami tool on every MCP server you have and list your MCP servers" \
  --allowedTools "mcp__notes__whoami mcp__team__whoami mcp__tracker__whoami"
```

An illustrative answer, from a session with `TRACKER_TOKEN` set:

```
I have three MCP servers, and called whoami on each:

- notes: "notes answering; token present: no"
- team: "team answering; token present: no"
- tracker: "tracker answering; token present: yes"
```

Only `tracker` was given a token. Without `--isolated-mcp` the list would also include whatever servers your own Claude Code configuration carries, and the harness's servers would be named `plugin:.claude:notes` and so on, so the tool names in `--allowedTools` would change with them.

## Select a profile or a focus from an include

An include's profiles and focuses change nothing until you select them as `namespace:name`. The namespace is the included harness's name. Apply the `readonly` profile of `github-lite`:

```bash
ynd preview my-team -v claude --profile github-lite:readonly -o out/readonly > /dev/null 2>&1
python3 servers.py out/readonly/.claude/.mcp.json
```

Expected output:

```
notes -> notes
team -> team
tracker -> tracker-readonly
```

`tracker` changed; the root's `team` did not. The profile applied to `github-lite` alone, before its own includes were followed. To choose a profile for the root in the same run, give `--profile` twice, once unqualified. Give `my-team` a profile `quiet` that swaps its server, then select both:

```bash
python3 - << 'EOF'
import json

path = "my-team/.agents/harness/plugin.json"
manifest = json.load(open(path))
manifest["profiles"] = {
    "quiet": {
        "mcp_servers": {
            "team": {
                "command": "python3",
                "args": ["${PLUGIN_ROOT}/server.py"],
                "env": {"SERVER_NAME": "team-quiet"},
            }
        }
    }
}
json.dump(manifest, open(path, "w"), indent=2)
EOF

ynd preview my-team -v claude --profile quiet --profile github-lite:readonly -o out/both > /dev/null 2>&1
python3 servers.py out/both/.claude/.mcp.json
```

Expected output:

```
notes -> notes
team -> team-quiet
tracker -> tracker-readonly
```

A focus brings its prompt and its profile. `triage` binds the `readonly` profile to a prompt, so `--focus github-lite:triage` is a non-interactive run with both. The run's prompt comes from the focus; the profile is applied to `github-lite`:

*This launches:* `claude --plugin-dir ~/.ynh/run/_inline-.../.claude --add-dir ~/.ynh/run/_inline-... --append-system-prompt You are a team assistant. ... -p Triage the open issues using the tracker server. Do not change anything.`
```bash
ynh run ./my-team --focus github-lite:triage
```

Selecting something that is not there is an error that lists what is:

```bash
ynd preview my-team -v claude --profile nope:x -o out/nope 2>&1
ynd preview my-team -v claude --profile github-lite:x -o out/nope 2>&1
```

Expected output:

```
Error: resolving includes: no included harness has namespace "nope" (available: github-lite, notes)
Error: resolving includes: profile "x" not defined in included harness "github-lite" (available: [readonly])
```

Namespaces include harnesses reached through another: `notes` is addressable too, as `notes:...`.

## Two includes with one name

Suppose a second harness is also called `notes`. It has a profile `draft` that adds a scratch server:

```bash
mkdir -p notes-v2/.agents/harness
cp server.py notes-v2/

cat > notes-v2/.agents/harness/plugin.json << 'EOF'
{
  "name": "notes",
  "version": "2.0.0",
  "profiles": {
    "draft": {
      "mcp_servers": {
        "scratch": {
          "command": "python3",
          "args": ["${PLUGIN_ROOT}/server.py"],
          "env": { "SERVER_NAME": "scratch" }
        }
      }
    }
  }
}
EOF

cat > my-team/.agents/harness/plugin.json << 'EOF'
{
  "name": "my-team",
  "version": "0.1.0",
  "default_vendor": "claude",
  "includes": [
    { "local": "/tmp/ynh-tutorial/composing-tools/github-lite" },
    { "local": "/tmp/ynh-tutorial/composing-tools/notes-v2" }
  ],
  "mcp_servers": {
    "team": {
      "command": "python3",
      "args": ["${PLUGIN_ROOT}/server.py"],
      "env": { "SERVER_NAME": "team" }
    },
    "noisy": null
  }
}
EOF

ynd preview my-team -v claude -o out/ambiguous > /dev/null 2>&1
python3 servers.py out/ambiguous/.claude/.mcp.json
```

Expected output:

```
notes -> notes
team -> team
tracker -> tracker
```

Two harnesses sharing a name is fine until you select by it. Then the namespace is ambiguous:

```bash
ynd preview my-team -v claude --profile notes:draft -o out/ambiguous 2>&1
```

Expected output:

```
Error: resolving includes: namespace "notes" is ambiguous: /tmp/ynh-tutorial/composing-tools/github-lite > /tmp/ynh-tutorial/composing-tools/notes and /tmp/ynh-tutorial/composing-tools/notes-v2; give one include an "as" alias
```

Give the second include an `as` alias, which becomes its namespace. On a Git include, `ynh include add <harness> <url> --as notes-v2` writes the same field; for a local include, you write it:

```bash
cat > my-team/.agents/harness/plugin.json << 'EOF'
{
  "name": "my-team",
  "version": "0.1.0",
  "default_vendor": "claude",
  "includes": [
    { "local": "/tmp/ynh-tutorial/composing-tools/github-lite" },
    { "local": "/tmp/ynh-tutorial/composing-tools/notes-v2", "as": "notes-v2" }
  ],
  "mcp_servers": {
    "team": {
      "command": "python3",
      "args": ["${PLUGIN_ROOT}/server.py"],
      "env": { "SERVER_NAME": "team" }
    },
    "noisy": null
  }
}
EOF

ynd preview my-team -v claude --profile notes-v2:draft -o out/alias > /dev/null 2>&1
python3 servers.py out/alias/.claude/.mcp.json
```

Expected output:

```
notes -> notes
scratch -> scratch
team -> team
tracker -> tracker
```

`scratch` came from the second harness's `draft` profile, selected through its alias, and nothing else moved. Take the second include out again:

```bash
cat > my-team/.agents/harness/plugin.json << 'EOF'
{
  "name": "my-team",
  "version": "0.1.0",
  "default_vendor": "claude",
  "includes": [
    { "local": "/tmp/ynh-tutorial/composing-tools/github-lite" }
  ],
  "mcp_servers": {
    "team": {
      "command": "python3",
      "args": ["${PLUGIN_ROOT}/server.py"],
      "env": { "SERVER_NAME": "team" }
    },
    "noisy": null
  }
}
EOF
```

## Turn the hook on

The warning has been there since the first preview: `github-lite` declares an `on_session_start` hook, and `my-team` has not agreed to run it. Consent is per include and is set with `"hooks": true`. On a Git include, `ynh include add <harness> <url> --hooks` (with `--replace` for one that exists) writes it; for a local include you write it:

```bash
cat > my-team/.agents/harness/plugin.json << 'EOF'
{
  "name": "my-team",
  "version": "0.1.0",
  "default_vendor": "claude",
  "includes": [
    { "local": "/tmp/ynh-tutorial/composing-tools/github-lite", "hooks": true }
  ],
  "mcp_servers": {
    "team": {
      "command": "python3",
      "args": ["${PLUGIN_ROOT}/server.py"],
      "env": { "SERVER_NAME": "team" }
    },
    "noisy": null
  }
}
EOF

ynd preview my-team -v claude -o out/active 2>&1
```

Expected output:

```
Preview written to out/active

MCP servers from included harnesses:
  notes (from /tmp/ynh-tutorial/composing-tools/github-lite > /tmp/ynh-tutorial/composing-tools/notes)
  tracker (from /tmp/ynh-tutorial/composing-tools/github-lite)

Focuses from included harnesses:
  github-lite:triage

Profiles from included harnesses:
  github-lite:readonly

Hooks from included harnesses:
  on_session_start (from /tmp/ynh-tutorial/composing-tools/github-lite)
```

The warning is gone and the hook is listed as active. See what it produced:

```bash
find out/active -type f \( -path '*hooks*' -o -path '*scripts*' \) | sort
cat out/active/.claude/hooks/hooks.json
```

Expected output:

```
out/active/.claude/hooks/hooks.json
out/active/.claude/scripts/_include/github-lite/scripts/mark.sh
{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "\"${CLAUDE_PLUGIN_ROOT}\"/scripts/_include/github-lite/scripts/mark.sh"
          }
        ]
      }
    ]
  }
}
```

The script `github-lite` ships was copied under `scripts/_include/github-lite/`, keeping its path inside its harness, and the command was rewritten to reach it. The directory is named for the include's namespace, so the root's own `scripts/mark.sh`, if it had one, would not collide.

Now a real session. Nothing has written the marker yet:

```bash
test -e /tmp/ynh-tutorial/composing-tools/markers/github-lite.started && echo present || echo absent
```

Expected output:

```
absent
```

Start a session, then look again:

*Your output will differ: it shows what the model did.*
```bash
ynh run ./my-team "say hello"
test -e /tmp/ynh-tutorial/composing-tools/markers/github-lite.started && echo present || echo absent
```

An illustrative result:

```
present
```

The hook ran when the session started, from the copy in the run directory, and left its marker. Run the same thing with the include's `"hooks": true` taken out and the marker never appears.

## Give a delegate its own tools

A delegate (`delegates_to`) is a harness that runs as a subagent, and the MCP servers it declares are its own. The main session never connects to them; only the delegate's subagent does. Make an `ops` harness that ships the same server as `pager`, with a token of its own. A delegate must be a Git repository, so commit it:

```bash
mkdir -p ops/.agents/harness
cp server.py ops/

cat > ops/.agents/harness/plugin.json << 'EOF'
{
  "name": "ops",
  "version": "0.1.0",
  "default_vendor": "claude",
  "mcp_servers": {
    "pager": {
      "command": "python3",
      "args": ["${PLUGIN_ROOT}/server.py"],
      "env": { "SERVER_NAME": "pager", "TOKEN": "${PAGER_TOKEN}" }
    }
  },
  "env_passthrough": ["PAGER_TOKEN"]
}
EOF

cat > ops/instructions.md << 'EOF'
You are the on-call assistant. Use the pager server to answer.
EOF

git -C ops init -q
git -C ops add .
git -C ops commit -q -m "init"
```

Add it to `my-team` as a delegate, keeping the hook consent from the last section:

```bash
cat > my-team/.agents/harness/plugin.json << 'EOF'
{
  "name": "my-team",
  "version": "0.1.0",
  "default_vendor": "claude",
  "includes": [
    { "local": "/tmp/ynh-tutorial/composing-tools/github-lite", "hooks": true }
  ],
  "delegates_to": [
    { "git": "/tmp/ynh-tutorial/composing-tools/ops" }
  ],
  "mcp_servers": {
    "team": {
      "command": "python3",
      "args": ["${PLUGIN_ROOT}/server.py"],
      "env": { "SERVER_NAME": "team" }
    },
    "noisy": null
  }
}
EOF
```

`pager` reads `${PAGER_TOKEN}`, which nothing sets yet. As for any harness, an unset variable is an error, and it names the delegate:

```bash
ynd preview my-team -v claude -o out/delegate 2>&1
```

Expected output:

```
Error: assembling delegates: delegate ops: mcp server "pager": env.TOKEN references ${PAGER_TOKEN}, which is not set
```

Set it and preview again:

```bash
export PAGER_TOKEN=demo-pager-token
ynd preview my-team -v claude -o out/delegate 2>&1
```

Expected output:

```
Preview written to out/delegate

MCP servers from included harnesses:
  notes (from /tmp/ynh-tutorial/composing-tools/github-lite > /tmp/ynh-tutorial/composing-tools/notes)
  tracker (from /tmp/ynh-tutorial/composing-tools/github-lite)

MCP servers of delegates:
  ops: pager (its own)

Focuses from included harnesses:
  github-lite:triage

Profiles from included harnesses:
  github-lite:readonly

Hooks from included harnesses:
  on_session_start (from /tmp/ynh-tutorial/composing-tools/github-lite)
```

The delegate's server has its own section. It is not in the root's MCP config, and the agent file written for `ops` carries it instead:

```bash
python3 servers.py out/delegate/.claude/.mcp.json
sed -n '1,5p' out/delegate/.claude/agents/ops.md | sed -E 's#"/[^"]*/server.py"#"<cache>/server.py"#'
```

Expected output:

```
notes -> notes
team -> team
tracker -> tracker
---
name: ops
description: Delegate harness "ops".
mcpServers: [{"pager":{"command":"python3","args":["<cache>/server.py"],"env":{"SERVER_NAME":"pager","TOKEN":"${PAGER_TOKEN}"}}}]
---
```

There is no `pager` in the root's servers. In the agent file `${PAGER_TOKEN}` is still the literal text: the preview resolved the variable to check it, and kept the value out of what it wrote. `<cache>` stands for the delegate's cached copy under `~/.ynh/cache/`.

Only Claude Code can give a subagent its own servers. Another vendor gets the agent without them, and says so:

```bash
ynd preview my-team -v cursor -o out/delegate-cursor 2>&1 >/dev/null
```

Expected output:

```
  warning: delegate ops declares MCP servers (pager) that Cursor subagents cannot carry; they are not available to it
```

Now launch it. Claude Code ignores `mcpServers` in a plugin's agents, so `ynh run` hands the delegate to Claude with `--agents`. A command line is visible to every process on the machine, and Claude does not expand `${VAR}` inside `--agents`, so the token cannot go there. The server is wrapped in `ynh mcp-exec`, which reads the variables from a file in the run directory and starts the real server. The `...` below stands for text that varies between runs or is too long to show:

*This launches:* `claude --plugin-dir ~/.ynh/run/_inline-.../.claude --add-dir ~/.ynh/run/_inline-... --append-system-prompt You are a team assistant. ... --agents ...mcp-exec","--env-file","~/.ynh/run/_inline-.../delegates/ops/.env.ynh","--",... -p Ask the ops agent to call its whoami tool --strict-mcp-config --mcp-config=~/.ynh/run/_inline-.../.claude/.mcp.json`
```bash
ynh run ./my-team --isolated-mcp "Ask the ops agent to call its whoami tool"
```

The arguments name `mcp-exec` and the file, and no token. `--isolated-mcp` keeps the delegate's server: it limits the session's servers, not the ones passed with `--agents`. The token lives only in `.env.ynh`, which ynh writes for this run, mode 0600 in a private directory:

```bash
(cd ~/.ynh/run/_inline-*/delegates/ops && ls -l .env.ynh | awk '{print substr($1, 2, 9), $NF}' && cat .env.ynh)
```

Expected output:

```
rw------- .env.ynh
PAGER_TOKEN="demo-pager-token"
```

With a real model, ask the delegate for its tool. The tool is `mcp__pager__whoami`; name it in `--allowedTools`:

*Your output will differ: it shows what the model did.*
```bash
ynh run ./my-team "Use the ops agent to call its whoami tool and report exactly what it returned" --allowedTools mcp__pager__whoami
```

An illustrative answer:

```
The ops agent called whoami and it returned: "pager answering; token present: yes"
```

The main session has no `pager` server of its own: ask it to list its servers and `pager` is not among them. The delegate has it, with the token, and the token never appeared on a command line.

## Clean up

```bash
cd ~
rm -rf /tmp/ynh-tutorial/composing-tools
rmdir /tmp/ynh-tutorial 2>/dev/null
unset TRACKER_TOKEN PAGER_TOKEN
```

## What You Learned

- An include that holds a harness manifest brings its MCP servers, and those of the harnesses it includes, with the include
- `ynd preview` lists each included server with its provenance, as `include > nested include`
- Each server expands in its own harness's context: `${PLUGIN_ROOT}` is the declaring harness's directory, and `${VAR}` needs that harness's own `env_passthrough`
- The root wins by redeclaring a server whole, and removes one with `null`
- Two includes declaring the same server differently is an error until the root decides; a cycle is an error that names the chain
- `--isolated-mcp` launches with only the harness's servers, on Claude Code via `--strict-mcp-config` and `--mcp-config`
- Profiles and focuses of an include apply only when selected as `namespace:name`, to that include alone; `--profile` repeats once per namespace
- A name shared by two includes is ambiguous only when selected, and an `as` alias on an include settles it
- An included harness's hooks are declared but inactive, with a warning, until the include says `"hooks": true`; its scripts are then placed under `scripts/_include/<namespace>/`
- A delegate's MCP servers are its own: they reach its subagent through `--agents`, the main session never sees them, and its secrets travel in a private `.env.ynh` read by `ynh mcp-exec`, never on a command line

## Next

[Project-Local Config](project-local-config.md): use `.agents/harness/plugin.json` in your project root for zero-install configuration.

To go further with what you built here, [Delegation](delegation.md) chains harnesses as subagents instead of including them, and the [MCP reference](../mcp.md) lists every rule for servers from included harnesses.
