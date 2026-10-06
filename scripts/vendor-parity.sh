#!/usr/bin/env bash
# Vendor parity checks for the harness this repo ships.
#
# ynh's whole promise is that one harness reaches every vendor. Nothing verified
# that. Copilot shipped as a working adapter and every skill still described
# three vendors, because no check ever compared the vendor list against anything.
#
# Four assertions:
#
#   A. Every vendor `ynh vendors` reports has a row in the vendor-adapters
#      reference index. A new adapter that nobody documented fails here.
#
#   B. Every vendor assembles the same artifact set from this repo's harness,
#      compared after normalising the vendor-specific prefixes away. An adapter
#      that silently drops skills or agents fails here.
#
#   C. The eval sandbox stubs every vendor CLI. An eval once launched the real
#      Cursor CLI because the eval page named `cursor` and the binary is `agent`.
#      Every `cli` that `ynh vendors` reports, and every program the Go source
#      launches by name, must be in the STUBS line of .claude/agents/evals.md,
#      unless it is a local tool an eval may run.
#
#   D. Every eval line in docs/tutorial/ (`*This launches:* ...`, the
#      model-output line, `*Replace ...*`) is well formed, a launch or output
#      line sits directly above a bash block, a launch names a stubbed program,
#      and no HTML comment carries an eval directive. run.sh checks the calls.
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

# --- C. the eval sandbox stubs every vendor CLI -----------------------------
echo
echo "== C. eval stubs =="
EVALS="$ROOT/.claude/agents/evals.md"
# Programs an eval may run for real: none of them is a vendor CLI or needs a
# network or Docker.
EVAL_LOCAL_TOOLS="git sh bash /bin/sh"
stub_lines=$(grep -c '^STUBS="' "$EVALS" || true)
if [ "$stub_lines" -ne 1 ]; then
	echo "  FAIL     $(basename "$EVALS") must have exactly one STUBS=\"...\" line, found $stub_lines"
	fail=1
else
	stubs=" $(sed -n 's/^STUBS="\([^"]*\)".*/\1/p' "$EVALS") "
	jq -r 'if type == "array" then . else (.payload // .vendors // .data) end | .[].cli' \
		"$TMP/vendors.json" | sort -u > "$TMP/clis.txt"
	# Every program the Go source launches or looks up by a literal name.
	grep -rhoE --include='*.go' --exclude='*_test.go' \
		'exec\.(LookPath|Command|CommandContext)\((ctx, )?"[^"]+"' "$ROOT/cmd" "$ROOT/internal" \
		| sed -E 's/.*"([^"]+)"$/\1/' | sort -u > "$TMP/launched.txt"
	while read -r bin; do
		case "$stubs" in *" $bin "*) echo "  ok       $bin (vendor CLI)" ;; *)
			echo "  MISSING  $bin: a vendor CLI, not in STUBS in $(basename "$EVALS")"
			fail=1 ;;
		esac
	done < "$TMP/clis.txt"
	while read -r bin; do
		case " $EVAL_LOCAL_TOOLS " in *" $bin "*) continue ;; esac
		grep -qx "$bin" "$TMP/clis.txt" && continue
		case "$stubs" in *" $bin "*) echo "  ok       $bin" ;; *)
			echo "  MISSING  $bin: launched by ynh or ynd, not in STUBS in $(basename "$EVALS")"
			fail=1 ;;
		esac
	done < "$TMP/launched.txt"
fi

# --- D. tutorial eval lines are well formed -------------------------------
# A tutorial says what each launch hands the vendor in a visible line above the
# block, `*This launches:* `<command>``, and run.sh checks the stub call against
# it. Model-dependent output and reader-supplied values have their own fixed
# lines. A line that drifted off its block, is misspelt, or names a program
# that is not stubbed would be read by nobody, so all three are checked here.
# Nothing the eval reads may be hidden: an HTML comment carrying a directive fails.
echo
echo "== D. tutorial eval lines =="
markers=0
for md in "$ROOT"/docs/tutorial/*.md; do
	out=$(awk -v stubs="${stubs:-}" -v file="$(basename "$md")" '
		function bad(msg) { printf "  BAD      %s:%d: %s\n", file, NR, msg; nbad++ }
		/^```/ { infence = !infence }
		infence && !/^```bash$/ { next }
		/^\*Replace `/ {
			if ($0 !~ /^\*Replace `[^`]+` with .* \(here: `.*`\)\.\*$/) bad("expected *Replace `<from>` with ... (here: `<to>`).*")
			n++; next
		}
		/^\*Your output will differ/ {
			if ($0 != "*Your output will differ: it shows what the model did.*") bad("expected *Your output will differ: it shows what the model did.*")
			n++; pending = NR; next
		}
		/^\*This launches/ {
			if ($0 !~ /^\*This launches:\* `[^`]+`$/) { bad("expected *This launches:* `<command>`"); n++; next }
			m = $0; sub(/^\*This launches:\* `/, "", m); sub(/`$/, "", m); split(m, w, " ")
			if (index(stubs, " " w[1] " ") == 0) bad("launches \"" w[1] "\", which is not in STUBS")
			n++; pending = NR; next
		}
		pending && /^```bash$/ { pending = 0; next }
		pending && /^[[:space:]]*$/ { next }
		pending { bad("line " pending " is not followed by a ```bash block"); pending = 0 }
		END { if (pending) bad("line " pending " is not followed by a ```bash block"); printf "COUNT %d %d\n", n, nbad }
	' "$md")
	printf '%s\n' "$out" | grep -v '^COUNT ' || true
	set -- $(printf '%s\n' "$out" | sed -n 's/^COUNT //p')
	markers=$((markers + $1))
	[ "$2" -eq 0 ] || fail=1
done
hidden=$(grep -rn '<!-- *eval' "$ROOT/docs" "$ROOT/.claude" 2>/dev/null || true)
if [ -n "$hidden" ]; then
	printf '%s\n' "$hidden" | sed "s|^$ROOT/|  HIDDEN   |"
	fail=1
fi
echo "  $markers eval lines checked"

echo
if [ "$fail" -ne 0 ]; then
	echo "Vendor parity FAILED."
	exit 1
fi
echo "Vendor parity OK across $VENDOR_COUNT vendors."
