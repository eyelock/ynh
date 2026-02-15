# Branch Protection Configuration

## Main Branch Required Checks

The following CI checks must pass before merging to `main`:

- **Build**: Verify code compiles successfully
- **Test**: Run all unit and integration tests with race detection
- **Lint**: golangci-lint code quality checks
- **Format Check**: goimports and gofmt code style verification

All checks are configured via GitHub Actions on every pull request.
The `All Clear` aggregator job is the single required status check in branch protection.

## Claude Code Review

The `claude-review` workflow runs automatically on PRs touching Go source files and posts review comments.

**Note**: Currently this check is **informational only** - it posts comments but doesn't block merges.

See `.github/workflows/claude-code-review.yml` for the review workflow.
