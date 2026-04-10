# @ynh/cli

NPM integration for [ynh](https://github.com/eyelock/ynh) — the AI coding harness manager.

`npm install` makes ynh available. Config in `package.json`. `npm run ai:review` just works.

## Quick Start

```bash
npm install --save-dev @ynh/cli
```

Add a `ynh` block to your `package.json`:

```json
{
  "ynh": {
    "vendor": "claude",
    "focus": {
      "review": { "prompt": "Review staged changes for quality" },
      "security": { "profile": "ci", "prompt": "Audit for OWASP Top 10" }
    }
  },
  "scripts": {
    "ai": "ynh run",
    "ai:review": "ynh run --focus review",
    "ai:security": "ynh run --focus security"
  }
}
```

```bash
npm run ai:review
```

## How It Works

1. `npm install` downloads the ynh binary for your platform via optional dependencies
2. The `ynh` block in `package.json` is read and translated to a `.harness.json`
3. The generated file is written to `node_modules/.cache/ynh/` (not the project root)
4. `ynh run --harness-file <cache-path>` is exec'd with the generated config

The cache directory is automatic and gitignored (inside `node_modules/`). Your project root stays clean.

### Custom output directory

If you need control over where the generated `.harness.json` is written:

```json
{
  "ynh": {
    "vendor": "claude",
    "outputDir": ".build/ynh"
  }
}
```

Or via environment variable: `YNH_OUTPUT_DIR=.build/ynh`

### Local artifacts

Place skills, rules, agents, and commands alongside `package.json`. When using `ynd preview <project-dir>`, they'll be discovered and assembled. For `ynh run` via the npm shim, pass `--harness-file` explicitly if you need local artifact discovery from a different directory.

## Config Reference

The `ynh` block accepts the same fields as `.harness.json`, with one convenience translation:

| package.json | .harness.json | Notes |
|-------------|--------------|-------|
| `vendor` | `default_vendor` | Translated automatically |
| `includes` | `includes` | Same format |
| `hooks` | `hooks` | Same format |
| `mcp_servers` | `mcp_servers` | Same format |
| `profiles` | `profiles` | Same format |
| `focus` | `focus` | Same format |

`name` and `version` are NOT included — they come from `package.json` itself, and NPM scoped names (`@org/pkg`) are incompatible with ynh's name format.

## Platform Support

| Platform | Architecture | Package |
|----------|-------------|---------|
| macOS | ARM64 (Apple Silicon) | `@ynh/cli-darwin-arm64` |
| macOS | x64 (Intel) | `@ynh/cli-darwin-x64` |
| Linux | x64 | `@ynh/cli-linux-x64` |
| Linux | ARM64 | `@ynh/cli-linux-arm64` |

Windows is not yet supported. If ynh is on your PATH (e.g., via Homebrew), the platform packages are not needed.

## CI Usage

```yaml
# .github/workflows/review.yml
jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
      - run: npm ci
      - run: npm run ai:review
        env:
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
```

Or use environment variables directly:

```yaml
env:
  YNH_FOCUS: security
```
