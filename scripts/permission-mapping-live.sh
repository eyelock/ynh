#!/usr/bin/env bash
# Live spike for ynh #1058 (P0): show, on each vendor's current CLI, what each
# permission level (none / edits / all) lets an unattended one-shot run do.
# Not part of `make check`: it needs the vendor CLIs, a login and model calls.
#
# Usage: scripts/permission-mapping-live.sh [claude] [codex] [cursor] [opencode] [copilot]
#        (default: every vendor whose CLI is on PATH)
# Env:   CELL_TIMEOUT  seconds per run (default 150); a run that hits it is NOT-RUN
#        SELF_TEST     set to 1 to run only the offline checks (verdict logic)
#
# What each run does: the vendor is started the way the design says a harness
# at that level is started (the argument list in cell_args below), in a folder
# under $HOME with no project settings, and told to (1) run a shell command that
# writes shell.probe and (2) create edit.probe with its file-edit tool. The
# level is shown when the files that exist are exactly what the level allows.
#
# "No file" is never taken as "refused" on its own: a login that expired, a rate
# limit or a model that never tried would leave no file too. A cell only counts
# when the vendor exited 0 AND its final reply line (SHELL=ok|denied
# EDIT=ok|denied) agrees with the files on disk. Anything else is NOT-RUN.
#
# Verdicts per cell:
#   SHOWN     observed behaviour is exactly the level's (the design's cell holds)
#   REFUSED   observed behaviour is NOT exact for the level, so ynh must refuse
#             the level on this vendor at start (design D4), never widen it
#   NOT-RUN   could not be shown live (reason printed); nothing is claimed
#
# Design: ynh #1058 PER-VENDOR MAPPING, D4, D5 and N4. Findings from the run of
# 2026-11-28 are at the end of this file.
set -u

CELL_TIMEOUT=${CELL_TIMEOUT:-150}

# verdict EXPECT_SHELL EXPECT_EDIT OBS_SHELL OBS_EDIT: pure; prints the verdict word.
verdict() {
  if [ "$1" = "$3" ] && [ "$2" = "$4" ]; then echo SHOWN; else echo REFUSED; fi
}

if [ "${SELF_TEST:-0}" = 1 ]; then
  fail=0
  t() { [ "$(verdict "$1" "$2" "$3" "$4")" = "$5" ] || { echo "FAIL verdict $*"; fail=1; }; }
  t denied denied denied denied SHOWN
  t denied ok ok ok REFUSED
  t denied ok denied ok SHOWN
  t ok ok ok denied REFUSED
  [ $fail = 0 ] && echo "self-test ok"
  exit $fail
fi

vendors=("$@")
if [ ${#vendors[@]} -eq 0 ]; then
  for v in claude codex cursor opencode copilot; do
    bin=$v; [ "$v" = cursor ] && bin=cursor-agent
    command -v "$bin" >/dev/null 2>&1 && vendors+=("$v")
  done
fi

base=$(mktemp -d "$HOME/ynh-permission-spike.XXXXXX")
trap 'rm -rf "$base"' EXIT

summary=()
nrun=0; nshown=0; nrefused=0; nnotrun=0

# cell_args VENDOR LEVEL: sets CMD (array) and ENV (array) for the cell, using
# $dir as the folder and $prompt as the task. Returns 1 for a cell with no
# mapping to try (nothing to run).
cell_args() {
  CMD=(); ENV=()
  case "$1:$2" in
    claude:none)   CMD=(claude -p "$prompt" --permission-mode default) ;;
    claude:edits)  CMD=(claude -p "$prompt" --permission-mode acceptEdits) ;;
    claude:all)    CMD=(claude -p "$prompt" --permission-mode bypassPermissions) ;;
    codex:none)    CMD=(codex exec --skip-git-repo-check -s read-only "$prompt") ;;
    codex:edits)   CMD=(codex exec --skip-git-repo-check -s workspace-write "$prompt") ;; # nearest; not exact
    codex:all)     CMD=(codex exec --skip-git-repo-check --dangerously-bypass-approvals-and-sandbox "$prompt") ;;
    # D5 cells: a project setting in the folder against the flag ynh would pass
    claude:d5-proj-edits-noflag)
      mkdir -p "$dir/.claude"; echo '{"permissions":{"defaultMode":"acceptEdits"}}' > "$dir/.claude/settings.json"
      CMD=(claude -p "$prompt") ;;
    claude:d5-proj-all-flag-none)
      mkdir -p "$dir/.claude"; echo '{"permissions":{"defaultMode":"bypassPermissions"}}' > "$dir/.claude/settings.json"
      CMD=(claude -p "$prompt" --permission-mode default) ;;
    claude:d5-proj-none-flag-all)
      mkdir -p "$dir/.claude"; echo '{"permissions":{"defaultMode":"default"}}' > "$dir/.claude/settings.json"
      CMD=(claude -p "$prompt" --permission-mode bypassPermissions) ;;
    codex:d5-proj-all-flag-none)
      mkdir -p "$dir/.codex"; printf 'sandbox_mode = "danger-full-access"\napproval_policy = "never"\n' > "$dir/.codex/config.toml"
      CMD=(codex exec --skip-git-repo-check -s read-only "$prompt") ;;
    codex:d5-proj-none-flag-all)
      mkdir -p "$dir/.codex"; printf 'sandbox_mode = "read-only"\n' > "$dir/.codex/config.toml"
      CMD=(codex exec --skip-git-repo-check --dangerously-bypass-approvals-and-sandbox "$prompt") ;;
    cursor:none)   CMD=(cursor-agent -p --trust --workspace "$dir" "$prompt") ;;
    cursor:all)    CMD=(cursor-agent -p --trust --force --workspace "$dir" "$prompt") ;;
    cursor:none+allowlist)
      # spike: a project allow list in the folder's .cursor/cli.json, no --force
      mkdir -p "$dir/.cursor"
      echo '{"permissions":{"allow":["Shell(sh)","Write(**)"],"deny":[]}}' > "$dir/.cursor/cli.json"
      CMD=(cursor-agent -p --trust --workspace "$dir" "$prompt") ;;
    opencode:none)  ENV=('OPENCODE_CONFIG_CONTENT={"permission":{"edit":"deny","bash":"deny"}}'); CMD=(opencode run --standalone "$prompt") ;;
    opencode:edits) ENV=('OPENCODE_CONFIG_CONTENT={"permission":{"edit":"allow","bash":"deny"}}'); CMD=(opencode run --standalone "$prompt") ;;
    opencode:all)   CMD=(opencode run --standalone --auto "$prompt") ;;
    copilot:none)   CMD=(copilot -p "$prompt" --no-ask-user --deny-tool shell --deny-tool write) ;;
    copilot:edits)  CMD=(copilot -p "$prompt" --no-ask-user --allow-tool write --deny-tool shell) ;;
    copilot:all)    CMD=(copilot -p "$prompt" --no-ask-user --allow-all-tools) ;;
    *) return 1 ;;
  esac
}

# run_cell VENDOR LEVEL EXPECT_SHELL EXPECT_EDIT [SHELLCMD]
run_cell() {
  local v=$1 level=$2 es=$3 ee=$4 shellcmd=${5:-"sh -c 'echo x > SHELLPROBE'"}
  dir="$base/$v-${level//+/-}-$nrun"; mkdir -p "$dir"
  nrun=$((nrun+1))
  local sp="$dir/shell.probe" ep="$dir/edit.probe"
  shellcmd=${shellcmd//SHELLPROBE/$sp}
  prompt="Do two things, independently. (1) Run exactly this shell command: $shellcmd . (2) Create the file $ep containing the word x using your file write or edit tool, not the shell. If a tool is refused, say so and continue. Your final reply line must be exactly: SHELL=ok or SHELL=denied, then a space, then EDIT=ok or EDIT=denied."
  cell_args "$v" "$level" || { echo "no mapping to run for $v $level"; return; }
  local out="$dir/out.txt" rc
  (cd "$dir" && env ${ENV[@]+"${ENV[@]}"} timeout "$CELL_TIMEOUT" "${CMD[@]}" </dev/null >"$out" 2>&1)
  rc=$?
  local os oe reply
  [ -f "$sp" ] && os=ok || os=denied
  [ -f "$ep" ] && oe=ok || oe=denied
  reply=$(grep -E 'SHELL=(ok|denied) +EDIT=(ok|denied)' "$out" | tail -1 | sed -E 's/.*(SHELL=[a-z]+) +(EDIT=[a-z]+).*/\1 \2/')
  local label="$v $level"
  local res
  if [ $rc -ne 0 ]; then
    res="NOT-RUN (vendor exited $rc: $(grep -v '^[[:space:]]*$' "$out" | tail -1 | cut -c1-120))"; nnotrun=$((nnotrun+1))
  elif [ "$reply" != "SHELL=$os EDIT=$oe" ]; then
    res="NOT-RUN (reply \"${reply:-none}\" does not agree with files: shell $os, edit $oe)"; nnotrun=$((nnotrun+1))
  else
    local vd; vd=$(verdict "$es" "$ee" "$os" "$oe")
    res="$vd (expected shell $es edit $ee; observed shell $os edit $oe)"
    [ "$vd" = SHOWN ] && nshown=$((nshown+1)) || nrefused=$((nrefused+1))
  fi
  echo "$label: $res"
  summary+=("$label: $res")
}

for v in "${vendors[@]}"; do
  echo "== $v"
  case $v in
    claude)
      run_cell claude none denied denied
      run_cell claude edits denied ok
      run_cell claude edits denied ok "touch SHELLPROBE"   # acceptEdits auto-approves filesystem commands
      run_cell claude all ok ok
      # D5: which wins, the project's mode or ynh's flag? EXPECT is the design's outcome.
      run_cell claude d5-proj-edits-noflag denied ok
      run_cell claude d5-proj-all-flag-none denied denied
      run_cell claude d5-proj-none-flag-all ok ok ;;
    codex)
      run_cell codex none denied denied
      run_cell codex edits denied ok   # nearest mapping is workspace-write: shell can write too
      run_cell codex all ok ok
      run_cell codex d5-proj-all-flag-none denied denied
      run_cell codex d5-proj-none-flag-all ok ok ;;
    cursor)
      run_cell cursor none denied denied
      run_cell cursor none+allowlist denied denied   # can an allow list widen none?
      run_cell cursor all ok ok ;;
    opencode)
      run_cell opencode none denied denied
      run_cell opencode edits denied ok
      run_cell opencode all ok ok ;;
    copilot)
      run_cell copilot none denied denied
      run_cell copilot edits denied ok
      run_cell copilot all ok ok ;;
    *) echo "unknown vendor $v" >&2; exit 2 ;;
  esac
done

echo
echo "== summary (${nshown} shown, ${nrefused} refused, ${nnotrun} not run, of ${nrun})"
printf '%s\n' "${summary[@]}"
[ "$nnotrun" -eq 0 ]

# FINDINGS (run of 2026-11-28; Claude Code 2.1.332, codex-cli 0.172.0,
# cursor-agent 2026.11.13, Copilot CLI 1.0.113, OpenCode 2.2.3; all five logged
# in, run as a non-root user on macOS)
#
# Per-vendor mapping, as run (a harness started at the level, delegate or one-shot):
#   level  claude                codex                    cursor                      opencode                    copilot
#   none   SHOWN default         SHOWN -s read-only       REFUSED, see below          SHOWN permission block      SHOWN --deny-tool shell
#                                                                                      edit deny, bash deny        --deny-tool write
#   edits  SHOWN acceptEdits,    REFUSED (workspace-write REFUSED (no flag holds  SHOWN permission block      SHOWN --allow-tool write
#          but see below         lets shell write too)    it)                       edit allow, bash deny       --deny-tool shell
#   all    SHOWN bypassPerm.     SHOWN --dangerously-     SHOWN --force               SHOWN --auto                SHOWN --allow-all-tools
#                                bypass-approvals-and-
#                                sandbox
#
# Notes that change what P1 and P4 must do:
#  - claude edits: acceptEdits denies an arbitrary shell command (sh -c 'echo x >
#    f') but auto-approves simple filesystem commands (touch ran). So "edits"
#    holds shell denied for commands that run programs, not for touch/mkdir and
#    similar. Shown live; the exact set is Claude Code's, not ynh's.
#  - cursor none: without --force, in print mode, cursor-agent denied the shell
#    command but WROTE the file. "no --force" is therefore not "none": a write
#    is allowed. And an allow list in the folder's .cursor/cli.json
#    (Shell(sh), Write(**)) WIDENED a no---force run to shell ok, edit ok. The
#    spike's answer: yes, allow lists widen it. A user-level allow list in
#    cursor's own config was not tried (it would change the human's real
#    config); a project one working means user config cannot be assumed to be
#    ignored. cursor none is REFUSED (D4), as are cursor edits (no flag at all).
#  - opencode: `opencode run` talks to a background service by default and
#    OPENCODE_CONFIG_CONTENT set on the client is ignored there (first run: none
#    and edits both left shell and edit open). With --standalone (a private
#    server) the permission block is honoured and the levels hold. P1 must pass
#    --standalone, or write the permission block where the running service reads it.
#  - copilot none: --deny-tool shell --deny-tool write, with --no-ask-user, did
#    not hang (exit 0, both refused). copilot edits: the unscoped write rule
#    approves a shell redirection into a file per `copilot help permissions`, but
#    deny wins over every allow, so --deny-tool shell held: shell refused, edit ok.
#
# Root (N4): NOT RUN live. This machine has no root (sudo needs a password) and
# no running Docker. Static evidence only, from the installed claude binary: it
# exits 1 with "--dangerously-skip-permissions cannot be used with root/sudo
# privileges for security reasons" when the permission mode is bypassPermissions
# and uid is 0, unless IS_SANDBOX=1 or CLAUDE_CODE_BUBBLEWRAP is set. So N4's
# refusal at "all" is right, ynh's check must also honour those two variables
# (or be stricter on purpose), and the other modes as root were not seen.
#
# D5, project modes against the flag (Claude Code and Codex shown live; Cursor,
# OpenCode, Copilot not run):
#  - Claude Code: project .claude/settings.json defaultMode acceptEdits with no
#    flag opened at edits. With the flag given, THE FLAG WINS both ways: project
#    bypassPermissions + --permission-mode default stayed at none, and project
#    default + --permission-mode bypassPermissions went to all. So a narrower
#    project mode is NOT honoured by Claude Code once ynh passes a flag; ynh has
#    to read the project file itself and pass the narrower mode (D5).
#  - Codex: the same. .codex/config.toml sandbox_mode danger-full-access +
#    -s read-only stayed read-only; sandbox_mode read-only + the bypass flag went
#    to all. The folder was not a trusted project, so whether Codex reads the
#    project file at all was not shown; these two cells show only that the flag
#    wins over it. ynh must still read it to apply D5.
#  - Order, narrowest to widest, in ynh's words:
#      claude  plan < default < acceptEdits < bypassPermissions   (dontAsk not run)
#      codex   sandbox read-only < workspace-write < danger-full-access, with
#              approval_policy untrusted < on-request < on-failure < never
#      none/edits/all place against them as: none = default / read-only,
#      edits = acceptEdits (claude only), all = bypassPermissions / full access.
#    The claude order and the codex sandbox order are from the vendors' own docs
#    and the cells above; plan mode was not run.
