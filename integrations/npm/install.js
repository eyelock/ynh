#!/usr/bin/env node

// Postinstall script: resolve the platform-specific binary package.
// Pattern: same as @biomejs/biome, esbuild — optional deps per platform.

const { platform, arch } = process;
const path = require("path");
const fs = require("fs");

const PLATFORMS = {
  "darwin-arm64": "@ynh/cli-darwin-arm64",
  "darwin-x64": "@ynh/cli-darwin-x64",
  "linux-x64": "@ynh/cli-linux-x64",
  "linux-arm64": "@ynh/cli-linux-arm64",
};

const key = `${platform}-${arch}`;
const pkg = PLATFORMS[key];

if (!pkg) {
  console.error(
    `@ynh/cli: unsupported platform ${key}. ` +
      `Supported: ${Object.keys(PLATFORMS).join(", ")}`
  );
  process.exit(1);
}

// Verify the optional dep resolved (npm/pnpm/yarn install it automatically)
try {
  const binPath = require.resolve(`${pkg}/bin/ynh`);
  // Ensure it's executable
  fs.chmodSync(binPath, 0o755);
} catch {
  console.error(
    `@ynh/cli: platform package ${pkg} not found. ` +
      `This usually means your package manager skipped optional dependencies. ` +
      `Try: npm install --include=optional`
  );
  process.exit(1);
}
