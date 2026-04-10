// Locate the ynh binary.
//
// Resolution order:
// 1. Platform-specific optional dep (@ynh/cli-darwin-arm64, etc.)
// 2. ynh on PATH (user installed globally or via brew)

const { platform, arch } = process;
const { execFileSync } = require("child_process");
const path = require("path");

const PLATFORMS = {
  "darwin-arm64": "@ynh/cli-darwin-arm64",
  "darwin-x64": "@ynh/cli-darwin-x64",
  "linux-x64": "@ynh/cli-linux-x64",
  "linux-arm64": "@ynh/cli-linux-arm64",
};

function getBinaryPath() {
  // Try platform package first
  const pkg = PLATFORMS[`${platform}-${arch}`];
  if (pkg) {
    try {
      return require.resolve(`${pkg}/bin/ynh`);
    } catch {
      // Fall through to PATH
    }
  }

  // Try PATH
  try {
    const result = execFileSync("which", ["ynh"], { encoding: "utf8" }).trim();
    if (result) return result;
  } catch {
    // Not on PATH
  }

  throw new Error(
    `ynh binary not found. Install @ynh/cli with optional deps, ` +
      `or install ynh globally: brew install eyelock/tap/ynh`
  );
}

module.exports = { getBinaryPath };
