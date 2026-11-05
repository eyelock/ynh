#!/usr/bin/env bash
# Live proof that a workspace folder grant is what lets Claude Code and Codex
# reach a folder, and that --no-workspace is what refuses it. Not part of
# `make check`: it needs the vendor CLIs, a login and a model call.
#
# Usage: scripts/workspace-effect-live.sh [claude] [codex]   (default: both)
# Env:   YNH  path to the ynh binary (default: bin/ynh)
#
# The check would fail if the grant did nothing. The folder lives under $HOME,
# outside the project and outside every path the vendors allow by default
# (Codex's workspace-write sandbox writes /tmp and $TMPDIR freely, which made
# an earlier check under /tmp pass without the grant). Each vendor is put in
# the mode where an unlisted folder is refused:
#   claude  --permission-mode acceptEdits   (edits only inside granted dirs)
#   codex   -s workspace-write              (writes only inside writable roots)
# Per vendor it passes when the write succeeds with the grant AND is refused
# with --no-workspace. A pass needs both; either alone proves nothing.
set -u
YNH=${YNH:-bin/ynh}; YNH=$(cd "$(dirname "$YNH")" && pwd)/$(basename "$YNH")
vendors=("$@"); [ ${#vendors[@]} -eq 0 ] && vendors=(claude codex)

base=$(mktemp -d "$HOME/ynh-workspace-effect.XXXXXX")
trap 'rm -rf "$base"' EXIT
mkdir -p "$base/seat/.agents/harness" "$base/shared" "$base/home"
cat > "$base/seat/.agents/harness/plugin.json" <<J
{"name":"wsprobe","version":"0.1.0","description":"workspace effect probe","workspace":["../shared"]}
J
export YNH_HOME="$base/home" YNH_WORKSPACE_ROOTS="$base"
target="$base/shared/probe.txt"
prompt="Create the file $target containing the word granted, using a shell command or your file write tool. Then reply DONE or DENIED."


fail=0
for v in "${vendors[@]}"; do
  case $v in
    claude) mode=(-- --permission-mode acceptEdits) ;;
    codex)  mode=(-- -s workspace-write) ;;
    *) echo "unknown vendor $v" >&2; exit 2 ;;
  esac
  # The flag sits before the prompt, so the mode args go after it.
  run() { # extra ynh flag or empty
    rm -f "$target"
    local flag=(); [ -n "$1" ] && flag=("$1")
    (cd "$base/seat" && "$YNH" run "$base/seat" -v "$v" ${flag[@]+"${flag[@]}"} "$prompt" "${mode[@]}" </dev/null >/dev/null 2>&1)
    [ -f "$target" ]
  }
  if run ""; then g=ok; else g=MISSING; fi
  if run "--no-workspace"; then n=WROTE; else n=refused; fi
  if [ $g = ok ] && [ $n = refused ]; then echo "PASS $v: with the grant the write succeeded; with --no-workspace it was $n"
  else echo "FAIL $v: with the grant $g; with --no-workspace $n"; fail=1; fi
done
exit $fail
