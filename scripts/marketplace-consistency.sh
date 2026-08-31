#!/usr/bin/env bash
#
# The marketplace indexes are committed, not generated, so that this repository
# can be installed as a plugin marketplace without ynh present — the skills
# teach ynh, and needing ynh to get them is a chicken-and-egg problem.
#
# Committed means hand-maintained, and hand-maintained means drift. This asserts
# the four indexes still agree with the two plugin manifests they describe, so
# the drift is caught here rather than by someone whose install half-works.
#
# Deliberately not `ynd marketplace build` output: that produces a standalone
# marketplace with plugins copied into ./plugins/, which would duplicate every
# skill in the repository and require CI to commit generated files.

set -euo pipefail
cd "$(dirname "$0")/.."

fail=0
note() { printf "  %-9s %s\n" "$1" "$2"; }

# plugin dir -> expected name.  The repo root is the ynh-guide plugin; .claude
# is the ynh-dev plugin. Both are real directories, not copies.
check_plugin() {
	local dir="$1" want="$2" vendor="$3"
	local manifest="$dir/.${vendor}-plugin/plugin.json"

	if [ ! -f "$manifest" ]; then
		note "MISSING" "$manifest"
		fail=1
		return
	fi
	local got
	got=$(python3 -c "import json,sys; print(json.load(open('$manifest')).get('name',''))")
	if [ "$got" != "$want" ]; then
		note "FAIL" "$manifest declares name=$got, marketplace lists $want"
		fail=1
	else
		note "ok" "$manifest ($got)"
	fi
}

# index -> the plugin names it lists, one per line
index_names() {
	python3 -c "
import json,sys
d=json.load(open('$1'))
for p in d.get('plugins',[]): print(p.get('name',''))"
}

echo "==> marketplace indexes"
for idx in .claude-plugin/marketplace.json \
           .cursor-plugin/marketplace.json \
           .agents/plugins/marketplace.json \
           .github/plugin/marketplace.json; do
	if [ ! -f "$idx" ]; then
		note "MISSING" "$idx"
		fail=1
		continue
	fi
	names=$(index_names "$idx" | sort | tr '\n' ' ')
	if [ "$names" != "ynh-dev ynh-guide " ]; then
		note "FAIL" "$idx lists [$names], want [ynh-dev ynh-guide ]"
		fail=1
	else
		note "ok" "$idx"
	fi
done

echo "==> plugin manifests match what the indexes claim"
check_plugin "."       "ynh-guide" claude
check_plugin "."       "ynh-guide" cursor
check_plugin ".claude" "ynh-dev"   claude
check_plugin ".claude" "ynh-dev"   cursor

# Cursor's index is a different shape from Claude's — metadata.description
# rather than a top-level description, and no per-plugin version. ynh's own
# generator does not yet honour that (ynh#301), which is why these are written
# by hand; assert the distinction survives someone "tidying" them together.
echo "==> Cursor keeps its own documented shape"
if python3 -c "
import json,sys
d=json.load(open('.cursor-plugin/marketplace.json'))
sys.exit(0 if 'metadata' in d and 'description' not in d else 1)"; then
	note "ok" "metadata.description, not top-level (references/cursor.md:127)"
else
	note "FAIL" ".cursor-plugin/marketplace.json should nest description under metadata"
	fail=1
fi

echo "==> Codex keeps its own documented shape"
if python3 -c "
import json,sys
d=json.load(open('.agents/plugins/marketplace.json'))
p=d.get('plugins',[{}])[0]
sys.exit(0 if 'interface' in d and isinstance(p.get('source'),dict) else 1)"; then
	note "ok" "interface.displayName + source{source,path} (references/codex.md)"
else
	note "FAIL" ".agents/plugins/marketplace.json should use Codex's source/policy shape"
	fail=1
fi

if [ "$fail" -ne 0 ]; then
	echo
	echo "Marketplace indexes are out of step with the plugin manifests."
	echo "They are committed on purpose — see the header of this script."
	exit 1
fi

echo
echo "Marketplace OK — 2 plugins, 4 vendor indexes, 3 distinct formats."
