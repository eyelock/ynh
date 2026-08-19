---
name: add-vendor-adapter
description: Scaffold a brand-new ynh vendor adapter end-to-end (a new AI CLI harness like GitHub Copilot CLI, Gemini CLI, etc.). Use when onboarding a vendor that has no adapter yet — as opposed to vendor-adapters, which maintains adapters that already exist against evolving vendor specs.
---

# Add a New Vendor Adapter

Use this skill when ynh needs to support a **new** AI coding CLI that has no
`internal/vendor/<name>.go` yet — e.g. adding GitHub Copilot CLI, Gemini CLI,
or any future vendor. If the vendor already has an adapter and you're just
reacting to a spec change, use the `vendor-adapters` skill instead — it owns
the per-vendor documentation links and format-mapping tables that this skill
depends on.

## Before writing code: research

1. Research the vendor CLI's actual behavior — install method, config file
   layout (project-level and user-level), plugin/extension system (if any),
   skills/agents/rules/commands support, MCP support, hooks, instructions
   file, and CLI flags for interactive/non-interactive/initial-prompt launch.
   Cite official docs, not blog posts, wherever possible — vendor CLIs change
   fast and secondary sources go stale.
2. Add a new section to `.claude/skills/vendor-adapters/SKILL.md`'s
   documentation-links table and format-mapping tables for the new vendor.
   That skill is the single source of truth for vendor doc links — do not
   duplicate them here.
3. Decide the vendor's **launch strategy** up front — this drives most other
   decisions:
   - **Native plugin loading** (like Claude's `--plugin-dir`): `syscall.Exec`,
     no symlinks, `NeedsSymlinks() == false`.
   - **No native plugin loading** (like Codex, Cursor): symlink install into
     the project's vendor config dir + managed child process with signal
     forwarding, `NeedsSymlinks() == true`.

## Checklist: files to touch

Every one of these was touched when Cursor was added; treat it as the
canonical touch-list. Search the codebase for `cursor` (case-insensitive) at
any point to cross-check completeness — `grep -rli cursor --include="*.go" --include="*.md" .`

### Core adapter

- [ ] `internal/vendor/<name>.go` — new file implementing the full `Adapter`
  interface (`internal/vendor/adapter.go`). Self-register in `init()` via
  `Register(&<Name>{})`. No other wiring needed for the adapter itself to be
  reachable by name.
- [ ] `internal/vendor/<name>_test.go` — table-driven tests for every method,
  matching the depth of `internal/vendor/cursor_test.go`.
- [ ] If the vendor needs symlinks, reuse `installSymlinks`/`cleanSymlinks`
  from `internal/vendor/symlinks.go` — don't reimplement.
- [ ] If the vendor launches as a managed child process, reuse
  `runChildProcess` from `internal/vendor/process.go` for signal forwarding.

### Exporter (vendor-native plugin distribution)

- [ ] `internal/exporter/exporter.go` — add the vendor's export layout
  (skills/agents/rules/commands directory names may differ from runtime
  `ArtifactDirs()` — see Codex's `.agents/skills/` vs runtime `.codex/`).
- [ ] `internal/exporter/manifest.go` — vendor-native plugin manifest
  generation if the export format differs from `GeneratePluginManifest`.

### Marketplace

- [ ] `internal/marketplace/marketplace.go` — add the vendor to the default
  vendor list (search for `[]string{"claude", "cursor", "codex"}`) and add a
  vendor-specific marketplace index struct if its `marketplace.json` schema
  differs from the existing ones (see `codexMarketplaceJSON` for a
  vendor-specific example).

### `ynh agent run` backend (if the CLI supports scriptable turn-by-turn sessions)

- [ ] `internal/agent/<name>.go` — implement `WorkerBackend`
  (`internal/agent/worker.go`) if the vendor CLI can run non-interactively
  with session resume (see `internal/agent/cursor.go`, `codex.go`,
  `claude.go` for the three existing resume strategies — they differ
  significantly, don't assume one pattern fits).
- [ ] `internal/agent/loop.go` — register in `selectBackend`.
- [ ] This is a **separate surface from `vendor.Adapter`** — a vendor can have
  one without the other, but if the CLI supports non-interactive prompting at
  all, prefer adding both in the same PR since they share research.

### Docker image support

- [ ] `cmd/ynh/image.go` — add `COPY --link` line for the new vendor's staged
  config, and confirm the default-vendor fallback logic still makes sense.

### Documentation

- [ ] `docs/vendors.md` — vendor capability table.
- [ ] `docs/hooks.md` — hook event mapping table.
- [ ] `docs/mcp.md` — MCP config format table.
- [ ] `docs/marketplace.md` — marketplace format table.
- [ ] `docs/artifacts.md`, `docs/skills-standard.md` — skills/agents/rules/
  commands format notes if the vendor has quirks (e.g. Claude's plugin loader
  demoting certain frontmatter fields).
- [ ] `docs/getting-started.md`, `docs/harness-engineering.md`, `README.md`,
  `AGENTS.md` — mentions of the supported-vendor list.
- [ ] `docs/tutorial/02-vendors-and-symlinks.md` and any other tutorial that
  enumerates vendors by name.
- [ ] `docs/tutorial/manual-test-plan.md` — add vendor-specific manual test
  steps.
- [ ] `.github/CONTRIBUTING.md` — the format-mapping tables in the "Vendor
  Adapters" section quote every vendor inline; add a column.
- [ ] `.claude/CLAUDE.md`, `.claude/agents/ynh-contributor.md`,
  `.claude/agents/evals.md`, `.claude/skills/ynh-dev/references/architecture.md`
  — project-level mentions of the vendor list.

### Tests

- [ ] Unit tests alongside every changed file (errcheck is strict — check all
  returned errors).
- [ ] `test/e2e/` — add the new vendor to the end-to-end matrix once the
  adapter is stable enough to exercise against real fixtures (this is the
  release gate, not a per-PR gate — see `.github/CONTRIBUTING.md` § E2E test
  suite).

## Order of operations

1. Research + update `vendor-adapters` skill docs/tables (this makes the
   adapter code review-able against a written spec, not tribal knowledge).
2. Write the adapter + its test, get `make test FILE=./internal/vendor`
   green in isolation.
3. Wire exporter + marketplace + docker image.
4. Wire `ynh agent run` backend if applicable.
5. Update all documentation touch points.
6. `make check` (full CI: deps, format, lint, test, build).
7. Manual smoke test: `make install`, create a throwaway harness, `ynh run -v
   <name>` against the real vendor CLI, confirm launch, skills/rules/MCP/hooks
   actually take effect.
8. Only then: commit, push, PR into `develop` per the branching model and
   pre-remote gates (`.claude/rules/branching.md`, `.claude/rules/pre-remote.md`).

## Common pitfalls

- **Don't force a lowest-common-denominator design.** Per
  `.github/CONTRIBUTING.md` § Design Stance, "vendor-neutral" means every
  *feature* translates cleanly to every vendor — not that every vendor must
  implement every feature identically. It's fine for `SupportsExportDelegates()`
  or `ExportArtifactDirs()` to return "not supported" for a vendor that
  genuinely lacks the concept; document the gap, don't fake support.
- **Don't assume the CLI's flags are stable.** Vendor CLIs that are under
  active development (weekly releases) explicitly warn that flags change —
  verify against the CLI's own `--help` output at implementation time, not
  just docs pulled during research.
- **Don't invent a new artifact directory shape** unless the vendor genuinely
  requires it — reuse `DefaultArtifactDirs()` (`skills`, `agents`, `rules`,
  `commands`) unless the vendor's own conventions differ enough to justify a
  custom `ArtifactDirs()` override (see Codex's `.agents/skills/` handling).
- **Skills-as-directories vs skills-as-files**: some vendors (Claude, Cursor)
  read `SKILL.md` under vendor-owned directories; some read it under
  cross-vendor directories like `.agents/skills/` or even another vendor's
  directory natively (a vendor recognizing `.claude/skills/` directly, for
  example) — check whether the new vendor's own docs list *other* vendors'
  conventions as inputs it accepts natively before assuming it needs its own
  private copy.
