# Workspace Folders

A harness can ask for extra folders to be reachable by the vendor CLI, such as the sibling repositories its work happens in. The harness requests; you grant. In this tutorial a harness asks for two folders, and you see it refused until you trust it or set an allow-list, granted, rejected for a path that is never allowed, and switched off for one run.

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

## A request grants nothing

Run it from the `project` folder. No allow-list is set and the harness has not been accepted, so both folders are skipped, with the reason on stderr:

*This launches:* `claude --plugin-dir ~/.ynh/run/local--seat/.claude --add-dir ~/.ynh/run/local--seat -p hello`

```bash
cd /tmp/ynh-tutorial/ws/project
ynh run local/seat -- "hello"
```

Expected on stderr:

```
  warning: workspace: "../ynh" not granted: no workspace allow-list is set and the harness is not trusted: run 'ynh trust accept local/seat', or set YNH_WORKSPACE_ROOTS
  warning: workspace: "../../elsewhere" not granted: no workspace allow-list is set and the harness is not trusted: run 'ynh trust accept local/seat', or set YNH_WORKSPACE_ROOTS
Launching claude...
```

The only `--add-dir` is the one ynh always passes for its own assembled directory.

## Grant with an allow-list

`YNH_WORKSPACE_ROOTS` (or `workspace_roots` in `~/.ynh/config.json`) is a boundary the operator draws. A requested folder that resolves inside one of its roots is granted without any trust step. One outside it is refused:

*This launches:* `claude --plugin-dir ~/.ynh/run/local--seat/.claude --add-dir ~/.ynh/run/local--seat -p hello --add-dir .../ynh-tutorial/ws/ynh`

```bash
cd /tmp/ynh-tutorial/ws/project
YNH_WORKSPACE_ROOTS=/tmp/ynh-tutorial/ws ynh run local/seat -- "hello"
```

Expected on stderr (paths shown are where they resolve; on macOS `/tmp` is `/private/tmp`):

```
  warning: workspace: "../../elsewhere" not granted: /tmp/ynh-tutorial/elsewhere is outside the YNH_WORKSPACE_ROOTS allow-list (/tmp/ynh-tutorial/ws)
  workspace: granted /tmp/ynh-tutorial/ws/ynh (inside the YNH_WORKSPACE_ROOTS allow-list)
Launching claude...
```

The granted folder became Claude Code's own `--add-dir` flag. The refused one did not. With an allow-list set, trust cannot grant what the list does not cover.

## Grant with trust

With no allow-list, `ynh trust show` lists the folders as part of what you are asked to accept:

```bash
ynh trust show local/seat | sed -n '1,/^No allow-list/p'
```

Expected output includes:

```
Workspace folders, requested for the vendor CLI to reach (a request, not a grant):

  ../ynh
    resolves to: /tmp/ynh-tutorial/ws/ynh
  ../../elsewhere
    resolves to: /tmp/ynh-tutorial/elsewhere

No allow-list is set, so these are granted only while this harness is accepted.
```

Accept it, and the folders are granted. The digest covers the requested entries, so changing one in the manifest later reports `changed` and the grant stops until you accept again:

```bash
ynh trust accept local/seat
```

Expected output: `Recorded local/seat: 0 command(s), 2 workspace folder(s), sha256:...`

*This launches:* `claude --plugin-dir ~/.ynh/run/local--seat/.claude --add-dir ~/.ynh/run/local--seat -p hello --add-dir .../ynh-tutorial/ws/ynh --add-dir .../ynh-tutorial/elsewhere`

```bash
cd /tmp/ynh-tutorial/ws/project
ynh run local/seat -- "hello"
```

Expected on stderr: a `workspace: granted ... (harness trust-accepted)` line for each of the two folders.

## Paths that are never granted

Some paths are refused whatever the allow-list or trust say: the filesystem root, your home directory, a folder that contains the harness or the project, and one that does not exist. A harness that asks for your home directory gets nothing, even when the allow-list covers it:

*This launches:* `claude --plugin-dir ~/.ynh/run/_inline-.../.claude --add-dir ~/.ynh/run/_inline-... -p hello`

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

*This launches:* `codex exec --add-dir .../ynh-tutorial/ws/ynh hello`

```bash
cd /tmp/ynh-tutorial/ws/project
YNH_WORKSPACE_ROOTS=/tmp/ynh-tutorial/ws ynh run local/seat -v codex -- "hello"
```

The Cursor CLI has no flag that adds a directory, so ynh says so and grants nothing, rather than dropping the folder silently:

*This launches:* `agent -p hello`

```bash
cd /tmp/ynh-tutorial/ws/project
YNH_WORKSPACE_ROOTS=/tmp/ynh-tutorial/ws ynh run local/seat -v cursor -- "hello"
```

Expected on stderr includes `warning: workspace: /tmp/ynh-tutorial/ws/ynh not granted: cursor: the Cursor CLI has no flag that adds a directory to a session`.

## Switch it off for one run

`--no-workspace` grants none of the requested folders for that run, and says so:

*This launches:* `claude --plugin-dir ~/.ynh/run/local--seat/.claude --add-dir ~/.ynh/run/local--seat -p hello`

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
