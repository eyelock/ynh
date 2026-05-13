# Published JSON Schemas for ynh CLI Output

Every `ynh` command that supports `--format json` has a published JSON Schema. Consumers — TermQ, IDE integrations, scripts — can validate responses, codegen types, or branch on the schema at runtime.

This document is the contract: the schemas live in `docs/schema/cli/` and `docs/schema/shared/`, and a mirror under `internal/clischema/schema/` is embedded into the `ynh` and `ynd` binaries. A parity test fails CI if the two trees drift.

## Where to read schemas

Three equivalent forms:

| Form | Source |
|---|---|
| File on disk in the repo | `docs/schema/cli/<name>.schema.json` |
| Embedded in the binary | `ynh schema <name>` |
| One-shot manifest | `ynh schema --all --format json` |

`ynh schema --all --format json` returns `{capabilities, ynh_version, schemas: {"cli/version": {...}, "cli/list": {...}, ...}}` in a single invocation — meant for MCP servers and codegen tools that want every schema at startup without forking N subprocesses.

## Validation

```bash
ynh ls --format json | ynd validate-output --schema list
```

`ynd validate-output --schema <name>` reads JSON on stdin and exits non-zero on a validation failure. Useful for ad-hoc checks; downstream consumers normally pick their own validator at their own layer (see [Consumer boundary](#consumer-boundary)).

## Capability versioning

Every structured response carries a `capabilities` field — `config.CapabilitiesVersion`, currently `0.4.0`. Schemas carry the same value as an `x-capabilities` annotation.

**Capability bumps:**
- Removing a field; renaming a field; changing a field's type
- Narrowing a type (e.g. `string` → `enum`)
- Tightening an enum (removing a member)
- Making an optional field required
- Changing the meaning of an existing field value

**Do NOT bump:**
- Adding an optional field
- Adding a new enum member to an open-set enum (consumers MUST tolerate unknowns)
- Adding a new schema entirely
- Relaxing a constraint

The full rule is documented in `.claude/CLAUDE.md` and enforced via `.claude/rules/pre-remote.md` Gate 4.

## Envelope shape

Every command's schema is a top-level `oneOf: [<success envelope>, <error envelope>]`. A validator sees one or the other.

**Success envelopes** (where present) carry:
- `capabilities` (string) — wire-protocol version, required
- `ynh_version` (string) — binary release version, required
- `schema_version` (integer) — on-disk format version of `~/.ynh`, where relevant

Envelopes use `additionalProperties: true` at the top level — adding a new envelope field is not a capabilities bump. Nested payload `$def`s use `additionalProperties: false`.

**Note:** `ynh version`, `ynh search`, `ynh sources list`, `ynh registry list`, `ynh paths`, `ynh status`, and `ynh vendors` currently emit shapes without the full envelope (a bare object or array). Envelope retrofit is a separate, capabilities-bumping change tracked outside this initial schema publication.

## Error envelope

When a structured command fails, stderr carries one JSON object:

```json
{
  "error": {
    "code": "not_found",
    "message": "harness \"missing\" is not installed"
  }
}
```

Fields:
- `code` (string, required) — stable identifier. Closed enum in `shared/enums.schema.json#/$defs/ErrorCode`: `invalid_input`, `not_found`, `config_error`, `io_error`. New codes are a schema PR.
- `message` (string, required) — human-readable. Do not parse.
- `category`, `retryable`, `hint` — additive fields planned for a future capabilities bump. Consumers MUST tolerate either shape today.

## Shared schemas

Reusable shapes live in `docs/schema/shared/`:

| Schema | Purpose |
|---|---|
| `envelope.schema.json` | Base envelope ($defs/Envelope) for list/info-style responses |
| `enums.schema.json` | Closed enums: HarnessSource, UpdateState, ErrorCategory, ErrorCode |
| `harness.schema.json` | Harness, HarnessWithManifest, InstalledFrom, ForkedFrom, Artifacts, Include, Delegate |

Every per-command schema `$ref`s into these rather than copy-pasting. A consumer wrapping ynh responses in their own envelope can `allOf` against the shared types cleanly.

## Schema URLs

```
https://eyelock.github.io/ynh/schema/cli/<name>.schema.json
https://eyelock.github.io/ynh/schema/shared/<name>.schema.json
```

URLs are identifiers, not currently served endpoints. They match the existing `plugin`/`marketplace`/`harness` schema URL pattern. No version segment — wire-protocol versioning rides on the `capabilities` field, not on the URL.

## Consumer boundary

Schemas are a contract checked at YNH CI via golden round-trip:

```
test/golden/<command>.json  →  internal/jsonschema validator  →  PASS
live ynh <command> --format json  →  internal/jsonschema validator  →  PASS
```

Any drift between Go emission and the schema fails CI. Schemas are the published contract.

**Runtime validation in consumers is the consumer's choice and the consumer's dependency.** YNH does not ship a runtime validator library; the in-tree `internal/jsonschema` is consumer-internal. Consumers (TermQ MCP server, IDE plugins, codegen tools) pick their own validator at their own layer.

## Validator subset

The in-tree `internal/jsonschema` validator covers a deliberate JSON Schema draft 2020-12 subset:

**Supported:** `$id`, `$ref`, `$defs`, `$schema`, `$comment`, `description`, `title`, `x-*` (annotation), `type`, `enum`, `const`, `properties`, `required`, `additionalProperties` (bool or schema), `items`, `pattern`, `minLength`/`maxLength`, `minimum`/`maximum`, `oneOf`/`allOf`/`anyOf`.

**Rejected at load time** (not silently passed): `if`/`then`/`else`, `dependentSchemas`, `unevaluatedProperties`, `$dynamicRef`, format assertions. Schema authors cannot accidentally write a contract the validator does not check.

## Adding a new schema

1. Author the schema under `internal/clischema/schema/cli/<name>.schema.json`.
2. Mirror it to `docs/schema/cli/<name>.schema.json` (the parity test enforces byte-identity).
3. Add a representative `test/golden/<name>.json` fixture.
4. Add a `TestCmdXxx_JSONSchemaRoundTrip` in the command's `_test.go` that validates live emission against the schema.
5. Add a golden validation in `internal/clischema/embed_test.go`.

## Adding a field to an existing schema

If additive (new optional field, new enum member to an open-set enum, relaxed constraint): update the schema, regenerate the golden, no `CapabilitiesVersion` bump.

If breaking (rename, type change, required-flip, enum-tightening): bump `config.CapabilitiesVersion`, update goldens, update `docs/cli-structured.md` if the contract surface changes, and coordinate with downstream consumers.
