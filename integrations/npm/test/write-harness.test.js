const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("fs");
const os = require("os");
const path = require("path");
const { writeHarness, resolveOutputDir } = require("../lib/write-harness");

function withProject(ynhBlock, fn) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "ynh-test-"));
  // Create node_modules so the default cache dir can be created
  fs.mkdirSync(path.join(dir, "node_modules"), { recursive: true });
  const pkg = { name: "test-project", version: "1.0.0" };
  if (ynhBlock !== undefined) pkg.ynh = ynhBlock;
  const pkgPath = path.join(dir, "package.json");
  fs.writeFileSync(pkgPath, JSON.stringify(pkg));
  const resolved = { config: { ...ynhBlock }, packageJsonPath: pkgPath };
  // Translate vendor like resolve.js does
  if (resolved.config && resolved.config.vendor) {
    resolved.config.default_vendor = resolved.config.vendor;
    delete resolved.config.vendor;
  }
  delete resolved.config.outputDir;
  try {
    fn(dir, resolved);
  } finally {
    fs.rmSync(dir, { recursive: true });
  }
}

describe("resolveOutputDir", () => {
  it("defaults to node_modules/.cache/ynh/", () => {
    withProject({ vendor: "claude" }, (dir, resolved) => {
      const outDir = resolveOutputDir(resolved);
      assert.equal(outDir, path.join(dir, "node_modules", ".cache", "ynh"));
      assert.ok(fs.existsSync(outDir));
    });
  });

  it("respects ynh.outputDir in package.json", () => {
    withProject({ vendor: "claude", outputDir: ".build/ynh" }, (dir, resolved) => {
      const outDir = resolveOutputDir(resolved);
      assert.equal(outDir, path.join(dir, ".build", "ynh"));
      assert.ok(fs.existsSync(outDir));
    });
  });

  it("respects YNH_OUTPUT_DIR env var", () => {
    const prev = process.env.YNH_OUTPUT_DIR;
    withProject({ vendor: "claude" }, (dir, resolved) => {
      const customDir = path.join(dir, "custom-out");
      process.env.YNH_OUTPUT_DIR = customDir;
      try {
        const outDir = resolveOutputDir(resolved);
        assert.equal(outDir, customDir);
        assert.ok(fs.existsSync(outDir));
      } finally {
        if (prev === undefined) delete process.env.YNH_OUTPUT_DIR;
        else process.env.YNH_OUTPUT_DIR = prev;
      }
    });
  });
});

describe("writeHarness", () => {
  it("writes .harness.json to cache dir, not project root", () => {
    withProject({ vendor: "claude" }, (dir, resolved) => {
      const filePath = writeHarness(resolved);
      // Should be in cache, NOT in project root
      assert.ok(filePath.includes("node_modules/.cache/ynh"));
      assert.ok(!filePath.startsWith(dir + "/.harness.json"));
      assert.ok(fs.existsSync(filePath));
    });
  });

  it("does not clobber existing .harness.json in project root", () => {
    withProject({ vendor: "claude" }, (dir, resolved) => {
      // Create an existing .harness.json the user manages
      const userFile = path.join(dir, ".harness.json");
      fs.writeFileSync(userFile, '{"name":"user-managed","version":"1.0.0"}\n');

      writeHarness(resolved);

      // User file should be untouched
      const content = JSON.parse(fs.readFileSync(userFile, "utf8"));
      assert.equal(content.name, "user-managed");
    });
  });

  it("writes valid JSON with correct content", () => {
    const config = {
      vendor: "claude",
      hooks: { before_tool: [{ command: "echo lint" }] },
      focus: { review: { prompt: "Review code" } },
    };
    withProject(config, (dir, resolved) => {
      const filePath = writeHarness(resolved);
      const content = JSON.parse(fs.readFileSync(filePath, "utf8"));
      assert.equal(content.default_vendor, "claude");
      assert.ok(content.hooks);
      assert.ok(content.focus);
      assert.equal(content.vendor, undefined); // translated
      assert.equal(content.outputDir, undefined); // stripped
    });
  });
});
