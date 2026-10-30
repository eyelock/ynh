#!/usr/bin/env bash
# Hook event parity: every canonical hook event must be mapped, or explicitly
# declared unsupported with a reason, for every vendor. A new event added to the
# manifest schema then cannot be forgotten on one vendor.
#
# Usage: scripts/hook-event-parity.sh <repo-root> <vendor>...
#
# Reads, under <repo-root>:
#   docs/schema/plugin.schema.json   the canonical events ($defs.hooks.propertyNames.enum)
#   docs/schema/harness.schema.json  the same list, which must agree
#   internal/plugin/plugin.go        ValidHookEvents, which must list the same set
#   internal/vendor/<vendor>.go      <x>HookEventMap          canonical -> native
#                                    <x>UnsupportedHookEvents canonical -> reason
#                                    <x>HookTimeoutField      native timeout field, or ""
#                                    <x>HookTimeoutUnsupported reason, required when the field is ""
#
# The schema, ValidHookEvents, and each vendor's two maps are plain Go and JSON
# literals, so this is a text check: no build, no vendor CLI.

set -euo pipefail

ROOT="${1:?usage: hook-event-parity.sh <repo-root> <vendor>...}"
shift
[ "$#" -gt 0 ] || { echo "hook-event-parity: no vendors given" >&2; exit 1; }
command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }

SCHEMA="$ROOT/docs/schema/plugin.schema.json"
HARNESS_SCHEMA="$ROOT/docs/schema/harness.schema.json"
PLUGIN_GO="$ROOT/internal/plugin/plugin.go"

# keys <file> <var-regex>: the quoted keys of one map literal, one per line.
keys() {
	awk -v re="$2" '
		$0 ~ re && /\{$/ { f = 1; next }
		f && /^\}/ { f = 0 }
		f { print }
	' "$1" | sed -nE 's/^[[:space:]]*"([a-z_]+)":.*/\1/p' | sort -u
}

# reasons <file> <var-regex>: key<TAB>reason for each entry of an unsupported map.
reasons() {
	awk -v re="$2" '
		$0 ~ re && /\{$/ { f = 1; next }
		f && /^\}/ { f = 0 }
		f { print }
	' "$1" | sed -nE 's/^[[:space:]]*"([a-z_]+)":[[:space:]]*"([^"]*)".*/\1\t\2/p'
}

events=$(jq -r '."$defs".hooks.propertyNames.enum[]' "$SCHEMA" | sort -u)
[ -n "$events" ] || { echo "  FAIL     no hook events found in $SCHEMA" >&2; exit 1; }

fail=0

# Both schemas and ValidHookEvents describe the same set.
go_events=$(keys "$PLUGIN_GO" 'ValidHookEvents = map')
harness_events=$(jq -r '."$defs".hooks.propertyNames.enum[]' "$HARNESS_SCHEMA" | sort -u)
for pair in "plugin.go ValidHookEvents:$go_events" "harness.schema.json:$harness_events"; do
	label=${pair%%:*}; other=${pair#*:}
	if [ "$events" != "$other" ]; then
		echo "  MISMATCH plugin.schema.json and $label list different events"
		diff <(printf '%s\n' "$events") <(printf '%s\n' "$other") | sed 's/^/      /' || true
		fail=1
	else
		echo "  ok       plugin.schema.json and $label agree"
	fi
done

for v in "$@"; do
	src="$ROOT/internal/vendor/$v.go"
	if [ ! -f "$src" ]; then
		echo "  MISSING  $v: no $src"
		fail=1
		continue
	fi
	mapped=$(keys "$src" 'HookEventMap = map')
	declared=$(keys "$src" 'UnsupportedHookEvents = map')
	if [ -z "$mapped" ] && [ -z "$declared" ]; then
		echo "  MISSING  $v: no <x>HookEventMap or <x>UnsupportedHookEvents in internal/vendor/$v.go"
		fail=1
		continue
	fi
	bad=0
	for e in $events; do
		in_map=0; in_decl=0
		printf '%s\n' "$mapped" | grep -qx "$e" && in_map=1
		printf '%s\n' "$declared" | grep -qx "$e" && in_decl=1
		if [ "$in_map" -eq 0 ] && [ "$in_decl" -eq 0 ]; then
			echo "  MISSING  $v: $e is neither mapped nor declared unsupported"
			bad=1
		elif [ "$in_map" -eq 1 ] && [ "$in_decl" -eq 1 ]; then
			echo "  CONFLICT $v: $e is both mapped and declared unsupported"
			bad=1
		elif [ "$in_decl" -eq 1 ]; then
			reason=$(reasons "$src" 'UnsupportedHookEvents = map' | awk -F'\t' -v e="$e" '$1 == e { print $2 }')
			if [ -z "$reason" ]; then
				echo "  MISSING  $v: $e is declared unsupported without a reason"
				bad=1
			fi
		fi
	done
	# A vendor either has a per-hook timeout field or says why it has none.
	tfield=$(sed -nE 's/^(const |var )?[a-z]+HookTimeoutField = "([^"]*)".*/\2/p' "$src")
	tdecl=$(grep -cE '^(const |var )?[a-z]+HookTimeoutField = "' "$src" || true)
	tunsup=$(sed -nE 's/^(const |var )?[a-z]+HookTimeoutUnsupported = "([^"]*)".*/\2/p' "$src")
	tudecl=$(grep -cE '^(const |var )?[a-z]+HookTimeoutUnsupported = "' "$src" || true)
	if [ "$tdecl" -ne 1 ] || [ "$tudecl" -ne 1 ]; then
		echo "  MISSING  $v: must declare <x>HookTimeoutField and <x>HookTimeoutUnsupported exactly once (a timeout field, or a reason it has none)"
		bad=1
	elif [ -z "$tfield" ] && [ -z "$tunsup" ]; then
		echo "  MISSING  $v: no timeout field and no reason in <x>HookTimeoutUnsupported"
		bad=1
	elif [ -n "$tfield" ] && [ -n "$tunsup" ]; then
		echo "  CONFLICT $v: has a timeout field ($tfield) and also declares it unsupported"
		bad=1
	fi
	if [ "$bad" -eq 0 ]; then
		echo "  ok       $v: $(printf '%s\n' "$events" | wc -l | tr -d ' ') events accounted for, timeout ${tfield:-unsupported}"
	else
		fail=1
	fi
done

exit "$fail"
