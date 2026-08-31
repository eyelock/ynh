# Composition patterns

Worked shapes, the rules that decide what wins, and the traps. Everything here
was run against a real harness rather than read off a schema.

## Include: the manifest form

```json
{
  "includes": [
    {
      "git": "github.com/org/shared-skills",
      "ref": "v1.2.0",
      "path": "harnesses/team",
      "pick": ["skills/commit", "agents/reviewer"]
    },
    { "local": "vendor-shared" }
  ]
}
```

| Field | Meaning |
|---|---|
| `git` | shorthand (`github.com/user/repo`), SSH, or HTTPS |
| `ref` | tag, branch or commit — **pin third-party includes** |
| `path` | subdirectory, for a monorepo holding several harnesses |
| `pick` | take only these paths instead of everything |
| `local` | a directory in this repo — no CLI, hand-written |

`ynh include add` covers `git`, `--ref` and `--path`. For `pick` and `local`,
edit the manifest.

### The local-include boundary

A local include may not traverse above the harness directory, and `ynd validate`
enforces it:

```console
$ ynd validate app          # includes: [ { "local": "../shared" } ]
Error: validation failed
```

The reason is that a harness must be self-contained to be installable: an
include pointing at a sibling directory resolves on the author's machine and
nowhere else. Copy or symlink the shared content inside the harness rather than
pointing up.

### What an include brings

Files only: `skills/`, `agents/`, `rules/`, `commands/`. An included harness's
manifest is never opened, so its hooks, MCP servers, profiles, focuses and
sensors do **not** come with it.

That is deliberate for sensors: it keeps "what observes this repository" in one
committed file a reviewer can read, and stops a composed harness turning
included content into an execution surface the root author never declared.

## Pattern: the shared team baseline

Everyone includes the team harness; each person's own harness adds to it.

```json
{
  "name": "david",
  "includes": [ { "git": "github.com/acme/team-standards", "ref": "v3" } ],
  "default_vendor": "claude"
}
```

Team standards update by moving the tag. Nobody re-copies anything.

**Why `ref: "v3"` and not a branch:** an include is markdown that becomes
instructions inside an agent. Tracking `main` means whoever can push to that repo
can change how your assistant behaves, silently, on your next run. Pin it, and
move the pin deliberately.

## Pattern: cherry-picking from a large repo

```json
{
  "includes": [
    {
      "git": "github.com/awesome/skills",
      "ref": "v2.0.0",
      "pick": ["skills/code-review", "skills/commit"]
    }
  ]
}
```

Two skills from a repo of thirty. Everything not picked never reaches the
vendor, so it costs no context.

## Pattern: profile per situation

```json
{
  "profiles": {
    "ci": {
      "hooks": { "on_stop": [ { "command": "make check" } ] }
    },
    "review": {
      "includes": [ { "git": "github.com/acme/review-skills", "ref": "v1" } ]
    }
  }
}
```

```bash
ynh run <harness> --profile ci
ynd preview <harness> --profile ci      # see what it resolves to
```

A profile can add `includes`, `hooks`, `mcp_servers` and `env_passthrough`. It
**cannot** add or override sensors — out of scope in v1, so sensors stay
root-only.

## Pattern: focus as the front door

```json
{
  "focuses": {
    "ship":   { "prompt": "Walk me through shipping this change.", "profile": "ci" },
    "review": { "prompt": "Review the diff against main for correctness and security.", "profile": "review" },
    "learn":  { "prompt": "Introduce this codebase to me from the README." }
  }
}
```

A focus without a profile is fine — most are just a prompt.

```bash
ynh run <harness> --focus ship
ynh run <harness> --focus ship --interactive   # stay in the session afterwards
```

A focus implies non-interactive by default, which surprises people. `--interactive`
is the override.

## Includes vs delegates

The distinction people get wrong:

| | Include | Delegate |
|---|---|---|
| What happens | artifacts merge into this harness | the other harness is callable as a subagent |
| Context | shared — one session | separate — its own |
| Use when | you want their skills | you want their *specialist*, with its own setup |
| Manifest | `includes` | `delegates_to` |

Merging a large harness you only occasionally need costs context on every run.
That is the case for a delegate.

## Fork

```bash
ynh fork <name> --to ./my-version
```

Copies an installed harness into a directory you own, carrying an
`installed_from` provenance record so the origin is not lost. Use when you want
to diverge; use an include when you want to track.

## Resolution order

| Thing | Order |
|---|---|
| Vendor | `-v` flag > harness `default_vendor` > `~/.ynh/config.json` |
| Instructions | last source wins — the harness's own `AGENTS.md` beats an included one |
| Sensors | root harness only; includes contribute none |
| Profiles | overlay the base manifest; `--profile` selects |

## Reading the result

```bash
ynd compose <harness> --format text     # what resolved, and from where
ynd compose <harness> --format json     # same, machine-readable
ynd preview <harness> -v claude         # what the vendor actually receives
ynd diff <harness> claude cursor        # what differs between two vendors
```

`compose` answers "where did this come from". `preview` answers "what will the
vendor see". Reach for `compose` when an artifact appears you did not expect,
and `preview` when something you declared is missing.

Both kinds of include attribute, so an artifact you did not expect can be traced
to whichever include supplied it:

```console
$ ynd compose app --format text
Artifacts (1 total):
  TYPE   NAME    SOURCE
  skill  commit  vendor-shared
```
