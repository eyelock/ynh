// Check that the ynh binary meets the minimum version required by this package.
// Prevents cryptic DisallowUnknownFields errors when focus/profiles fields
// are written to a .harness.json but the binary doesn't understand them.

const { execFileSync } = require("child_process");

const MIN_VERSION = "0.1.0";

function checkVersion(binaryPath) {
  let version;
  try {
    version = execFileSync(binaryPath, ["version"], {
      encoding: "utf8",
      timeout: 5000,
    }).trim();
  } catch {
    // Can't check — let it fail later with a more specific error
    return;
  }

  // Dev builds: "dev-branch-sha" — skip check
  if (version.startsWith("dev-")) return;

  // Release builds: "0.1.0" or "v0.1.0"
  const clean = version.replace(/^v/, "");
  if (!meetsMinimum(clean, MIN_VERSION)) {
    console.error(
      `@ynh/cli requires ynh >= ${MIN_VERSION}, but found ${version}.\n` +
        `Update with: brew upgrade ynh`
    );
    process.exit(1);
  }
}

function meetsMinimum(version, minimum) {
  const v = version.split(".").map(Number);
  const m = minimum.split(".").map(Number);
  for (let i = 0; i < 3; i++) {
    if ((v[i] || 0) > (m[i] || 0)) return true;
    if ((v[i] || 0) < (m[i] || 0)) return false;
  }
  return true; // equal
}

module.exports = { checkVersion, meetsMinimum, MIN_VERSION };
