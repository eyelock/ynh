package io.ynh.maven.util;

import java.io.File;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;

/**
 * Locate the ynh binary.
 *
 * Resolution order:
 * 1. ynh.binaryPath system property (explicit override)
 * 2. ynh on PATH
 * 3. ~/.ynh/bin/ynh (Homebrew/make install location)
 */
public final class BinaryLocator {

    private BinaryLocator() {}

    public static String find() throws IOException {
        // 1. Explicit path
        String explicit = System.getProperty("ynh.binaryPath");
        if (explicit != null && new File(explicit).canExecute()) {
            return explicit;
        }

        // 2. PATH lookup
        String pathResult = findOnPath("ynh");
        if (pathResult != null) {
            return pathResult;
        }

        // 3. Default install location
        Path defaultPath = Path.of(System.getProperty("user.home"), ".ynh", "bin", "ynh");
        if (Files.isExecutable(defaultPath)) {
            return defaultPath.toString();
        }

        throw new IOException(
            "ynh binary not found. Install via: brew install eyelock/tap/ynh\n" +
            "Or set -Dynh.binaryPath=/path/to/ynh"
        );
    }

    private static String findOnPath(String name) {
        try {
            ProcessBuilder pb = new ProcessBuilder("which", name);
            pb.redirectErrorStream(true);
            Process p = pb.start();
            String result = new String(p.getInputStream().readAllBytes()).trim();
            if (p.waitFor() == 0 && !result.isEmpty()) {
                return result;
            }
        } catch (Exception ignored) {
            // which not available or failed
        }
        return null;
    }
}
