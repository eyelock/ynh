# Agent Plugins schemas

The files under `1.0.0/` are the official JSON Schemas published by the Agent
Plugins project, copied verbatim from
https://github.com/agentplugins/agent-plugins-spec/tree/main/schemas/1.0.0
(also served at https://agent-plugins.org/schemas/1.0.0/). They are licensed
under the Apache License 2.0 by the Agent Plugins project; see
https://github.com/agentplugins/agent-plugins-spec/blob/main/LICENSE.md.

They are embedded rather than fetched because the specification forbids a
client from retrieving a schema while loading a plugin (§5.2, §7.2.1). Do not
edit them: a change to either schema is a new specification release with a
new canonical identifier, and the copy here must stay byte-identical to what
that identifier names.
