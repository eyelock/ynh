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
#
# "No file" is never taken as "refused": a ynh error, an expired login, a rate
# limit or a model that never tried would all leave no file. Each half checks
# ynh's exit status and the output, and the refused half needs positive
# evidence that the vendor started, attempted the write and was refused:
#   claude  runs with --output-format stream-json --verbose, and the output must
#           hold a tool call for the target and the vendor's permission_denied
#           event for that path
#   codex   the output must show the workspace-write sandbox and a reply that
#           begins DENIED and quotes the shell's error naming the target
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
target="$base/shared/probe.txt"
prompt="Create the file $target containing the word granted, using a shell command or your file write tool. If the tool reports an error, reply DENIED followed by the error; if it succeeds reply DONE."

fail=0
for v in "${vendors[@]}"; do
  case $v in
    claude) mode=(-- --permission-mode acceptEdits --output-format stream-json --verbose) ;;
    codex)  mode=(-- -s workspace-write) ;;
    *) echo "unknown vendor $v" >&2; exit 2 ;;
  esac
  out="$base/$v.out"
  # run FLAG: one ynh run, output in $out, ynh's exit status in $rc. The flag
  # sits before the prompt, so the vendor's mode args go after it.
  run() {
    rm -f "$target"
    local flag=(); [ -n "$1" ] && flag=("$1")
    (cd "$base/seat" && "$YNH" run "$base/seat" -v "$v" ${flag[@]+"${flag[@]}"} "$prompt" "${mode[@]}" </dev/null >"$out" 2>&1)
    rc=$?
  }

  # With the grant: ynh succeeded, the vendor started, and the file holds the text.
  run ""
  if [ $rc -eq 0 ] && grep -q "^Launching " "$out" && grep -q "workspace: granted" "$out" \
     && [ "$(cat "$target" 2>/dev/null)" = granted ]; then g=ok; else g="FAILED (ynh exit $rc, file $([ -f "$target" ] && echo present || echo absent))"; fi

  # With --no-workspace: ynh succeeded and applied the flag, the vendor started,
  # attempted the write and reported the refusal, and no file exists.
  run "--no-workspace"
  n=""
  if [ $rc -ne 0 ]; then n="ynh exited $rc, so the vendor may never have run"
  elif ! grep -q "workspace: --no-workspace" "$out"; then n="ynh did not report --no-workspace"
  elif ! grep -q "^Launching " "$out"; then n="the vendor was not launched"
  elif [ -f "$target" ]; then n="WROTE the file"
  else
    case $v in
      claude) grep -q '"name":"\(Write\|Edit\|Bash\)"' "$out" && grep -q '"subtype":"permission_denied"' "$out" \
                && grep -q "permission_denied.*probe.txt\|probe.txt.*permission_denied" "$out" \
                || n="no attempted write refused by the vendor in the output" ;;
      codex)  grep -q "sandbox: workspace-write" "$out" && grep -q "^DENIED.*probe.txt" "$out" \
                || n="no DENIED reply quoting the sandbox's error for the target" ;;
    esac
  fi
  if [ -z "$n" ]; then n=refused; fi
  if [ "$g" = ok ] && [ "$n" = refused ]; then echo "PASS $v: with the grant the write succeeded; with --no-workspace the vendor attempted it and was refused"
  else echo "FAIL $v: with the grant $g; with --no-workspace: $n"; sed "s/^/    | /" <(tail -5 "$out"); fail=1; fi
done
exit $fail
