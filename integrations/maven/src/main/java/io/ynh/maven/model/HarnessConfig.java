package io.ynh.maven.model;

import java.util.List;
import java.util.Map;

/**
 * Top-level harness configuration, mirroring .harness.json schema.
 * Used as the {@code <inlineConfig>} parameter in pom.xml.
 */
public class HarnessConfig {

    private String defaultVendor;
    private List<Include> includes;
    private List<Delegate> delegatesTo;
    private Map<String, List<HookEntry>> hooks;
    private Map<String, MCPServer> mcpServers;
    private Map<String, Profile> profiles;
    private Map<String, Focus> focus;

    public String getDefaultVendor() { return defaultVendor; }
    public void setDefaultVendor(String defaultVendor) { this.defaultVendor = defaultVendor; }

    public List<Include> getIncludes() { return includes; }
    public void setIncludes(List<Include> includes) { this.includes = includes; }

    public List<Delegate> getDelegatesTo() { return delegatesTo; }
    public void setDelegatesTo(List<Delegate> delegatesTo) { this.delegatesTo = delegatesTo; }

    public Map<String, List<HookEntry>> getHooks() { return hooks; }
    public void setHooks(Map<String, List<HookEntry>> hooks) { this.hooks = hooks; }

    public Map<String, MCPServer> getMcpServers() { return mcpServers; }
    public void setMcpServers(Map<String, MCPServer> mcpServers) { this.mcpServers = mcpServers; }

    public Map<String, Profile> getProfiles() { return profiles; }
    public void setProfiles(Map<String, Profile> profiles) { this.profiles = profiles; }

    public Map<String, Focus> getFocus() { return focus; }
    public void setFocus(Map<String, Focus> focus) { this.focus = focus; }
}
