package io.ynh.maven.model;

import java.util.List;
import java.util.Map;

public class Profile {

    private Map<String, List<HookEntry>> hooks;
    private Map<String, MCPServer> mcpServers;

    public Map<String, List<HookEntry>> getHooks() { return hooks; }
    public void setHooks(Map<String, List<HookEntry>> hooks) { this.hooks = hooks; }

    public Map<String, MCPServer> getMcpServers() { return mcpServers; }
    public void setMcpServers(Map<String, MCPServer> mcpServers) { this.mcpServers = mcpServers; }
}
