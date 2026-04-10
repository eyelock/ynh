const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const { meetsMinimum } = require("../lib/version");

describe("meetsMinimum", () => {
  it("equal versions pass", () => {
    assert.ok(meetsMinimum("0.1.0", "0.1.0"));
  });

  it("higher major passes", () => {
    assert.ok(meetsMinimum("1.0.0", "0.1.0"));
  });

  it("higher minor passes", () => {
    assert.ok(meetsMinimum("0.2.0", "0.1.0"));
  });

  it("higher patch passes", () => {
    assert.ok(meetsMinimum("0.1.1", "0.1.0"));
  });

  it("lower major fails", () => {
    assert.ok(!meetsMinimum("0.0.9", "0.1.0"));
  });

  it("lower minor fails", () => {
    assert.ok(!meetsMinimum("0.0.5", "0.1.0"));
  });

  it("handles missing patch", () => {
    assert.ok(meetsMinimum("1.0", "0.1.0"));
  });
});
