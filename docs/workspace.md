# Workspace Folders

A harness often works somewhere other than where its session starts. A seat's session must start in its own repository, because its session-start hook runs from there, but its work is in sibling repositories. Without help that takes `--add-dir ../ynh` on every launch.

`workspace` lets the harness say which folders it needs. It does not let the harness take them.

## Request versus grant

**A harness requests; the operator grants.** The manifest names the folders:

```json
{
  "name": "seat",
  "version": "0.1.0",
  "workspace": ["../ynh", "../ynm", "~/code/shared"]
}
```

Relative entries resolve against the harness's own directory, like other relative paths in the manifest. Absolute paths and `~/` are allowed and expanded. A request on its own grants nothing.

A requested folder is granted in one of two ways, decided in this order:

1. **The operator allow-list.** If one is set, a folder that resolves inside one of its roots is granted, with no trust step. A folder outside the roots is refused with a warning that names the allow-list. **Trust cannot override the allow-list.**
2. **Trust.** With no allow-list, a folder is granted only while the harness is accepted with `ynh trust accept <harness>`, and the accepted digest covers its current `workspace` entries. Anything else is skipped with a warning that says to run `ynh trust accept`. A harness run from a path rather than installed has no trust record, so it needs an allow-list.

### The allow-list

Set `workspace_roots` in `~/.ynh/config.json`. `~/` expands:

```json
{
  "workspace_roots": ["~/code"]
}
```

`YNH_WORKSPACE_ROOTS` is the same list, colon-separated. When it is set and non-empty it **replaces** the configured list for that run, so a tool that launches ynh (a container runner, say) can set the boundary for one launch without editing the operator's config:

```bash
YNH_WORKSPACE_ROOTS=/work/repos ynh run local/seat
```

Roots must be absolute after `~/` expansion. A root that is relative or does not exist is ignored, and ynh says so.

**The trade-off.** With a root allowed, any locally installed harness can reach any folder under it. Choose roots as narrowly as the work allows: `~/code/team`, not `~/`.

## Never granted

These are refused whatever the allow-list and trust say. Paths are fully resolved first, symlinks included, so a symlink cannot smuggle one in:

- a path that does not exist or is not a directory;
- the filesystem root;
- your home directory itself;
- any directory that contains the harness directory or the project directory (an ancestor, or the directory itself);
- anything requested by a harness **installed from a remote source** (a git URL or a registry).

A remote harness is somebody else's text. It can still be used, but it cannot ask for your other repositories. A harness installed from a local path, a fork, or a configured source, and a harness run from a path, is yours, and may request. ynh decides this from the install record (`source_type` in `installed.json`): `git`, `registry`, and any value it does not recognise count as remote; `local` and `source` do not.

## What you see at launch

Every launch prints one line per folder on stderr, in the style of the hook warnings:

```
  workspace: granted /work/repos/ynh (inside the YNH_WORKSPACE_ROOTS allow-list)
  warning: workspace: "../../elsewhere" not granted: /elsewhere is outside the YNH_WORKSPACE_ROOTS allow-list (/work/repos)
```

`ynh trust show <harness>` lists the requested folders in their own section, with the strings as written and where they resolve to, and says whether the allow-list covers each one. It lists them even when the allow-list would grant them without trust. `--format json` carries the same in an optional `workspace` field.

## Per vendor

Each granted folder becomes the vendor's own flag. `ynh run` and the worker that `ynh agent run` starts both use it.

| Vendor | Flag | Notes |
|--------|------|-------|
| Claude Code | `--add-dir <dir>` | [CLI reference](https://code.claude.com/docs/en/cli-reference): "Add additional working directories for Claude to read and edit files." File access only: Claude Code does not discover most `.claude/` configuration from an added directory. |
| GitHub Copilot CLI | `--add-dir <dir>` | The [command reference](https://docs.github.com/en/copilot/reference/cli-command-reference) describes `/add-dir` as allowing file access to a directory and loading its `.github/skills` and `.github/agents` as trusted configurations. **A granted folder's agent configuration is loaded as trusted**, a wider grant than file access. Treat a Copilot workspace root accordingly. |
| OpenAI Codex | `--add-dir <dir>` | [CLI reference](https://developers.openai.com/codex/cli/reference): "Grant additional directories write access alongside the main workspace. Repeat for multiple paths." Listed as a global flag; whether `codex exec` and `codex exec resume` accept it is unverified. |
| Cursor | none | The [CLI parameters](https://cursor.com/docs/cli/reference/parameters) have no flag that adds a directory (`--workspace` names the single workspace). ynh warns, naming the vendor and the folder, and grants nothing. |
| OpenCode | none | `opencode --help` (2.2.3) lists no flag that adds a directory; its positional `<directory>` names the single project. ynh warns, naming the vendor and the folder, and grants nothing. |

A vendor with no equivalent is never silently dropped: `scripts/workspace-parity.sh` (run by `make check-vendor-parity`) requires every vendor adapter to declare its flag or the reason it has none.

ynh already passes `--add-dir` for its own assembled directory on Claude and Copilot. The granted folders are additional `--add-dir` flags beside it.

## Switching it off

`ynh run --no-workspace` grants none of the harness's requested folders for that run, and says so:

```
  workspace: --no-workspace, 2 requested folder(s) not granted
```

## Exports

`workspace` is not carried into exports. A plugin cannot grant its host a folder; only the operator running the harness can. `ynd export` warns when the harness has `workspace` entries that were dropped.

## Authoring checks

`ynd validate` checks the shape of `workspace` and reports as errors any entry that resolves to a never-granted path it can see: the filesystem root, your home directory, a directory containing the harness. A folder that does not exist on the machine validating is a note rather than an error, since sibling checkouts are often absent in CI; ynh still refuses it at launch.

`ynd validate` cannot tell where the harness will be installed from, so it cannot warn that a remote install will be refused. Publishing a harness for others to install from git: its `workspace` does nothing for them.

## Try it

The [Workspace Folders tutorial](tutorial/workspace.md) runs all of this offline.
