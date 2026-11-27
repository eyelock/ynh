# Workspace Folders

A harness can ask for extra folders to be reachable by the vendor CLI, such as the sibling repositories its work happens in. The harness requests; you grant. In this tutorial a harness asks for two folders, and you see it refused until you accept it, granted by trust, narrowed by an allow-list, rejected for a path that is never allowed, and switched off for one run.

Every launch below reaches a stub, so nothing here starts a real vendor CLI.

## Prerequisites

```bash
# Clean up from any previous run
rm -rf /tmp/ynh-tutorial
```

## A harness that requests folders

The harness lives in `seat`. Its work is in the sibling `ynh` folder, and in `elsewhere`, a folder outside the tree you will allow later. Relative entries resolve against the harness's own directory.

```bash
mkdir -p /tmp/ynh-tutorial/ws/seat/.agents/harness /tmp/ynh-tutorial/ws/ynh /tmp/ynh-tutorial/ws/project /tmp/ynh-tutorial/elsewhere

cat > /tmp/ynh-tutorial/ws/seat/.agents/harness/plugin.json << 'EOF'
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "seat",
  "version": "0.1.0",
  "default_vendor": "claude",
  "workspace": ["../ynh", "../../elsewhere"]
}
EOF

ynh install /tmp/ynh-tutorial/ws/seat
```

Expected output includes `Installed harness "seat"`.

## An unaccepted harness does not run

Run it from the `project` folder. The harness requests folders, and a request is part of what you review, so the [trust gate](../trust.md#the-gate) refuses an installed harness that asks for folders until you have accepted it. Nothing launches, and the command exits 1:

```bash
cd /tmp/ynh-tutorial/ws/project
ynh run local/seat -- "hello"
```

Expected on stderr (paths shown are where they resolve; on macOS `/tmp` is `/private/tmp`):

```
Error: refusing to run local/seat: you have not accepted what it asks for.
It requests workspace folders:
  ../ynh (resolves to /tmp/ynh-tutorial/ws/ynh)
  ../../elsewhere (resolves to /tmp/ynh-tutorial/elsewhere)
Review them: ynh trust show local/seat
Then accept:  ynh trust accept local/seat
```

The harness runs no commands, so what the refusal lists, and what there is to review, is the two folders it requests. An allow-list does not get past the gate either: `YNH_WORKSPACE_ROOTS=/tmp/ynh-tutorial/ws ynh run local/seat -- "hello"` is refused the same way. The allow-list decides which folders an accepted harness is granted, not whether it runs.

## Review the request

`ynh trust show` lists the folders as part of what you are asked to accept:

```bash
ynh trust show local/seat | sed -n '1,/^No allow-list/p'
```

Expected output includes (paths shown are where they resolve; on macOS `/tmp` is `/private/tmp`):

```
Workspace folders, requested for the vendor CLI to reach (a request, not a grant):

  ../ynh
    resolves to: /tmp/ynh-tutorial/ws/ynh
  ../../elsewhere
    resolves to: /tmp/ynh-tutorial/elsewhere

No allow-list is set, so these are granted only while this harness is accepted.
```

## Grant with trust

Accept it. That lets the harness run and, with no allow-list set, grants the folders. The digest covers the requested entries, so changing one in the manifest later reports `changed`, and the gate refuses the harness until you accept again:

```bash
ynh trust accept local/seat
```

Expected output: `Accepted local/seat (accept): 0 command(s), 2 workspace folder(s), sha256:...`. It is the same review and acceptance you did in [Your First Trusted Harness](first-trusted-harness.md).

*This launches:* `claude --plugin-dir ~/.ynh/run/.snapshots/local--seat/.../.claude --allowedTools=Read(/~/.ynh/run/.snapshots/local--seat/.../**) -p hello --add-dir .../ynh-tutorial/ws/ynh --add-dir .../ynh-tutorial/elsewhere`

```bash
cd /tmp/ynh-tutorial/ws/project
ynh run local/seat -- "hello"
```

Expected on stderr:

```
  workspace: granted /tmp/ynh-tutorial/ws/ynh (harness trust-accepted)
  workspace: granted /tmp/ynh-tutorial/elsewhere (harness trust-accepted)
Launching claude...
```

No `--add-dir` is passed for ynh's own assembled directory: Claude Code reads it through an allow rule (`--allowedTools=Read(//<dir>/**)`), because it asks to trust every `--add-dir` folder it has not seen, and the directory's path changes with the harness. Each granted folder became Claude Code's own `--add-dir` flag.

## Narrow it with an allow-list

`YNH_WORKSPACE_ROOTS` (or `workspace_roots` in `~/.ynh/config.json`) is a boundary the operator draws. Once it is set, a requested folder is granted only when it resolves inside one of its roots, and one outside it is refused, even though the harness is accepted:

*This launches:* `claude --plugin-dir ~/.ynh/run/.snapshots/local--seat/.../.claude --allowedTools=Read(/~/.ynh/run/.snapshots/local--seat/.../**) -p hello --add-dir .../ynh-tutorial/ws/ynh`

```bash
cd /tmp/ynh-tutorial/ws/project
YNH_WORKSPACE_ROOTS=/tmp/ynh-tutorial/ws ynh run local/seat -- "hello"
```

Expected on stderr:

```
warning: workspace: "../../elsewhere" not granted: /tmp/ynh-tutorial/elsewhere is outside the YNH_WORKSPACE_ROOTS allow-list (/tmp/ynh-tutorial/ws)
  workspace: granted /tmp/ynh-tutorial/ws/ynh (inside the YNH_WORKSPACE_ROOTS allow-list)
Launching claude...
```

The granted folder became `--add-dir`. The refused one did not. With an allow-list set, trust cannot grant what the list does not cover.

## Paths that are never granted

Some paths are refused whatever the allow-list or trust say: the filesystem root, your home directory, a folder that contains the harness or the project, and one that does not exist. A harness that asks for your home directory gets nothing, even when the allow-list covers it. This one is run from its folder, not installed: a run from a folder is not trust-checked, and ynh says so first:

*This launches:* `claude --plugin-dir ~/.ynh/run/.snapshots/_inline-.../.../.claude --allowedTools=Read(/~/.ynh/run/.snapshots/_inline-.../.../**) -p hello`

```bash
mkdir -p /tmp/ynh-tutorial/ws/greedy/.agents/harness
cat > /tmp/ynh-tutorial/ws/greedy/.agents/harness/plugin.json << 'EOF'
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "greedy",
  "version": "0.1.0",
  "default_vendor": "claude",
  "workspace": ["~", "/"]
}
EOF

cd /tmp/ynh-tutorial/ws/project
YNH_WORKSPACE_ROOTS="$HOME" ynh run /tmp/ynh-tutorial/ws/greedy -- "hello"
```

Expected on stderr:

```
ynh: greedy runs from a folder, so its own commands are not trust-checked; the commands its remote includes bring are.
warning: workspace: "~" not granted: ... is your home directory
warning: workspace: "/" not granted: / is the filesystem root
Launching claude...
```

`ynd validate` reports the same entries as errors, before anyone runs the harness:

```bash
ynd validate /tmp/ynh-tutorial/ws/greedy
```

Expected output includes `workspace "~" is never granted: is your home directory` and `workspace "/" is never granted: is the filesystem root`. The command exits with an error.

## Other vendors

Each granted folder becomes the vendor's own flag. Codex takes `--add-dir` too:

*This launches:* `codex exec --skip-git-repo-check --add-dir .../ynh-tutorial/ws/ynh hello`

```bash
cd /tmp/ynh-tutorial/ws/project
YNH_WORKSPACE_ROOTS=/tmp/ynh-tutorial/ws ynh run local/seat -v codex -- "hello"
```

Cursor takes `--add-dir` as well:

*This launches:* `agent -p hello --trust --add-dir .../ynh-tutorial/ws/ynh`

```bash
cd /tmp/ynh-tutorial/ws/project
YNH_WORKSPACE_ROOTS=/tmp/ynh-tutorial/ws ynh run local/seat -v cursor -- "hello"
```

Expected on stderr includes `workspace: granted /tmp/ynh-tutorial/ws/ynh`. Whether the flag changes what Cursor can reach is unverified: in print mode it wrote to sibling folders with and without it. See [the vendor table](../workspace.md#per-vendor).

OpenCode has no flag. ynh instead adds an `external_directory` allow rule for each granted folder to the agents in the run directory, and prints the same `workspace: granted` line.

## Switch it off for one run

`--no-workspace` grants none of the requested folders for that run, and says so:

*This launches:* `claude --plugin-dir ~/.ynh/run/.snapshots/local--seat/.../.claude --allowedTools=Read(/~/.ynh/run/.snapshots/local--seat/.../**) -p hello`

```bash
cd /tmp/ynh-tutorial/ws/project
YNH_WORKSPACE_ROOTS=/tmp/ynh-tutorial/ws ynh run local/seat --no-workspace -- "hello"
```

Expected on stderr: `workspace: --no-workspace, 2 requested folder(s) not granted`.

## Clean up

```bash
ynh uninstall local/seat
rm -rf /tmp/ynh-tutorial
```

## Next

[Developer Tools](developer-tools.md): scaffold, lint, validate, format, compress, inspect with ynd.
