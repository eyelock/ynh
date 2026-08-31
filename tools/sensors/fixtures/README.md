# Sensor reference fixtures

Each directory here is a **deliberately broken** input that one sensor must
report a failure on. `ynh check --calibrate` runs the sensor against the
fixture and compares the outcome to the `expect` in `.ynh-plugin/plugin.json`.

They exist because nothing else proves a sensor still detects anything. A
sensor is a command plus an expectation about its exit code; if the command
quietly stops examining things — a path that no longer matches, a rule renamed
upstream — it exits 0 and the gate reports green.

Do not "fix" these files. A fixture that stops failing is a calibration that
stops proving anything.

| Fixture | Sensor | Why it fails |
|---|---|---|
| `harness-invalid/` | `harness-valid` | a sensor source setting both `command` and `files`, which the schema forbids as a strict one-of |

## Why there is only one

A sensor in **this** manifest ships to everyone who runs
`ynh install github.com/eyelock/ynh`. It may therefore only invoke things that
travel with it — which means the `ynh` and `ynd` binaries, and nothing else. A
sensor calling a script under `tools/` exits 127 in a user's project, and since
#294 that is a broken gate rather than a quiet false pass.

Checks that need this repository's own scripts belong in `make check-artifacts`
and CI, not in the shipped manifest.
