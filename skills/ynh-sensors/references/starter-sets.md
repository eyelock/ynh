# Starter sensor sets, by stack

Proposals, not prescriptions. Every one of these should be replaced by whatever
the project already runs — a sensor wrapping a command the team already trusts
gets kept; one you invented gets switched off.

Each set deliberately covers all three categories. If a set below has no
`architecture` sensor, that is a gap to fill with the user, not a sign the
category does not apply.

All new sensors start `advisory`. Promote to `blocking` once the repo is clean
for them.

## About the `match` patterns

Every set that wraps a tool with decorated output declares `output.match`,
because without it ynh fingerprints **every** line the tool prints. A tool's
headers, source context and summary counts then become recorded debt, and
fixing one real finding turns `7 issues:` into `6 issues:`, a line the baseline
has never seen. The gate reports a correct repair as a new finding. See
`docs/sensors.md`.

Two of these were verified against the tools' real output while writing them:

- **golangci-lint**: one finding line and four decoration lines, of which the
  pattern selects exactly the finding.
- **go test**: `match` alone is not enough. `--- FAIL: TestX (0.00s)` carries a
  duration, so its fingerprint changes between runs even when nothing else
  did, and an identical failure reported two new findings on a second run. The
  command therefore strips durations before ynh sees them, and only then does
  the pattern give a stable result.

The rest are derived from each tool's documented output format rather than run
here. **Check yours against real output before promoting the sensor to
`blocking`**, and note that a pattern selecting nothing is reported rather than
silent: `ynh check` says so instead of recording an empty baseline.

`npm run lint` has no pattern because what it runs is whatever the project
configured, so no pattern would be right for everyone. Add one for your linter.

## Go

```json
"sensors": {
  "fmt": {
    "category": "maintainability",
    "tolerance": "blocking",
    "version_command": "gofmt -h",
    "source": { "command": "sh -c 'out=$(gofmt -l cmd internal); [ -z \"$out\" ] || { echo \"$out\" >&2; exit 1; }'" },
    "output": { "format": "text" }
  },
  "lint": {
    "category": "maintainability",
    "tolerance": "advisory",
    "version_command": "golangci-lint --version",
    "source": { "command": "golangci-lint run" },
    "output": { "format": "text", "match": "^[^ ]+\\.go:[0-9]+:[0-9]+:" }
  },
  "test": {
    "category": "behaviour",
    "tolerance": "blocking",
    "version_command": "go version",
    "source": { "command": "sh -c 'out=$(go test -race ./... 2>&1); rc=$?; printf \"%s\\n\" \"$out\" | sed -E \"s/ \\([0-9.]+s\\)$//\"; exit $rc'" },
    "output": { "format": "text", "match": "^--- FAIL: " }
  },
  "coverage": {
    "category": "behaviour",
    "tolerance": "report",
    "source": { "files": ["coverage.out"] },
    "observes": ["cmd/**", "internal/**"],
    "output": { "format": "text" }
  },
  "layering": {
    "category": "architecture",
    "tolerance": "advisory",
    "source": { "command": "sh -c '! grep -rn \"cmd/\" internal/ --include=*.go'" },
    "output": { "format": "text" }
  }
}
```

`layering` is the one worth explaining: `internal/` importing from `cmd/` is
backwards, and nothing else catches it. Adapt the direction to the project's
actual boundary.

## Node / TypeScript

```json
"sensors": {
  "format": {
    "category": "maintainability",
    "tolerance": "blocking",
    "version_command": "npx prettier --version",
    "source": { "command": "npx prettier --check ." },
    "output": { "format": "text", "match": "^\\[warn\\] " }
  },
  "lint": {
    "category": "maintainability",
    "tolerance": "advisory",
    "version_command": "npx eslint --version",
    "source": { "command": "npm run lint --silent" },
    "output": { "format": "text" }
  },
  "typecheck": {
    "category": "maintainability",
    "tolerance": "blocking",
    "version_command": "npx tsc --version",
    "source": { "command": "npx tsc --noEmit" },
    "output": { "format": "text", "match": "^[^ ]+\\([0-9]+,[0-9]+\\): error " }
  },
  "tests": {
    "category": "behaviour",
    "tolerance": "blocking",
    "source": { "files": ["junit.xml"] },
    "observes": ["src/**", "test/**"],
    "output": { "format": "junit-xml" }
  },
  "any-count": {
    "category": "maintainability",
    "tolerance": "advisory",
    "ratchet": "count",
    "source": { "command": "sh -c 'n=$(grep -rn \": any\" src/ | wc -l); echo \"$n explicit any\"; [ \"$n\" -eq 0 ]'" },
    "output": { "format": "text" }
  },
  "bundle-size": {
    "category": "architecture",
    "tolerance": "advisory",
    "source": { "files": ["dist/stats.json"] },
    "observes": ["src/**", "package.json"],
    "output": { "format": "json" }
  }
}
```

`any-count` is the canonical `ratchet: count` case — the identities churn every
refactor, so fingerprinting them is noise; the number is the finding.

## Python

```json
"sensors": {
  "format": {
    "category": "maintainability",
    "tolerance": "blocking",
    "version_command": "ruff --version",
    "source": { "command": "ruff format --check ." },
    "output": { "format": "text", "match": "^Would reformat: " }
  },
  "lint": {
    "category": "maintainability",
    "tolerance": "advisory",
    "version_command": "ruff --version",
    "source": { "command": "ruff check ." },
    "output": { "format": "text", "match": "^[^ ]+:[0-9]+:[0-9]+:" }
  },
  "types": {
    "category": "maintainability",
    "tolerance": "advisory",
    "version_command": "mypy --version",
    "source": { "command": "mypy src" },
    "output": { "format": "text", "match": "^[^ ]+:[0-9]+: (error|note):" }
  },
  "tests": {
    "category": "behaviour",
    "tolerance": "blocking",
    "version_command": "pytest --version",
    "source": { "command": "pytest -q" },
    "output": { "format": "text", "match": "^FAILED " }
  },
  "import-boundary": {
    "category": "architecture",
    "tolerance": "advisory",
    "source": { "command": "sh -c '! grep -rn \"from api\" src/domain/'" },
    "output": { "format": "text" }
  }
}
```

## Rust

```json
"sensors": {
  "fmt": {
    "category": "maintainability",
    "tolerance": "blocking",
    "version_command": "cargo fmt --version",
    "source": { "command": "cargo fmt --check" },
    "output": { "format": "text" }
  },
  "clippy": {
    "category": "maintainability",
    "tolerance": "advisory",
    "version_command": "cargo clippy --version",
    "source": { "command": "cargo clippy -- -D warnings" },
    "output": { "format": "text", "match": "^(warning|error)(\\[[A-Z0-9]+\\])?: " }
  },
  "test": {
    "category": "behaviour",
    "tolerance": "blocking",
    "version_command": "cargo --version",
    "source": { "command": "cargo test" },
    "output": { "format": "text", "match": "^test .* FAILED$" }
  },
  "unsafe-count": {
    "category": "architecture",
    "tolerance": "advisory",
    "ratchet": "count",
    "source": { "command": "sh -c 'n=$(grep -rn \"unsafe \" src/ | wc -l); echo \"$n unsafe blocks\"; [ \"$n\" -eq 0 ]'" },
    "output": { "format": "text" }
  }
}
```

## JVM (Gradle)

```json
"sensors": {
  "build": {
    "category": "maintainability",
    "tolerance": "blocking",
    "version_command": "./gradlew --version",
    "source": { "command": "./gradlew compileJava" },
    "output": { "format": "text" }
  },
  "test": {
    "category": "behaviour",
    "tolerance": "blocking",
    "source": { "files": ["build/test-results/test/*.xml"] },
    "observes": ["src/main/**", "src/test/**"],
    "output": { "format": "junit-xml" }
  },
  "deps": {
    "category": "architecture",
    "tolerance": "advisory",
    "source": { "command": "./gradlew checkArchitecture" },
    "output": { "format": "text" }
  }
}
```

## Polyglot monorepo

Do not declare one sensor per language and call it done — a monorepo's real risk
is cross-package, and per-language sensors cannot see it.

```json
"sensors": {
  "affected-build": {
    "category": "maintainability",
    "tolerance": "blocking",
    "source": { "command": "make build-affected" },
    "output": { "format": "text" }
  },
  "affected-test": {
    "category": "behaviour",
    "tolerance": "blocking",
    "source": { "command": "make test-affected" },
    "output": { "format": "text" }
  },
  "package-boundaries": {
    "category": "architecture",
    "tolerance": "blocking",
    "source": { "command": "make check-imports" },
    "output": { "format": "text" }
  },
  "lockfile-drift": {
    "category": "maintainability",
    "tolerance": "blocking",
    "source": { "command": "sh -c 'make lockfiles && git diff --quiet -- \"**/*.lock\"'" },
    "output": { "format": "text" }
  }
}
```

`lockfile-drift` is the monorepo sensor people wish they had earlier: it fails
when regenerating lockfiles changes them, which is how a dependency bump lands
in one package and silently not another.

## A note on `focus` sensors

A `focus` sensor asks the model a question as a measurement:

```json
"security-review": {
  "category": "behaviour",
  "tolerance": "report",
  "source": { "focus": { "prompt": "Are there security regressions in the diff against main?" } },
  "output": { "format": "markdown" }
}
```

`ynh check` **defers** these (`status: deferred`) — running one needs an agent
runtime ynh does not own, so it never blocks whatever tolerance you declare. A
loop driver picks them up. Declare them for what they give a loop, not
expecting them to gate a PR.
