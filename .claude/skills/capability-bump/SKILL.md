---
name: capability-bump
description: Change the JSON shape of a ynh command safely — decide whether it bumps CapabilitiesVersion, then update the schema, goldens and docs together. Use when adding, removing or changing any field in a --format json response.
---

# Change a structured-output shape

`config.CapabilitiesVersion` is the wire-protocol version every enveloped
response carries. Consumers — loop drivers, CI scripts, `ynd validate-output` —
read it to know what they are talking to.

`.claude/CLAUDE.md` states the rule. This skill is how you carry it out, because
the rule is a decision and the execution is four files that must move together.

## Step 1 — Decide: does this bump?

**Bumps** — a consumer must adapt:

- removing a field
- renaming a field
- changing a field's type
- narrowing a type (`string` → enum)
- tightening an enum (removing a member)
- making an optional field required
- changing the meaning of an existing value

**Does not bump** — a tolerant consumer is unaffected:

- adding an optional field
- adding a member to an open-set enum (consumers MUST tolerate unknowns, per
  `docs/cli-structured.md`)
- adding a whole new schema
- relaxing a constraint

**If unsure, treat it as a bump.** A needless bump costs a consumer one version
check. A missed one breaks them silently.

Note the asymmetry: *adding* an enum member does not bump, *removing* one does.
Open-set enums put the tolerance obligation on the consumer, which only works if
the set only ever grows.

## Step 2 — Know which responses are enveloped

**Only 9 of 21 golden responses carry `capabilities`.** Most commands return a
bare array or object with no envelope at all:

```console
$ ynh version --format json
{ "version": "...", "capabilities": "0.8.0" }

$ ynh vendors --format json
[ { "name": "claude", ... } ]        ← bare array, no envelope, no version

$ ynh paths --format json
{ "home": "...", "config": "..." }   ← bare object, no version
```

Enveloped (9): `agent-run`, `baseline`, `check`, `check-calibrate`, `fork`,
`info`, `installed`, `list`, `version`.

Not enveloped (12): `backend`, `error`, `migrate`, `paths`, `quarantine`,
`registry`, `search`, `sensors`, `sensors-show`, `sources`, `status`,
`vendors`.

Check rather than trust that list — it moves. #271 published five new schemas
and goldens, all of them bare, taking the split from 9-of-16 to 9-of-21:

```bash
for f in test/golden/*.json; do
  grep -q '"capabilities"' "$f" && echo "ENV  $f" || echo "bare $f"
done
```

Two consequences:

- **A bump touches every enveloped golden**, not just the command you changed —
  they all carry the version string.
- Changing the shape of a *non*-enveloped command still needs its schema and
  golden updated, but there is no version for a consumer to check. Worth raising
  rather than silently relying on.

## Step 3 — Move all four together

A change that lands in fewer than these is incomplete:

| What | Where |
|---|---|
| the code | `cmd/ynh/<command>.go` |
| the schema | `docs/schema/cli/<command>.schema.json` |
| the goldens | `test/golden/<command>.json` — **plus every enveloped golden if bumping** |
| the version | `internal/config/config.go` — `CapabilitiesVersion` |

Plus, when relevant: `docs/schema/plugin.schema.json` if a manifest field
changed, and `docs/schema-cli.md` if the contract narrative did.

Schemas are embedded from `docs/schema/` by `internal/clischema`, so the
published file and the one the binary validates against are the same tree. There
is no second copy to forget.

## The worked example — #254

The freshness gate is the cleanest recent bump to copy. It added `freshness` and
`freshness_basis` to check results, added `observes` to the sensor schema, and
took `CapabilitiesVersion` from `0.7.0` to `0.8.0`:

```
cmd/ynh/check.go
docs/schema/cli/check.schema.json
docs/schema/cli/check-calibrate.schema.json
docs/schema/plugin.schema.json
docs/schema-cli.md
internal/config/config.go
test/golden/{agent-run,baseline,check,check-calibrate,fork,info,installed,list,version}.json
```

Note the nine goldens. Only two are check-related; the other seven changed
solely because the version string in their envelope moved. **If your diff bumps
the version and touches one golden, you have missed eight.**

Worth asking whether it needed to bump at all: `freshness` was an added optional
field, which by the rule does not bump. It bumped because a `files` sensor's
*gating behaviour* changed — `absent` and `stale` began failing where they
previously reported. That is "changing the meaning of an existing field value",
and it is the subtle branch of the rule. **The shape is not the only thing a
consumer depends on.**

## Step 4 — Verify

```bash
make test FILE=./cmd/ynh          # golden comparisons live here
make test FILE=./internal/clischema
make check
```

Then check the contract by hand, the way a consumer would:

```bash
ynh <command> --format json | ynd validate-output --schema <command>
ynh schema <command>              # the embedded schema
ynh schema --all --format json    # every schema, as a manifest
```

The schema name is bare — `--schema version`, not `--schema cli/version`, even
though the file lives at `docs/schema/cli/version.schema.json`. The prefixed
form errors with `unknown schema`.

`ynd validate-output` exists so a harness author can assert against the contract
without writing a schema loader. If your change makes real output fail its own
published schema, that is the bug — not the test.

## Step 5 — Say so in the PR

State explicitly:

- whether it bumps, and **which rule clause** decided it
- the old and new version
- that goldens and schema moved with it

A reviewer cannot check "did this need a bump" without knowing which clause you
applied. `docs/cli-structured.md` is the contract consumers read; if the change
alters what it promises, it changes too.

## The failure this prevents

A consumer polls `ynh check --format json`, reads `capabilities`, and branches
on it. Change a field's meaning without bumping and that consumer keeps its old
branch, silently, against output that no longer means what it did.

Nothing fails. The gate goes on reporting, and the thing it reports is wrong —
which is the same class of failure `reference` fixtures exist to catch in
sensors, one layer up.
