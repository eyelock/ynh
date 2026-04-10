const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const { getBinaryPath } = require("../lib/binary");

describe("getBinaryPath", () => {
  it("finds ynh on PATH", () => {
    // This test only works if ynh is installed
    try {
      const path = getBinaryPath();
      assert.ok(path.length > 0);
      assert.ok(path.includes("ynh"));
    } catch (e) {
      // ynh not installed — skip gracefully
      if (!e.message.includes("ynh binary not found")) {
        throw e;
      }
    }
  });

  it("throws descriptive error when not found", () => {
    // Save and clear PATH to simulate missing binary
    const origPath = process.env.PATH;
    const origHome = process.env.HOME;
    process.env.PATH = "/nonexistent";
    process.env.HOME = "/nonexistent";
    try {
      assert.throws(() => getBinaryPath(), /ynh binary not found/);
    } finally {
      process.env.PATH = origPath;
      process.env.HOME = origHome;
    }
  });
});
