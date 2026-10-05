// Package agentplugin reads and validates packages in the Agent Plugins
// format (https://agent-plugins.org, specification 1.0.0).
//
// An Agent Plugin is a directory with a closed root plugin.json, skills under
// skills/<name>/SKILL.md and MCP servers in a root mcp.json. Everything else
// a package carries is client-specific and lives under a reverse-domain
// namespace that other clients ignore.
//
// The package applies the specification's failure boundaries rather than a
// single pass/fail: a fatal manifest violation rejects the plugin, an
// invalid top-level mcp.json disables MCP for the plugin, and an invalid
// skill or server entry is skipped and reported while its siblings load.
// Every function that skips something says so through a Diagnostic, because
// §11.3 asks clients to report what they could not load.
//
// Section references in comments (§5.2, §7.2.1, ...) are to the 1.0.0
// specification text, which is authoritative where the schema disagrees.
package agentplugin
