// End-to-end tests using the sample-project fixture.
// Simulates what happens when a team uses @ynh/cli in their npm project.
// Requires ynh/ynd on PATH.

const { describe, it, before } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("fs");
const path = require("path");
const os = require("os");
const { execFileSync } = require("child_process");
const { resolve } = require("../lib/resolve");
const { writeHarness, resolveOutputDir } = require("../lib/write-harness");

const FIXTURE = path.join(__dirname, "fixtures", "sample-project");
let yndBin;

before(() => {
  try {
    const ynhBin = execFileSync("which", ["ynh"], { encoding: "utf8" }).trim();
    yndBin = path.join(path.dirname(ynhBin), "ynd");
  } catch {
    // Not on PATH
  }
});

function skipIfNoYnh() {
  return !yndBin;
}

// Copy fixture to a temp dir so tests don't mutate it
function copyFixture() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "ynh-e2e-"));
  copyDirRecursive(FIXTURE, dir);
  // Create node_modules so cache dir works
  fs.mkdirSync(path.join(dir, "node_modules"), { recursive: true });
  return dir;
}

function copyDirRecursive(src, dest) {
  fs.mkdirSync(dest, { recursive: true });
  for (const entry of fs.readdirSync(src, { withFileTypes: true })) {
    const srcPath = path.join(src, entry.name);
    const destPath = path.join(dest, entry.name);
    if (entry.isDirectory()) {
      copyDirRecursive(srcPath, destPath);
    } else {
      fs.copyFileSync(srcPath, destPath);
    }
  }
}

describe("e2e: sample-project", () => {
  it("fixture project source is valid", () => {
    // Verify the fixture project's source can be required without error
    const { getUsers, createUser } = require(path.join(FIXTURE, "src", "users"));
    assert.ok(Array.isArray(getUsers()));
    assert.throws(() => createUser({}), /name and email are required/);
  });

  it("resolve reads ynh config from fixture package.json", () => {
    const result = resolve(FIXTURE);
    assert.notEqual(result, null);
    assert.equal(result.config.default_vendor, "claude");
    assert.equal(result.config.vendor, undefined);
    assert.ok(result.config.hooks);
    assert.ok(result.config.mcp_servers);
    assert.ok(result.config.profiles);
    assert.ok(result.config.focus);
    assert.equal(result.config.focus.review.profile, "ci");
    assert.ok(result.config.focus.review.prompt.includes("staged changes"));
    assert.ok(result.config.focus.security.prompt.includes("OWASP"));
    assert.equal(result.config.focus.docs.profile, undefined);
  });

  it("writeHarness puts file in cache, not project root", () => {
    const dir = copyFixture();
    try {
      const result = resolve(dir);
      const harnessFile = writeHarness(result);

      // In cache dir
      assert.ok(
        harnessFile.includes("node_modules/.cache/ynh"),
        `Expected cache path, got: ${harnessFile}`
      );

      // NOT in project root
      assert.ok(
        !fs.existsSync(path.join(dir, ".harness.json")),
        "Should not write .harness.json to project root"
      );

      // File is valid JSON
      const content = JSON.parse(fs.readFileSync(harnessFile, "utf8"));
      assert.equal(content.default_vendor, "claude");
      assert.equal(content.name, undefined); // scoped name omitted
    } finally {
      fs.rmSync(dir, { recursive: true });
    }
  });

  it("generated config has correct profile merge semantics", { skip: skipIfNoYnh() }, () => {
    const dir = copyFixture();
    try {
      const result = resolve(dir);
      const harnessFile = writeHarness(result);

      // Add name+version for ynd preview
      const content = JSON.parse(fs.readFileSync(harnessFile, "utf8"));
      content.name = "acme-web-api";
      content.version = "1.0.0";
      fs.writeFileSync(harnessFile, JSON.stringify(content, null, 2) + "\n");

      // Write a .harness.json in the fixture dir for preview
      // (ynd preview needs a dir, not a file path)
      const previewDir = fs.mkdtempSync(path.join(os.tmpdir(), "ynh-preview-"));
      fs.writeFileSync(
        path.join(previewDir, ".harness.json"),
        JSON.stringify(content, null, 2) + "\n"
      );

      // Preview with --focus review (activates ci profile)
      const output = execFileSync(
        yndBin,
        ["preview", previewDir, "-v", "claude", "--focus", "review"],
        { encoding: "utf8" }
      );

      // ci profile merges: replaces before_tool, inherits nothing else
      // ci profile removes github MCP via null
      assert.ok(
        output.includes("strict-lint"),
        `Expected ci profile's strict-lint hook: ${output}`
      );
      assert.ok(
        output.includes("ci-guard"),
        `Expected ci profile's ci-guard hook: ${output}`
      );
      assert.ok(
        !output.includes("mcpServers"),
        `Expected no MCP servers (github removed by null): ${output}`
      );

      // Preview with --focus docs (no profile, uses base config)
      const docsOutput = execFileSync(
        yndBin,
        ["preview", previewDir, "-v", "claude", "--focus", "docs"],
        { encoding: "utf8" }
      );

      // Base config: has github MCP, has lint-gate hook
      assert.ok(
        docsOutput.includes("mcpServers"),
        `Expected MCP servers in base config: ${docsOutput}`
      );
      assert.ok(
        docsOutput.includes("lint-gate"),
        `Expected base lint-gate hook: ${docsOutput}`
      );

      fs.rmSync(previewDir, { recursive: true });
    } finally {
      fs.rmSync(dir, { recursive: true });
    }
  });

  it("local artifacts are discoverable via preview", { skip: skipIfNoYnh() }, () => {
    const dir = copyFixture();
    try {
      const result = resolve(dir);
      const content = { ...result.config, name: "acme-web-api", version: "1.0.0" };
      fs.writeFileSync(
        path.join(dir, ".harness.json"),
        JSON.stringify(content, null, 2) + "\n"
      );

      const output = execFileSync(
        yndBin,
        ["preview", dir, "-v", "claude"],
        { encoding: "utf8" }
      );

      // Should find rules/api-standards.md
      assert.ok(
        output.includes("api-standards.md"),
        `Expected api-standards rule: ${output}`
      );

      // Should find skills/lint-check/
      assert.ok(
        output.includes("lint-check"),
        `Expected lint-check skill: ${output}`
      );
    } finally {
      fs.rmSync(dir, { recursive: true });
    }
  });

  it("ynd validate accepts the generated config", { skip: skipIfNoYnh() }, () => {
    const dir = copyFixture();
    try {
      const result = resolve(dir);
      const content = { ...result.config, name: "acme-web-api", version: "1.0.0" };
      fs.writeFileSync(
        path.join(dir, ".harness.json"),
        JSON.stringify(content, null, 2) + "\n"
      );

      const output = execFileSync(yndBin, ["validate", dir], {
        encoding: "utf8",
      });
      assert.ok(output.includes("valid"), `Expected valid: ${output}`);
    } finally {
      fs.rmSync(dir, { recursive: true });
    }
  });

  it("custom outputDir via package.json", () => {
    const dir = copyFixture();
    try {
      // Patch package.json with custom outputDir
      const pkgPath = path.join(dir, "package.json");
      const pkg = JSON.parse(fs.readFileSync(pkgPath, "utf8"));
      pkg.ynh.outputDir = ".build/ai";
      fs.writeFileSync(pkgPath, JSON.stringify(pkg, null, 2));

      const result = resolve(dir);
      const harnessFile = writeHarness(result);

      assert.ok(
        harnessFile.includes(".build/ai"),
        `Expected custom output dir: ${harnessFile}`
      );
      assert.ok(fs.existsSync(harnessFile));
    } finally {
      fs.rmSync(dir, { recursive: true });
    }
  });
});
