package io.ynh.maven;

import io.ynh.maven.model.*;
import io.ynh.maven.util.HarnessWriter;
import org.junit.Test;
import org.junit.Rule;
import org.junit.rules.TemporaryFolder;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.Map;

import static org.junit.Assert.*;

public class HarnessWriterTest {

    @Rule
    public TemporaryFolder tempDir = new TemporaryFolder();

    @Test
    public void writesValidJson() throws IOException {
        HarnessConfig config = new HarnessConfig();

        Focus review = new Focus();
        review.setProfile("ci");
        review.setPrompt("Review staged changes");

        config.setFocus(Map.of("review", review));

        Path file = HarnessWriter.write(config, "claude", tempDir.getRoot().toPath());

        assertTrue(Files.exists(file));
        String content = Files.readString(file);
        assertTrue(content.contains("\"default_vendor\": \"claude\""));
        assertTrue(content.contains("\"prompt\": \"Review staged changes\""));
        assertTrue(content.contains("\"profile\": \"ci\""));
        assertFalse(content.contains("\"vendor\"")); // vendor → default_vendor
    }

    @Test
    public void translatesVendorToDefaultVendor() throws IOException {
        HarnessConfig config = new HarnessConfig();

        Path file = HarnessWriter.write(config, "codex", tempDir.getRoot().toPath());
        String content = Files.readString(file);

        assertTrue(content.contains("\"default_vendor\": \"codex\""));
    }

    @Test
    public void writesHooksWithSnakeCaseKeys() throws IOException {
        HarnessConfig config = new HarnessConfig();

        HookEntry hook = new HookEntry();
        hook.setMatcher("Write");
        hook.setCommand("./scripts/lint.sh");

        config.setHooks(Map.of("before_tool", List.of(hook)));

        Path file = HarnessWriter.write(config, null, tempDir.getRoot().toPath());
        String content = Files.readString(file);

        assertTrue(content.contains("\"before_tool\""));
        assertTrue(content.contains("\"matcher\": \"Write\""));
        assertTrue(content.contains("\"command\": \"./scripts/lint.sh\""));
    }

    @Test
    public void writesMcpServersWithSnakeCaseKeys() throws IOException {
        HarnessConfig config = new HarnessConfig();

        MCPServer server = new MCPServer();
        server.setCommand("npx");
        server.setArgs(List.of("-y", "@modelcontextprotocol/server-github"));
        server.setEnv(Map.of("GITHUB_TOKEN", "${GITHUB_TOKEN}"));

        config.setMcpServers(Map.of("github", server));

        Path file = HarnessWriter.write(config, null, tempDir.getRoot().toPath());
        String content = Files.readString(file);

        assertTrue(content.contains("\"mcp_servers\""));
        assertTrue(content.contains("\"github\""));
        assertTrue(content.contains("\"command\": \"npx\""));
    }

    @Test
    public void writesProfiles() throws IOException {
        HarnessConfig config = new HarnessConfig();

        HookEntry ciHook = new HookEntry();
        ciHook.setCommand("./scripts/strict-lint.sh");
        ciHook.setMatcher("Bash");

        Profile ci = new Profile();
        ci.setHooks(Map.of("before_tool", List.of(ciHook)));

        config.setProfiles(Map.of("ci", ci));

        Path file = HarnessWriter.write(config, null, tempDir.getRoot().toPath());
        String content = Files.readString(file);

        assertTrue(content.contains("\"profiles\""));
        assertTrue(content.contains("\"ci\""));
        assertTrue(content.contains("\"before_tool\""));
    }

    @Test
    public void writesIncludes() throws IOException {
        HarnessConfig config = new HarnessConfig();

        Include inc = new Include();
        inc.setGit("github.com/myorg/shared-skills");
        inc.setRef("v2.1.0");
        inc.setPath("packages/backend");
        inc.setPick(List.of("skills/deploy"));

        config.setIncludes(List.of(inc));

        Path file = HarnessWriter.write(config, null, tempDir.getRoot().toPath());
        String content = Files.readString(file);

        assertTrue(content.contains("\"includes\""));
        assertTrue(content.contains("\"git\": \"github.com/myorg/shared-skills\""));
        assertTrue(content.contains("\"ref\": \"v2.1.0\""));
        assertTrue(content.contains("\"path\": \"packages/backend\""));
    }

    @Test
    public void doesNotWriteProjectRoot() throws IOException {
        HarnessConfig config = new HarnessConfig();
        config.setFocus(Map.of("review", new Focus() {{ setPrompt("Review"); }}));

        Path outDir = tempDir.newFolder("target", "ynh").toPath();
        Path file = HarnessWriter.write(config, "claude", outDir);

        // File should be in the output dir, not project root
        assertTrue(file.toString().contains("target"));
        assertEquals(".harness.json", file.getFileName().toString());
    }

    @Test
    public void toJsonHandlesPrimitives() {
        assertEquals("\"hello\"", HarnessWriter.toJson("hello"));
        assertEquals("42", HarnessWriter.toJson(42));
        assertEquals("true", HarnessWriter.toJson(true));
        assertEquals("null", HarnessWriter.toJson(null));
    }

    @Test
    public void toJsonEscapesSpecialChars() {
        assertEquals("\"line1\\nline2\"", HarnessWriter.toJson("line1\nline2"));
        assertEquals("\"tab\\there\"", HarnessWriter.toJson("tab\there"));
        assertEquals("\"with \\\"quotes\\\"\"", HarnessWriter.toJson("with \"quotes\""));
    }

    @Test
    public void fullConfig() throws IOException {
        HarnessConfig config = new HarnessConfig();

        Include inc = new Include();
        inc.setGit("github.com/myorg/skills");
        inc.setRef("v1.0.0");
        config.setIncludes(List.of(inc));

        HookEntry hook = new HookEntry();
        hook.setCommand("echo lint");
        hook.setMatcher("Write");
        config.setHooks(Map.of("before_tool", List.of(hook)));

        MCPServer server = new MCPServer();
        server.setCommand("npx");
        server.setArgs(List.of("-y", "server-github"));
        config.setMcpServers(Map.of("github", server));

        Focus review = new Focus();
        review.setProfile("ci");
        review.setPrompt("Review code");
        config.setFocus(Map.of("review", review));

        Path file = HarnessWriter.write(config, "claude", tempDir.getRoot().toPath());
        String content = Files.readString(file);

        // Verify all sections present
        assertTrue(content.contains("\"default_vendor\": \"claude\""));
        assertTrue(content.contains("\"includes\""));
        assertTrue(content.contains("\"hooks\""));
        assertTrue(content.contains("\"mcp_servers\""));
        assertTrue(content.contains("\"focus\""));

        // Verify it ends with newline
        assertTrue(content.endsWith("\n"));
    }
}
