// Write a .harness.json file for ynh to consume.
//
// The file is written to a cache directory (default: node_modules/.cache/ynh/)
// to avoid polluting the project root. The caller passes the file path to
// ynh via --harness-file so ynh knows where to find it.
//
// The output directory is configurable via:
//   1. "ynh.outputDir" in package.json
//   2. YNH_OUTPUT_DIR environment variable
//   3. Default: node_modules/.cache/ynh/

const fs = require("fs");
const path = require("path");

function resolveOutputDir(resolved) {
  const projectDir = path.dirname(resolved.packageJsonPath);

  // Check ynh.outputDir in config (stripped during resolve, check raw package.json)
  const pkg = JSON.parse(fs.readFileSync(resolved.packageJsonPath, "utf8"));
  if (pkg.ynh && pkg.ynh.outputDir) {
    const dir = path.resolve(projectDir, pkg.ynh.outputDir);
    fs.mkdirSync(dir, { recursive: true });
    return dir;
  }

  // Check environment variable
  if (process.env.YNH_OUTPUT_DIR) {
    const dir = path.resolve(projectDir, process.env.YNH_OUTPUT_DIR);
    fs.mkdirSync(dir, { recursive: true });
    return dir;
  }

  // Default: node_modules/.cache/ynh/
  const dir = path.join(projectDir, "node_modules", ".cache", "ynh");
  fs.mkdirSync(dir, { recursive: true });
  return dir;
}

function writeHarness(resolved) {
  const outputDir = resolveOutputDir(resolved);
  const filePath = path.join(outputDir, ".harness.json");

  const json = JSON.stringify(resolved.config, null, 2);
  fs.writeFileSync(filePath, json + "\n");
  return filePath;
}

module.exports = { writeHarness, resolveOutputDir };
