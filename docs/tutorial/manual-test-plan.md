# Manual Test Plan

Verification checklist for ynh and ynd. Each test references a tutorial step or is an edge case tested here.

Run every tutorial in `docs/tutorial/` in sequence to cover the happy path. This file adds
edge cases and error-handling tests the tutorials do not cover, plus a reference table for
tracking.

The count is deliberately not written down. It has been wrong twice — it read 15 when there
were 20, and still read 15 when there were 22 — because a number in prose does not move when
a tutorial is added. Every tutorial should have a section below; a missing one is a gap to
close, not a number to correct.

---

## Prerequisites

Before running any tests, install the dev binaries so `ynh` and `ynd` resolve to your local build everywhere — including outside the repo:

```bash
make install
```

This builds both binaries and copies them to `~/.ynh/bin/`. Verify you're running the dev build:

```bash
ynd version
# Expected: dev-<branch>-<sha> (not a release tag)
```

Re-run `make install` after any code change you want to test.

---

## Test Reference

### First Harness

| Test | Tutorial step |
|---|---|
| Create harness structure | [Create the harness structure](first-harness.md#create-the-harness-structure) |
| Add all artifact types | [Add artifacts](first-harness.md#add-artifacts) |
| Verify structure | [Verify structure](first-harness.md#verify-structure) |
| Install from local path | [Install from local path](first-harness.md#install-from-local-path) |
| List installed harnesses | [List installed harnesses](first-harness.md#list-installed-harnesses) |
| Inspect harness detail | [Inspect harness detail](first-harness.md#inspect-harness-detail) |
| Run interactive | [Run interactive](first-harness.md#run-interactive) |
| Run non-interactive | [Run non-interactive](first-harness.md#run-non-interactive) |
| Run with vendor flags | [Run with vendor flags](first-harness.md#run-with-vendor-flags) |
| Run with --instructions | [Run with per-invocation instructions](first-harness.md#run-with-per-invocation-instructions) |
| Inspect assembled output | [Inspect the assembled output](first-harness.md#inspect-the-assembled-output) |
| Uninstall | [Uninstall](first-harness.md#uninstall) |

### Vendors & Symlinks

| Test | Tutorial step |
|---|---|
| Create and install test harness | [Create and install a test harness](vendors-and-symlinks.md#create-and-install-a-test-harness) |
| List available vendors | [List available vendors](vendors-and-symlinks.md#list-available-vendors) |
| Switch vendors | [Switch vendors](vendors-and-symlinks.md#switch-vendors) |
| Automatic symlink prompt | [Symlinks — automatic prompt](vendors-and-symlinks.md#symlinks-automatic-prompt) |
| Explicit install and clean | [Symlinks — explicit install and clean](vendors-and-symlinks.md#symlinks-explicit-install-and-clean) |
| Claude no-symlinks | [Symlinks — Claude doesn't need them](vendors-and-symlinks.md#symlinks-claude-doesn-t-need-them) |
| Prune orphans | [Prune orphaned installations](vendors-and-symlinks.md#prune-orphaned-installations) |

### Composition

| Test | Tutorial step |
|---|---|
| Pick skills from own repo | [Own repo — pick skills from eyelock/assistants](composition.md#own-repo-pick-skills-from-eyelock-assistants) |
| Local checkout include | [Own repo — local checkout (no clone)](composition.md#own-repo-local-checkout-no-clone) |
| Anthropic third-party skills | [Third-party — Anthropic's official skills](composition.md#third-party-anthropic-s-official-skills) |
| Vercel third-party skills | [Third-party — Vercel's skills](composition.md#third-party-vercel-s-skills) |
| Mix own + third-party | [Mixed sources — own + third-party](composition.md#mixed-sources-own-third-party) |
| Embedded local skills | [Local — embedded skills in the harness](composition.md#local-embedded-skills-in-the-harness) |
| Local Git repo include | [Local — include from a local Git repo](composition.md#local-include-from-a-local-git-repo) |
| Pin with ref | [Pin a version with ref](composition.md#pin-a-version-with-ref) |
| Update Git sources | [Update Git sources](composition.md#update-git-sources) |
| Install from monorepo | [Install from a monorepo](composition.md#install-from-a-monorepo) |
| Allow-list deny | [Allow-list — deny a source](composition.md#allow-list-deny-a-source) |
| Allow-list allow | [Allow-list — allow a source](composition.md#allow-list-allow-a-source) |

### Delegation

| Test | Tutorial step |
|---|---|
| Create delegate harness | [Create a delegate harness](delegation.md#create-a-delegate-harness) |
| Create parent with delegates | [Create a parent harness with delegates](delegation.md#create-a-parent-harness-with-delegates) |
| Install and verify | [Install and verify](delegation.md#install-and-verify) |
| Inspect delegate agents | [Inspect delegate agent files](delegation.md#inspect-delegate-agent-files) |
| Test delegation | [Test delegation](delegation.md#test-delegation) |

### Export

| Test | Tutorial step |
|---|---|
| Create export harness | [Create a harness to export](export.md#create-a-harness-to-export) |
| Export all vendors | [Export for all vendors](export.md#export-for-all-vendors) |
| Verify Claude export | [Verify Claude export](export.md#verify-claude-export) |
| Verify Cursor export | [Verify Cursor export](export.md#verify-cursor-export) |
| Verify Codex export | [Verify Codex export](export.md#verify-codex-export) |
| Export specific vendor | [Export for specific vendors](export.md#export-for-specific-vendors) |
| Export merged mode | [Export in merged mode](export.md#export-in-merged-mode) |
| Export --clean | [Export with --clean](export.md#export-with-clean) |
| Export from Git URL | [Export from a Git URL](export.md#export-from-a-git-url) |
| Export no instructions | [Export with no instructions](export.md#export-with-no-instructions) |

### Marketplace

| Test | Tutorial step |
|---|---|
| Set up source material | [Set up source material](marketplace.md#set-up-source-material) |
| Create marketplace config | [Create the marketplace config](marketplace.md#create-the-marketplace-config) |
| Build marketplace | [Build the marketplace](marketplace.md#build-the-marketplace) |
| Verify output | [Verify the output](marketplace.md#verify-the-output) |
| Test with Claude Code | [Test with Claude Code](marketplace.md#test-with-claude-code) |
| Build --clean | [Build with --clean](marketplace.md#build-with-clean) |
| Build specific vendors | [Build for specific vendors](marketplace.md#build-for-specific-vendors) |

### Registry & Discovery

| Test | Tutorial step |
|---|---|
| Create local registry | [Create a local registry](registry-and-discovery.md#create-a-local-registry) |
| Add registry | [Add the registry](registry-and-discovery.md#add-the-registry) |
| List registries | [List registries](registry-and-discovery.md#list-registries) |
| Search | [Search](registry-and-discovery.md#search) |
| Install by exact name | [Install — by exact name](registry-and-discovery.md#install-by-exact-name) |
| Install with qualifier | [Install — with registry qualifier](registry-and-discovery.md#install-with-registry-qualifier) |
| Direct URL precedence | [Install — direct URL still works](registry-and-discovery.md#install-direct-url-still-works) |
| Partial match | [Install — partial match suggests results](registry-and-discovery.md#install-partial-match-suggests-results) |
| No match error | [Install — no match error](registry-and-discovery.md#install-no-match-error) |
| Update registries | [Update registries](registry-and-discovery.md#update-registries) |
| Remove registry | [Remove a registry](registry-and-discovery.md#remove-a-registry) |
| Add local source | [Add a local source](registry-and-discovery.md#add-a-local-source) |
| List sources | [List sources](registry-and-discovery.md#list-sources) |
| Search includes source harnesses | [Search includes source harnesses](registry-and-discovery.md#search-includes-source-harnesses) |
| Install from source | [Install from source](registry-and-discovery.md#install-from-source) |
| Uninstall removes source entry | [Uninstall removes the source entry](registry-and-discovery.md#uninstall-removes-the-source-entry) |

### Developer Tools

| Test | Tutorial step |
|---|---|
| Scaffold harness | [Scaffold a harness](developer-tools.md#scaffold-a-harness) |
| Scaffold artifacts | [Scaffold artifacts](developer-tools.md#scaffold-artifacts) |
| Author content | [Author content](developer-tools.md#author-content) |
| Lint | [Lint](developer-tools.md#lint) |
| Validate | [Validate](developer-tools.md#validate) |
| Format | [Format](developer-tools.md#format) |
| Compress | [Compress](developer-tools.md#compress) |
| Inspect | [Inspect](developer-tools.md#inspect) |

### Docker Images

Requires Docker installed and running.

| Test | Tutorial step |
|---|---|
| Pull or build base image | [Pull the base image](docker-image.md#pull-the-base-image) |
| Create and install tutorial harness | [Create and install a tutorial harness](docker-image.md#create-and-install-a-tutorial-harness) |
| Build a harness image | [Build a harness image](docker-image.md#build-a-harness-image) |
| Run the harness image | [Run the harness image](docker-image.md#run-the-harness-image) |
| Switch vendors at runtime | [Switch vendors at runtime](docker-image.md#switch-vendors-at-runtime) |
| Pass vendor flags | [Pass vendor flags](docker-image.md#pass-vendor-flags) |
| Inspect with --dry-run | [Inspect with --dry-run](docker-image.md#inspect-with-dry-run) |
| Build from Git source | [Build from Git source](docker-image.md#build-from-git-source) |
| Override entrypoint | [Override entrypoint](docker-image.md#override-entrypoint) |
| CI/CD matrix example | [CI/CD matrix example](docker-image.md#ci-cd-matrix-example) |

### Hooks

| Test | Tutorial step |
|---|---|
| Add hooks to .ynh-plugin/plugin.json | [Add hooks to a harness](hooks.md#add-hooks-to-a-harness) |
| Preview for Claude — verify hooks.json | [Preview for Claude](hooks.md#preview-for-claude) |
| Preview for Cursor — verify hooks.json | [Preview for Cursor](hooks.md#preview-for-cursor) |
| Preview for Codex — verify hooks.json | [Preview for Codex](hooks.md#preview-for-codex) |
| Write a blocking hook script | [Write a blocking hook example](hooks.md#write-a-blocking-hook-example) |
| Diff hook config across vendors | [Compare hook config across vendors](hooks.md#compare-hook-config-across-vendors) |

### MCP Servers

| Test | Tutorial step |
|---|---|
| Add stdio MCP server to harness | [Add a stdio MCP server to a harness](mcp-servers.md#add-a-stdio-mcp-server-to-a-harness) |
| Preview for Claude — verify .claude/.mcp.json | [Preview for Claude](mcp-servers.md#preview-for-claude) |
| Preview for Cursor — verify .cursor/mcp.json | [Preview for Cursor](mcp-servers.md#preview-for-cursor) |
| Preview for Codex — verify JSON | [Preview for Codex](mcp-servers.md#preview-for-codex) |
| Add HTTP MCP server — verify URL | [Add an HTTP MCP server](mcp-servers.md#add-an-http-mcp-server) |
| Diff MCP config across vendors | [Compare MCP config across vendors](mcp-servers.md#compare-mcp-config-across-vendors) |

### Developer Preview

| Test | Tutorial step |
|---|---|
| Preview harness for Claude | [Preview a harness for Claude](developer-preview.md#preview-a-harness-for-claude) |
| Preview harness for Cursor | [Preview the same harness for Cursor](developer-preview.md#preview-the-same-harness-for-cursor) |
| Diff Claude vs Cursor | [Compare Claude vs Cursor output](developer-preview.md#compare-claude-vs-cursor-output) |
| Preview with hooks — inspect config | [Preview a harness with hooks](developer-preview.md#preview-a-harness-with-hooks) |
| Preview with MCP — inspect per vendor | [Preview a harness with MCP servers](developer-preview.md#preview-a-harness-with-mcp-servers) |

### Profiles

| Test | Tutorial step |
|---|---|
| Add profiles to the plugin manifest | [Add profiles to the plugin manifest](profiles.md#add-profiles-to-the-plugin-manifest) |
| Validate profiles | [Validate profiles](profiles.md#validate-profiles) |
| Preview with --profile ci | [Preview with --profile ci](profiles.md#preview-with-profile-ci) |
| Run with --profile ci | [Run with --profile ci](profiles.md#run-with-profile-ci) |
| Try --profile nonexistent | [Try --profile nonexistent](profiles.md#try-profile-nonexistent) |
| Use YNH_PROFILE env var | [Edit profiles from the command line](profiles.md#edit-profiles-from-the-command-line) |
| Flag wins over env var | [Both flag and env var — flag wins](profiles.md#both-flag-and-env-var-flag-wins) |
| Diff with --profile | [Use ynd diff --profile ci](profiles.md#use-ynd-diff-profile-ci) |

### Structured Output

| Test | Tutorial step |
|---|---|
| Show resolved paths — text | [Show resolved paths — text](structured-output.md#show-resolved-paths-text) |
| Show resolved paths — JSON | [Show resolved paths — JSON](structured-output.md#show-resolved-paths-json) |
| Pipe to jq | [Pipe to jq](structured-output.md#pipe-to-jq) |
| Explicit text format | [Explicit text format](structured-output.md#explicit-text-format) |
| Error handling — text mode | [Error handling — text mode](structured-output.md#error-handling-text-mode) |
| Error handling — JSON error envelope | [Error handling — JSON error envelope](structured-output.md#error-handling-json-error-envelope) |
| Space-separated flags only | [Space-separated flags only](structured-output.md#space-separated-flags-only) |
| List harnesses — JSON | [List installed harnesses — JSON](structured-output.md#list-installed-harnesses-json) |
| List harnesses — jq extraction | [List harnesses — extract with jq](structured-output.md#list-harnesses-extract-with-jq) |
| Empty list — JSON | [Empty list — JSON](structured-output.md#empty-list-json) |
| YNH_HOME override | [Check for updates — `--check-updates`](structured-output.md#check-for-updates-check-updates) |
| Inspect install provenance | [Inspect install provenance](structured-output.md#inspect-install-provenance) |
| Validate output against the published schema | [Validate output against the published schema](structured-output.md#validate-output-against-the-published-schema) |

### Focus

| Test | Tutorial step |
|---|---|
| Create a harness with focus entries | [Create a harness with focus entries](focus.md#create-a-harness-with-focus-entries) |
| Validate focus entries | [Validate focus entries](focus.md#validate-focus-entries) |
| Preview with --focus review | [Preview with --focus review](focus.md#preview-with-focus-review) |
| Focus and profile are mutually exclusive | [Focus and profile are mutually exclusive](focus.md#focus-and-profile-are-mutually-exclusive) |
| Unknown focus is an error | [Unknown focus is an error](focus.md#unknown-focus-is-an-error) |
| Use YNH_FOCUS env var | [Use YNH_FOCUS env var](focus.md#use-ynh-focus-env-var) |
| Install and view focus in ynh info | [Install and view focus in ynh info](focus.md#install-and-view-focus-in-ynh-info) |
| Edit focuses from the command line | [Edit focuses from the command line](focus.md#edit-focuses-from-the-command-line) |
| Clean up | [Clean up](focus.md#clean-up) |
| What You Learned | [What You Learned](focus.md#what-you-learned) |

### Project-Local Config

| Test | Tutorial step |
|---|---|
| Create a project with .ynh-plugin/plugin.json | [Create a project with .ynh-plugin/plugin.json](project-local-config.md#create-a-project-with-ynh-plugin-plugin-json) |
| Validate the project config | [Validate the project config](project-local-config.md#validate-the-project-config) |
| Preview the assembled output | [Preview the assembled output](project-local-config.md#preview-the-assembled-output) |
| Preview with --focus | [Preview with --focus](project-local-config.md#preview-with-focus) |
| Clean up | [Clean up](project-local-config.md#clean-up) |
| What You Learned | [What You Learned](project-local-config.md#what-you-learned) |
| Composition with focus | [Composition with focus](project-local-config.md#composition-with-focus) |

### Include Editing

| Test | Tutorial step |
|---|---|
| Add an include | [Add an include](include-editing.md#add-an-include) |
| Add with flags | [Add with flags](include-editing.md#add-with-flags) |
| Duplicate add → error | [Duplicate add → error](include-editing.md#duplicate-add-error) |
| Replace an existing include | [Replace an existing include](include-editing.md#replace-an-existing-include) |
| Update an include | [Update an include](include-editing.md#update-an-include) |
| Update a path value | [Update a path value](include-editing.md#update-a-path-value) |
| Remove an include | [Remove an include](include-editing.md#remove-an-include) |
| Disambiguating a monorepo | [Disambiguating a monorepo](include-editing.md#disambiguating-a-monorepo) |
| Installed harnesses — name-based targeting (network required) | [Installed harnesses — name-based targeting (network required)](include-editing.md#installed-harnesses-name-based-targeting-network-required) |
| Path resolution — id vs path | [Path resolution — id vs path](include-editing.md#path-resolution-id-vs-path) |
| Clean up | [Clean up](include-editing.md#clean-up) |

### Namespacing & Migration

| Test | Tutorial step |
|---|---|
| Canonical ids — the new identity model | [Canonical ids — the new identity model](namespacing-and-migration.md#canonical-ids-the-new-identity-model) |
| Demo — two registries, two `david` harnesses | [Demo — two registries, two `david` harnesses](namespacing-and-migration.md#demo-two-registries-two-david-harnesses) |
| Search returns both entries | [Search returns both entries](namespacing-and-migration.md#search-returns-both-entries) |
| Disambiguate install with `@<registry>` | [Disambiguate install with `@<registry>`](namespacing-and-migration.md#disambiguate-install-with) |
| Inspect by canonical id | [Inspect by canonical id](namespacing-and-migration.md#inspect-by-canonical-id) |
| Uninstall by canonical id | [Uninstall by canonical id](namespacing-and-migration.md#uninstall-by-canonical-id) |
| Migrate a legacy harness with `ynd migrate` | [Migrate a legacy harness with `ynd migrate`](namespacing-and-migration.md#migrate-a-legacy-harness-with-ynd-migrate) |
| Recursive migration | [Recursive migration](namespacing-and-migration.md#recursive-migration) |
| Transparent migration on use | [Transparent migration on use](namespacing-and-migration.md#transparent-migration-on-use) |
| `ynh migrate` — upgrade `~/.ynh` schema | [`ynh migrate` — upgrade `~/.ynh` schema](namespacing-and-migration.md#ynh-migrate-upgrade-ynh-schema) |
| `ynh quarantine` — recover from broken installs | [`ynh quarantine` — recover from broken installs](namespacing-and-migration.md#ynh-quarantine-recover-from-broken-installs) |
| Clean up | [Clean up](namespacing-and-migration.md#clean-up) |
| What You Learned | [What You Learned](namespacing-and-migration.md#what-you-learned) |

### Sensors

| Test | Tutorial step |
|---|---|
| A `files` sensor | [A `files` sensor](sensors.md#a-files-sensor) |
| A `command` sensor | [A `command` sensor](sensors.md#a-command-sensor) |
| A `focus`-referencing sensor | [A `focus`-referencing sensor](sensors.md#a-focus-referencing-sensor) |
| An inline-focus sensor | [An inline-focus sensor](sensors.md#an-inline-focus-sensor) |
| Discovery — `ynh sensors ls/show` | [Discovery — `ynh sensors ls/show`](sensors.md#discovery-ynh-sensors-ls-show) |
| Validation — every rule produces a clear error | [Validation — every rule produces a clear error](sensors.md#validation-every-rule-produces-a-clear-error) |
| Hook–sensor pairing | [Hook–sensor pairing](sensors.md#hook-sensor-pairing) |
| Install round-trip preserves sensors | [Install round-trip preserves sensors](sensors.md#install-round-trip-preserves-sensors) |
| What a loop driver does with this | [What a loop driver does with this](sensors.md#what-a-loop-driver-does-with-this) |

### Check & Baseline

| Test | Tutorial step |
|---|---|
| Declare sensors with a tolerance | [Declare sensors with a tolerance](check.md#declare-sensors-with-a-tolerance) |
| A clean run | [A clean run](check.md#a-clean-run) |
| A blocking failure gates | [A blocking failure gates](check.md#a-blocking-failure-gates) |
| A `files` sensor gates on freshness | [A `files` sensor gates on freshness](check.md#a-files-sensor-gates-on-freshness) |
| The problem with inheriting a repository | [The problem with inheriting a repository](check.md#the-problem-with-inheriting-a-repository) |
| Record a baseline | [Record a baseline](check.md#record-a-baseline) |
| Read what the ratchet forgives | [Read what the ratchet forgives](check.md#read-what-the-ratchet-forgives) |
| Only new failures gate | [Only new failures gate](check.md#only-new-failures-gate) |
| Moving code is not a new failure | [Moving code is not a new failure](check.md#moving-code-is-not-a-new-failure) |
| Paying off debt tightens the ratchet | [Paying off debt tightens the ratchet](check.md#paying-off-debt-tightens-the-ratchet) |
| CI cannot write a baseline | [CI cannot write a baseline](check.md#ci-cannot-write-a-baseline) |
| Exit codes and structured output | [Exit codes and structured output](check.md#exit-codes-and-structured-output) |
| Wire it into the edit loop | [Wire it into the edit loop](check.md#wire-it-into-the-edit-loop) |

### Agent Loop

| Test | Tutorial step |
|---|---|
| A harness with one sensor and a focus | [A harness with one sensor and a focus](agent-loop.md#a-harness-with-one-sensor-and-a-focus) |
| Run the loop | [Run the loop](agent-loop.md#run-the-loop) |
| Budgets, and where the defaults come from | [Budgets, and where the defaults come from](agent-loop.md#budgets-and-where-the-defaults-come-from) |
| Convergence | [Convergence](agent-loop.md#convergence) |
| Running against a scratch checkout | [Running against a scratch checkout](agent-loop.md#running-against-a-scratch-checkout) |
| Reading a trajectory | [Reading a trajectory](agent-loop.md#reading-a-trajectory) |
| The run result | [The run result](agent-loop.md#the-run-result) |
| Exit codes | [Exit codes](agent-loop.md#exit-codes) |
| Resuming | [Resuming](agent-loop.md#resuming) |
| The controls a run does not have | [The controls a run does not have](agent-loop.md#the-controls-a-run-does-not-have) |
| Summary | [Summary](agent-loop.md#summary) |

### Shadow Mode

| Test | Tutorial step |
|---|---|
| Select candidates | [Select candidates](shadow-mode.md#select-candidates) |
| Build the base state | [Build the base state](shadow-mode.md#build-the-base-state) |
| Pin the harness, do not inherit it | [Pin the harness, do not inherit it](shadow-mode.md#pin-the-harness-do-not-inherit-it) |
| Capture the finding as it stood | [Capture the finding as it stood](shadow-mode.md#capture-the-finding-as-it-stood) |
| Run the loop against the base state | [Run the loop against the base state](shadow-mode.md#run-the-loop-against-the-base-state) |
| Compare | [Compare](shadow-mode.md#compare) |
| Grade blind | [Grade blind](shadow-mode.md#grade-blind) |
| Read the result honestly | [Read the result honestly](shadow-mode.md#read-the-result-honestly) |
| Aggregate | [Aggregate](shadow-mode.md#aggregate) |
| Summary | [Summary](shadow-mode.md#summary) |

---

## Edge Cases

Tests not covered by tutorials. Run these after completing the tutorials.

### E1: Version output

```bash
ynh version        # Expected: version string
ynh --version      # Expected: same
ynd version        # Expected: version string
ynd --version      # Expected: same
```

### E2: Help output

```bash
ynh help           # Expected: usage text
ynh --help         # Expected: same
ynh -h             # Expected: same
ynd help           # Expected: usage text
ynd --help         # Expected: same
```

### E3: Install with invalid --path

```bash
mkdir -p /tmp/ynh-edge/repo
echo '{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"edge","version":"0.1.0"}' > /tmp/ynh-edge/repo/.ynh-plugin/plugin.json

ynh install /tmp/ynh-edge/repo --path nonexistent/path
# Expected: Error: path "nonexistent/path" not found in source
```

### E4: Install duplicate harness

```bash
mkdir -p /tmp/ynh-edge/dup
echo '{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"dup","version":"0.1.0"}' > /tmp/ynh-edge/dup/.ynh-plugin/plugin.json

ynh install /tmp/ynh-edge/dup
ynh install /tmp/ynh-edge/dup
# Expected: overwrites without error (idempotent)

ynh uninstall local/dup
```

### E5: Uninstall nonexistent harness

```bash
ynh uninstall local/nonexistent-harness
# Expected: Error: harness "local/nonexistent-harness" is not installed
```

### E6: Run nonexistent harness

```bash
ynh run nonexistent-harness
# Expected: Error: "nonexistent-harness" is not a valid harness id. Use a canonical id like 'github.com/<org>/<repo>/<name>' or 'local/<name>', or './<path>' for a local harness directory. Run 'ynh ls' to see installed ids

ynh run local/nonexistent
# Expected: Error: harness "local/nonexistent": harness not found
```

### E7: Export unknown vendor

```bash
ynd export /tmp/ynh-edge/repo -v fakevend
# Expected: Error: unknown vendor "fakevend" (available: [... order varies ...])
```

### E8: Export missing source

```bash
ynd export
# Expected: Error: usage: ynd export <harness-dir|git-url> [--harness dir] [flags]
```

### E9: Marketplace build without config

```bash
cd /tmp
ynd marketplace build
# Expected: Error: reading marketplace config: open marketplace.json: no such file or directory
```

### E10: Search with no registries

```bash
cp ~/.ynh/config.json ~/.ynh/config.json.bak
echo '{"default_vendor":"claude"}' > ~/.ynh/config.json

ynh search "anything"
# Expected: No results for "anything"
# (no error — unified search succeeds with empty results when no registries or sources are configured)

mv ~/.ynh/config.json.bak ~/.ynh/config.json
```

### E11: Install plain word with no registries

```bash
cp ~/.ynh/config.json ~/.ynh/config.json.bak
echo '{"default_vendor":"claude"}' > ~/.ynh/config.json

ynh install somename
# Expected:
#   Error: no registries configured.
#     Add one with: ynh registry add <url>
#     Or specify a Git URL: ynh install github.com/user/somename

mv ~/.ynh/config.json.bak ~/.ynh/config.json
```

### E12: SSH URL not confused with registry

```bash
ynh install git@github.com:eyelock/nonexistent.git 2>&1 | head -1
# Expected: git clone error, NOT a registry lookup error
```

### E13: Create duplicate scaffold

```bash
cd /tmp
ynd create harness edge-test
ynd create harness edge-test
# Expected: error about already existing

rm -rf edge-test
```

### E14: Validate broken harness

```bash
cd /tmp
ynd create harness broken-test
mkdir -p broken-test/skills/orphan
ynd validate broken-test
# Expected:
#   broken-test: INVALID
#     - skills/orphan/ missing SKILL.md

rm -rf broken-test
```

### E15: Empty allow-list blocks all sources

```bash
cp ~/.ynh/config.json ~/.ynh/config.json.bak
echo '{"default_vendor":"claude","allowed_remote_sources":[]}' > ~/.ynh/config.json

# Any harness with remote includes should fail at both install and run time
# (install my-dev first if not already installed)
my-dev "hello" 2>&1 | head -1
# Expected: Error about remote source not allowed

mv ~/.ynh/config.json.bak ~/.ynh/config.json
```

### E16: Info on installed harness

```bash
ynh info local/my-harness
# Expected: Name, Vendor, Installed timestamp, Source (local path), no includes, no delegates
```

### E17: Info on non-existent harness

```bash
ynh info nonexistent
# Expected: Error: "nonexistent" is not a valid harness id. Use a canonical id like 'github.com/<org>/<repo>/<name>' or 'local/<name>', or './<path>' for a local harness directory. Run 'ynh ls' to see installed ids

ynh info local/nonexistent
# Expected: Error: harness "local/nonexistent": harness not found
```

### E18: Info with no args

```bash
ynh info
# Expected: Error: usage: ynh info <harness-name>
```

### E19: Focus and profile mutual exclusivity

```bash
ynd preview /tmp/some-harness -v claude --focus review --profile ci
# Expected: Error: cannot use --focus and --profile together
```

### E20: Unknown focus name

```bash
ynd preview /tmp/some-harness -v claude --focus nonexistent
# Expected: Error: focus "nonexistent" not defined in harness
```

### E21: Focus with missing prompt in .ynh-plugin/plugin.json

```bash
mkdir -p /tmp/ynh-bad-focus
mkdir -p /tmp/ynh-bad-focus/.ynh-plugin
cat > /tmp/ynh-bad-focus/.ynh-plugin/plugin.json << 'EOF'
{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"bad","version":"0.1.0","focuses":{"review":{"profile":"ci"}}}
EOF
ynd validate /tmp/ynh-bad-focus
# Expected: INVALID with "focus.review: prompt must not be empty"
rm -rf /tmp/ynh-bad-focus
```

### E22: Focus referencing unknown profile

```bash
mkdir -p /tmp/ynh-bad-focus
mkdir -p /tmp/ynh-bad-focus/.ynh-plugin
cat > /tmp/ynh-bad-focus/.ynh-plugin/plugin.json << 'EOF'
{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"bad","version":"0.1.0","focuses":{"review":{"profile":"nonexistent","prompt":"Review code"}}}
EOF
ynd validate /tmp/ynh-bad-focus
# Expected: INVALID with "focus.review: references unknown profile"
rm -rf /tmp/ynh-bad-focus
```

### E23: Fork uninstall via canonical id

`ynh fork` registers a pointer-shaped install. `ynh uninstall local/<name>` must resolve the schema-1 pointer and remove the registration cleanly — this is the form JSON consumers pass back.

```bash
# Create a minimal harness to fork from
mkdir -p /tmp/ynh-fork-src/.ynh-plugin
cat > /tmp/ynh-fork-src/.ynh-plugin/plugin.json << 'EOF'
{"name":"fork-src","version":"1.0.0","default_vendor":"claude"}
EOF

# Install it, then fork by canonical id. `ynh fork` takes an installed
# harness, not a source directory — see `ynh fork <harness-name>` in
# docs/reference.md. Forking a path has never been supported.
ynh install /tmp/ynh-fork-src
ynh fork local/fork-src --to /tmp/ynh-fork-copy --name fork-copy
# Expected: Forked harness "fork-src" as "fork-copy" to /tmp/ynh-fork-copy
#             Source:  /tmp/ynh-fork-src (local)
#             Version: 1.0.0

# Verify it appears in ls
ynh ls --format json | jq -r '.harnesses[] | select(.name=="fork-copy") | .id'
# Expected: local/fork-copy

# Uninstall via canonical id (the form machine consumers use)
ynh uninstall local/fork-copy
# Expected: Uninstalled harness "fork-copy"
#             Source tree left in place: /tmp/ynh-fork-copy

# Verify the pointer is gone
ynh ls --format json | jq -r '.harnesses[] | select(.name=="fork-copy") | .id'
# Expected: (empty — no output)

rm -rf /tmp/ynh-fork-src /tmp/ynh-fork-copy
```

### E25: Fork and registry install sharing a leaf name both appear in ls

A fork (`local/<name>`) and a registry install (`<host>/…/<name>`) that share the same leaf name but have distinct canonical ids must both appear in `ynh ls`. This is the central scenario the canonical-id work enabled.

```bash
export YNH_HOME=$(mktemp -d)

# Simulate a registry install (schema-2 tree). installed.json is not optional:
# a tree without it is a broken entry, and auto-migration aborts on one rather
# than guessing where it came from.
mkdir -p "$YNH_HOME/harnesses/github.com--eyelock--assistants--shared/.ynh-plugin"
cat > "$YNH_HOME/harnesses/github.com--eyelock--assistants--shared/.ynh-plugin/plugin.json" << 'EOF'
{"name":"shared","version":"1.0.0","default_vendor":"claude"}
EOF
cat > "$YNH_HOME/harnesses/github.com--eyelock--assistants--shared/.ynh-plugin/installed.json" << 'EOF'
{"source_type":"registry","source":"github.com/eyelock/assistants","namespace":"github.com/eyelock/assistants","registry_name":"shared","installed_at":"2026-01-01T00:00:00Z"}
EOF

# Register a fork with the same leaf name
mkdir -p /tmp/ynh-fork-shared/.ynh-plugin
cat > /tmp/ynh-fork-shared/.ynh-plugin/plugin.json << 'EOF'
{"name":"shared","version":"2.0.0","default_vendor":"claude"}
EOF
cat > "$YNH_HOME/installed/shared.json" << 'EOF'
{"name":"shared","source_type":"local","source":"/tmp/ynh-fork-shared","installed_at":"2026-01-01T00:00:00Z"}
EOF

ynh ls --format json | jq '[.harnesses[] | select(.name=="shared") | .id]'
# Expected (both ids present, order may vary):
# [
#   "local/shared",
#   "github.com/eyelock/assistants/shared"
# ]

rm -rf /tmp/ynh-fork-shared "$YNH_HOME"
unset YNH_HOME
```

### E24: Broken fork appears as local-fork-broken in ls JSON

When a fork's source directory exists but has no `.ynh-plugin/plugin.json`, `ynh ls --format json` must tag it as `kind: "local-fork-broken"` with a non-empty `broken_reason` rather than emitting an empty-field `local-fork` entry.

```bash
# Register a pointer to a directory with no manifest
mkdir -p /tmp/ynh-hollow-src   # exists but no .ynh-plugin/
export YNH_HOME=$(mktemp -d)
cat > "$YNH_HOME/installed/hollow.json" << 'EOF'
{"name":"hollow","source_type":"local","source":"/tmp/ynh-hollow-src","installed_at":"2026-01-01T00:00:00Z"}
EOF

ynh ls --format json | jq '.harnesses[] | select(.name=="hollow") | {kind, broken_reason}'
# Expected:
# {
#   "kind": "local-fork-broken",
#   "broken_reason": "no harness manifest found in /tmp/ynh-hollow-src"
# }

rm -rf /tmp/ynh-hollow-src "$YNH_HOME"
unset YNH_HOME
```

### E26: Local model backend spec — unknown vendor, and vendors listing fallback

```bash
mkdir -p /tmp/ynh-backend-edge/.ynh-plugin
cat > /tmp/ynh-backend-edge/.ynh-plugin/plugin.json << 'EOF'
{"name":"backend-edge","version":"0.1.0","default_vendor":"claude"}
EOF
export YNH_HOME=$(mktemp -d)
echo '{"backends":{"ollama":{"vendors":{"claude":{"base_url":"http://localhost:11434","auth_token":"ollama"}}}}}' > "$YNH_HOME/config.json"

cd /tmp/ynh-backend-edge
ynh run -v ollama/codex
# Expected: Error: backend "ollama" has no config for vendor "codex" (add backends.ollama.vendors.codex to ~/.ynh/config.json)

ynh vendors --format json | jq '.[] | select(.name=="ollama/claude")'
# Expected: a row present (name, display_name, cli, config_dir, available, supports_initial_prompt).
# No live Ollama server is required for this check: model discovery is best-effort — an
# unreachable/unconfigured-type backend falls back to the bare "<backend>/<vendor>" row
# instead of erroring the whole `ynh vendors` listing.

rm -rf /tmp/ynh-backend-edge "$YNH_HOME"
unset YNH_HOME
```

---

## Sensors

### S1: Declare a command sensor and run it

```bash
mkdir -p /tmp/ynh-sensors/.ynh-plugin
cat > /tmp/ynh-sensors/.ynh-plugin/plugin.json << 'EOF'
{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "sensor-test",
  "version": "0.1.0",
  "default_vendor": "claude",
  "sensors": {
    "build": {
      "category": "maintainability",
      "source": { "command": "echo built && exit 0" },
      "output": { "format": "text" }
    }
  }
}
EOF
ynd validate /tmp/ynh-sensors
ynh install /tmp/ynh-sensors
ynh sensors ls local/sensor-test
ynh sensors run local/sensor-test build | jq '.exit_code, .output.stdout'
ynh uninstall local/sensor-test
rm -rf /tmp/ynh-sensors
```

Expected: `valid`, sensor listed in ls output, run result with `exit_code: 0` and stdout `"built\n"`. No `passed` field in run output.

### S2: focus-source sensor returns resolved payload

Re-run S1 with a focus-source sensor and verify `ynh sensors run` returns the resolved focus declaration plus a note that ynh does not invoke the agent runtime.

### S3: Validation rejects two-source declaration

`source` with both `command` and `files` set must error: `sensor "X": source must have exactly one of files, command, focus, github_status, github_check`.

---

## Summary

| Section | Tests |
|---------|-------|
| First Harness | 11 |
| Vendors & Symlinks | 7 |
| Composition | 12 |
| Delegation | 5 |
| Export | 10 |
| Marketplace | 7 |
| Registry & Discovery | 11 |
| Developer Tools | 8 |
| Docker Images | 10 |
| Hooks | 6 |
| MCP Servers | 6 |
| Developer Preview | 5 |
| Profiles | 8 |
| Focus | 7 |
| Project-Local Config | 4 |
| Structured Output | 11 |
| Sensors | 3 |
| Edge Cases | 26 |
| **Total** | **154** |
