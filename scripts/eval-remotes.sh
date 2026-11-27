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

# harness <repo> <dir> <name> <description> [includes-json] [instructions]: write
# a harness manifest, and <dir>/instructions.md only when [instructions] is
# given. A harness's own files change what a reader sees (a delegate's agent
# file has an Instructions section only when the delegate has instructions; an
# export writes AGENTS.md only then), so a stand-in has only the files its real
# harness has, and `make check-eval-stand-ins` compares which are present.
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
	[ -z "${6:-}" ] || file "$1" "$2/instructions.md" "$6"
}

# publish <repo> [tag...]: commit the working tree, tag it with each tag given, and serve it as a bare repository.
publish() {
	local src=$REMOTES/src/$1 bare=$REMOTES/repos/github.com/$1.git
	git -C "$src" init --quiet --initial-branch=main
	git -C "$src" add -A
	git -C "$src" -c commit.gpgsign=false commit --quiet -m "fixtures for $1"
	local t
	for t in "${@:2}"; do git -C "$src" -c tag.gpgsign=false tag "$t"; done
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
for s in dev-project dev-quality dev-review dev-security dev-backend dev-ui dev-debug dev-test dev-docs; do skill $A skills/dev/skills "$s"; done
for s in go-lang python-lang typescript-lang; do skill $A skills/tech/skills "$s"; done
for s in docker terraform gh-os-repo terraform-backend-aws; do skill $A skills/infra/skills "$s"; done
for s in help-me-answer take-a-moment; do skill $A skills/pause/skills "$s"; done
skill $A plugins/media-management/skills media-import
file $A plugins/media-management/.claude-plugin/plugin.json '{
  "name": "media-management",
  "version": "0.1.0",
  "description": "Music library processing and Apple Music import"
}'

# The real includes each pin a release tag of their package (assistants #321), so
# a stand-in carries the same ref, and publish tags the stand-in with each one so
# ynh can fetch it. Moving a pin upstream means moving it here too.
PIN_DEV=@eyelock-assistants/dev-skills@0.2.0
PIN_TECH=@eyelock-assistants/tech-skills@0.2.1
PIN_INFRA=@eyelock-assistants/infra-skills@0.2.0
PIN_PAUSE=@eyelock-assistants/pause-skills@0.1.1
inc() { # inc <path> <ref> <pick,...>
	local picks
	picks=$(printf '"skills/%s", ' ${3//,/ })
	printf '\n    {"git": "github.com/eyelock/assistants", "path": "%s", "ref": "%s", "pick": [%s]}' "$1" "$2" "${picks%, }"
}
# The harnesses match the real ones on eyelock/assistants' default branch in
# name, description, default_vendor, includes (each with its pinned ref) and delegates, and in which of
# their own files they have (none of the four has instructions, rules, skills,
# agents or commands of its own):
# `make check-eval-stand-ins` compares them, and a difference is fixed here.
harness $A ynh/david david "David's personal development persona with full skill set" \
	"$(inc skills/dev $PIN_DEV dev-project,dev-quality,dev-review,dev-security,dev-backend,dev-ui,dev-debug),$(inc skills/tech $PIN_TECH go-lang),$(inc skills/infra $PIN_INFRA gh-os-repo,terraform-backend-aws),$(inc skills/pause $PIN_PAUSE help-me-answer,take-a-moment)"
harness $A ynh/planner planner "Architecture and implementation planning persona" \
	"$(inc skills/dev $PIN_DEV dev-project,dev-review),$(inc skills/infra $PIN_INFRA gh-os-repo)"
harness $A ynh/tester tester "Testing and quality assurance persona" \
	"$(inc skills/dev $PIN_DEV dev-quality),$(inc skills/tech $PIN_TECH go-lang)"
harness $A ynh/researcher researcher "Research and analysis persona for investigating codebases, APIs, and technical topics" "$(inc skills/dev $PIN_DEV dev-review)"
publish $A v1.0.0 "$PIN_DEV" "$PIN_TECH" "$PIN_INFRA" "$PIN_PAUSE"

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

# --- github.com/eyelock/ynh ---------------------------------------------------
# ynh's own tutorial harnesses (docs/tutorial/harnesses), copied from this
# checkout rather than written out here, so the fixture is the real file.
Y=eyelock/ynh
YNH_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
mkdir -p "$REMOTES/src/$Y/docs/tutorial"
cp -R "$YNH_ROOT/docs/tutorial/harnesses" "$REMOTES/src/$Y/docs/tutorial/harnesses"
publish $Y

# Served from the sandbox's own home, so only this sandbox's git sees it.
cp "$REMOTES/gitconfig" "$SANDBOX/home/.gitconfig"
echo "eval-remotes: serving $(grep -c '^\[url' "$REMOTES/gitconfig") repositories from $REMOTES"
