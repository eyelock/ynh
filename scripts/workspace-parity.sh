#!/usr/bin/env bash
# Workspace parity: every vendor must declare how a granted workspace folder
# reaches its CLI, or why it cannot. A new adapter then cannot forget the
# question, and a vendor that cannot do it says so rather than dropping folders.
#
# Usage: scripts/workspace-parity.sh <repo-root> <vendor>...
#
# Reads internal/vendor/<vendor>.go for exactly one of each:
#   <x>WorkspaceFlag        the CLI flag that grants a directory, or ""
#   <x>WorkspaceUnsupported the reason there is none, required when the flag is ""
# Exactly one of the two is non-empty.

set -euo pipefail

ROOT="${1:?usage: workspace-parity.sh <repo-root> <vendor>...}"
shift
[ "$#" -gt 0 ] || { echo "workspace-parity: no vendors given" >&2; exit 1; }

fail=0
for v in "$@"; do
	src="$ROOT/internal/vendor/$v.go"
	if [ ! -f "$src" ]; then
		echo "  MISSING  $v: no $src"
		fail=1
		continue
	fi
	flag=$(sed -nE 's/^(const |var )?[a-z]+WorkspaceFlag = "([^"]*)".*/\2/p' "$src")
	fdecl=$(grep -cE '^(const |var )?[a-z]+WorkspaceFlag = "' "$src" || true)
	unsup=$(sed -nE 's/^(const |var )?[a-z]+WorkspaceUnsupported = "([^"]*)".*/\2/p' "$src")
	udecl=$(grep -cE '^(const |var )?[a-z]+WorkspaceUnsupported = "' "$src" || true)
	if [ "$fdecl" -ne 1 ] || [ "$udecl" -ne 1 ]; then
		echo "  MISSING  $v: must declare <x>WorkspaceFlag and <x>WorkspaceUnsupported exactly once (a flag, or a reason it has none)"
		fail=1
	elif [ -z "$flag" ] && [ -z "$unsup" ]; then
		echo "  MISSING  $v: no workspace flag and no reason in <x>WorkspaceUnsupported"
		fail=1
	elif [ -n "$flag" ] && [ -n "$unsup" ]; then
		echo "  CONFLICT $v: has a workspace flag ($flag) and also declares it unsupported"
		fail=1
	else
		echo "  ok       $v: workspace ${flag:-unsupported}"
	fi
done
exit "$fail"
