# Delegate Pinning: ref vs sha

This note is for tools that build on top of ynh — delegate-management UIs, dashboards, CI integrations — that need to decide what to pre-fill or pin when composing a harness reference.

## TL;DR

- **`ref` is the primary identifier.** Default to it. It's the user's stated tracking intent at install time.
- **`sha` is an optional integrity pin.** Offer it as an explicit, opt-in choice ("pin to exact commit"). Don't make it the default.
- **Don't use `marketplace.version`** for anything other than display. It's cosmetic; ynh's resolver does not consult it.

## The model

ynh inherits Claude Code's git-ref-based marketplace model. Identity is a git ref, optionally anchored to a commit SHA. There is no semver-style version resolver — "track version 1.0" is expressed as `"ref": "v1.0"`.

A harness's `installed.json` records, after install:

| Field | Meaning |
|---|---|
| `source` | Where the harness came from (git URL or local path) |
| `ref` | What the user asked to track: a tag (`"v1.0"`), a branch (`"main"`), or a SHA |
| `sha` | The commit that was actually fetched at install time |

`ref` and `sha` are independent. `ref` reflects *intent*; `sha` reflects *what we got*. They can disagree — installing `--ref main` today gives you `sha = abc123…`; tomorrow `main` will be a different SHA.

## What this means for your tool

When your tool offers a UI to compose a delegate, include, or harness reference, the choice of pre-filled value matters:

| Pre-filled value | Semantic | Drift behaviour |
|---|---|---|
| `installedFrom.ref` (e.g. `"v1.0"`) | "Track what `v1.0` means" | Floats forward when upstream moves the tag |
| `installedFrom.ref` (e.g. `"main"`) | "Track the branch" | Floats freely with every push |
| `installedFrom.sha` | "Exact bytes, never drift" | Frozen forever |

**Defaulting to `installedFrom.sha` is over-pinning by default.** It silently throws away the user's symbolic tracking intent. A user who installed with `--ref v1.0` expecting to follow the v1.0 tag should not, by default, be SHA-pinned in derived references.

### Recommended UX

1. Pre-fill the ref field from `installedFrom.ref`.
2. Show the resolved SHA as additional information (so the user knows what bytes that ref currently points at).
3. Offer a checkbox like **"Pin to exact commit (integrity check)"**. When ticked, pass both `--ref <original-ref>` and the SHA — or, if your YNH version's CLI doesn't accept both, substitute the SHA as the ref. Either form works because ynh's resolver supports SHA-as-ref.

### When `installedFrom.ref` is empty or a SHA

- **Empty**: the harness was installed without an explicit ref (tracking HEAD). Pre-fill empty; the delegate will track HEAD too. Surface this clearly so the user knows they're floating.
- **A SHA**: the user already chose immutable pinning. Pre-fill the SHA. No further pin opt-in needed.

## Why not just default to SHA for safety?

Because it lies about what the user wants. A user who installs `acme-tools --ref v1.0` is saying "give me 1.0, including future patch releases of 1.0." Auto-converting their delegate to `sha = abc123…` means they'll never receive those patches even though their original install would. The "safe" default ends up subtly wrong.

If your tool genuinely needs hermetic builds (CI pipelines, reproducible deployments), surface SHA pinning prominently and make it explicit — but as a deliberate choice, not a hidden default.

## Cross-references

- [`docs/marketplace.md` § Pinning: refs and SHAs](../marketplace.md#pinning-refs-and-shas) — user-facing docs
- [`.github/CONTRIBUTING.md` § Versioning & Identifiers](../../.github/CONTRIBUTING.md#versioning--identifiers) — contributor-facing rationale and rules
- [`docs/tutorial/07-registry-and-discovery.md` § T7.6b](../tutorial/07-registry-and-discovery.md) — worked example
