---
name: add-vendor-adapter
description: Scaffold a brand-new ynh vendor adapter end-to-end (a new AI CLI harness like GitHub Copilot CLI, Gemini CLI, OpenCode, etc.). Use when onboarding a vendor that has no adapter yet, as opposed to vendor-adapters, which maintains adapters that already exist against evolving vendor specs.
---

# Add a New Vendor Adapter

Use this skill when ynh needs to support a **new** AI coding CLI that has no
`internal/vendor/<name>.go` yet. If the vendor already has an adapter and you
are reacting to a spec change, use the `vendor-adapters` skill instead. That
skill owns the per-vendor references this skill asks you to write.

## Before writing code: research

1. Research the vendor CLI's actual behaviour, from official docs and the
   CLI's own `--help`, not blog posts. Vendor CLIs change fast and secondary
   sources go stale. Cover every row of the feature map below.
2. Write `.claude/skills/vendor-adapters/references/<vendor>.md`, modelled on
   `references/copilot.md`: doc URLs, plugin format, directory layout, hook
   events, MCP schema, CLI flags, and known gaps. Mark what you hand-tested as
   **CONFIRMED** and leave the rest as documentation claims.
3. Add the vendor's row to the per-vendor reference index and a documentation
   links section in `.claude/skills/vendor-adapters/SKILL.md`.
   `make check-vendor-parity` (check A) fails until the row exists.
4. Decide the **launch strategy** up front. It drives most other decisions:
   - **Native bundle loading by flag** (Claude, Copilot: `--plugin-dir`):
     `syscall.Exec`, no symlinks, `NeedsSymlinks() == false`.
   - **Native bundle loading by environment variable** (a CLI that takes a
     config file or config directory from an env var): also `syscall.Exec`
     with the variable added to the environment, no symlinks. Confirm by hand
     which artifact types the vendor actually reads from that location; docs
     often list fewer than the loader takes, or more.
   - **No native bundle loading** (Codex, Cursor): symlink install into the
     project's vendor config dir plus a managed child process with signal
     forwarding, `NeedsSymlinks() == true`.

## Feature map: what ynh needs from a vendor

Fill this in for the vendor before writing code. Each row is a method or
surface; "not supported" is a valid answer if documented (see pitfalls).

| ynh feature | Adapter surface | Ask of the vendor |
|---|---|---|
| Identity | `Name`, `DisplayName`, `CLIName` | Binary name; install method |
| Layout | `ConfigDir`, `ArtifactDirs` | Project config dir; where it reads skills, agents, rules, commands |
| Instructions | `InstructionsFile`, `GenerateSystemPrompt` | Which file it reads (AGENTS.md? its own?) |
| Per-run context | `ApplyRuntimeInstructions` | A system-prompt flag, or a file to append to |
| Install | `NeedsSymlinks`, `Install`, `Clean` | See launch strategy |
| Launch | `LaunchInteractive`, `LaunchNonInteractive`, `LaunchWithInitialPrompt` | TUI command, one-shot command, initial-prompt flag |
| Resume | `SupportsResume`, `ResolveLastSession`, `LaunchResume` (+ `SessionBinder`) | Continue-last form; explicit-id form; a readable on-disk session store; can a resumed session take a prompt, interactively and one-shot? Can a new session take a caller-chosen id? |
| Hooks | `GenerateHookConfig` (+ `PluginHookGenerator`, `SessionHookScriptDirer`) | Declarative hook config? Event names mapped to ynh's canonical events |
| Hook events | `<x>HookEventMap`, `<x>UnsupportedHookEvents` (+ `<x>MatcherEvents`) | Native name for each canonical event, or why there is none; which native events honour a matcher |
| Hook timeouts | `<x>HookTimeoutField`, `<x>HookTimeoutUnsupported`, `<x>HookTimeoutCaps` | A per-hook timeout field, its unit, and any per-event cap |
| Session-start context | `<x>SessionStartContext` | Does a session-start hook's stdout reach the model as written, only through a JSON field, or not at all? |
| Workspace | `<x>WorkspaceFlag`, `<x>WorkspaceUnsupported` | A flag that grants a folder beside the project (`--add-dir`), or why there is none |
| MCP | `GenerateMCPConfig` (+ `PluginMCPGenerator`) | Config key and shape; transport spelling for `stdio`, `streamable-http`, `sse` |
| MCP in a plugin | `mcpPluginRootVar` | A variable the plugin loader expands to the plugin's install path in an MCP server's command and cwd |
| Artifact rewrite | `ArtifactTransformer` (optional) | Does it need a different file name or frontmatter (Cursor's `.mdc`)? |
| Export / plugin | `GeneratePluginManifest`, `PluginManifestDir`, `ExportArtifactDirs`, `SupportsExportDelegates` (+ `ExportManifestGenerator`) | A plugin manifest format? Which artifact types a plugin may carry |
| Marketplace | `MarketplaceManifestDir`, `GenerateMarketplaceIndex` | A marketplace index format, or none |
| Agent Plugins | `AgentPluginLayout` | Does it read agent-plugins.org packages? Its namespace |
| Agent loop | `internal/agent` `WorkerBackend` | Non-interactive JSON output, session id, resume by id, effort flag, auto-approve modes |
| ynd LLM | `llmVendors` | One-shot prompt on stdin, answer as plain text on stdout |

The required methods are documented in `internal/vendor/adapter.go`; read the
doc comment of each before implementing it. The optional interfaces (marked
`+` above) live in `internal/exporter/exporter.go` (`PluginHookGenerator`,
`PluginMCPGenerator`, `ExportManifestGenerator`) and
`internal/assembler/` (`SessionHookScriptDirer` in `hooks.go`,
`ArtifactTransformer` in `assembler.go`). Implement one only when the vendor
genuinely differs in that way; each doc comment names the vendor that needed it.

## Checklist: files to touch

The fastest completeness check is the last vendor added. At any point:

```bash
grep -rli copilot --include="*.go" --include="*.md" --include="*.json" \
  --include="*.sh" --include="*.yaml" . | grep -v '^./.git/'
```

Every hit is a candidate. Most need the new vendor added; a few are about
Copilot alone. The grep misses prose that counts vendors rather than naming
them, so also look for spelled-out counts:

```bash
grep -rniE "all (four|five|six) |(four|five|six) vendors" \
  --include="*.go" --include="*.md" --include="*.sh" . | grep -v '^./.git/'
```

### Core adapter

- [ ] `internal/vendor/<name>.go`: implements the full `Adapter` interface.
  Self-register in `init()` via `Register(&<Name>{})`. Spell the binary only in
  `CLIName`; everything else asks the adapter (#524).
- [ ] `internal/vendor/<name>_test.go`: table-driven tests for every method,
  matching the depth of `internal/vendor/copilot_test.go`. The shared contract
  tests in `adapter_contract_test.go` run over every registered adapter, so
  read their failures first.
- [ ] `internal/vendor/adapter_contract_test.go`: add the vendor's row to
  `want` in `TestVendorDeclarativeContract`. A vendor with no plugin manifest
  or no marketplace format also has to relax the tests that assume one:
  `TestPluginManifestPaths` and `TestPluginManifestDirMatchesGeneratedManifest`
  (an empty `PluginManifestDir` must write no manifest, rather than fail), and
  `TestBuildDefaultsToEveryRegisteredVendor` in
  `internal/marketplace/agentplugin_build_test.go` (skip a vendor with an empty
  `MarketplaceManifestDir`). OpenCode (#580) needed all three.
- [ ] Tests that count or list vendors: `TestExportAllVendors`
  (`internal/exporter/exporter_test.go`) checks the number of export results,
  and `cmd/ynh/image_test.go` lists every vendor's `COPY` line. The
  per-vendor tables in `internal/vendor/workspace_test.go`
  (`TestWorkspaceArgs`) and `internal/agent/workspace_test.go`
  (`TestWorkerArgsRenderWorkspace`) need a row or subtest for the vendor.
  `TestHookWarnings` in `internal/vendor/hook_events_test.go` assumes every
  vendor maps all six events; a vendor that cannot carry one is skipped there
  and gets its own subtest pinning what ynh reports for it (as OpenCode's
  does in #580).
- [ ] Symlink vendors reuse `installSymlinks` / `cleanSymlinks`
  (`internal/vendor/symlinks.go`). Managed child processes reuse
  `runChildProcess` (`internal/vendor/process.go`).
- [ ] Resume follows the two rules in `internal/vendor/resume.go`: session ids
  come from the vendor's on-disk store, never from terminal output, and a bare
  resume flag is never emitted (it opens a picker and hangs an unattended
  relaunch). Return `ErrNoResumableSession` and
  `ErrSessionLookupUnavailable` for the two different cases; callers fall back
  differently.
- [ ] `LaunchResume(configPath, sessionID, prompt, interactive, extraArgs)`:
  an empty prompt resumes interactively; a prompt is sent into the resumed
  session, which stays open when `interactive` is true and is a one-shot run
  when it is false. A combination the CLI has no documented form for returns
  `ErrResumePromptNotSupported` before launching anything; never drop the
  prompt (`internal/vendor/adapter.go`).
- [ ] `SessionBinder` (`adapter.go`) is optional: implement it only if the CLI
  takes a caller-chosen id for a new session and ynh can read its store
  (Claude alone today, for `ynh run --session-name`). No stub otherwise.
- [ ] MCP transports: map ynh's canonical names (`internal/plugin/plugin.go`,
  `MCPType*`) to the vendor's spelling.
- [ ] `mcpPluginRootVar` (`internal/vendor/mcpanchor.go`): add the vendor only
  if its plugin loader documents (or you confirm by hand) a variable that
  expands to the plugin's install path in an MCP server's command and cwd.
  Without an entry, an exported `./` server command is left as written and
  warned about, which is the right answer when there is no such variable.

### Per-vendor declarations

These are plain variables in `internal/vendor/<name>.go`, read both by Go
tables and, as text, by the parity scripts (checks F and G below). Declare
every one, even when the answer is "not supported": an undeclared vendor fails
`make check-vendor-parity`. A table entry that names an undeclared variable
does not compile, and a vendor missing from `hookEventSupport` gets no hook
warnings at all from `HookWarnings`.

- [ ] Hook events: `<x>HookEventMap` (canonical event to native name) and
  `<x>UnsupportedHookEvents` (canonical event to the reason it is not
  written). Every canonical event is in exactly one of the two. Add the
  vendor's case to `hookEventSupport` in `internal/vendor/hookwarn.go`.
- [ ] Matchers: if the vendor honours a matcher on some native events only,
  declare `<x>MatcherEvents` and add the vendor's case to `matcherWarnings`
  (`hookwarn.go`), as Cursor and Copilot do. A vendor that passes a matcher
  through on every event, like Claude and Codex, needs neither.
- [ ] Hook timeouts: `<x>HookTimeoutField` (the native field, or `""`),
  `<x>HookTimeoutUnsupported` (required when the field is `""`) and
  `<x>HookTimeoutCaps` (canonical event to seconds, empty when uncapped). Add
  the vendor's case to `hookTimeoutSupport` in `internal/vendor/hooktimeout.go`.
- [ ] Session-start context: `<x>SessionStartContext`, one of `plain`,
  `json-field:<name>` or `unsupported:<why>`, added to `sessionStartContext`
  in `internal/vendor/sessioncontext.go`. A `json-field` vendor gets
  `hooks/ynh-session-context.sh` wrapped around its `on_session_start`
  commands; confirm by hand which form the CLI actually reads.
- [ ] Workspace: `<x>WorkspaceFlag` (the flag that grants a folder, or `""`)
  and `<x>WorkspaceUnsupported` (the reason, required when the flag is
  `""`); exactly one is non-empty. Add the vendor to `workspaceDeclarations`
  in `internal/vendor/workspace.go`; `ynh run` and `ynh agent run` render
  granted folders through `vendor.WorkspaceArgs`.

### Schema

- [ ] `docs/schema/plugin.schema.json`: add the vendor to the
  `default_vendor` enum. Without it `ynd validate` rejects a harness that
  defaults to the new vendor. Adding an enum member here is additive; see the
  `capability-bump` skill if you also change a `--format json` shape.

### Exporter and marketplace

Both iterate over registered adapters (`vendor.Available()`), so there is no
vendor list to edit. What you own is the adapter's output:

- [ ] `GeneratePluginManifest` (and `GenerateExportPluginManifest` if the
  run-dir layout differs from the export layout). The exporter reaches it via
  `exporter.PluginManifest`; marketplace builds use the same function.
- [ ] `GenerateMarketplaceIndex`, called from `internal/marketplace/index.go`.
  Each adapter declares its own structs.

  Do **not** copy Claude's implementation and rename it. `Cursor` did exactly
  that, and shipped the wrong shape until ynh#301 was fixed: Cursor's
  documented format nests `description` under `metadata`, carries no
  per-plugin `version`, and uses a bare `"plugin-name"` source. Nothing caught
  it, because it compiled and produced valid JSON; it was just the wrong
  vendor's JSON. Write it from `references/<vendor>.md` and check the result
  against that file, not against another adapter.

  `codexMarketplaceJSON` in `marketplace.go` is a **test** unmarshalling
  helper, not the generator. Do not add a sibling expecting it to produce
  output.
- [ ] `AgentPluginLayout`, used by `internal/exporter/agentplugin.go`. Check
  that file for per-vendor special cases (it branches on `"cursor"` for MCP)
  and decide whether the new vendor needs one.

### `ynh agent run` backend (if the CLI supports scriptable sessions)

This is a separate surface from `vendor.Adapter`; a vendor can have one
without the other. If the CLI supports non-interactive prompting at all, add
both in the same PR since they share research.

- [ ] `internal/agent/<name>.go`: implement `WorkerBackend`
  (`internal/agent/worker.go`). Claude, Codex and Cursor use three different
  resume strategies; read all three before choosing.
- [ ] Find the CLI with `lookWorkerCLI(b.Name())`, never a literal.
- [ ] Render `StartOptions.Workspace` with `workspaceArgs(<vendor>,
  opts.Workspace)` (`worker.go`, which calls `vendor.WorkspaceArgs`), as the
  Claude and Codex backends do. Never spell the flag in the backend.
- [ ] `internal/agent/loop.go`: `validateBackend` (accepted names and the
  "supported" error text) and `selectBackend` (construction and its error
  text).
- [ ] `internal/agent/effort.go`: does the backend accept a reasoning effort?
  Return nil, or an error that says why and what to do instead.
- [ ] `internal/agent/autoapprove.go`: which of `edits` and `all` the backend
  can honour. Refuse a level it cannot enforce; never widen it silently.
- [ ] The `Backend` doc comments in `worker.go` and `loop.go` that list the
  backends.
- [ ] `cmd/ynh/help.go`: the `ynh agent run` help lists the backends under
  `--backend`, says which accept `--effort`, and which honour
  `--auto-approve`. Update all three.

### ynd

- [ ] `cmd/ynd/llm.go`: add the vendor to `llmVendors` if its CLI can take a
  one-shot prompt on stdin and print only the answer as text. Arguments only;
  the binary comes from the adapter.

### Docker image

- [ ] `cmd/ynh/image.go`: add a `COPY --chown=ynh:ynh` line (no `--link`) for
  the vendor's staged config, and confirm the default-vendor fallback still
  makes sense.

### Evals and parity

- [ ] `.claude/agents/evals.md`: add the vendor's CLI binary to the `STUBS`
  line and the stub table, so no eval can launch the real CLI.
- [ ] `make check-vendor-parity` (`scripts/vendor-parity.sh`). `make check`
  runs it, and so does CI. Its checks:
  - A: the vendor has a row in the reference index.
  - B: every vendor assembles the same artifact set, `plugin/` manifest
    included. A vendor with no plugin manifest format goes in
    `NO_MANIFEST_VENDORS` in the script (added for OpenCode in #580), which
    compares it on everything else and fails if it writes `plugin/` anyway.
    Name it there explicitly; the list is not inferred, so a broken manifest
    cannot exempt itself.
  - C: every vendor CLI ynh or ynd launches is stubbed in evals.
  - D: tutorial eval lines (`*This launches:* ...`) are well formed and name
    a stubbed program. Any tutorial block you add for the vendor needs them.
  - E: every repository a tutorial command names is served by
    `scripts/eval-remotes.sh`.
  - F: `scripts/hook-event-parity.sh`: the vendor's hook event map,
    unsupported events, timeout field (or reason) and session-start context
    are declared.
  - G: `scripts/workspace-parity.sh`: the vendor declares a workspace flag or
    the reason it has none.

### Documentation

Every file that enumerates vendors. The grep above finds them; at the time of
writing these were:

- [ ] Reference: `docs/vendors.md`, `docs/hooks.md`, `docs/mcp.md`,
  `docs/marketplace.md`, `docs/artifacts.md`, `docs/skills-standard.md`,
  `docs/docker.md`, `docs/ynd.md`, `docs/agent.md` (the `ynh agent run`
  backends, and the `--auto-approve` table, which has a column per backend),
  `docs/workspace.md` (the per-vendor workspace flag table) and
  `docs/focus.md` (the per-vendor resume table).
- [ ] Entry points: `README.md`, `AGENTS.md`, `docs/getting-started.md`
  (switching vendors, and symlink installation for a symlink vendor),
  `docs/harness-engineering.md`.
- [ ] Tutorials: `docs/tutorial/README.md`, `vendors-and-symlinks.md`,
  `export.md`, `hooks.md` (how each vendor's hook file is carried in an
  export), `marketplace.md` (the per-vendor manifests and indexes a build
  writes), `mcp-servers.md`, `docker-image.md`, `developer-tools.md` (the LLM
  CLIs `ynd compress` and `ynd inspect` accept), and `manual-test-plan.md`
  (add vendor-specific manual steps).
- [ ] Contributor docs: `.github/CONTRIBUTING.md` (format-mapping tables quote
  every vendor; add a column), `.claude/CLAUDE.md`,
  `.claude/agents/ynh-contributor.md`,
  `.claude/skills/ynh-dev/references/architecture.md`.
- [ ] Shipped artifacts that teach users about vendors: `skills/ynh-*/`
  (create-harness, adopt, team-setup, troubleshoot), `skills/ynd-inspect/`
  (`references/inspect-workflow.md`) and `agents/ynh-harness-reviewer.md`.
- [ ] `.skillspector-baseline.yaml`: the new
  `.claude/skills/vendor-adapters/references/<vendor>.md` is scanned by
  `make scan-artifacts`, and a reference that documents a vendor's
  auto-approve flag or permission rules can trip it (#580 needed `EA2` and
  `PE1` for OpenCode). Add a rule scoped by `path:` to that one file, with a
  `reason:`, only for a finding you have triaged as a false positive.

### Tests

- [ ] Unit tests alongside every changed file (errcheck is strict).
- [ ] `test/e2e/`: add the vendor to the end-to-end matrix once the adapter is
  stable enough for real fixtures. This is the release gate, not a per-PR gate
  (`.github/CONTRIBUTING.md` § E2E test suite).

## Order of operations

1. Research, fill in the feature map, write `references/<vendor>.md` and the
   index row. The adapter is then reviewable against a written spec.
2. Adapter and its test; `make test FILE=./internal/vendor` green in
   isolation.
3. Per-vendor declarations (hook events, timeouts, session-start context,
   workspace, MCP plugin root), schema enum, exporter and marketplace output,
   Agent Plugins layout, docker image.
4. `ynh agent run` backend and `llmVendors`, if applicable.
5. Evals stubs, then every documentation touch point.
6. `make check` (which includes `check-vendor-parity`) and `make scan-artifacts`.
7. Manual smoke test: `make install`, create a throwaway harness in `/tmp/`,
   `ynh run -v <name>` against the real CLI, and confirm launch, skills,
   instructions, MCP and hooks actually take effect.
8. Only then: commit, push, PR into `develop` per
   `.claude/rules/branching.md` and `.claude/rules/pre-remote.md` (evals and
   the manual spot-check are required for this kind of change).

## Common pitfalls

- **Don't force a lowest-common-denominator design.** Per
  `.github/CONTRIBUTING.md` § Design Stance, "vendor-neutral" means every
  *feature* translates cleanly to every vendor, not that every vendor
  implements every feature identically. It is fine for
  `SupportsExportDelegates()` or `GenerateMarketplaceIndex()` to say "not
  supported" for a vendor that lacks the concept; document the gap, don't
  fake support.
- **Hooks may not be declarative.** Some vendors expose hooks only through a
  code plugin (JS, TS) rather than a config file. Generating a plugin that
  shells out to the harness's hook commands is a design decision, not a
  mapping; raise it with the user before building it, and document the event
  mapping in `docs/hooks.md` either way.
- **Don't assume the CLI's flags are stable.** Verify against the CLI's own
  `--help` at implementation time, not just docs pulled during research.
  Undocumented behaviour you rely on (stdin handling, JSON event fields) goes
  in the reference as such.
- **Check which major version is installed before trusting any doc.**
  OpenCode's main docs described 1.x while the installed binary was 2.x,
  with a different launch model, config schema and plugin API. Research
  against `<cli> --version` and its `--help`, then pick the docs that match.
- **Test each claim with a check that does not depend on the model.** An
  agent that is "not found", a hook that writes to a log file, or a tool
  call that errors with your message is a yes or no answer. Asking the model
  "what is the codeword?" is not; it answered with the wrong file's codeword
  and hid that the `instructions` key was ignored. Free or cheap models are
  enough for this, since the question is whether a feature takes effect.
- **A CLI with a background service may ignore your environment.** If
  config comes from an env var, confirm the variable reaches whichever
  process reads config, not just the client you launched (OpenCode 2 needs
  `--standalone` for this).
- **Don't invent a new artifact directory shape** unless the vendor requires
  it. Reuse `DefaultArtifactDirs()` unless the vendor's own conventions differ
  enough to justify an override (see Codex's `.agents/skills/`).
- **Check which other vendors' layouts it already reads.** Many CLIs read
  `AGENTS.md`, `.agents/skills/` or even `.claude/skills/` and `CLAUDE.md`
  natively. That can mean the vendor needs no private copy, or that a run
  loads the same skill twice from two places. Know which before choosing a
  layout, and look for a switch that turns the fallback off.
