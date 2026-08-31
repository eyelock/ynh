#!/usr/bin/env bash
#
# Stamp the harness version into every manifest that carries one.
#
# The version reached 0.1.0 across eight files while the product reached
# v0.6.0, because each was hand-maintained and nothing compared them. Someone
# browsing the marketplace saw a plugin that looked abandoned.
#
# So the version is now derived from one place — the git tag — and written by
# this script rather than by hand. `scripts/marketplace-consistency.sh` then
# refuses to pass if the files disagree, which is what stops the drift
# returning.
#
#   ./scripts/stamp-harness-version.sh 0.6.0     # explicit
#   ./scripts/stamp-harness-version.sh           # from the latest tag
#   ./scripts/stamp-harness-version.sh --check   # report drift, write nothing
#
# Run by the release procedure (.claude/commands/release.md) after the version
# is chosen and before the release branch is cut.

set -euo pipefail
cd "$(dirname "$0")/.."

# Every manifest that carries a harness version.
#
# The calibration fixture under tools/sensors/fixtures/ is deliberately absent:
# it is a broken manifest whose whole purpose is to fail validation, and
# stamping it would be editing a fixture to match the code it is meant to test.
FILES=(
	.ynh-plugin/plugin.json
	.claude-plugin/plugin.json
	.cursor-plugin/plugin.json
	.claude-plugin/marketplace.json
	.github/plugin/marketplace.json
	.claude/.ynh-plugin/plugin.json
	.claude/.claude-plugin/plugin.json
	.claude/.cursor-plugin/plugin.json
)

check_only=0
version=""
case "${1:-}" in
	--check) check_only=1 ;;
	"")      version="$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//')" ;;
	*)       version="${1#v}" ;;
esac

if [ "$check_only" = "0" ]; then
	if [ -z "$version" ]; then
		echo "no version given and no git tag found" >&2
		exit 2
	fi
	if ! printf '%s' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
		echo "version must be X.Y.Z (got \"$version\")" >&2
		exit 2
	fi
fi

python3 - "$check_only" "$version" "${FILES[@]}" <<'PY'
import json, sys, collections

check_only = sys.argv[1] == "1"
want = sys.argv[2]
files = sys.argv[3:]

def versions_in(doc):
    """Every place a version lives in one document."""
    out = []
    if "version" in doc:
        out.append(("version", doc))
    for p in doc.get("plugins", []):
        if "version" in p:
            out.append((f"plugins[{p.get('name','?')}].version", p))
    return out

seen = collections.defaultdict(list)
changed = []

for f in files:
    with open(f) as fh:
        doc = json.load(fh)
    slots = versions_in(doc)
    if not slots:
        print(f"  ---       {f} carries no version")
        continue
    for label, holder in slots:
        seen[holder["version"]].append(f"{f}:{label}")
    if check_only:
        continue
    if all(h["version"] == want for _, h in slots):
        print(f"  ok        {f}")
        continue
    for _, holder in slots:
        holder["version"] = want
    with open(f, "w") as fh:
        json.dump(doc, fh, indent=2)
        fh.write("\n")
    changed.append(f)
    print(f"  stamped   {f} -> {want}")

if check_only:
    if len(seen) <= 1:
        only = next(iter(seen), "none")
        print(f"  ok        all manifests agree at {only}")
        sys.exit(0)
    print("  FAIL      harness versions disagree:")
    for v, where in sorted(seen.items()):
        print(f"              {v}: {', '.join(where)}")
    print("            run: make stamp-version VERSION=X.Y.Z")
    sys.exit(1)

if changed:
    print(f"\n  {len(changed)} file(s) stamped to {want} — commit them")
PY
