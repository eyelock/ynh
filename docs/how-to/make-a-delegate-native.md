# Make a delegate native instead

You want a delegate to run as a vendor subagent inside the parent's session, not as its own ynh session.
After these steps `ynh run` assembles it into the parent's run dir.

> **Watch out.** A native delegate carries its instructions on every vendor, its MCP servers only on Claude
> Code and its hooks and workspace folders nowhere
> ([known limit: delegates carry only part of a harness](../vendor-parity.md#l12-delegates-carry-only-part-of-a-harness)),
> and it ends with the parent's session. The comparison is the table under
> [Transport](../delegates-reference.md#transport).

## Steps

1. In the parent's `.agents/harness/plugin.json`, set `transport` on the one entry, or at the top of the
   manifest for all of them. An entry's value wins:

   ```json
   {
     "delegates_to": [
       {"git": "github.com/eyelock/team-dev-harness", "transport": "native"}
     ]
   }
   ```

   or `"transport": "native"` at the top level for the harness default.

2. Check the manifest. `ynd validate` checks the harness in the current folder, or the one at `[path]`:

   ```
   ynd validate
   ```

   A `native` delegate on a vendor with no native subagents is refused here, by `ynh install` and by
   `ynh run`. Today only Cursor declares none; there is no fallback to `a2a`. The fix is the message's:
   `transport: a2a` on that delegate, or another vendor.

3. Read and accept the parent again. Trust is ynh's record that you read what a harness will run
   ([what that covers](../sensors.md#what-a-command-sensor-can-reach)). The parent's digest covers each entry's settled transport, so flipping it reads
   `changed`, and the delegate's MCP servers and focus prompt are now in the parent's digest:

   ```
   ynh trust show <parent>
   ynh trust accept <parent>
   ```

4. Confirm: `ynh info <parent's installed id>` prints `transport=native (entry)` or `(harness)`.

A native delegate needs no installed harness and no `ynh trust accept` of its own. `ynd export` exports it;
an `a2a` delegate is left out with one warning.

## Checked against

[Transport](../delegates-reference.md#transport), [harnesses.md Transport](../harnesses.md#transport) and
[reference.md](../reference.md), v0.18.0 (develop `4c05c4b5`), 2026-11-15, and `ynd validate --help`. Not run on any vendor.
