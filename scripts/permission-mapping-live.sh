#!/usr/bin/env bash
# Live spike for ynh #1058 (P0): show, on each vendor's current CLI, what each
# permission level (none / edits / all) lets an unattended one-shot run do.
# Not part of `make check`: it needs the vendor CLIs, a login and model calls.
#
# Usage: scripts/permission-mapping-live.sh [claude] [codex] [cursor] [opencode] [copilot]
#        (default: every vendor whose CLI is on PATH)
# Env:   CELL_TIMEOUT  seconds per run (default 150); a run that hits it is NOT-RUN
#        SPIKE_BASE    folder to make the cell folders under (default $HOME); a Claude Code cell
#                      about project rules needs one Claude Code already trusts
#        KEEP          set to 1 to keep each cell's folder and out.txt for inspection
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

base=$(mktemp -d "${SPIKE_BASE:-$HOME}/ynh-permission-spike.XXXXXX")
[ "${KEEP:-0}" = 1 ] && echo "keeping $base" || trap 'rm -rf "$base"' EXIT

summary=()
nrun=0; nshown=0; nrefused=0; nnotrun=0

# cell_args VENDOR LEVEL: sets CMD (array) and ENV (array) for the cell, using
# $dir as the folder and $prompt as the task. Returns 1 for a cell with no
# mapping to try (nothing to run).
cell_args() {
  CMD=(); ENV=()
  case "$1:$2" in
    claude:none)   CMD=(claude -p "$prompt" --permission-mode default) ;;
    claude:edits)  CMD=(claude -p "$prompt" --permission-mode acceptEdits --disallowedTools Bash) ;; # decided on ynw-mpnjz: no commands at all
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
    cursor:d5-proj-deny-flag-all)
      mkdir -p "$dir/.cursor"
      echo '{"permissions":{"allow":[],"deny":["Shell(sh)","Write(**)"]}}' > "$dir/.cursor/cli.json"
      CMD=(cursor-agent -p --trust --force --workspace "$dir" "$prompt") ;;
    opencode:d5-proj-allow-env-none)
      echo '{"permission":{"edit":"allow","bash":"allow"}}' > "$dir/opencode.json"
      ENV=('OPENCODE_CONFIG_CONTENT={"permission":{"edit":"deny","bash":"deny"}}'); CMD=(opencode run --standalone "$prompt") ;;
    opencode:d5-proj-deny-flag-all)
      echo '{"permission":{"edit":"deny","bash":"deny"}}' > "$dir/opencode.json"
      CMD=(opencode run --standalone --auto "$prompt") ;;
    copilot:d5-proj-all-noflag)
      git -C "$dir" init -q; mkdir -p "$dir/.github/copilot"
      echo '{"defaultPermissionMode":"allow-all"}' > "$dir/.github/copilot/settings.json"
      CMD=(copilot -p "$prompt" --no-ask-user) ;;
    copilot:d5-proj-all-flag-none)
      git -C "$dir" init -q; mkdir -p "$dir/.github/copilot"
      echo '{"defaultPermissionMode":"allow-all"}' > "$dir/.github/copilot/settings.json"
      CMD=(copilot -p "$prompt" --no-ask-user --deny-tool shell --deny-tool write) ;;
    # allow-list widening (ynw-sg5jw): a project allow rule against none and edits
    claude:ctl-deny-bypass)
      # positive control: proves a project rule is read at all (a deny removes Bash under bypass)
      mkdir -p "$dir/.claude"; echo '{"permissions":{"deny":["Bash"]}}' > "$dir/.claude/settings.json"
      CMD=(claude -p "$prompt" --permission-mode bypassPermissions) ;;
    claude:allow-*)
      mkdir -p "$dir/.claude"; echo '{"permissions":{"allow":["Bash","Write","Edit"]}}' > "$dir/.claude/settings.json"
      case $2 in
        allow-none)               CMD=(claude -p "$prompt" --permission-mode default) ;;
        allow-none-srcuser)       CMD=(claude -p "$prompt" --permission-mode default --setting-sources user) ;;
        allow-none-disallowed)    CMD=(claude -p "$prompt" --permission-mode default --disallowedTools Bash Write Edit) ;;
        allow-edits)              CMD=(claude -p "$prompt" --permission-mode acceptEdits) ;;
        allow-edits-srcuser)      CMD=(claude -p "$prompt" --permission-mode acceptEdits --setting-sources user) ;;
        allow-edits-disallowed)   CMD=(claude -p "$prompt" --permission-mode acceptEdits --disallowedTools Bash) ;;
      esac ;;
    opencode:allow-edits)
      echo '{"permission":{"edit":"allow","bash":"allow"}}' > "$dir/opencode.json"
      ENV=('OPENCODE_CONFIG_CONTENT={"permission":{"edit":"allow","bash":"deny"}}'); CMD=(opencode run --standalone "$prompt") ;;
    cursor:plan-allowlist)
      mkdir -p "$dir/.cursor"; echo '{"permissions":{"allow":["Shell(sh)","Write(**)"],"deny":[]}}' > "$dir/.cursor/cli.json"
      CMD=(cursor-agent -p --trust --mode plan --workspace "$dir" "$prompt") ;;
    cursor:mode-ask)     CMD=(cursor-agent -p --trust --mode ask --workspace "$dir" "$prompt") ;;
    cursor:mode-plan)    CMD=(cursor-agent -p --trust --mode plan --workspace "$dir" "$prompt") ;;
    cursor:sandbox-on)   CMD=(cursor-agent -p --trust --sandbox enabled --workspace "$dir" "$prompt") ;;
    cursor:allowlist-sandbox-on)
      mkdir -p "$dir/.cursor"; echo '{"permissions":{"allow":["Shell(sh)","Write(**)"],"deny":[]}}' > "$dir/.cursor/cli.json"
      CMD=(cursor-agent -p --trust --sandbox enabled --workspace "$dir" "$prompt") ;;
    # codex D5 in a folder trusted for this run only (-c, nothing written to the user's config)
    codex:d5t-proj-all-noflag)
      mkdir -p "$dir/.codex"; printf 'sandbox_mode = "danger-full-access"\napproval_policy = "never"\n' > "$dir/.codex/config.toml"
      CMD=(codex exec --skip-git-repo-check -c "projects.\"$dir\".trust_level=\"trusted\"" "$prompt") ;;
    codex:d5t-proj-all-flag-none)
      mkdir -p "$dir/.codex"; printf 'sandbox_mode = "danger-full-access"\napproval_policy = "never"\n' > "$dir/.codex/config.toml"
      CMD=(codex exec --skip-git-repo-check -c "projects.\"$dir\".trust_level=\"trusted\"" -s read-only "$prompt") ;;
    codex:d5t-proj-none-flag-all)
      mkdir -p "$dir/.codex"; printf 'sandbox_mode = "read-only"\n' > "$dir/.codex/config.toml"
      CMD=(codex exec --skip-git-repo-check -c "projects.\"$dir\".trust_level=\"trusted\"" --dangerously-bypass-approvals-and-sandbox "$prompt") ;;
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

# root_cells: N4. Claude Code as uid 0 in a throwaway node container (needs a
# running Docker and the network). No login carries into the container, so only
# what happens before the first model call can be seen: the startup refusal.
root_cells() {
  if ! docker info >/dev/null 2>&1; then
    echo "claude-root: NOT-RUN (docker is not running)"; summary+=("claude-root: NOT-RUN (docker is not running)"); nnotrun=$((nnotrun+1)); return
  fi
  local out
  out=$(docker run --rm node:22-slim bash -c '
    npm i -g @anthropic-ai/claude-code >/dev/null 2>&1; echo "uid $(id -u), $(claude --version)"
    t() { printf "%s => " "$1"; shift; timeout 40 "$@" </dev/null 2>&1 | head -1 | cut -c1-110; }
    t "mode default" claude -p hi --permission-mode default
    t "mode acceptEdits" claude -p hi --permission-mode acceptEdits
    t "mode bypassPermissions" claude -p hi --permission-mode bypassPermissions
    t "bypassPermissions IS_SANDBOX=1" env IS_SANDBOX=1 claude -p hi --permission-mode bypassPermissions
    t "bypassPermissions CLAUDE_CODE_BUBBLEWRAP=1" env CLAUDE_CODE_BUBBLEWRAP=1 claude -p hi --permission-mode bypassPermissions' 2>&1 | tail -6)
  printf '%s\n' "$out"
  summary+=("claude-root (refusal seen at startup only; no login in the container):" "$out")
  case $out in *"bypassPermissions =>"*"cannot be used with root"*) nshown=$((nshown+1)) ;; *) nnotrun=$((nnotrun+1)) ;; esac
  nrun=$((nrun+1))
}

for v in "${vendors[@]}"; do
  echo "== $v"
  case $v in
    claude)
      run_cell claude none denied denied
      run_cell claude edits denied ok
      run_cell claude edits denied ok "touch SHELLPROBE"   # acceptEdits alone auto-approves these; --disallowedTools Bash must stop them
      run_cell claude edits denied ok "mkdir -p SHELLPROBE.d && touch SHELLPROBE"
      run_cell claude all ok ok
      # D5: the project's mode against the flag ynh passes. The expectation is D5's text:
      # the narrower of the two applies, so a wider project mode never widens the run and a
      # narrower one narrows it. "REFUSED" here means the vendor does not do that itself.
      run_cell claude d5-proj-edits-noflag denied ok
      run_cell claude d5-proj-all-flag-none denied denied
      run_cell claude d5-proj-none-flag-all denied denied
      # allow-list widening and the two controls that might stop it (ynw-sg5jw)
      run_cell claude ctl-deny-bypass denied ok
      run_cell claude allow-none denied denied
      run_cell claude allow-none-srcuser denied denied
      run_cell claude allow-none-disallowed denied denied
      run_cell claude allow-edits denied ok
      run_cell claude allow-edits-srcuser denied ok
      run_cell claude allow-edits-disallowed denied ok ;;
    codex)
      run_cell codex none denied denied
      run_cell codex edits denied ok   # nearest mapping is workspace-write: shell can write too
      run_cell codex all ok ok
      run_cell codex d5-proj-all-flag-none denied denied
      run_cell codex d5-proj-none-flag-all denied denied
      run_cell codex d5t-proj-all-noflag ok ok   # does a trusted folder's project file take effect at all
      run_cell codex d5t-proj-all-flag-none denied denied
      run_cell codex d5t-proj-none-flag-all denied denied ;;
    cursor)
      run_cell cursor none denied denied
      run_cell cursor none+allowlist denied denied   # can an allow list widen none?
      run_cell cursor all ok ok
      run_cell cursor d5-proj-deny-flag-all denied denied
      run_cell cursor mode-ask denied denied
      run_cell cursor mode-plan denied denied
      run_cell cursor plan-allowlist denied denied
      run_cell cursor sandbox-on denied denied
      run_cell cursor allowlist-sandbox-on denied denied ;;
    opencode)
      run_cell opencode none denied denied
      run_cell opencode edits denied ok
      run_cell opencode all ok ok
      run_cell opencode d5-proj-allow-env-none denied denied
      run_cell opencode d5-proj-deny-flag-all denied denied
      run_cell opencode allow-edits denied ok ;;
    copilot)
      run_cell copilot none denied denied
      run_cell copilot edits denied ok
      run_cell copilot all ok ok
      run_cell copilot d5-proj-all-noflag denied denied   # does a project allow-all widen a headless run?
      run_cell copilot d5-proj-all-flag-none denied denied ;;
    claude-root) root_cells ;;
    *) echo "unknown vendor $v" >&2; exit 2 ;;
  esac
done

echo
echo "== summary (${nshown} shown, ${nrefused} refused, ${nnotrun} not run, of ${nrun})"
printf '%s\n' "${summary[@]}"
[ "$nnotrun" -eq 0 ]

# FINDINGS (runs of 2026-11-28; Claude Code 2.1.332, codex-cli 0.172.0,
# cursor-agent 2026.11.13, Copilot CLI 1.0.113, OpenCode 2.2.3; all five logged
# in, run as a non-root user on macOS; every cell below was seen unless it says
# NOT RUN). A cell is SHOWN, REFUSED (not exact, so ynh refuses the level on that
# vendor at start, D4) or NOT RUN.
#
#   level  claude                         codex                 cursor           opencode              copilot
#   none   SHOWN --permission-mode       SHOWN -s read-only    REFUSED (below)  SHOWN permission      SHOWN --deny-tool shell
#          default                                                               block, --standalone   --deny-tool write
#   edits  SHOWN acceptEdits             REFUSED               REFUSED          SHOWN permission      SHOWN --allow-tool write
#          --disallowedTools Bash                                                block, --standalone   --deny-tool shell
#   all    SHOWN bypassPermissions       SHOWN --dangerously-  SHOWN --force    SHOWN --auto          SHOWN --allow-all-tools
#          (refused as root)             bypass-...
#
# Claude edits (ynw-mpnjz, decided by the human): acceptEdits ALONE auto-approves
# touch/mkdir, so it was not exact (seen: touch ran). With --disallowedTools Bash
# the three probes, sh -c, touch and mkdir, were all refused while the Write tool
# worked: SHOWN. Hence the mapping is acceptEdits plus --disallowedTools Bash.
#
# Codex edits: workspace-write lets the shell write too (seen). REFUSED.
#
# Cursor (ynw-sg5jw): none is REFUSED.
#  - no --force: the shell command was denied but the file WAS written.
#  - --mode plan: shell and edit both denied (the only cursor setting that
#    held none), BUT a project .cursor/cli.json allow list (Shell(sh)) widened
#    it: shell ran. So plan holds only until a project allow list exists.
#  - --mode ask: edit allowed. REFUSED. --sandbox enabled: shell and edit both
#    ran. REFUSED. A project allow list widened no-flag and sandbox runs
#    (shell and edit ran). A project DENY list beat --force (d5-proj-deny-flag-all).
#  - A user-level allow list was not tried (it would change the real config).
#
# Allow-list widening on the other vendors (ynw-sg5jw):
#  - Claude Code: a project .claude/settings.json permissions.allow of Bash,
#    Write, Edit did NOT widen none or edits (default and acceptEdits), with or
#    without --setting-sources user or --disallowedTools (ynw-22war: also in a
#    folder under /Users/david/Storage/Workspace/eyelock, an ancestor already
#    marked trusted in the user's own ~/.claude.json, nothing changed there, via
#    SPIKE_BASE). A project deny rule in the same place WAS read (ctl-deny-bypass:
#    Bash removed under bypassPermissions), so project rules are read and an allow
#    rule simply does not widen default or acceptEdits in a one-shot run. So
#    Claude none is exact against a project allow list. That Claude Code applied
#    the ancestor's trust was not itself shown, only that the project's deny rule
#    took effect there and in the home folder alike.
#  - OpenCode: a project opencode.json allowing edit and bash did not widen the
#    permission block in OPENCODE_CONFIG_CONTENT (none and edits both held), so
#    the env block wins. A project deny beat --auto.
#  - Copilot: a project .github/copilot/settings.json defaultPermissionMode
#    allow-all did not widen a run with no allow flags, nor beat --deny-tool
#    (deny wins). Copilot's user-level saved approvals (permissions-config.json)
#    were not tried (real config). The repo settings may be ignored for an
#    untrusted folder; not shown either way.
#
# OpenCode: `opencode run` talks to a background service and ignores
# OPENCODE_CONFIG_CONTENT set on the client (first run left both open); with
# --standalone the block holds. P1 must pass --standalone.
# Copilot none: --no-ask-user with both denies exited 0, no hang.
#
# Root (N4, ynw-purnb): Claude Code 2.1.332 as uid 0 in a throwaway node:22-slim
# container (root_cells; Docker Desktop was started with `open -a Docker`, no
# password). No login reaches the container, so only the startup is seen, NOT
# the behaviour of a level once running:
#   --permission-mode default             starts (reaches "Not logged in")
#   --permission-mode acceptEdits         starts (same)
#   --permission-mode bypassPermissions   exits 1: "--dangerously-skip-permissions
#                                         cannot be used with root/sudo privileges..."
#   bypassPermissions + IS_SANDBOX=1      refusal lifted (reaches "Not logged in")
#   bypassPermissions + CLAUDE_CODE_BUBBLEWRAP=1   refusal lifted (same)
#   --dangerously-skip-permissions        same refusal as bypassPermissions
# NOT RUN: what none, edits (acceptEdits --disallowedTools Bash) and all do as root
# once logged in, because no login could be put in the container.
# So N4's "all as root is refused" is seen live; ynh's check must honour
# IS_SANDBOX=1 and CLAUDE_CODE_BUBBLEWRAP or be stricter on purpose.
#
# D5 (ynw-jxcnd), project mode against the flag, per vendor. D5's text: a
# narrower project mode applies, a wider one does not widen. The cells expect
# that outcome; REFUSED means the vendor does not do it itself, so ynh must.
#  - Claude Code: project defaultMode acceptEdits with no flag opened at edits
#    (the file is read). With a flag the flag wins both ways: project bypass +
#    default stayed none (good); project default + bypassPermissions went to all
#    (REFUSED: ynh must read the file and pass the narrower mode).
#    Order narrowest to widest: plan < default < acceptEdits < bypassPermissions
#    (plan and dontAsk not run).
#  - Codex: in a folder trusted for the run only (-c projects."<dir>".trust_level),
#    project sandbox_mode danger-full-access with no flag still ran read-only
#    (REFUSED: the project file was not shown to take effect even trusted, so the
#    two flag cells show the flag wins, nothing more). Flag -s read-only beat a
#    wider project; the bypass flag beat a narrower project (ran as all, REFUSED).
#    Order: sandbox read-only < workspace-write < danger-full-access
#    (workspace-write not run as a project mode).
#  - Cursor: no project mode, only project allow and deny lists. Allow widens a
#    no-flag run; deny beats --force. Order of the flags: --mode plan < ask <
#    no flag < --force; a project allow list widens any of them but all.
#  - OpenCode: env block beats a wider project file; a project deny beats --auto.
#    Order: deny < allow per permission key; --auto is above both only where
#    nothing is denied.
#  - Copilot: a project allow-all setting did not widen a headless run; --deny-tool
#    beats it. Order: --deny-tool > --allow-tool > project setting; --allow-all-tools
#    last, but deny still wins over it.
