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
# fields that decide what a reader sees: name, description, default_vendor,
# and each include and delegate with its git, path, ref and picks. Version,
# $schema and file layout are not compared. The git address is compared without
# its scheme or .git, since ynh treats those forms as one repository.
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

# The fields compared, normalised so that formatting and equivalent git
# addresses do not count as differences.
normalise() {
	jq -S '
		def addr: sub("^(https?://|ssh://git@|git@)"; "") | sub("^github\\.com:"; "github.com/") | sub("\\.git$"; "") | sub("/$"; "");
		def ref: {git: (.git | addr), path, ref, pick} | with_entries(select(.value != null));
		{
			name, description, default_vendor,
			includes: [(.includes // [])[] | ref],
			delegates_to: [(.delegates_to // [])[] | ref]
		}' "$1"
}

fail=0 checked=0
for repo_dir in "$SRC"/*/*; do
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
		if ! normalise "$repo_dir/$m" > "$SB/stand-in.json" 2> "$SB/jq.log"; then
			echo "  FAIL     $dir: the stand-in's manifest is not valid JSON: $(cat "$SB/jq.log")"
			fail=1
			continue
		fi
		if ! normalise "$real_m" > "$SB/real.json" 2> "$SB/jq.log"; then
			echo "  FAIL     $dir: the real manifest is not valid JSON: $(cat "$SB/jq.log")"
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
if [ "$fail" -ne 0 ]; then
	echo "FAIL: a stand-in in scripts/eval-remotes.sh differs from the real harness."
	echo "Update the stand-in (and any tutorial output that changes with it) to match."
	echo "Never change the real harness to suit the evals."
	exit 1
fi
echo "OK: $checked stand-in harnesses match the real ones."
