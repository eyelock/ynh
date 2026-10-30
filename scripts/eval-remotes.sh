#!/usr/bin/env bash
# Build the GitHub repositories the tutorials fetch from as local bare
# repositories inside an eval sandbox, and tell git to serve them in place of
# GitHub.
#
#   scripts/eval-remotes.sh /tmp/ynh-eval-<slug>
#
# A tutorial installs harnesses, includes skills and delegates to harnesses
# from github.com/eyelock/assistants and a few other repositories. Run as
# written it would need the network, so an eval used to skip every block that
# touched them (#534). Instead of rewriting the commands, this script gives the
# sandbox's git a global `url.<local repo>.insteadOf` entry for each repository
# (the form ynh clones with, `git@github.com:<org>/<repo>`, and the https one).
# The tutorial's text, the ids ynh derives from it (github.com/eyelock/assistants/david)
# and the allow-list checks all stay exactly as a reader sees them; only the
# transport changes. The sandbox runs with GIT_ALLOW_PROTOCOL=file, so a
# repository missing from this list fails instead of reaching GitHub.
#
# Everything is written under the sandbox. The fixtures hold the minimum each
# tutorial needs, under the same layout as the real repositories, and
# `make check-vendor-parity` fails if a tutorial names a github.com repository
# this script does not serve.
set -euo pipefail

SANDBOX=${1:?usage: eval-remotes.sh /tmp/ynh-eval-<slug>}
case $SANDBOX in
/tmp/ynh-eval-?*) ;;
*) echo "eval-remotes: refusing sandbox $SANDBOX: not under /tmp/ynh-eval-" >&2; exit 1 ;;
esac
case /$SANDBOX/ in */../*) echo "eval-remotes: refusing $SANDBOX: contains .." >&2; exit 1 ;; esac
[ -d "$SANDBOX/home" ] || { echo "eval-remotes: $SANDBOX/home does not exist: set the sandbox up first" >&2; exit 1; }

REMOTES=$SANDBOX/remotes
rm -rf "$REMOTES"
mkdir -p "$REMOTES/src" "$REMOTES/repos"

# A fixed identity and no signing, whatever the developer's own git config says.
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME="ynh eval" GIT_COMMITTER_NAME="ynh eval"
export GIT_AUTHOR_EMAIL=eval@example.invalid GIT_COMMITTER_EMAIL=eval@example.invalid
export GIT_CEILING_DIRECTORIES=/private/tmp:/tmp

# file <repo> <path> <body>: write a file into a repository's working tree.
file() {
	mkdir -p "$(dirname "$REMOTES/src/$1/$2")"
	printf '%s\n' "$3" > "$REMOTES/src/$1/$2"
}

# skill <repo> <dir> <name>: write <dir>/<name>/SKILL.md.
skill() {
	file "$1" "$2/$3/SKILL.md" "---
name: $3
description: Fixture skill $3 for the ynh tutorials.
---

This is the $3 skill."
}

# harness <repo> <dir> <name> <description> [includes-json]: write a harness manifest.
harness() {
	local inc=""
	[ -n "${5:-}" ] && inc=",
  \"includes\": [$5
  ]"
	file "$1" "$2/.agents/harness/plugin.json" "{
  \"\$schema\": \"https://eyelock.github.io/ynh/schema/plugin.schema.json\",
  \"name\": \"$3\",
  \"version\": \"0.1.0\",
  \"description\": \"$4\",
  \"default_vendor\": \"claude\"$inc
}"
	file "$1" "$2/instructions.md" "You are the $3 harness. $4."
}

# publish <repo> <tag>: commit the working tree, tag it, and serve it as a bare repository.
publish() {
	local src=$REMOTES/src/$1 bare=$REMOTES/repos/github.com/$1.git
	git -C "$src" init --quiet --initial-branch=main
	git -C "$src" add -A
	git -C "$src" -c commit.gpgsign=false commit --quiet -m "fixtures for $1"
	[ -z "${2:-}" ] || git -C "$src" -c tag.gpgsign=false tag "$2"
	mkdir -p "$(dirname "$bare")"
	git clone --quiet --bare "$src" "$bare"
	# ynh fetches a pinned commit by SHA, which a server refuses unless this is set.
	git -C "$bare" config uploadpack.allowReachableSHA1InWant true
	# <prefix> is the clone URL without .git, so the .git ynh appends carries over.
	{
		printf '[url "file://%s"]\n' "${bare%.git}"
		printf '\tinsteadOf = git@github.com:%s\n' "${1%.git}"
		printf '\tinsteadOf = https://github.com/%s\n' "${1%.git}"
	} >> "$REMOTES/gitconfig"
}

: > "$REMOTES/gitconfig"

# --- github.com/eyelock/assistants -------------------------------------------
A=eyelock/assistants
for s in dev-project dev-quality dev-review dev-backend dev-ui dev-test dev-docs; do skill $A skills/dev/skills "$s"; done
for s in go-lang python-lang typescript-lang; do skill $A skills/tech/skills "$s"; done
for s in docker terraform; do skill $A skills/infra/skills "$s"; done
for s in help-me-answer take-a-moment; do skill $A skills/pause/skills "$s"; done
skill $A plugins/media-management/skills media-import
file $A plugins/media-management/.claude-plugin/plugin.json '{
  "name": "media-management",
  "version": "0.1.0",
  "description": "Music library processing and Apple Music import"
}'

inc() { # inc <path> <pick,...>
	local picks
	picks=$(printf '"skills/%s", ' ${2//,/ })
	printf '\n    {"git": "github.com/eyelock/assistants", "path": "%s", "pick": [%s]}' "$1" "${picks%, }"
}
harness $A ynh/david david "Full-stack development harness with Go expertise" \
	"$(inc skills/dev dev-project,dev-quality),$(inc skills/tech go-lang),$(inc skills/infra docker),$(inc skills/pause help-me-answer)"
harness $A ynh/planner planner "Project planning and architecture harness" "$(inc skills/pause take-a-moment)"
harness $A ynh/tester tester "Test planning and execution harness" "$(inc skills/dev dev-test)"
harness $A ynh/researcher researcher "Researches a code base and reports what it finds" ""
publish $A v1.0.0

# --- github.com/anthropics/skills ---------------------------------------------
for s in frontend-design pdf docx; do skill anthropics/skills skills "$s"; done
publish anthropics/skills

# --- github.com/vercel-labs/skills --------------------------------------------
for s in find-skills vercel-deploy; do skill vercel-labs/skills skills "$s"; done
publish vercel-labs/skills

# --- github.com/agentplugins/agent-plugins-example ----------------------------
# The specification project's reference package: a root plugin.json, one skill.
P=agentplugins/agent-plugins-example
file $P plugin.json '{
  "$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json",
  "name": "agent-plugins-example",
  "version": "1.0.0",
  "description": "Reference Agent Plugin: migrates a plugin to the Agent Plugins format"
}'
skill $P skills migrate-agent-plugin
file $P README.md "# agent-plugins-example

A reference Agent Plugin."
file $P LICENSE "Fixture licence text."
publish $P

# Served from the sandbox's own home, so only this sandbox's git sees it.
cp "$REMOTES/gitconfig" "$SANDBOX/home/.gitconfig"
echo "eval-remotes: serving $(grep -c '^\[url' "$REMOTES/gitconfig") repositories from $REMOTES"
