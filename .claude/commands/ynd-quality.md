Run the quality pipeline over the harness artifacts this repo ships. Fix any issues found.

```bash
make check-artifacts
```

This runs `ynd validate .` plus `ynd lint` over `skills/ agents/ rules/ .claude/`
— the artifacts this repository ships and the one it uses on itself.

**Do not run bare `ynd lint` at the repo root.** It walks `testdata/`, which
contains deliberately malformed migration fixtures the test suite depends on.
"Fixing" those breaks tests. `make check-artifacts` scopes to the directories
that are meant to be clean.

`ynd fmt` is deliberately not part of this. It rewrites files, and a quality
check that silently edits the tree on success is a surprise. Run it explicitly
when you want formatting applied.
