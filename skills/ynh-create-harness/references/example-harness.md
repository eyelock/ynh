# A worked harness, end to end

Everything below ships with this skill, so you can read it wherever the harness
is installed. Use it as the shape to copy — then replace the content with
something specific to the user's actual work.

The artifacts here follow the rules in `rules/artifact-authoring.md`: specific to
a real project, actionable steps, no generic filler.

## Layout

```
acme-api/
├── .ynh-plugin/
│   └── plugin.json
├── AGENTS.md                    # optional project instructions
├── skills/
│   └── db-migration/SKILL.md
├── agents/
│   └── schema-reviewer.md
├── rules/
│   └── error-handling.md
└── commands/
    └── check.md
```

## `.ynh-plugin/plugin.json`

```json
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "acme-api",
  "version": "0.1.0",
  "description": "Harness for the Acme billing API — Go, Postgres, sqlc",
  "author": { "name": "Acme Platform Team" },
  "keywords": ["go", "postgres", "billing"],
  "default_vendor": "claude"
}
```

`name` becomes the launcher command on PATH, so keep it short and lowercase.

## A skill — `skills/db-migration/SKILL.md`

```markdown
---
name: db-migration
description: Write and apply a Postgres migration for the billing API. Use when adding or altering a table, column, or index.
---

# Add a database migration

## Steps

1. Create the pair — `migrations/` uses timestamped up/down files:

   ```bash
   make migrate-new NAME=add_invoice_status
   ```

2. Write the `up` migration. Every column added to an existing table must be
   nullable or carry a default — the deploy runs migrations before the new
   binary, so the old binary must still be able to INSERT.

3. Write the `down` migration so it actually reverses the change. A `down` that
   drops the whole table is not a reversal.

4. Regenerate the query layer — the repo uses sqlc, not an ORM:

   ```bash
   make sqlc
   ```

5. Apply against the local database and confirm both directions:

   ```bash
   make migrate-up && make migrate-down && make migrate-up
   ```

## Watch for

- A migration that locks a large table. `ALTER TABLE ... ADD COLUMN` with a
  non-null default rewrites the table; add the column nullable, backfill in
  batches, then set the constraint.
- Index creation without `CONCURRENTLY` blocks writes for the duration.
```

Note what makes this a skill rather than a note: every step is a command the
reader can run, and the "watch for" section encodes a project-specific hazard
that a general model would not know.

## An agent — `agents/schema-reviewer.md`

```markdown
---
name: schema-reviewer
description: Reviews Postgres migrations for lock risk, reversibility, and deploy ordering. Delegate to before any migration is merged.
tools: Read, Grep, Glob
---

You review database migrations for the Acme billing API.

## Check each migration for

1. **Deploy ordering** — migrations run before the new binary. Any column the
   old binary must still INSERT into has to be nullable or defaulted.
2. **Lock risk** — flag `ADD COLUMN` with a non-null default on any table over
   ~1M rows, and any `CREATE INDEX` missing `CONCURRENTLY`.
3. **Reversibility** — the `down` file must reverse the `up`, not drop the
   table.
4. **Query layer** — if the schema changed, `make sqlc` must have been re-run;
   check that generated files in `internal/db/` are in the same commit.

## Report

For each finding: the file and line, what breaks, and the concrete fix. If a
migration is safe, say so in one line rather than padding the review.
```

`tools` is required on an agent. Give it `Bash` only if it genuinely needs to run
commands — this one reads, so it does not.

## A rule — `rules/error-handling.md`

Rules are plain markdown, loaded as context every session. No frontmatter.

```markdown
Return errors, never panic in request paths. Wrap with context at each boundary:
`fmt.Errorf("charging invoice %s: %w", id, err)`.

Errors crossing the HTTP boundary map through `internal/httperr` — never write a
status code inline in a handler.

Handle an error once. Log it or return it, not both.
```

## A command — `commands/check.md`

```markdown
---
name: check
description: Run the full local pipeline before pushing
---

Run the checks in order and fix what fails before moving on:

```bash
make fmt && make lint && make test && make sqlc-diff
```

`sqlc-diff` fails when generated code is out of date with the schema — re-run
`make sqlc` and commit the result.
```

## Project instructions — `AGENTS.md`

Optional, at the harness root. What each vendor receives is in
`artifact-formats.md`; the short version is that ynh adapts it per vendor, so
write it once here.

```markdown
# Acme Billing API

Go 1.25, Postgres 16, sqlc for the query layer. No ORM.

- `cmd/api/` — HTTP entry point
- `internal/billing/` — domain logic, no framework imports
- `internal/db/` — sqlc-generated, never edited by hand
- `migrations/` — timestamped up/down pairs

Run `make check` before pushing. Migrations need review from the
`schema-reviewer` agent.
```

## Then

```bash
ynd validate ./acme-api          # structure and frontmatter
ynd lint ./acme-api              # formatting and shell syntax
ynd preview ./acme-api -v claude # exactly what the vendor receives
ynh install ./acme-api
acme-api                         # launch it
```
