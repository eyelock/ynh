#!/usr/bin/env bash
# Parity page check: docs/vendor-parity.md must agree with ynh (#718).
#
# The page is the one copy of the vendor feature matrices. It is written by
# hand, so this checks it; it does not generate it. It fails unless:
#
#   1. every vendor `ynh vendors` reports has a column, in the order it lists
#      them, and there is no column for a vendor it does not report. A header
#      names a vendor when it is the vendor's display_name, or its display_name
#      without a leading word ("Codex" for "OpenAI Codex");
#   2. every cell is a legend mark (Y, P, N, X, U or –), and every cell that is
#      not Y or – names a limit, [L<n>](#...), whose `### L<n>:` heading exists;
#   3. every limit has an Owner and an Evidence line, and every limit an N cell
#      names has a Tracking line that links a ynh issue;
#   4. the Hooks, Session context and Workspace folders cells agree with what
#      each vendor adapter declares: a vendor that declares a gap (an
#      unsupported hook event, an unsupported session-start context, a reason
#      it has no workspace flag) is not Y there, and a vendor that declares
#      support is not X there;
#   5. no other .md file holds a `| Feature | Claude Code |` table.
#
# scripts/stale-claim-refs.sh then catches a limit left behind after its
# issue closes.
#
# Usage: scripts/parity-page.sh <repo-root> <vendors.json>
#   <vendors.json> is the output of `ynh vendors --format json`.
#
# Reads, under <repo-root>: docs/vendor-parity.md, every other *.md, and
# internal/vendor/<vendor>.go for the declarations scripts/hook-event-parity.sh
# and scripts/workspace-parity.sh check.

set -euo pipefail

ROOT="${1:?usage: parity-page.sh <repo-root> <vendors.json>}"
VENDORS_JSON="${2:?usage: parity-page.sh <repo-root> <vendors.json>}"
PAGE="$ROOT/docs/vendor-parity.md"
command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }
[ -f "$PAGE" ] || { echo "  MISSING  docs/vendor-parity.md"; exit 1; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

jq -r 'if type == "array" then . else (.payload // .vendors // .data) end
       | .[] | "\(.name)\t\(.display_name)"' "$VENDORS_JSON" > "$TMP/vendors.tsv"
[ -s "$TMP/vendors.tsv" ] || { echo "  FAIL     no vendors in $VENDORS_JSON"; exit 1; }

# keys <file> <var-regex>: the quoted keys of one map literal, one per line
# (the same reading as scripts/hook-event-parity.sh).
keys() {
	awk -v re="$2" '
		$0 ~ re && /\{$/ { f = 1; next }
		f && /^\}/ { f = 0 }
		f { print }
	' "$1" | sed -nE 's/^[[:space:]]*"([a-z_]+)":.*/\1/p' | sort -u
}
decl() { sed -nE "s/^(const |var )?[a-z]+$2 = \"([^\"]*)\".*/\\2/p" "$1"; }

# What each adapter declares, one line per vendor:
#   name  display  hooks  session  workspace
# where each of the last three is gap, ok or none (nothing declared; the
# scripts behind assertions F and G report that, so it is not judged here).
while IFS=$'\t' read -r name display; do
	src="$ROOT/internal/vendor/$name.go"
	hooks=none session=none ws=none
	if [ -f "$src" ]; then
		if [ -n "$(keys "$src" 'UnsupportedHookEvents = map')" ]; then hooks=gap
		elif [ -n "$(keys "$src" 'HookEventMap = map')" ]; then hooks=ok; fi
		s=$(decl "$src" SessionStartContext)
		case "$s" in unsupported:*) session=gap ;; '') ;; *) session=ok ;; esac
		if [ -n "$(decl "$src" WorkspaceUnsupported)" ]; then ws=gap
		elif [ -n "$(decl "$src" WorkspaceFlag)$(decl "$src" WorkspaceDefault)" ]; then ws=ok; fi
	fi
	printf '%s\t%s\t%s\t%s\t%s\n' "$name" "$display" "$hooks" "$session" "$ws"
done < "$TMP/vendors.tsv" > "$TMP/declared.tsv"

fail=0

# Rules 1 to 4, in one pass over the page.
if ! awk -F'\t' -v page="docs/vendor-parity.md" '
	function trim(s) { gsub(/^[[:space:]]+|[[:space:]]+$/, "", s); return s }
	function bad(msg) { printf "  BAD      %s:%d: %s\n", page, FNR, msg; nbad++ }
	# A column header names vendor i when it is its display name, or the
	# display name less its leading words.
	function names(h, i,   d) { d = disp[i]; return h == d || (length(d) > length(h) && substr(d, length(d) - length(h)) == " " h) }
	FNR == NR { nv++; vname[nv] = $1; disp[nv] = $2; dhooks[nv] = $3; dsess[nv] = $4; dws[nv] = $5; next }
	FNR == 1 { FS = "|" }
	/^## / { section = substr($0, 4); intable = 0 }
	/^### L[0-9]+:/ {
		id = $0; sub(/^### /, "", id); sub(/:.*/, "", id)
		if (id in limit) bad("limit " id " is defined twice")
		limit[id] = FNR; cur = id; next
	}
	/^##/ { cur = "" }
	cur != "" && /^- \*\*Owner:\*\* *[^ ]/    { owner[cur] = 1 }
	cur != "" && /^- \*\*Evidence:\*\* *[^ ]/ { evidence[cur] = 1 }
	cur != "" && /^- \*\*Tracking:\*\*/ && /github\.com\/eyelock\/ynh\/issues\/[0-9]+/ { issue[cur] = 1 }
	/^\| Feature \|/ {
		ntables++; intable = 1; ncol = NF - 3
		for (c = 1; c <= ncol; c++) hdr[c] = trim($(c + 2))
		for (i = 1; i <= nv; i++) {
			found = 0
			for (c = 1; c <= ncol; c++) if (names(hdr[c], i)) { found = c; break }
			if (!found) bad("the " section " table has no column for " vname[i] " (" disp[i] ")")
			else if (found != i) bad("the " section " table has " vname[i] " in column " found ", but ynh vendors lists it " i "th")
		}
		for (c = 1; c <= ncol; c++) {
			known = 0
			for (i = 1; i <= nv; i++) if (names(hdr[c], i)) known = 1
			if (!known) bad("the " section " table has a column \"" hdr[c] "\" for a vendor ynh vendors does not report")
		}
		next
	}
	intable && /^\|[-| ]+\|$/ { next }
	intable && /^\|/ {
		feature = trim($2); nrows++
		if (NF - 3 != ncol) bad(section " / " feature ": " (NF - 3) " cells, the header has " ncol)
		for (c = 1; c <= ncol && c <= nv; c++) {
			cell = trim($(c + 2)); mark = cell; rest = ""
			if (match(cell, / /)) { mark = substr(cell, 1, RSTART - 1); rest = substr(cell, RSTART + 1) }
			if (mark !~ /^(Y|P|N|X|U|–)$/) { bad(section " / " feature " / " vname[c] ": \"" cell "\" is not a legend mark (Y, P, N, X, U or –)"); continue }
			if (rest != "" && rest !~ /^\[L[0-9]+\]\(#[^)]+\)( \[L[0-9]+\]\(#[^)]+\))*$/) {
				bad(section " / " feature " / " vname[c] ": \"" cell "\": after the mark, only limit links [L<n>](#...)")
				continue
			}
			nl = 0
			while (match(rest, /\[L[0-9]+\]/)) {
				id = substr(rest, RSTART + 1, RLENGTH - 2); rest = substr(rest, RSTART + RLENGTH)
				nl++; ref[++nref] = id; refat[nref] = FNR; refmark[nref] = mark
				refwhere[nref] = section " / " feature " / " vname[c]
			}
			if (nl == 0 && mark != "Y" && mark != "–") bad(section " / " feature " / " vname[c] ": " mark " names no limit")
			# Rule 4: the cells the adapters declare.
			d = ""
			if (feature == "Hooks") d = dhooks[c]
			else if (feature ~ /^Session context/) d = dsess[c]
			else if (feature == "Workspace folders") d = dws[c]
			if (feature == "Hooks") seenhooks = 1
			if (feature == "Workspace folders") seenws = 1
			if (d == "gap" && mark == "Y") bad(section " / " feature " / " vname[c] ": Y, but internal/vendor/" vname[c] ".go declares it unsupported")
			if (d == "ok" && mark == "X") bad(section " / " feature " / " vname[c] ": X, but internal/vendor/" vname[c] ".go declares it supported")
		}
		next
	}
	intable { intable = 0 }
	END {
		if (ntables == 0) bad("no | Feature | table found")
		if (!seenhooks) bad("no Hooks row, so the hook cells cannot be checked against the adapters")
		if (!seenws) bad("no Workspace folders row, so it cannot be checked against the adapters")
		for (r = 1; r <= nref; r++) {
			id = ref[r]
			if (!(id in limit)) { printf "  BAD      %s:%d: %s names %s, which has no ### %s: heading\n", page, refat[r], refwhere[r], id, id; nbad++; continue }
			if (refmark[r] == "N" && !(id in issue)) { printf "  BAD      %s:%d: %s is N, and %s has no Tracking line linking a ynh issue\n", page, refat[r], refwhere[r], id; nbad++; issue[id] = 1 }
		}
		for (id in limit) {
			if (!(id in owner))    { printf "  BAD      %s:%d: %s has no Owner\n", page, limit[id], id; nbad++ }
			if (!(id in evidence)) { printf "  BAD      %s:%d: %s has no Evidence\n", page, limit[id], id; nbad++ }
			nlim++
		}
		if (nbad) exit 1
		printf "  ok       %d tables, %d rows, %d limits, %d vendors\n", ntables, nrows, nlim, nv
	}
' "$TMP/declared.tsv" "$PAGE"; then
	fail=1
fi

# Rule 5: the page is the only copy of the table.
others=$(find "$ROOT" -name '*.md' -not -path '*/.git/*' -not -path '*/node_modules/*' \
	-not -path "$PAGE" -exec grep -l '^| Feature | Claude Code |' {} + 2>/dev/null || true)
if [ -n "$others" ]; then
	while read -r f; do
		echo "  COPY     ${f#"$ROOT"/} holds a | Feature | Claude Code | table; link docs/vendor-parity.md instead"
	done <<< "$others"
	fail=1
else
	echo "  ok       no other page holds a | Feature | Claude Code | table"
fi

exit "$fail"
