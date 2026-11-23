#!/usr/bin/env bash
# Run the tutorial evals (.claude/agents/evals.md) headless and turn the agent's
# last line into an exit status.
#
#   scripts/evals.sh            (make evals)
#
# The evals are an LLM agent run through the Claude Code CLI, so this needs
# `claude` on PATH and a login (or CLAUDE_CODE_OAUTH_TOKEN / ANTHROPIC_API_KEY).
# It is not part of `make check`: it costs model calls and takes a while.
#
# Exit status:
#   0  the agent ended with `EVALS: PASS (...)`
#   1  it ended with `EVALS: FAIL (...)`
#   2  no verdict line: the agent did not finish, or claude failed to start.
#      That is a failed gate, never a pass.
#
# The agent's full report goes to $EVALS_REPORT (default evals-report.txt in
# the repository root, git-ignored) and the verdict line to $GITHUB_STEP_SUMMARY
# when it is set. EVALS_CLAUDE overrides the claude binary (the test seam).
set -uo pipefail

repo=$(git rev-parse --show-toplevel) || exit 2
cd "$repo" || exit 2
report=${EVALS_REPORT:-$repo/evals-report.txt}
claude_bin=${EVALS_CLAUDE:-claude}

command -v "$claude_bin" >/dev/null 2>&1 || {
  echo "evals: $claude_bin not found on PATH" >&2
  exit 2
}

# Only the tools the agent's own definition lists; no permission bypass.
# The prompt comes first: --allowedTools takes a list and would swallow it.
"$claude_bin" --print "Run the full eval suite now, as your instructions say, and end with the verdict line." \
  --agent evals --allowedTools "Bash,Read,Glob" \
  >"$report" 2>&1
claude_status=$?

verdict=$(grep -E '^EVALS: (PASS|FAIL) ' "$report" | tail -n 1)
if [ -z "$verdict" ]; then
  echo "evals: no verdict line in $report (claude exited $claude_status)" >&2
  verdict="EVALS: NO VERDICT (claude exited $claude_status)"
  status=2
elif [ "${verdict#EVALS: PASS }" != "$verdict" ]; then
  status=0
else
  status=1
fi

echo "$verdict"
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  {
    echo "## Tutorial evals"
    echo
    echo "$verdict"
    echo
    echo "Commit: \`$(git rev-parse --short HEAD)\`"
  } >>"$GITHUB_STEP_SUMMARY"
fi
exit "$status"
