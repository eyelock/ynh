const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("fs");
const path = require("path");
const os = require("os");
const { resolve, findPackageJson } = require("../lib/resolve");

function withFixture(ynhBlock, fn) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "ynh-test-"));
  const pkg = { name: "test-project", version: "1.0.0" };
  if (ynhBlock !== undefined) pkg.ynh = ynhBlock;
  fs.writeFileSync(path.join(dir, "package.json"), JSON.stringify(pkg));
  try {
    fn(dir);
  } finally {
    fs.rmSync(dir, { recursive: true });
  }
}

describe("findPackageJson", () => {
  it("finds package.json in current dir", () => {
    withFixture({}, (dir) => {
      const found = findPackageJson(dir);
      assert.equal(found, path.join(dir, "package.json"));
    });
  });

  it("walks up to find package.json", () => {
    withFixture({}, (dir) => {
      const subDir = path.join(dir, "src", "lib");
      fs.mkdirSync(subDir, { recursive: true });
      const found = findPackageJson(subDir);
      assert.equal(found, path.join(dir, "package.json"));
    });
  });

  it("returns null when not found", () => {
    const found = findPackageJson("/");
    assert.equal(found, null);
  });
});

describe("resolve", () => {
  it("returns null when no ynh block", () => {
    withFixture(undefined, (dir) => {
      assert.equal(resolve(dir), null);
    });
  });

  it("returns config from ynh block", () => {
    const ynhBlock = {
      vendor: "claude",
      focus: { review: { prompt: "Review code" } },
    };
    withFixture(ynhBlock, (dir) => {
      const result = resolve(dir);
      assert.notEqual(result, null);
      assert.equal(result.config.default_vendor, "claude");
      assert.equal(result.config.vendor, undefined); // translated
      assert.deepEqual(result.config.focus, { review: { prompt: "Review code" } });
    });
  });

  it("translates vendor to default_vendor", () => {
    withFixture({ vendor: "codex" }, (dir) => {
      const result = resolve(dir);
      assert.equal(result.config.default_vendor, "codex");
      assert.equal(result.config.vendor, undefined);
    });
  });

  it("preserves config without vendor field", () => {
    withFixture({ hooks: { on_stop: [{ command: "echo done" }] } }, (dir) => {
      const result = resolve(dir);
      assert.equal(result.config.default_vendor, undefined);
      assert.ok(result.config.hooks);
    });
  });

  it("includes packageJsonPath", () => {
    withFixture({ vendor: "claude" }, (dir) => {
      const result = resolve(dir);
      assert.equal(result.packageJsonPath, path.join(dir, "package.json"));
    });
  });
});
