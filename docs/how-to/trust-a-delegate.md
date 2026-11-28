<!-- How-to. Checked against a build of ynh v0.18.0 (develop) in a scratch YNH_HOME, 2026-11-27, with the delegate served over file:// and a stand-in vendor CLI on PATH. -->

# Trust a Delegate

Your parent harness hands work to a delegate, and `ynh run` of the parent stops with:

```
Error: delegate <source> of <parent> is installed as <id> but not trusted
  Review with 'ynh trust show <id>', then run: ynh trust accept <id>
```

These steps let the parent start with its delegate. How to add a delegate in the first place is in
[Add a delegate](add-a-delegate.md).

## Which harness to accept

It depends on the delegate's transport, which `ynh info <parent>` prints beside each delegate:

- **`a2a`** (the default): the delegate runs as its own ynh session, so it has its own trust record.
  Accepting the **parent** accepts it, and every a2a delegate below it, each with its own record: one
  command, `ynh trust accept <parent>`.
- **`native`**: the delegate runs inside the parent's session, as the vendor's subagent. Its MCP
  servers are part of the parent's review: accept the **parent**, as
  [Accept a harness again after it changes](accept-a-harness-again.md) says.

## Trust the parent and its `a2a` delegates

1. Read what the parent and the whole tree of its delegates run:

   ```bash
   ynh trust show <parent>
   ```

   After the parent's own commands it lists every a2a delegate below it, nested ones included, each
   with its own trust state, the level it opens at when run unattended, and its hooks, MCP servers and
   sensors, and those its includes bring. A delegate that is not installed yet is listed too, read from
   where it points.

2. Accept them together:

   ```bash
   ynh trust accept <parent>
   ```

   ```
   A2A delegate <source> of lead, opens at all when run unattended (default):
   It executes:
     MCP server search: npx
   Accepted local/reviewer (accept, with local/lead): 2 command(s), sha256:b6a68a3387879e68, opens at all when run unattended
   Accepted local/lead (accept): 0 command(s), sha256:..., opens at all when run unattended
   ```

   What each delegate runs is shown before anything is recorded. A delegate that is not installed is
   installed, and must match what was shown. It is all or nothing: if any delegate cannot be accepted
   (it cannot be read in full, or fails to install), nothing is recorded, the parent included, and the
   error names the delegate and why. `ynh trust ls` shows a delegate accepted this way as
   `with <parent>`.

3. Run the parent. A refusal about a delegate names the parent and this command.

4. Run the parent again. ynh opens the delegate's endpoint and starts the vendor:

   ```
   Opened 1 A2A delegate endpoint(s): reviewer
   Launching claude...
   ```

To install and accept a parent and its delegates in one step, as on CI, pin the parent and every a2a
delegate entry to a full commit SHA and use `--trust`:

```bash
ynh install <source> --ref <40-character-sha> --trust
```

## When the delegate changes

When the delegate's commands change after you accepted it, the parent's run is refused, naming the
delegate:

```
Error: delegate <source> of lead is installed as local/reviewer but its commands changed since you accepted them
  Review with 'ynh trust show local/reviewer', then run: ynh trust accept local/reviewer
```

Review and accept again with `ynh trust show <parent>` and `ynh trust accept <parent>`. Only the
delegate that changed is re-gated: each keeps its own record, and the others stay accepted. (Accepting
the delegate on its own, `ynh trust accept <id>`, works too.)

## Choosing the delegate's model and effort

Trust covers what a delegate runs, not which model runs it. From v0.18.1 (v0.18.0 has no model or effort settings), a parent picks the model and effort per call, per
entry (`model` and `effort` on the `delegates_to` entry) or leaves them to the delegate's own `default_model`
and `default_effort`; changing any of them moves no digest and needs no `ynh trust accept`. `ynh info <parent>`
shows what each delegate opens with and where it came from. See
[Model and effort](../delegates-reference.md#model-and-effort).

## Trust a `native` delegate

A parent whose `native` delegate brings an MCP server is refused until you accept the parent, and the
list names the delegate:

```
Error: refusing to run local/lead3: you have not accepted what it executes.
It executes:
  MCP server search: npx of delegate searcher
Review them: ynh trust show local/lead3
Then accept:  ynh trust accept local/lead3
```

```bash
ynh trust show <parent>
ynh trust accept <parent>
```

A native delegate needs no install and no record of its own. If its entry in the parent's manifest has
no full 40-character commit SHA ref, `ynh trust accept <parent>` still accepts it, at the commit it is at
now, and prints and records that commit (`native delegate ...: no ref, so it floats with the default
branch; accepted at commit <sha>`). `--trust` is the route that needs the pin; see
[Make a delegate native instead](make-a-delegate-native.md).

Every refusal a delegate can cause is in
[Delegates reference: refusals](../delegates-reference.md#refusals-when-a-delegate-cannot-be-matched).
