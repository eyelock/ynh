#!/usr/bin/env bash
# Native-subagent parity: every vendor adapter must declare whether it has
# subagents a delegate can run as under `transport: native`, as a bare true or
# false, so a new adapter cannot forget the question. Harness.CheckDelegateTransports
# reads the answer through Adapter.SupportsNativeSubagents, and refuses a native
# delegate on a vendor that says false.
#
# Usage: scripts/subagent-parity.sh <repo-root> <vendor>...
#
# Reads internal/vendor/<vendor>.go for exactly one
#   func (x *X) SupportsNativeSubagents() bool { return true|false }

set -euo pipefail

ROOT="${1:?usage: subagent-parity.sh <repo-root> <vendor>...}"
shift
[ "$#" -gt 0 ] || { echo "subagent-parity: no vendors given" >&2; exit 1; }

fail=0
for v in "$@"; do
	src="$ROOT/internal/vendor/$v.go"
	if [ ! -f "$src" ]; then
		echo "  MISSING  $v: no $src"
		fail=1
		continue
	fi
	n=$(grep -cE '^func \([a-z]+ \*[A-Za-z]+\) SupportsNativeSubagents\(\) bool' "$src" || true)
	ans=$(sed -nE 's/^func \([a-z]+ \*[A-Za-z]+\) SupportsNativeSubagents\(\) bool \{ return (true|false) \}.*/\1/p' "$src")
	if [ "$n" -ne 1 ]; then
		echo "  MISSING  $v: must declare SupportsNativeSubagents exactly once"
		fail=1
	elif [ -z "$ans" ]; then
		echo "  MISSING  $v: SupportsNativeSubagents must return a bare true or false"
		fail=1
	else
		echo "  ok       $v: native subagents $ans"
	fi
done
exit "$fail"
