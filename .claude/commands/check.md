Run the full CI pipeline: format, lint, test, build, check-artifacts, check-vendor-parity and check-marketplace. Fix any issues found.

```bash
make check
```

If any step fails, fix the issue and re-run until all checks pass. Do not skip steps.
