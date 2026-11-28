<!-- How-to. Rewritten 2026-11-28 for ynh #1058 (a parent's acceptance covers its a2a delegates), against a build of ynh develop at 960ecb6f (P0 to P4 of ynh #1058 merged) plus this pull request, in a scratch YNH_HOME with the parent and delegate served over file:// and a stand-in vendor CLI on PATH. Lines marked TODO(live) need a vendor session, which the stand-in cannot give; they are checked in the release's live check on all five vendors. -->

# Trust a Delegate

Your parent harness hands work to a delegate, and `ynh run` of the parent is refused because the
delegate is not installed or not accepted. These steps accept the parent and its delegates together,
so the parent starts. How to add a delegate in the first place is in
[Add a delegate](add-a-delegate.md).

## Accept the parent

Whatever the delegate's transport, accept the **parent**. Its acceptance covers its delegates:

- an **`a2a`** delegate (the default) runs as its own ynh session and keeps its own trust record.
  Accepting the parent installs the delegate if it is not installed, and accepts it under its own id
  at its pinned commit;
- a **`native`** delegate runs inside the parent's session; its MCP servers are part of the parent's
  own review.

`ynh info <parent>` prints each delegate's transport.

1. Read what the parent and its delegates run:

   ```bash
   ynh trust show <parent>
   ```

   After the parent's own hooks, MCP servers and sensors, it lists the level the parent opens at when
   run unattended, and every a2a delegate below it, nested ones included, with its own state, the
   level it opens at and what it runs:

   ```
   Permission level when run unattended (a delegate session, a one-shot 'ynh run' or 'ynh agent run'):

     opens at all when run unattended (not declared; the default for an accepted harness)

   A2A delegates, the whole tree, each its own harness with its own record. 'ynh trust accept local/team' accepts them all, or none:

     <source>: (not installed; accepting installs it) [not-installed], declared by team
       opens at all when run unattended (default)
       hook on_session_start: echo Reviewer session started.
   ```

2. Accept the parent:

   ```bash
   ynh trust accept <parent>
   ```

   ```
   A2A delegate <source> of team, opens at all when run unattended (default):
   It executes:
     hook on_session_start: echo Reviewer session started.
   Installed harness "reviewer"
     Location: <YNH_HOME>/harnesses/local--reviewer
     Launcher: <YNH_HOME>/bin/reviewer
     Vendor:   claude
   Accepted local/reviewer (accept, with local/team): 1 command(s), sha256:abab6f64cd7a3573, opens at all when run unattended
   Accepted local/team (accept): 0 command(s), sha256:9ec767637d0d49d6, opens at all when run unattended
     opens at all when run unattended (default)
   ```

   What each delegate runs is shown before anything is recorded. The `Installed harness` lines appear
   only for a delegate that was not installed. It is all or nothing: if any delegate cannot be accepted,
   nothing is recorded, the parent included, and the error names the delegate. `ynh trust` lists a
   delegate accepted this way as `with <parent>`.

3. Run the parent again. ynh opens the delegate's endpoint and starts the vendor:

   ```
   Opened 1 A2A delegate endpoint(s): reviewer
   Launching claude...
   ```

On CI, accept the parent and its delegates in one step with `--trust`, every `delegates_to` entry
pinned to a full commit SHA; see [Run a harness on CI](run-a-harness-on-ci.md).

## When a delegate changes

When an a2a delegate's commands change after you accepted it, only the delegate is re-gated; the
parent's record is untouched. The parent's run is refused, naming the delegate:

```
Error: delegate <source> of lead is installed as local/reviewer but its commands changed since you accepted them
  Review with 'ynh trust show local/reviewer', then run: ynh trust accept local/reviewer
```

Review and accept the delegate, as the refusal says, or accept the parent again, which accepts it too.

## Choosing the delegate's model and effort

Trust covers what a delegate runs, not which model runs it. From v0.18.1 (v0.18.0 has no model or effort settings), a parent picks the model and effort per call, per
entry (`model` and `effort` on the `delegates_to` entry) or leaves them to the delegate's own `default_model`
and `default_effort`; changing any of them moves no digest and needs no `ynh trust accept`. `ynh info <parent>`
shows what each delegate opens with and where it came from. See
[Model and effort](../delegates-reference.md#model-and-effort).

## When the parent changes a delegate's level

An `auto_approve` on a `delegates_to` entry is part of the parent's review. Adding or changing it
re-gates the parent, not the delegate: accept the parent again. `ynh trust show <parent>` then lists the
delegate as `opens at <level> when run unattended (entry)`.

## A `native` delegate that brings an MCP server

The parent is refused until you accept it, and the list names the delegate:

```
Error: refusing to run local/lead3: you have not accepted what it executes.
It executes:
  MCP server search: npx of delegate searcher
Review them: ynh trust show local/lead3
Then accept:  ynh trust accept local/lead3
```

A native delegate needs no install and no record of its own. If its entry in the parent's manifest has
no full 40-character commit SHA ref, `ynh trust accept <parent>` still accepts it, at the commit it is at
now, and prints and records that commit (`native delegate ...: no ref, so it floats with the default
branch; accepted at commit <sha>`). `--trust` is the route that needs the pin; see
[Make a delegate native instead](make-a-delegate-native.md).

Every refusal a delegate can cause is in
[Delegates reference: refusals](../delegates-reference.md#refusals-when-a-delegate-cannot-be-matched).
