// @ynh/cli — programmatic API
//
// Most users invoke via bin/ynh.js (CLI shim) or npm scripts.
// This module exposes the resolver/invoker for programmatic use.

const { resolve, findPackageJson } = require("./lib/resolve");
const { writeHarness } = require("./lib/write-harness");
const { invoke } = require("./lib/invoke");
const { getBinaryPath } = require("./lib/binary");

module.exports = { resolve, findPackageJson, writeHarness, invoke, getBinaryPath };
