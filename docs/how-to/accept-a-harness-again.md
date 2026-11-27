<!-- How-to. Checked against a build of ynh v0.18.0 (develop) in a scratch YNH_HOME, 2026-11-27, with Git harnesses served over file://, and an acceptance made with ynh v0.17.2. -->

# Accept a Harness Again After It Changes

A harness you accepted now reads `changed`, and `ynh run`, `ynh agent run` and `ynh check` refuse it:

```
Error: refusing to run <harness>: its commands differ from what you accepted on <date>.
It executes:
  ...
Review them: ynh trust show <harness>
Then accept:  ynh trust accept <harness>
```

## When ynh asks again

A harness reads `changed` when:

- you edit a hook, script, sensor or MCP server in a folder you installed
- `ynh update` brings in new commands, or moves a Git harness to a new commit
- you add, change or remove an include and that moves what the harness runs
- you upgrade ynh and it now covers more of what the harness runs

> After an upgrade, nothing in the harness need have moved: the refusal says your acceptance predates
> the newer check, for example:
>
> ```
> Your acceptance predates the digest covering the contents of the scripts that
> hooks and sensors run (it covered only the command text), so this
> harness reads as changed even if nothing in it has. Review the scripts above,
> then accept again.
> ```
>
> Review, then accept again. `ynh trust` lists every harness; each one to do reads `changed`.

The steps below get it running again, whatever changed it; the sections after them say what to expect
after an update, an include change and an edit to a folder you installed.

## Steps

1. See what it runs now, and what you accepted before:

   ```bash
   ynh trust show <harness>
   ```

   The top of the page has both fingerprints, and the bottom says it has changed:

   ```
     state:    changed
     digest:   sha256:8d9672703ea2376d
     accepted: sha256:c96fc50490fa7639 on 2026-11-27T16:49:32Z
     via:      accept
   ...
   These commands have changed since you accepted them.
   'ynh trust accept <harness>' records the new set.
   ```

2. Read every hook, MCP server and sensor it lists, and each script marked `covered`. If you installed
   the harness from Git, its files are in the folder `ynh installed <harness>` names, under
   `$YNH_HOME/harnesses/`.

3. Accept the new set:

   ```bash
   ynh trust accept <harness>
   ```

   ```
   Accepted <harness> (accept): 2 command(s), sha256:8d9672703ea2376d
   ```

4. Run it, or check it without starting a vendor if it has sensors:

   ```bash
   ynh check <harness>
   ```

If you would rather not run it at all, leave it unaccepted, or remove it with `ynh uninstall <harness>`.

## After `ynh update`

`ynh update` says so straight away when the update changed what the harness runs:

```
local/notes is no longer trusted: what it executes changed since you accepted it on 2026-11-27T16:49:32Z, so ynh run, ynh agent run and ynh check refuse it.
  Review it: ynh trust show local/notes
  Then accept: ynh trust accept local/notes
```

An update that moves a Git harness to a new commit asks again even when only its instructions, skills
or rules changed: your acceptance was of the commit you reviewed. The refusal names both commits:

```
Error: refusing to run local/notes: your acceptance on 2026-11-27T17:03:36Z does not cover it: it was accepted at commit cc07139c481f2c421480a4afbaa9d9abc82896a3, and is now at 568ec6fb633ef3ad549e3d82ee613ba5d5a4b9ba.
```

Follow the steps above in either case.

## After adding, changing or removing an include

`ynh include add`, `ynh include update` and `ynh include remove` print the same `is no longer trusted`
message when the change moves what the harness runs. Follow the steps above.

For a harness you installed from a folder, you can accept in the same step as `include add` or
`include update` with `--trust`, when the include is pinned to a full 40-character commit SHA:

```bash
ynh include add local/<name> https://github.com/<org>/<repo> --ref <40-character-sha> --trust
```

```
Added include "https://github.com/<org>/<repo>"
Accepted local/<name> (include --trust): 3 command(s), sha256:d15c5ab982ac39ca
It executes:
  ...
```

## After editing a folder you installed

`ynh install ./<folder>` does not copy the folder: the installed harness `local/<name>` *is* that folder.
Editing a hook, a script, a sensor or an MCP server in it makes `local/<name>` read `changed` at once.
Follow the steps above after each edit you want to run.

## When the refusal is not "changed"

- **`cannot be reviewed yet: what it executes could not be read in full`**: an include is missing from
  ynh's cache. Run `ynh update <harness>`, which fetches it, then run again.
- **`a script its commands run cannot be read in full`**: a script is a link that leads outside the
  harness, or is missing. Accepting does not help. Make it a regular file inside the harness, as the
  `Fix:` line says, then run `ynh trust show <harness>`.
- **A delegate's refusal** names the delegate, not the parent: accept the delegate. See
  [Trust a delegate](trust-a-delegate.md).

Every event and its message are in [What invalidates trust](../what-invalidates-trust.md).
