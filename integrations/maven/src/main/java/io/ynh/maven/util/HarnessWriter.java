package io.ynh.maven.util;

import io.ynh.maven.model.*;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.stream.Collectors;

/**
 * Serialize a HarnessConfig to a .harness.json file.
 *
 * Handles the field translations:
 * - vendor (XML-natural) → default_vendor (ynh schema)
 * - camelCase (Java) → snake_case (JSON)
 *
 * Uses manual JSON serialization to avoid adding a dependency on Jackson/Gson.
 * The harness schema is small enough that this is practical.
 */
public final class HarnessWriter {

    private HarnessWriter() {}

    /**
     * Write the config to a .harness.json file in the given directory.
     * Creates the directory if it doesn't exist.
     *
     * @return the path to the written file
     */
    public static Path write(HarnessConfig config, String vendor, Path outputDir) throws IOException {
        Files.createDirectories(outputDir);
        Path file = outputDir.resolve(".harness.json");

        Map<String, Object> json = new LinkedHashMap<>();

        if (vendor != null) {
            json.put("default_vendor", vendor);
        } else if (config.getDefaultVendor() != null) {
            json.put("default_vendor", config.getDefaultVendor());
        }

        if (config.getIncludes() != null && !config.getIncludes().isEmpty()) {
            json.put("includes", config.getIncludes().stream()
                .map(HarnessWriter::includeToMap)
                .collect(Collectors.toList()));
        }

        if (config.getDelegatesTo() != null && !config.getDelegatesTo().isEmpty()) {
            json.put("delegates_to", config.getDelegatesTo().stream()
                .map(HarnessWriter::delegateToMap)
                .collect(Collectors.toList()));
        }

        if (config.getHooks() != null && !config.getHooks().isEmpty()) {
            json.put("hooks", hooksToMap(config.getHooks()));
        }

        if (config.getMcpServers() != null && !config.getMcpServers().isEmpty()) {
            json.put("mcp_servers", mcpServersToMap(config.getMcpServers()));
        }

        if (config.getProfiles() != null && !config.getProfiles().isEmpty()) {
            json.put("profiles", profilesToMap(config.getProfiles()));
        }

        if (config.getFocus() != null && !config.getFocus().isEmpty()) {
            json.put("focus", focusToMap(config.getFocus()));
        }

        Files.writeString(file, toJson(json) + "\n");
        return file;
    }

    // --- Map converters (Java objects → JSON-compatible maps with snake_case keys) ---

    private static Map<String, Object> includeToMap(Include inc) {
        Map<String, Object> m = new LinkedHashMap<>();
        m.put("git", inc.getGit());
        if (inc.getRef() != null) m.put("ref", inc.getRef());
        if (inc.getPath() != null) m.put("path", inc.getPath());
        if (inc.getPick() != null) m.put("pick", inc.getPick());
        return m;
    }

    private static Map<String, Object> delegateToMap(Delegate del) {
        Map<String, Object> m = new LinkedHashMap<>();
        m.put("git", del.getGit());
        if (del.getRef() != null) m.put("ref", del.getRef());
        if (del.getPath() != null) m.put("path", del.getPath());
        return m;
    }

    private static Map<String, Object> hooksToMap(Map<String, List<HookEntry>> hooks) {
        Map<String, Object> m = new LinkedHashMap<>();
        for (var entry : hooks.entrySet()) {
            m.put(entry.getKey(), entry.getValue().stream()
                .map(h -> {
                    Map<String, Object> hm = new LinkedHashMap<>();
                    if (h.getMatcher() != null) hm.put("matcher", h.getMatcher());
                    hm.put("command", h.getCommand());
                    return hm;
                })
                .collect(Collectors.toList()));
        }
        return m;
    }

    private static Map<String, Object> mcpServersToMap(Map<String, MCPServer> servers) {
        Map<String, Object> m = new LinkedHashMap<>();
        for (var entry : servers.entrySet()) {
            MCPServer s = entry.getValue();
            Map<String, Object> sm = new LinkedHashMap<>();
            if (s.getCommand() != null) sm.put("command", s.getCommand());
            if (s.getArgs() != null) sm.put("args", s.getArgs());
            if (s.getEnv() != null) sm.put("env", s.getEnv());
            if (s.getUrl() != null) sm.put("url", s.getUrl());
            if (s.getHeaders() != null) sm.put("headers", s.getHeaders());
            m.put(entry.getKey(), sm);
        }
        return m;
    }

    private static Map<String, Object> profilesToMap(Map<String, Profile> profiles) {
        Map<String, Object> m = new LinkedHashMap<>();
        for (var entry : profiles.entrySet()) {
            Profile p = entry.getValue();
            Map<String, Object> pm = new LinkedHashMap<>();
            if (p.getHooks() != null) pm.put("hooks", hooksToMap(p.getHooks()));
            if (p.getMcpServers() != null) pm.put("mcp_servers", mcpServersToMap(p.getMcpServers()));
            m.put(entry.getKey(), pm);
        }
        return m;
    }

    private static Map<String, Object> focusToMap(Map<String, Focus> focuses) {
        Map<String, Object> m = new LinkedHashMap<>();
        for (var entry : focuses.entrySet()) {
            Focus f = entry.getValue();
            Map<String, Object> fm = new LinkedHashMap<>();
            if (f.getProfile() != null) fm.put("profile", f.getProfile());
            fm.put("prompt", f.getPrompt());
            m.put(entry.getKey(), fm);
        }
        return m;
    }

    // --- Minimal JSON serializer (no external deps) ---

    @SuppressWarnings("unchecked")
    public static String toJson(Object obj) {
        if (obj == null) return "null";
        if (obj instanceof String s) return "\"" + escapeJson(s) + "\"";
        if (obj instanceof Number n) return n.toString();
        if (obj instanceof Boolean b) return b.toString();
        if (obj instanceof List<?> list) {
            return "[" + list.stream()
                .map(HarnessWriter::toJson)
                .collect(Collectors.joining(", ")) + "]";
        }
        if (obj instanceof Map<?, ?> map) {
            return "{" + ((Map<String, Object>) map).entrySet().stream()
                .map(e -> "\"" + escapeJson(e.getKey()) + "\": " + toJson(e.getValue()))
                .collect(Collectors.joining(", ")) + "}";
        }
        return "\"" + escapeJson(obj.toString()) + "\"";
    }

    private static String escapeJson(String s) {
        return s.replace("\\", "\\\\")
                .replace("\"", "\\\"")
                .replace("\n", "\\n")
                .replace("\r", "\\r")
                .replace("\t", "\\t");
    }
}
