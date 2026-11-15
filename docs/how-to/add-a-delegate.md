# Add a delegate to a harness

You have a parent harness and want it to hand work to another harness. After these steps `ynh run` of the
parent starts with that harness as an `a2a` delegate (its own ynh session, the default in v0.18.0).

For why a delegate is its own session, see [Delegate sessions](../delegate-sessions.md). The fields are in
[delegates_to](../harnesses.md#delegates-to-optional).

## Steps

1. Add the entry to the parent. `<url>` must be a git URL; local paths are not supported:

   ```
   ynh delegate add <parent> <url>
   ```

   Optional flags: `--ref`, `--path`, `--profile` or `--focus` (not both). To check the file by hand, the
   entry is under `delegates_to` in `.agents/harness/plugin.json`.

2. Install the delegate itself. The parent matches the entry to an installed harness by source:

   ```
   ynh install <url>
   ```

3. Read what the delegate would run, then accept it:

   ```
   ynh trust show <id>
   ynh trust accept <id>
   ```

   `<id>` is the id `ynh install` printed. A delegate that executes nothing (no sensors, hooks or MCP
   servers) needs no `ynh trust accept` to be matched.

4. If the parent was accepted before the entry was added, review and accept the parent again. The parent's
   digest covers every `delegates_to` entry, so `ynh trust` shows it as `changed`:

   ```
   ynh trust show <parent>
   ynh trust accept <parent>
   ```

5. Check the result. `ynh info` prints each delegate's resolved transport (`transport=a2a (default)`), and
   `ynh doctor` reports a delegate that no longer resolves and the allow rules a headless parent needs:

   ```
   ynh info <parent>
   ynh doctor
   ```

## If `ynh run` refuses

A delegate that is not installed, not trusted, changed since it was trusted, or unreadable refuses the
parent's whole run before the vendor starts, and the message names the command that fixes it. The four
messages are listed under
[Refusals when a delegate cannot be matched](../delegates-reference.md#refusals-when-a-delegate-cannot-be-matched).

## Checked against

The commands and messages are taken from [reference.md](../reference.md) and
[delegates-reference.md](../delegates-reference.md) at v0.18.0 (develop `4c05c4b5`), 2026-11-15. A delegate
installed from a remote git URL or registry entry, then started by its parent, is the designed path and has
not been shown live
([L40](../vendor-parity.md#l40-a2a-delegates-cursor-opencode-workspace-folders-and-a-person-answering-not-shown-live)).
