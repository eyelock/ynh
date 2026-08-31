// Package schema carries the ynh JSON Schemas as an embedded filesystem.
//
// The schemas live here, under docs/, rather than somewhere more
// conventional for one reason: this directory is what GitHub Pages publishes,
// so it is the only location where the files can be both the compiled-in
// source of truth and the documents their own $id URLs resolve to.
//
// The alternative — a copy under internal/ plus a published mirror — is what
// this package exists to prevent. The repo carried three such copies; the
// path-traversal guards in plugin.schema.json and marketplace.schema.json
// existed in the published copy and not in the one ynd enforced, so the
// documented guarantee was never applied. A parity test can detect that
// class of bug. One directory makes it unrepresentable.
//
// Consumers: internal/clischema compiles the CLI response schemas from FS;
// cmd/ynd validates author-written manifests against the author-facing ones.
package schema

import "embed"

// FS holds every schema, rooted at this directory: "cli/<command>.schema.json",
// "shared/<name>.schema.json", "agent/<name>.schema.json", and the author-facing
// schemas at the root.
//
// agent/ is the trajectory stream rather than a command response, so
// internal/clischema does not compile it — that package filters to cli/ and
// shared/. It is embedded here so a test can validate it against what the
// emitter really produces, and so its $id URL resolves on the published site.
//
//go:embed all:cli all:shared all:agent *.schema.json
var FS embed.FS
