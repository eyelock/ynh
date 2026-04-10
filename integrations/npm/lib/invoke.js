// Execute the ynh binary with --harness-file pointing to the generated config.
// stdio is inherited so the vendor CLI gets full terminal control.

const { spawnSync } = require("child_process");
const path = require("path");
const { getBinaryPath } = require("./binary");
const { findPackageJson } = require("./resolve");

function invoke(harnessFile, extraArgs = []) {
  const bin = getBinaryPath();
  const args = ["run"];

  if (harnessFile) {
    args.push("--harness-file", harnessFile);
  }

  args.push(...extraArgs);

  // Run from the project root
  const pkgPath = findPackageJson();
  const cwd = pkgPath ? path.dirname(pkgPath) : process.cwd();

  const result = spawnSync(bin, args, {
    stdio: "inherit",
    cwd,
    env: process.env,
  });

  if (result.error) {
    console.error(`Failed to execute ynh: ${result.error.message}`);
    process.exit(1);
  }

  process.exit(result.status ?? 1);
}

module.exports = { invoke };
