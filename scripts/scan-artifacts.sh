#!/usr/bin/env bash
# Security-scan the harness artifacts with SkillSpector, through the same gate
# CI runs (eyelock/actions/skillspector), using its published image.
#
# The scan sees what git sees: tracked files plus untracked ones that are not
# ignored, copied to a temporary directory. A working tree here holds other
# checkouts (.worktrees/, .claude/worktrees/), and scanning the directory
# itself would gate every one of them.
#
# The gate fails on any finding the baseline does not suppress. It takes the
# verdict from the SARIF, because `skillspector scan` exits 0 for a skill whose
# risk score is 50 or below whatever it found.
#
# Usage: scripts/scan-artifacts.sh IMAGE [gate options...]
# Exit codes: 0 clean, 1 findings, 2 scan error, 3 docker missing

set -euo pipefail

IMAGE="${1:?usage: scan-artifacts.sh IMAGE [gate options...]}"
shift

command -v docker >/dev/null 2>&1 || {
  echo "docker not found: scan-artifacts runs SkillSpector from $IMAGE" >&2
  exit 3
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

git ls-files -z --cached --others --exclude-standard -- . \
    ':(exclude).worktrees' ':(exclude).claude/worktrees' |
  while IFS= read -r -d '' file; do
    # A tracked file deleted in the working tree is still listed.
    if [[ -e "$file" ]]; then printf '%s\0' "$file"; fi
  done |
  tar --null -T - -cf - | tar -xf - -C "$tmp"

docker run --rm --user "$(id -u):$(id -g)" --volume "$tmp:/scan:ro" "$IMAGE" gate . "$@"
