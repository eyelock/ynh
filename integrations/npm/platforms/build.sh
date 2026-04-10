#!/bin/bash
set -euo pipefail

# Build platform-specific npm packages from goreleaser output.
# Called during release after goreleaser creates the archives.
#
# Usage: ./build.sh <version> <dist-dir>
#   version:  semver without v prefix (e.g. "0.1.0")
#   dist-dir: goreleaser dist directory containing archives

VERSION="${1:?usage: build.sh <version> <dist-dir>}"
DIST="${2:?usage: build.sh <version> <dist-dir>}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
OUT="${SCRIPT_DIR}/dist"

# Platform mapping: npm-name → goreleaser-archive-pattern → npm-os → npm-cpu
declare -A PLATFORMS=(
  ["darwin-arm64"]="ynh_${VERSION}_darwin_arm64|darwin|arm64"
  ["darwin-x64"]="ynh_${VERSION}_darwin_amd64|darwin|x64"
  ["linux-x64"]="ynh_${VERSION}_linux_amd64|linux|x64"
  ["linux-arm64"]="ynh_${VERSION}_linux_arm64|linux|arm64"
)

rm -rf "$OUT"

for platform in "${!PLATFORMS[@]}"; do
  IFS='|' read -r archive os cpu <<< "${PLATFORMS[$platform]}"
  pkg_dir="${OUT}/@ynh/cli-${platform}"

  echo "Building @ynh/cli-${platform}..."
  mkdir -p "${pkg_dir}/bin"

  # Extract ynh binary from goreleaser archive
  tar -xzf "${DIST}/${archive}.tar.gz" -C "${pkg_dir}/bin" ynh
  chmod 755 "${pkg_dir}/bin/ynh"

  # Generate package.json from template
  sed -e "s/{{PLATFORM}}/${platform}/g" \
      -e "s/{{VERSION}}/${VERSION}/g" \
      -e "s/{{OS}}/${os}/g" \
      -e "s/{{CPU}}/${cpu}/g" \
      "${SCRIPT_DIR}/package.json.tmpl" > "${pkg_dir}/package.json"

  echo "  → ${pkg_dir}"
done

echo ""
echo "Platform packages built in ${OUT}/"
echo "Publish with: cd ${OUT} && for d in @ynh/cli-*; do (cd \"\$d\" && npm publish --access public); done"
