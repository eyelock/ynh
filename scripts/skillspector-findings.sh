#!/usr/bin/env bash
# Fail the build on un-baselined SkillSpector findings.
#
# `skillspector scan` exits 0 whatever it finds. It reports; it does not gate.
# The Makefile used to rely on that exit code (`|| rc=1`), so the branch that
# prints "SkillSpector found un-baselined findings" was unreachable and the scan
# printed "clean" over an active error-level finding. A control that reports
# success without having observed anything is worse than no control, because it
# occupies the slot where a real one would go.
#
# So the verdict comes from the SARIF, which is the artifact that actually
# carries the findings. A result the baseline matched arrives with a
# `suppressions` entry; anything left is active and fails the build.
#
# Usage: scripts/skillspector-findings.sh <sarif-dir>

set -euo pipefail

SARIF_DIR="${1:?usage: skillspector-findings.sh <sarif-dir>}"

[ -d "$SARIF_DIR" ] || { echo "no SARIF directory at $SARIF_DIR" >&2; exit 1; }

# The scan wrote nothing at all: treat as a failure, not a pass. This is the
# same trap one level up: an empty directory must never read as "clean".
shopt -s nullglob
sarifs=("$SARIF_DIR"/*.sarif)
(( ${#sarifs[@]} )) || { echo "no .sarif files in $SARIF_DIR, so the scan produced nothing" >&2; exit 1; }

python3 - "$SARIF_DIR" <<'PY'
import glob, json, os, sys

sarif_dir = sys.argv[1]
active, suppressed, scanned = [], 0, 0

for path in sorted(glob.glob(os.path.join(sarif_dir, "*.sarif"))):
    with open(path, encoding="utf-8") as fh:
        doc = json.load(fh)
    for run in doc.get("runs", []):
        rules = {r["id"]: r for r in run.get("tool", {}).get("driver", {}).get("rules", [])}
        for res in run.get("results", []):
            scanned += 1
            if res.get("suppressions"):
                suppressed += 1
                continue
            loc = (res.get("locations") or [{}])[0].get("physicalLocation", {})
            rule = res.get("ruleId", "?")
            active.append((
                res.get("level", "warning"),
                rule,
                rules.get(rule, {}).get("name", ""),
                loc.get("artifactLocation", {}).get("uri", "?"),
                loc.get("region", {}).get("startLine", "?"),
                res.get("message", {}).get("text", "").strip(),
            ))

if active:
    order = {"error": 0, "warning": 1, "note": 2}
    print()
    print(f"SkillSpector: {len(active)} un-baselined finding(s):")
    print()
    for level, rule, name, uri, line, msg in sorted(active, key=lambda r: (order.get(r[0], 9), r[3], r[4])):
        label = f"{rule} {name}".strip()
        print(f"  [{level}] {label}")
        print(f"      {uri}:{line}")
        if msg and msg != name:
            print(f"      {msg}")
    print()
    print("Triage each one. Suppress only with a scoped `path:` and a written")
    print("`reason:` in the baseline, never a bare rule id, which silences the")
    print("whole rule family repo-wide.")
    sys.exit(1)

print(f"Skill security scan clean: {scanned} finding(s), {suppressed} baselined, 0 active.")
PY
