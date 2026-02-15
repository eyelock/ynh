#!/usr/bin/env bash
# Validate persona artifacts: frontmatter, naming, structure.
# Usage: bash skills/ynh-validate/scripts/validate.sh

set -euo pipefail

ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
errors=0

check_field() {
  local file="$1" field="$2"
  if ! head -20 "$file" | grep -q "^${field}:"; then
    echo "  FAIL  missing '$field' field"
    return 1
  fi
  return 0
}

check_delimiters() {
  local file="$1"
  if [ "$(head -1 "$file")" != "---" ]; then
    echo "  FAIL  missing opening --- delimiter"
    return 1
  fi
  # Check for closing delimiter (second occurrence of --- in first 20 lines)
  if [ "$(head -20 "$file" | grep -c "^---$")" -lt 2 ]; then
    echo "  FAIL  missing closing --- delimiter"
    return 1
  fi
  return 0
}

check_name_match() {
  local file="$1" expected="$2"
  local actual
  actual=$(head -20 "$file" | grep "^name:" | head -1 | sed 's/^name:[[:space:]]*//')
  if [ "$actual" != "$expected" ]; then
    echo "  FAIL  name '$actual' does not match expected '$expected'"
    return 1
  fi
  return 0
}

echo "Validating persona artifacts..."
echo ""

# --- Skills ---
echo "Skills:"
for skill_dir in "$ROOT"/skills/*/; do
  [ -d "$skill_dir" ] || continue
  skill_name=$(basename "$skill_dir")
  file="$skill_dir/SKILL.md"

  if [ ! -f "$file" ]; then
    echo "  $skill_name: FAIL  missing SKILL.md"
    errors=$((errors + 1))
    continue
  fi

  ok=true
  output=""
  output+=$(check_delimiters "$file" 2>&1) || ok=false
  output+=$(check_field "$file" "name" 2>&1) || ok=false
  output+=$(check_field "$file" "description" 2>&1) || ok=false
  output+=$(check_name_match "$file" "$skill_name" 2>&1) || ok=false

  if $ok; then
    echo "  $skill_name: ok"
  else
    echo "  $skill_name:"
    echo "$output" | grep "FAIL" | while read -r line; do echo "    $line"; done
    errors=$((errors + 1))
  fi
done

echo ""

# --- Agents ---
echo "Agents:"
for agent_file in "$ROOT"/agents/*.md; do
  [ -f "$agent_file" ] || continue
  agent_name=$(basename "$agent_file" .md)

  ok=true
  output=""
  output+=$(check_delimiters "$agent_file" 2>&1) || ok=false
  output+=$(check_field "$agent_file" "name" 2>&1) || ok=false
  output+=$(check_field "$agent_file" "description" 2>&1) || ok=false
  output+=$(check_field "$agent_file" "tools" 2>&1) || ok=false
  output+=$(check_name_match "$agent_file" "$agent_name" 2>&1) || ok=false

  if $ok; then
    echo "  $agent_name: ok"
  else
    echo "  $agent_name:"
    echo "$output" | grep "FAIL" | while read -r line; do echo "    $line"; done
    errors=$((errors + 1))
  fi
done

echo ""

# --- Summary ---
if [ "$errors" -eq 0 ]; then
  echo "All artifacts valid."
else
  echo "$errors artifact(s) with issues."
  exit 1
fi
