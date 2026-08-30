#!/usr/bin/env bash
# Vendor parity checks for the harness this repo ships.
#
# ynh's whole promise is that one harness reaches every vendor. Nothing verified
# that. Copilot shipped as a working adapter and every skill still described
# three vendors, because no check ever compared the vendor list against anything.
#
# Two assertions:
#
#   A. Every vendor `ynh vendors` reports has a row in the vendor-adapters
#      reference index. A new adapter that nobody documented fails here.
#
#   B. Every vendor assembles the same artifact set from this repo's harness,
#      compared after normalising the vendor-specific prefixes away. An adapter
#      that silently drops skills or agents fails here.
#
# Usage: scripts/vendor-parity.sh [path-to-harness]   (default: repo root)

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HARNESS="${1:-$ROOT}"
YNH="$ROOT/bin/ynh"
YND="$ROOT/bin/ynd"
INDEX="$ROOT/.claude/skills/vendor-adapters/SKILL.md"

for bin in "$YNH" "$YND"; do
	[ -x "$bin" ] || { echo "missing $bin — run 'make build' first" >&2; exit 1; }
done
command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# `ynh vendors --format json` is the authority on which vendors exist. Reading
# it rather than hardcoding is the entire point: a hardcoded list is what let
# Copilot go undocumented.
"$YNH" vendors --format json > "$TMP/vendors.json"
# Tolerate both a bare array and an enveloped payload.
jq -r 'if type == "array" then . else (.payload // .vendors // .data) end
       | .[] | "\(.name)\t\(.config_dir)"' "$TMP/vendors.json" > "$TMP/vendors.tsv"

VENDOR_COUNT=$(wc -l < "$TMP/vendors.tsv" | tr -d ' ')
[ "$VENDOR_COUNT" -gt 0 ] || { echo "ynh vendors returned nothing" >&2; exit 1; }
echo "Vendors reported by ynh: $VENDOR_COUNT"

fail=0

# --- A. every vendor is documented -----------------------------------------
# Matched on the adapter path (internal/vendor/<name>.go) rather than the
# reference filename, because Claude's reference is anthropic.md — the file name
# is a naming choice, the adapter path is not.
echo
echo "== A. reference coverage =="
while IFS=$'\t' read -r name _; do
	if grep -q "internal/vendor/${name}\.go" "$INDEX"; then
		echo "  ok       $name"
	else
		echo "  MISSING  $name — no row in $(basename "$INDEX") referencing internal/vendor/${name}.go"
		fail=1
	fi
done < "$TMP/vendors.tsv"

# --- B. assembled artifact parity ------------------------------------------
# Normalisation strips what is *supposed* to differ between vendors:
#   <config_dir>/     -> harness/     (.claude/, .codex/, .cursor/, .copilot/)
#   .<vendor>-plugin/ -> plugin/      (manifest dir; copilot nests its own)
#   .mdc              -> .md          (Cursor renders rules natively as .mdc)
# Top-level instructions files are counted, not name-compared: CLAUDE.md,
# codex.md, .cursorrules and AGENTS.md are all correct for their vendor.
echo
echo "== B. assembled artifact parity =="
while IFS=$'\t' read -r name cfgdir; do
	out="$TMP/out-$name"
	if ! "$YND" preview "$HARNESS" -v "$name" -o "$out" >/dev/null 2>&1; then
		echo "  FAIL     $name — ynd preview failed"
		fail=1
		continue
	fi

	( cd "$out" && find . -type f | sed 's|^\./||' ) | sed \
		-e "s|^${cfgdir}/|harness/|" \
		-e 's|^harness/\.[a-z-]*-plugin/|plugin/|' \
		-e 's|^\.[a-z-]*-plugin/|plugin/|' \
		-e 's|\.mdc$|.md|' \
		| sort > "$TMP/set-$name.txt"

	# One instructions file per vendor; its name is legitimately vendor-specific.
	instr=$( ( cd "$out" && find . -maxdepth 1 -type f | wc -l ) | tr -d ' ' )
	grep -v '^harness/\|^plugin/' "$TMP/set-$name.txt" > "$TMP/top-$name.txt" || true
	if [ "$instr" -ne 1 ]; then
		echo "  FAIL     $name — expected exactly 1 top-level instructions file, found $instr"
		fail=1
	fi
	# Compare the artifact tree only; the instructions filename is expected to differ.
	grep '^harness/\|^plugin/' "$TMP/set-$name.txt" > "$TMP/artifacts-$name.txt" || true
	count=$(wc -l < "$TMP/artifacts-$name.txt" | tr -d ' ')

	# Sets that are empty, or that lost their skills, compare equal to each
	# other and pass vacuously. An earlier revision of this script did exactly
	# that when normalisation broke: every vendor collapsed to 0 entries and
	# three of four pairs still reported "ok". Two floors make that impossible.
	if [ "$count" -eq 0 ]; then
		echo "  FAIL     $name — normalised artifact set is empty; normalisation is broken, not the adapter"
		fail=1
	fi
	if ! grep -q '^harness/skills/.*/SKILL\.md$' "$TMP/artifacts-$name.txt"; then
		echo "  FAIL     $name — no harness/skills/*/SKILL.md after normalisation"
		fail=1
	fi

	echo "  $name: $count artifacts, $instr instructions file"
done < "$TMP/vendors.tsv"

REF=$(head -1 "$TMP/vendors.tsv" | cut -f1)
echo
while IFS=$'\t' read -r name _; do
	[ "$name" = "$REF" ] && continue
	if ! diff -u "$TMP/artifacts-$REF.txt" "$TMP/artifacts-$name.txt" > "$TMP/diff-$name.txt"; then
		echo "  MISMATCH $REF vs $name:"
		sed 's/^/      /' "$TMP/diff-$name.txt"
		fail=1
	else
		echo "  ok       $REF == $name"
	fi
done < "$TMP/vendors.tsv"

echo
if [ "$fail" -ne 0 ]; then
	echo "Vendor parity FAILED."
	exit 1
fi
echo "Vendor parity OK across $VENDOR_COUNT vendors."
