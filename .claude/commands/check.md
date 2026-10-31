Run every CI check that needs neither Docker nor the network: deps, format, lint, check-tidy, test, build, check-artifacts, check-vendor-parity and check-marketplace. Fix any issues found.

```bash
make check
```

If any step fails, fix the issue and re-run until all checks pass. Do not skip steps.
