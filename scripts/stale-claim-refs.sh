#!/usr/bin/env bash
#
# Print every tracked reference to a ynh issue number.
#
# Closing a defect leaves the prose that documented it wrong, and nothing
# notices. Six times in one cycle a fix landed and the documentation *of that
# defect* became false, each caught by a person happening to read a merge and
# none by a gate (#316).
#
# This is the scan behind .github/workflows/stale-claims.yml, kept as a script
# rather than inline YAML for one reason: inline workflow shell cannot be run
# until it runs in anger. Writing it here meant discovering that the obvious
# pattern behaves differently across grep implementations, and that a second
# stale #199 claim had survived the hand sweep in #318.
#
#   scripts/stale-claim-refs.sh 199
#
# Exit 0 whether or not anything matches: absence of references is a normal
# answer, not a failure. The caller decides what to do with the output.

set -euo pipefail
cd "$(dirname "$0")/.."

num="${1:-}"
case "$num" in
	'' | *[!0-9]*) echo "usage: $0 <issue-number>" >&2; exit 2 ;;
esac

# Every tracked place a claim about ynh's behaviour has landed. Instances of
# the #316 pattern appeared in all of these.
paths=(skills agents rules .claude docs scripts AGENTS.md README.md)
present=()
for p in "${paths[@]}"; do
	[ -e "$p" ] && present+=("$p")
done
[ ${#present[@]} -gt 0 ] || exit 0

# Two patterns rather than one, and neither relies on `$` inside a group:
# that construct is honoured by GNU and BSD grep but not by every ERE engine,
# so a reference at end of line matched or vanished depending on which grep
# ran. The second pattern covers exactly that case.
#
# The leading class excludes other repositories' issues: the `#618` inside
# `github/copilot-cli#618` is not ours, while a bare `#618` or `eyelock/ynh#618`
# is. `ynh` is optional so both `#199` and `ynh#199` match, and the trailing
# check stops `#30` matching `#301`.
lead='(^|[^/[:alnum:]._-])(ynh)?'
git ls-files -z -- "${present[@]}" \
	| xargs -0 grep -nE \
		-e "${lead}#${num}([^0-9]|$)" \
		-e "${lead}#${num}$" \
		-- 2>/dev/null || true
