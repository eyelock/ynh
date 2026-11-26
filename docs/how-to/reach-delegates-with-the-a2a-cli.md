# Reach delegates with the a2a CLI

Your parent would rather use a shell command than MCP tools. After these steps it starts, follows and
answers a delegate with the `a2a` CLI.

The CLI route and the MCP route reach the same endpoints. Why there are two is in
[Delegate sessions](../delegate-sessions.md#the-a2a-cli-route); the facts are in the
[reference](../delegates-reference.md#the-a2a-cli-route).

## Before you start

- The parent has an installed, trusted `a2a` delegate ([Add a delegate](add-a-delegate.md)).
- A run from a baked image has no `ynh-delegates` MCP server, so the CLI is its route.

## Steps

1. Install the `a2a` binary, version 0.4.0 or newer. ynh does not install it:

   ```
   curl -fsSL https://raw.githubusercontent.com/a2aproject/a2a-cli/main/install.sh | A2A_VERSION=0.4.0 sh
   ```

   or `brew tap a2aproject/a2a-cli https://github.com/a2aproject/a2a-cli && brew install a2a`.

2. Allow the shell command for your vendor, as in
   [Allow a headless parent](allow-a-headless-parent.md#steps) step 3.

   > **Watch out.** On Codex the default sandbox cannot reach `127.0.0.1`: the command fails with `connect:
   > operation not permitted`. Turning `network_access` on fixes it, and means the shell is then no longer
   > sandboxed from localhost generally.

3. Check:

   ```
   ynh doctor
   ```

   It warns when `a2a` is missing or older than 0.4.0, when the vendored skill is missing from the run dir,
   and when an allow rule is missing.

4. Run the parent: `ynh run <parent>`. It writes two skills into the parent's vendor skill folder,
   `a2a-cli` and `ynh-a2a-delegates`; the second lists each delegate's card address.

5. The parent's commands, which it takes from those skills:

   ```
   a2a send -a <card address> --async "<task>"
   a2a task get -a <address> <id>
   a2a send --task-id <id> "<answer>"
   a2a task cancel <id>
   ```

   `task get` repeats until the state is `completed`, or `input-required`, which `a2a send --task-id` answers.
   The token comes from the credential helper ynh sets in `A2ACLI_CREDENTIAL_HELPER`, so a command carries
   no variable.

   > **Known issue.** Claude Code refuses a command that carries a variable under the rule `Bash(a2a:*)`
   > ([ynh #849](https://github.com/eyelock/ynh/issues/849)), which is why the skill writes the address into
   > the command and the token comes from the helper. A run from a baked image has its skill written before
   > any endpoint exists, so that skill teaches the variable form and hits this. Use the address from a live
   > `ynh run` until it is fixed.

## Checked against

[The a2a CLI route](../delegates-reference.md#the-a2a-cli-route), v0.18.0 (develop `4c05c4b5`), 2026-11-15.
Not run on any vendor; Cursor's shell route has not been run.
