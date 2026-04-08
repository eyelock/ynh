# Manual Test Plan

Verification checklist for ynh and ynd. Each test references a tutorial step or is an edge case tested here.

Run all 13 tutorials in sequence to cover the happy path. This file adds edge cases and error handling tests that tutorials don't cover, plus a reference table for tracking.

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

### Tutorial 1: First Harness

| ID | Test | Tutorial step |
|---|---|---|
| T1.1 | Create harness structure | [T1.1](tutorial/01-first-harness.md#t11-create-the-harness-structure) |
| T1.2 | Add all artifact types | [T1.2](tutorial/01-first-harness.md#t12-add-artifacts) |
| T1.3 | Verify structure | [T1.3](tutorial/01-first-harness.md#t13-verify-structure) |
| T1.4 | Install from local path | [T1.4](tutorial/01-first-harness.md#t14-install-from-local-path) |
| T1.5 | List installed harnesses | [T1.5](tutorial/01-first-harness.md#t15-list-installed-harnesses) |
| T1.5b | Inspect harness detail | [T1.5b](tutorial/01-first-harness.md#t15b-inspect-harness-detail) |
| T1.6 | Run interactive | [T1.6](tutorial/01-first-harness.md#t16-run-interactive) |
| T1.7 | Run non-interactive | [T1.7](tutorial/01-first-harness.md#t17-run-non-interactive) |
| T1.8 | Run with vendor flags | [T1.8](tutorial/01-first-harness.md#t18-run-with-vendor-flags) |
| T1.9 | Inspect assembled output | [T1.9](tutorial/01-first-harness.md#t19-inspect-the-assembled-output) |
| T1.10 | Uninstall | [T1.10](tutorial/01-first-harness.md#t110-uninstall) |

### Tutorial 2: Vendors & Symlinks

| ID | Test | Tutorial step |
|---|---|---|
| T2.1 | Create and install test harness | [T2.1](tutorial/02-vendors-and-symlinks.md#t21-create-and-install-a-test-harness) |
| T2.2 | List available vendors | [T2.2](tutorial/02-vendors-and-symlinks.md#t22-list-available-vendors) |
| T2.3 | Switch vendors | [T2.3](tutorial/02-vendors-and-symlinks.md#t23-switch-vendors) |
| T2.4 | Automatic symlink prompt | [T2.4](tutorial/02-vendors-and-symlinks.md#t24-how-symlinks-work) |
| T2.5 | Explicit install and clean | [T2.5](tutorial/02-vendors-and-symlinks.md#t25-explicit-install-and-clean) |
| T2.6 | Claude no-symlinks | [T2.6](tutorial/02-vendors-and-symlinks.md#t26-claude-doesnt-need-symlinks) |
| T2.7 | Prune orphans | [T2.7](tutorial/02-vendors-and-symlinks.md#t27-prune-orphaned-installations) |

### Tutorial 3: Composition

| ID | Test | Tutorial step |
|---|---|---|
| T3.1 | Pick skills from own repo | [T3.1](tutorial/03-composition.md#t31-source-1-your-own-skill-library-eyelockassistants) |
| T3.2 | Local checkout include | [T3.2](tutorial/03-composition.md#t32-using-the-local-checkout) |
| T3.3 | Anthropic third-party skills | [T3.3](tutorial/03-composition.md#t33-anthropics-official-skills) |
| T3.4 | Vercel third-party skills | [T3.4](tutorial/03-composition.md#t34-vercels-skills) |
| T3.5 | Mix own + third-party | [T3.5](tutorial/03-composition.md#t35-mixing-your-own-skills-with-third-party) |
| T3.6 | Embedded local skills | [T3.6](tutorial/03-composition.md#t36-embedded-skills-in-the-harness) |
| T3.7 | Local Git repo include | [T3.7](tutorial/03-composition.md#t37-include-skills-from-a-local-git-repo) |
| T3.8 | Pin with ref | [T3.8](tutorial/03-composition.md#t38-pin-a-version-with-ref) |
| T3.9 | Update Git sources | [T3.9](tutorial/03-composition.md#t39-update-git-sources) |
| T3.10 | Install from monorepo | [T3.10](tutorial/03-composition.md#t310-install-a-harness-directly-from-a-monorepo) |
| T3.11 | Allow-list deny | [T3.11](tutorial/03-composition.md#t311-test-deny-a-source) |
| T3.12 | Allow-list allow | [T3.12](tutorial/03-composition.md#t312-test-allow-a-source) |

### Tutorial 4: Delegation

| ID | Test | Tutorial step |
|---|---|---|
| T4.1 | Create delegate harness | [T4.1](tutorial/04-delegation.md#t41-create-a-delegate-harness) |
| T4.2 | Create parent with delegates | [T4.2](tutorial/04-delegation.md#t42-create-a-parent-harness-with-delegates) |
| T4.3 | Install and verify | [T4.3](tutorial/04-delegation.md#t43-install-and-verify) |
| T4.4 | Inspect delegate agents | [T4.4](tutorial/04-delegation.md#t44-inspect-delegate-agent-files) |
| T4.5 | Test delegation | [T4.5](tutorial/04-delegation.md#t45-test-delegation) |

### Tutorial 5: Export

| ID | Test | Tutorial step |
|---|---|---|
| T5.1 | Create export harness | [T5.1](tutorial/05-export.md#t51-create-a-harness-to-export) |
| T5.2 | Export all vendors | [T5.2](tutorial/05-export.md#t52-export-for-all-vendors) |
| T5.3 | Verify Claude export | [T5.3](tutorial/05-export.md#t53-verify-claude-export) |
| T5.4 | Verify Cursor export | [T5.4](tutorial/05-export.md#t54-verify-cursor-export) |
| T5.5 | Verify Codex export | [T5.5](tutorial/05-export.md#t55-verify-codex-export) |
| T5.6 | Export specific vendor | [T5.6](tutorial/05-export.md#t56-export-for-specific-vendors) |
| T5.7 | Export merged mode | [T5.7](tutorial/05-export.md#t57-export-in-merged-mode) |
| T5.8 | Export --clean | [T5.8](tutorial/05-export.md#t58-export-with---clean) |
| T5.9 | Export from Git URL | [T5.9](tutorial/05-export.md#t59-export-from-a-git-url) |
| T5.10 | Export no instructions | [T5.10](tutorial/05-export.md#t510-export-with-no-instructions) |

### Tutorial 6: Marketplace

| ID | Test | Tutorial step |
|---|---|---|
| T6.1 | Set up source material | [T6.1](tutorial/06-marketplace.md#t61-set-up-source-material) |
| T6.2 | Create marketplace config | [T6.2](tutorial/06-marketplace.md#t62-create-the-marketplace-config) |
| T6.3 | Build marketplace | [T6.3](tutorial/06-marketplace.md#t63-build-the-marketplace) |
| T6.4 | Verify output | [T6.4](tutorial/06-marketplace.md#t64-verify-the-output) |
| T6.5 | Test with Claude Code | [T6.5](tutorial/06-marketplace.md#t65-test-with-claude-code) |
| T6.6 | Build --clean | [T6.6](tutorial/06-marketplace.md#t66-build-with---clean) |
| T6.7 | Build specific vendors | [T6.7](tutorial/06-marketplace.md#t67-build-for-specific-vendors) |

### Tutorial 7: Registry & Discovery

| ID | Test | Tutorial step |
|---|---|---|
| T7.1 | Create local registry | [T7.1](tutorial/07-registry-and-discovery.md#t71-create-a-local-registry) |
| T7.2 | Add registry | [T7.2](tutorial/07-registry-and-discovery.md#t72-add-the-registry) |
| T7.3 | List registries | [T7.3](tutorial/07-registry-and-discovery.md#t73-list-registries) |
| T7.4 | Search | [T7.4](tutorial/07-registry-and-discovery.md#t74-search) |
| T7.5 | Install by exact name | [T7.5](tutorial/07-registry-and-discovery.md#t75-by-exact-name) |
| T7.6 | Install with qualifier | [T7.6](tutorial/07-registry-and-discovery.md#t76-with-registry-qualifier) |
| T7.7 | Direct URL precedence | [T7.7](tutorial/07-registry-and-discovery.md#t77-direct-url-still-works) |
| T7.8 | Partial match | [T7.8](tutorial/07-registry-and-discovery.md#t78-partial-match-suggests-results) |
| T7.9 | No match error | [T7.9](tutorial/07-registry-and-discovery.md#t79-no-match-error) |
| T7.10 | Update registries | [T7.10](tutorial/07-registry-and-discovery.md#t710-update-registries) |
| T7.11 | Remove registry | [T7.11](tutorial/07-registry-and-discovery.md#t711-remove-a-registry) |

### Tutorial 8: Developer Tools

| ID | Test | Tutorial step |
|---|---|---|
| T8.1 | Scaffold harness | [T8.1](tutorial/08-developer-tools.md#t81-scaffold-a-harness) |
| T8.2 | Scaffold artifacts | [T8.2](tutorial/08-developer-tools.md#t82-scaffold-artifacts) |
| T8.3 | Author content | [T8.3](tutorial/08-developer-tools.md#t83-author-content) |
| T8.4 | Lint | [T8.4](tutorial/08-developer-tools.md#t84-lint) |
| T8.5 | Validate | [T8.5](tutorial/08-developer-tools.md#t85-validate) |
| T8.6 | Format | [T8.6](tutorial/08-developer-tools.md#t86-format) |
| T8.7 | Compress | [T8.7](tutorial/08-developer-tools.md#t87-compress) |
| T8.8 | Inspect | [T8.8](tutorial/08-developer-tools.md#t88-inspect) |

### Tutorial 9: Docker Images

Requires Docker installed and running.

| ID | Test | Tutorial step |
|---|---|---|
| T9.1 | Pull or build base image | [T9.1](tutorial/09-docker-image.md#t91-pull-the-base-image) |
| T9.2 | Create and install tutorial harness | [T9.2](tutorial/09-docker-image.md#t92-create-and-install-a-tutorial-harness) |
| T9.3 | Build a harness image | [T9.3](tutorial/09-docker-image.md#t93-build-a-harness-image) |
| T9.4 | Run the harness image | [T9.4](tutorial/09-docker-image.md#t94-run-a-harness-image) |
| T9.5 | Switch vendors at runtime | [T9.5](tutorial/09-docker-image.md#t95-switch-vendors-at-runtime) |
| T9.6 | Pass vendor flags | [T9.6](tutorial/09-docker-image.md#t96-pass-vendor-flags) |
| T9.7 | Inspect with --dry-run | [T9.7](tutorial/09-docker-image.md#t97-inspect-with---dry-run) |
| T9.8 | Build from Git source | [T9.8](tutorial/09-docker-image.md#t98-build-from-git-source) |
| T9.9 | Override entrypoint | [T9.9](tutorial/09-docker-image.md#t99-override-entrypoint) |
| T9.10 | CI/CD matrix example | [T9.10](tutorial/09-docker-image.md#t910-cicd-matrix-example) |

### Tutorial 10: Hooks

| ID | Test | Tutorial step |
|---|---|---|
| T10.1 | Add hooks to harness.json | [T10.1](tutorial/10-hooks.md#t101-add-hooks-to-a-harness) |
| T10.2 | Preview for Claude — verify settings.json | [T10.2](tutorial/10-hooks.md#t102-preview-for-claude) |
| T10.3 | Preview for Cursor — verify hooks.json | [T10.3](tutorial/10-hooks.md#t103-preview-for-cursor) |
| T10.4 | Preview for Codex — verify hooks.json | [T10.4](tutorial/10-hooks.md#t104-preview-for-codex) |
| T10.5 | Write a blocking hook script | [T10.5](tutorial/10-hooks.md#t105-write-a-blocking-hook-example) |
| T10.6 | Diff hook config across vendors | [T10.6](tutorial/10-hooks.md#t106-compare-hook-config-across-vendors) |

### Tutorial 11: MCP Servers

| ID | Test | Tutorial step |
|---|---|---|
| T11.1 | Add stdio MCP server to harness | [T11.1](tutorial/11-mcp-servers.md#t111-add-a-stdio-mcp-server-to-a-harness) |
| T11.2 | Preview for Claude — verify .mcp.json | [T11.2](tutorial/11-mcp-servers.md#t112-preview-for-claude) |
| T11.3 | Preview for Cursor — verify .cursor/mcp.json | [T11.3](tutorial/11-mcp-servers.md#t113-preview-for-cursor) |
| T11.4 | Preview for Codex — verify TOML | [T11.4](tutorial/11-mcp-servers.md#t114-preview-for-codex) |
| T11.5 | Add HTTP MCP server — verify URL | [T11.5](tutorial/11-mcp-servers.md#t115-add-an-http-mcp-server) |
| T11.6 | Diff MCP config across vendors | [T11.6](tutorial/11-mcp-servers.md#t116-compare-mcp-config-across-vendors) |

### Tutorial 12: Developer Preview

| ID | Test | Tutorial step |
|---|---|---|
| T12.1 | Preview harness for Claude | [T12.1](tutorial/12-developer-preview.md#t121-preview-a-harness-for-claude) |
| T12.2 | Preview harness for Cursor | [T12.2](tutorial/12-developer-preview.md#t122-preview-the-same-harness-for-cursor) |
| T12.3 | Diff Claude vs Cursor | [T12.3](tutorial/12-developer-preview.md#t123-compare-claude-vs-cursor-output) |
| T12.4 | Preview with hooks — inspect config | [T12.4](tutorial/12-developer-preview.md#t124-preview-a-harness-with-hooks) |
| T12.5 | Preview with MCP — inspect per vendor | [T12.5](tutorial/12-developer-preview.md#t125-preview-a-harness-with-mcp-servers) |

### Tutorial 13: Profiles

| ID | Test | Tutorial step |
|---|---|---|
| T13.1 | Add profiles to harness.json | [T13.1](tutorial/13-profiles.md#t131-add-profiles-to-harnessjson) |
| T13.2 | Validate profiles | [T13.2](tutorial/13-profiles.md#t132-validate-profiles) |
| T13.3 | Preview with --profile ci | [T13.3](tutorial/13-profiles.md#t133-preview-with---profile-ci) |
| T13.4 | Run with --profile ci | [T13.4](tutorial/13-profiles.md#t134-run-with---profile-ci) |
| T13.5 | Try --profile nonexistent | [T13.5](tutorial/13-profiles.md#t135-try---profile-nonexistent) |
| T13.6 | Use YNH_PROFILE env var | [T13.6](tutorial/13-profiles.md#t136-use-ynh_profile-env-var) |
| T13.7 | Flag wins over env var | [T13.7](tutorial/13-profiles.md#t137-both-flag-and-env-var--flag-wins) |
| T13.8 | Diff with --profile | [T13.8](tutorial/13-profiles.md#t138-use-ynd-diff---profile-ci) |

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
echo '{"name":"edge","version":"0.1.0"}' > /tmp/ynh-edge/repo/harness.json

ynh install /tmp/ynh-edge/repo --path nonexistent/path
# Expected: Error: path "nonexistent/path" not found in source
```

### E4: Install duplicate harness

```bash
mkdir -p /tmp/ynh-edge/dup
echo '{"name":"dup","version":"0.1.0"}' > /tmp/ynh-edge/dup/harness.json

ynh install /tmp/ynh-edge/dup
ynh install /tmp/ynh-edge/dup
# Expected: overwrites without error (idempotent)

ynh uninstall dup
```

### E5: Uninstall nonexistent harness

```bash
ynh uninstall nonexistent-harness
# Expected: Error: harness "nonexistent-harness" is not installed
```

### E6: Run nonexistent harness

```bash
ynh run nonexistent-harness
# Expected: Error: harness "nonexistent-harness": harness not found
```

### E7: Export unknown vendor

```bash
ynd export /tmp/ynh-edge/repo -v fakevend
# Expected: Error: unknown vendor "fakevend"
```

### E8: Export missing source

```bash
ynd export
# Expected: Error: usage: ynd export <harness-dir|git-url> [flags]
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
# Expected: Error about no registries configured

mv ~/.ynh/config.json.bak ~/.ynh/config.json
```

### E11: Install plain word with no registries

```bash
cp ~/.ynh/config.json ~/.ynh/config.json.bak
echo '{"default_vendor":"claude"}' > ~/.ynh/config.json

ynh install somename
# Expected: Error: no registries configured. Add one with: ynh registry add <url>

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
# Expected: INVALID — skills/orphan/ missing SKILL.md

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
ynh info my-harness
# Expected: Name, Vendor, Installed timestamp, Source (local path), no includes, no delegates
```

### E17: Info on non-existent harness

```bash
ynh info nonexistent
# Expected: Error: harness "nonexistent": harness not found
```

### E18: Info with no args

```bash
ynh info
# Expected: Error: usage: ynh info <harness-name>
```

---

## Summary

| Section | Tests |
|---------|-------|
| Tutorial 1: First Harness | 11 |
| Tutorial 2: Vendors & Symlinks | 7 |
| Tutorial 3: Composition | 12 |
| Tutorial 4: Delegation | 5 |
| Tutorial 5: Export | 10 |
| Tutorial 6: Marketplace | 7 |
| Tutorial 7: Registry & Discovery | 11 |
| Tutorial 8: Developer Tools | 8 |
| Tutorial 9: Docker Images | 10 |
| Tutorial 10: Hooks | 6 |
| Tutorial 11: MCP Servers | 6 |
| Tutorial 12: Developer Preview | 5 |
| Tutorial 13: Profiles | 8 |
| Edge Cases | 18 |
| **Total** | **124** |
