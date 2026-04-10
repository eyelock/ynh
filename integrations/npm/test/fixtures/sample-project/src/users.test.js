const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const { getUsers, createUser } = require("./users");

describe("users", () => {
  it("getUsers returns array", () => {
    assert.ok(Array.isArray(getUsers()));
  });

  it("createUser requires name and email", () => {
    assert.throws(() => createUser({}), /name and email are required/);
  });
});
