<!-- Tutorial. Every command below was run, and its output copied, on a build of ynh v0.18.0 (develop) in a scratch YNH_HOME, 2026-11-27. -->

# Your First Trusted Harness

In this tutorial we make a small harness with a hook, watch ynh refuse to run it, read what it would run,
and accept it. Then we change the hook, see ynh notice and refuse again, and accept the new version.

By the end you will have used all three `ynh trust` commands and seen the trust gate refuse and allow a
harness. It takes about ten minutes and needs no model calls.

## Before you start

- ynh v0.18.0 or later on your `PATH` (`ynh version` prints the version).
- A terminal. Nothing here touches your own harnesses: we use a home of our own.

## Set up a scratch home

Make a folder for the tutorial and point ynh at a home inside it:

```bash
mkdir -p /tmp/ynh-trust && cd /tmp/ynh-trust
export YNH_HOME=/tmp/ynh-trust/home
```

`YNH_HOME` is where ynh keeps installed harnesses and its trust records. Because we set it, your usual
`~/.ynh` is left alone. Run every command below in this same terminal.

## Make a harness with a hook

Our harness, `greeter`, has one hook, a script that runs before every shell command the assistant
runs, and one sensor, a command `ynh check` runs. Hooks and sensors each have a tutorial of their own
later; here all that matters is that both run commands on your machine:

```bash
mkdir -p greeter/.agents/harness greeter/hooks
cat > greeter/.agents/harness/plugin.json <<'EOF'
{
  "name": "greeter",
  "version": "0.1.0",
  "default_vendor": "claude",
  "hooks": {
    "before_tool": [
      { "matcher": "Bash", "command": "./hooks/guard.sh" }
    ]
  },
  "sensors": {
    "hello": {
      "category": "maintainability",
      "source": { "command": "echo hello" },
      "output": { "format": "text" }
    }
  }
}
EOF
cat > greeter/hooks/guard.sh <<'EOF'
#!/bin/sh
# Runs before every Bash tool call. Allows everything.
exit 0
EOF
chmod +x greeter/hooks/guard.sh
echo "You greet people politely." > greeter/instructions.md
```

Install it:

```bash
ynh install ./greeter
```

```
Installed harness "greeter"
  Location: /tmp/ynh-trust/greeter
  Launcher: /tmp/ynh-trust/home/bin/greeter
  Vendor:   claude
```

On macOS the paths ynh prints begin `/private/tmp`, because `/tmp` is a link to it.

## See ynh refuse it

Try to start a session:

```bash
ynh run local/greeter
```

```
Error: refusing to run local/greeter: you have not accepted what it executes.
It executes:
  hook before_tool: ./hooks/guard.sh
  sensor hello (command): echo hello
Review them: ynh trust show local/greeter
Then accept:  ynh trust accept local/greeter
```

Nothing started: no assistant, no hook, no sensor. ynh lists the two things this harness would run on
our machine and names the two commands that come next.

Ask ynh for the state of every installed harness:

```bash
ynh trust
```

```
HARNESS        STATE       VIA  COMMANDS  SOURCE
local/greeter  unreviewed  -    2         /tmp/ynh-trust/greeter

Run 'ynh trust show <harness>' to see what a harness will execute.
```

`unreviewed` means we have never accepted it.

## Review it

```bash
ynh trust show local/greeter
```

```
local/greeter
  source:   /tmp/ynh-trust/greeter
  state:    unreviewed
  digest:   sha256:aff830900fd54227

Sensors, executed via /bin/sh -c in the tree being measured:

  hello (command)
    echo hello

Hooks, executed by the vendor CLI with your environment:

  before_tool [Bash]
    ./hooks/guard.sh
    script:  ./hooks/guard.sh (covered with its folder hooks: 1 file(s), sha256:2ba26b3474b456ed)

These commands inherit your full environment. 57 variable(s) reach them
that this harness never declared in env_passthrough:

  ...

Not yet reviewed. 'ynh trust accept local/greeter' records that you have read the above.
```

Your list of environment variables, and their count, depends on your shell; we have cut it to `...`.

Notice the `script:` line under the hook. ynh found the script `./hooks/guard.sh` and read the whole
`hooks` folder it lives in. The `digest` at the top is a fingerprint of everything on this page,
the script's contents included.

Open `greeter/hooks/guard.sh` and read it: it does nothing but `exit 0`. We are happy with that.

## Accept it

```bash
ynh trust accept local/greeter
```

```
Accepted local/greeter (accept): 2 command(s), sha256:aff830900fd54227
```

The digest it accepted is the one `ynh trust show` printed. Check the state again:

```bash
ynh trust
```

```
HARNESS        STATE     VIA     COMMANDS  SOURCE
local/greeter  accepted  accept  2         /tmp/ynh-trust/greeter
```

Now ynh lets the harness run. `ynh check` passes the same gate as `ynh run`, and runs only the sensor,
so we can see it work without starting an assistant:

```bash
ynh check local/greeter
```

```
  ✓  hello  pass  6ms

ok: 1 passed
```

## Change the hook

Now we change the script, but not the manifest. The hook's command is still `./hooks/guard.sh`; only
what the script does is different:

```bash
cat > greeter/hooks/guard.sh <<'EOF'
#!/bin/sh
# Runs before every Bash tool call. Logs each call, then allows it.
echo "bash tool called" >> "$HOME/greeter-hook.log"
exit 0
EOF
```

We installed `greeter` from its folder, so the installed harness is that folder: our edit is already
part of it.

```bash
ynh trust
```

```
HARNESS        STATE    VIA     COMMANDS  SOURCE
local/greeter  changed  accept  2         /tmp/ynh-trust/greeter

Run 'ynh trust show <harness>' to see what a harness will execute.
```

## See the refusal again

```bash
ynh run local/greeter
```

```
Error: refusing to run local/greeter: its commands differ from what you accepted on 2026-11-27T16:48:25Z.
It executes:
  hook before_tool: ./hooks/guard.sh
  sensor hello (command): echo hello
Review them: ynh trust show local/greeter
Then accept:  ynh trust accept local/greeter
```

Your date and time will be when you accepted it. ynh noticed a change we made inside a script, with the
command text untouched.

## Review and accept again

```bash
ynh trust show local/greeter
```

The page now begins with the old and the new fingerprints:

```
local/greeter
  source:   /tmp/ynh-trust/greeter
  state:    changed
  digest:   sha256:296fb74a19b43412
  accepted: sha256:aff830900fd54227 on 2026-11-27T16:48:25Z
  via:      accept
```

and ends:

```
These commands have changed since you accepted them.
'ynh trust accept local/greeter' records the new set.
```

Read `greeter/hooks/guard.sh` once more. It writes a line to a log file in your home folder, and we
are fine with that, so accept it:

```bash
ynh trust accept local/greeter
```

```
Accepted local/greeter (accept): 2 command(s), sha256:296fb74a19b43412
```

```bash
ynh check local/greeter
```

```
  ✓  hello  pass  7ms

ok: 1 passed
```

The harness runs again.

## What we did

We installed a harness that runs a hook and a sensor, and ynh refused it until we had read what it runs
(`ynh trust show`) and said so (`ynh trust accept`). When we changed the hook's script, ynh saw it,
refused again, and showed us the old and new fingerprints. One more review and acceptance and it ran.

To tidy up, remove the folder and leave the variable unset:

```bash
cd / && rm -rf /tmp/ynh-trust && unset YNH_HOME
```

## Next

[Hooks](hooks.md): declare vendor-agnostic lifecycle hooks.

From here on, whenever a tutorial installs a harness that runs commands, it has you review and accept it
the way we did here, before the first run.

More on trust:

- [Why ynh asks you to trust a harness](../why-trust.md): what the review protects, and what it leaves to you.
- [Accept a harness again after it changes](../how-to/accept-a-harness-again.md): updates, includes and upgrades.
- [What invalidates trust](../what-invalidates-trust.md): every event that makes ynh ask again.
