// Integration test: resolve → write-harness → ynd validate.
// Requires ynh/ynd binary on PATH (skips if not available).

const { describe, it, before } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("fs");
const path = require("path");
const os = require("os");
const { execFileSync } = require("child_process");
const { resolve } = require("../lib/resolve");
const { writeHarness } = require("../lib/write-harness");

let yndBin;

before(() => {
  try {
    const ynhBin = execFileSync("which", ["ynh"], { encoding: "utf8" }).trim();
    yndBin = path.join(path.dirname(ynhBin), "ynd");
  } catch {
    // Not on PATH — integration tests will skip
  }
});

function skipIfNoYnh() {
  return !yndBin;
}

function withProject(ynhBlock, fn) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "ynh-integ-"));
  fs.mkdirSync(path.join(dir, "node_modules"), { recursive: true });
  fs.writeFileSync(
    path.join(dir, "package.json"),
    JSON.stringify({
      name: "test-project",
      version: "1.0.0",
      ynh: ynhBlock,
    })
  );
  try {
    fn(dir);
  } finally {
    fs.rmSync(dir, { recursive: true });
  }
}

describe("integration", () => {
  it("resolve translates vendor and omits name", () => {
    withProject(
      {
        vendor: "claude",
        hooks: { before_tool: [{ command: "echo lint", matcher: "Write" }] },
        focus: { review: { prompt: "Review staged changes" } },
      },
      (dir) => {
        const result = resolve(dir);
        assert.notEqual(result, null);
        assert.equal(result.config.default_vendor, "claude");
        assert.equal(result.config.vendor, undefined);

        const harnessFile = writeHarness(result);
        assert.ok(fs.existsSync(harnessFile));
        assert.ok(
          harnessFile.includes("node_modules/.cache/ynh"),
          `Expected cache path: ${harnessFile}`
        );

        const content = JSON.parse(fs.readFileSync(harnessFile, "utf8"));
        assert.equal(content.default_vendor, "claude");
        assert.equal(content.name, undefined);
      }
    );
  });

  it("end-to-end: ynd validate accepts generated config", { skip: skipIfNoYnh() }, () => {
    withProject(
      {
        vendor: "claude",
        focus: { docs: { prompt: "Generate docs" } },
      },
      (dir) => {
        const result = resolve(dir);
        const harnessFile = writeHarness(result);

        // ynd validate needs a dir with .harness.json + name/version
        const validateDir = fs.mkdtempSync(path.join(os.tmpdir(), "ynh-val-"));
        const content = JSON.parse(fs.readFileSync(harnessFile, "utf8"));
        content.name = "test-project";
        content.version = "1.0.0";
        fs.writeFileSync(
          path.join(validateDir, ".harness.json"),
          JSON.stringify(content, null, 2) + "\n"
        );

        const output = execFileSync(yndBin, ["validate", validateDir], {
          encoding: "utf8",
        });
        assert.ok(output.includes("valid"), `Expected 'valid': ${output}`);
        fs.rmSync(validateDir, { recursive: true });
      }
    );
  });

  it("artifacts alongside package.json visible in preview", { skip: skipIfNoYnh() }, () => {
    withProject({ vendor: "claude" }, (dir) => {
      // Add a local rule
      fs.mkdirSync(path.join(dir, "rules"));
      fs.writeFileSync(
        path.join(dir, "rules", "no-console.md"),
        "Never use console.log in production.\n"
      );

      // Write .harness.json into the project dir (for ynd preview)
      const result = resolve(dir);
      const content = { ...result.config, name: "test-project", version: "1.0.0" };
      fs.writeFileSync(
        path.join(dir, ".harness.json"),
        JSON.stringify(content, null, 2) + "\n"
      );

      const output = execFileSync(yndBin, ["preview", dir, "-v", "claude"], {
        encoding: "utf8",
      });
      assert.ok(output.includes("no-console.md"), `Expected rule: ${output}`);
    });
  });
});
