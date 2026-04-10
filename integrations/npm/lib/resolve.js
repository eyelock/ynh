// Read the "ynh" block from the nearest package.json and translate
// it into a .harness.json-compatible config object.
//
// Field translation: "vendor" → "default_vendor"
// (package.json uses "vendor" naturally; .harness.json uses "default_vendor")
//
// Fields stripped (npm-integration only, not valid in .harness.json):
//   "outputDir" — controls where the generated .harness.json is written
//
// NPM scoped names are NOT written to the output — they fail ynh's
// name regex. The name field is optional in inline .harness.json.

const fs = require("fs");
const path = require("path");

function findPackageJson(startDir = process.cwd()) {
  let dir = startDir;
  while (true) {
    const candidate = path.join(dir, "package.json");
    if (fs.existsSync(candidate)) return candidate;
    const parent = path.dirname(dir);
    if (parent === dir) return null; // reached root
    dir = parent;
  }
}

function resolve(startDir) {
  const pkgPath = findPackageJson(startDir);
  if (!pkgPath) return null;

  const pkg = JSON.parse(fs.readFileSync(pkgPath, "utf8"));
  const ynhBlock = pkg.ynh;
  if (!ynhBlock) return null;

  // Build config — translate and strip npm-only fields
  const config = { ...ynhBlock };

  if (config.vendor) {
    config.default_vendor = config.vendor;
    delete config.vendor;
  }

  // Strip npm-integration-only fields (not valid in .harness.json)
  delete config.outputDir;

  return { config, packageJsonPath: pkgPath };
}

module.exports = { resolve, findPackageJson };
