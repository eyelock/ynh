#!/usr/bin/env bash
# Compare the stand-in harnesses scripts/eval-remotes.sh builds with the real
# harnesses on GitHub.
#
#   scripts/eval-stand-ins.sh          (make check-eval-stand-ins)
#
# An eval runs the tutorials against local stand-ins of the GitHub repositories
# they fetch, so that every block runs offline. The evals compare tutorial
# output only with those stand-ins, so when a real harness changes and its
# stand-in does not, the tutorials go on documenting the stand-in and no eval
# notices. That happened to ynh/tester: the stand-in had one include, the real
# one two, and a tutorial block printing the stand-in's count passed review.
#
# For every harness manifest the stand-ins hold, this clones the real
# repository's default branch (what `ynh install` fetches) and compares the
# whole manifest, every field but version and $schema, so that a field the
# stand-in lacks fails the check rather than passing unseen. Each git address,
# wherever it appears (an include, a delegate), is compared without its scheme
# or .git, since ynh treats those forms as one repository; a local include is
# compared as it is. It also compares which of the harness's own files are
# present beside the manifest (instructions.md, AGENTS.md, CLAUDE.md, rules/,
# skills/, agents/, commands/): a delegate's agent file has an Instructions
# section, and an export an AGENTS.md, only when the harness has instructions.
# The files' contents are not compared. It fails when it finds no stand-in
# harness to compare, since that means it checked nothing.
#
# It needs the network, so it is not part of `make check`. When it fails, update
# the stand-in in scripts/eval-remotes.sh to match the real harness, and any
# tutorial output that changes with it. Never change the real harness to suit
# the evals.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
command -v jq > /dev/null || { echo "eval-stand-ins: jq is required" >&2; exit 2; }

SB=$(mktemp -d /tmp/ynh-eval-stand-ins.XXXXXX)
trap 'case $SB in /tmp/ynh-eval-stand-ins.?*) rm -rf "$SB" ;; esac' EXIT
mkdir -p "$SB/home" "$SB/real"

if ! "$ROOT/scripts/eval-remotes.sh" "$SB" > "$SB/remotes.log" 2>&1; then
	sed 's/^/  /' "$SB/remotes.log" >&2
	echo "eval-stand-ins: scripts/eval-remotes.sh did not build the stand-ins" >&2
	exit 2
fi
SRC=$SB/remotes/src

# own_files <harness dir>: which of the harness's own files are present, as a
# JSON array.
OWN_FILES="instructions.md AGENTS.md CLAUDE.md rules/ skills/ agents/ commands/"
own_files() {
	local f present=()
	for f in $OWN_FILES; do
		case $f in
		*/) [ -d "$1/${f%/}" ] && present+=("$f") ;;
		*) [ -f "$1/$f" ] && present+=("$f") ;;
		esac
	done
	jq -cn '$ARGS.positional' --args ${present[@]+"${present[@]}"}
}

# normalise <manifest> <harness dir>: the whole manifest less version and
# $schema, with every git address normalised so that equivalent forms do not
# count as differences, and the harness's own files beside it.
normalise() {
	jq -S --argjson own "$(own_files "$2")" '
		def addr: sub("^(https?://|ssh://git@|git@)"; "") | sub("^github\\.com:"; "github.com/") | sub("\\.git$"; "") | sub("/$"; "");
		{
			manifest: (del(.version, ."$schema")
				| walk(if type == "object" and (.git | type) == "string" then .git |= addr else . end)),
			own_files: $own
		}' "$1"
}

fail=0 checked=0
for repo_dir in "$SRC"/*/*; do
	[ -d "$repo_dir" ] || continue
	repo=${repo_dir#"$SRC"/}
	manifests=$(cd "$repo_dir" && find . -path '*/.agents/harness/plugin.json' | sed 's|^\./||' | sort)
	[ -n "$manifests" ] || continue

	echo "== github.com/$repo =="
	real=$SB/real/$repo
	if ! git clone --quiet --depth 1 "https://github.com/$repo" "$real" 2> "$SB/clone.log"; then
		sed 's/^/  /' "$SB/clone.log"
		echo "  FAIL     could not clone https://github.com/$repo"
		fail=1
		continue
	fi
	sha=$(git -C "$real" rev-parse --short HEAD)

	for m in $manifests; do
		dir=${m%/.agents/harness/plugin.json}
		checked=$((checked + 1))
		if [ -f "$real/$m" ]; then
			real_m=$real/$m
		elif [ -f "$real/$dir/.ynh-plugin/plugin.json" ]; then
			real_m=$real/$dir/.ynh-plugin/plugin.json
		else
			echo "  MISSING  $dir: the stand-in has a harness here, github.com/$repo@$sha does not"
			fail=1
			continue
		fi
		if ! normalise "$repo_dir/$m" "$repo_dir/$dir" > "$SB/stand-in.json" 2> "$SB/jq.log"; then
			echo "  FAIL     $dir: could not read the stand-in's manifest: $(cat "$SB/jq.log")"
			fail=1
			continue
		fi
		if ! normalise "$real_m" "$real/$dir" > "$SB/real.json" 2> "$SB/jq.log"; then
			echo "  FAIL     $dir: could not read the real manifest: $(cat "$SB/jq.log")"
			fail=1
			continue
		fi
		if diff -u --label "stand-in $dir (scripts/eval-remotes.sh)" --label "real $dir (github.com/$repo@$sha)" \
			"$SB/stand-in.json" "$SB/real.json" > "$SB/diff.txt"; then
			echo "  OK       $dir"
		else
			echo "  DIFF     $dir"
			sed 's/^/    /' "$SB/diff.txt"
			fail=1
		fi
	done
done

echo
if [ "$checked" -eq 0 ]; then
	echo "FAIL: found no stand-in harness (.agents/harness/plugin.json) under the stand-ins"
	echo "scripts/eval-remotes.sh built, so nothing was compared. If the stand-ins moved,"
	echo "update scripts/eval-stand-ins.sh to find them."
	exit 1
fi
if [ "$fail" -ne 0 ]; then
	echo "FAIL: a stand-in in scripts/eval-remotes.sh differs from the real harness."
	echo "Update the stand-in (and any tutorial output that changes with it) to match."
	echo "Never change the real harness to suit the evals."
	exit 1
fi
echo "OK: $checked stand-in harnesses match the real ones."
