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
  Accept the **delegate**. The parent needs accepting only if it runs commands of its own.
- **`native`**: the delegate runs inside the parent's session, as the vendor's subagent. Its MCP
  servers are part of the parent's review: accept the **parent**, as
  [Accept a harness again after it changes](accept-a-harness-again.md) says.

## Trust an `a2a` delegate

1. Install the delegate, if the parent's refusal says it is not installed. The refusal gives the exact
   command:

   ```
   Error: delegate <source> of lead is not installed
     Run: ynh install <source> --ref 1c97ab94af922789d5cada7defd2f1bd2e1409de, then ynh trust accept <the installed id>
   ```

   ```bash
   ynh install <source> --ref <ref>
   ```

2. Read what it runs:

   ```bash
   ynh trust show <id>
   ```

   It lists the delegate's hooks, MCP servers and sensors, and those its includes bring.

3. Accept it:

   ```bash
   ynh trust accept <id>
   ```

   ```
   Accepted local/reviewer (accept): 2 command(s), sha256:b6a68a3387879e68
   ```

4. Run the parent again. ynh opens the delegate's endpoint and starts the vendor:

   ```
   Opened 1 A2A delegate endpoint(s): reviewer
   Launching claude...
   ```

To install and accept a delegate in one step, as on CI, pin it to a full commit SHA and use `--trust`:

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

Review and accept the delegate again, as in steps 2 and 3. The parent's own record is untouched.

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
