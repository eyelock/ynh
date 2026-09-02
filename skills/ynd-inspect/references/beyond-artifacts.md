# From signals to profiles, focuses and sensors

`ynd inspect` proposes **skills and agents**. That is half of "what does this
project need" — the half about what the assistant can *do*.

The other half is configuration: which variants of the harness the project
needs (`profiles`), which starting points people will actually use (`focuses`),
and what the project should *observe* (`sensors`). Inspect does not propose
these today, so the skill walks them afterwards, reasoning from the same
signals inspect already found.

That is the point worth holding on to: **the signals that tell you which skills
to write also tell you what to gate on.** A repo with `.golangci.yml` wants a
lint sensor as much as it wants a lint skill.

## The signal categories

These are the categories `ynd inspect` scans for (`cmd/ynd/signals.go`). Ask
the user which fired — or read the inspect output, which lists them.

| Category | Example signals |
|---|---|
| **Build** | `Makefile`, `justfile`, `go.mod`, `package.json`, `Cargo.toml`, `pyproject.toml`, `pom.xml`, `build.gradle` |
| **Lint/Format** | `.golangci.yml`, `.eslintrc*`, `.prettierrc`, `ruff.toml`, `.rubocop.yml` |
| **Test** | `jest.config.*`, `vitest.config.*`, `pytest.ini`, `conftest.py`, `tox.ini`, `.mocharc.*` |
| **CI/CD** | `.github/workflows/*`, `.gitlab-ci.yml`, `Jenkinsfile`, `.circleci/config.yml` |
| **Container** | `Dockerfile`, `docker-compose.yml`, `k8s/*.yml` |
| **Infrastructure** | `terraform/*.tf`, `Pulumi.yaml`, `serverless.yml`, `Vagrantfile` |
| **Release** | `.goreleaser.yml`, `release.config.js`, `lerna.json`, `.changeset` |
| **Docs** | `README`, `CHANGELOG`, `CONTRIBUTING`, `SECURITY.md` |
| **GitHub** | `.github/CODEOWNERS`, PR and issue templates |

## Signals → sensors

The strongest single move: **whatever CI already runs is a sensor waiting to be
declared.** It is a command the team already trusts, already maintained, and
already believed. Open the CI workflow and read the steps.

| Signal | Sensor it implies | Category |
|---|---|---|
| Lint/Format config present | wrap the linter | `maintainability` |
| Build file present | the build command | `maintainability` |
| Test config present | the test command | `behaviour` |
| Coverage output in CI | a `files` sensor on the report | `behaviour` |
| CI workflow | every gating step in it | mixed — read them |
| Container / IaC | image build, `terraform validate`, `terraform plan` exit status | `behaviour` |
| Release config | a dry-run or version-consistency check | `maintainability` |
| `.github/CODEOWNERS`, layered `src/` | a boundary check | `architecture` |

Hand off to the **`ynh-sensors`** skill once you know which ones apply — it
covers the closed vocabulary, the tolerance ladder, baselines for a dirty repo,
`observes` on `files` sensors, and calibration.

Two things to carry across from inspect:

- **Nothing gets `blocking` on the first pass.** The repo has never run these as
  a gate. Start `advisory`.
- If the project has a coverage or test-report artifact, that is a `files`
  sensor and it needs `observes` — otherwise any commit stales it.

## Signals → profiles

A profile is a named config variant. Propose one only where the user actually
works differently, not one per environment because environments exist.

| Signal | Profile worth proposing |
|---|---|
| CI config present | `ci` — stricter hooks, no interactive prompts |
| Container / IaC present | `infra` — includes the deployment runbook and infra skills |
| Monorepo (many build files) | one per major package, so context stays scoped |
| A `.github/CONTRIBUTING` with a review process | `review` — pairs with a reviewer agent |

Ask: *"is there a mode where you'd want different rules loaded?"* If the answer
is no, do not invent one. An unused profile is a maintenance cost with no reader.

## Signals → focuses

A focus binds a starting prompt to a profile. These are the highest-value and
lowest-cost thing to propose, because they turn "what do I even ask it" into a
named entry point.

Good focuses come from what the team does *repeatedly*:

| Signal | Focus |
|---|---|
| CI config | `fix-ci` — "the build is red, walk me through it" |
| Test config | `add-tests` — "cover this change" |
| Release config | `release` — pinned to the release runbook |
| Container / IaC | `deploy` |
| CHANGELOG | `changelog` — "summarise what changed since the last tag" |

Ask what they did three times last week. That is a focus.

```bash
ynh focus add <harness> fix-ci "The CI build is failing. Read the workflow, find the failing step, and work through it with me."
```

## Order matters

Do sensors last. Profiles and focuses are cheap and reversible; sensors change
what gates a merge, and a badly chosen blocking sensor is the fastest way to get
the whole harness switched off.

## What inspect does not do yet

`ynd inspect` proposes skills and agents only — it has no knowledge of sensors,
profiles or focuses (`grep -c sensor cmd/ynd/inspect*.go` returns 0). Everything
above is the skill reasoning from the same signals by hand.

If that changes, this reference should shrink to a pointer rather than
duplicating what the tool proposes.
