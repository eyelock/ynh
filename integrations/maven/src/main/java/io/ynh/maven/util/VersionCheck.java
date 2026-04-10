package io.ynh.maven.util;

import org.apache.maven.plugin.MojoExecutionException;
import org.apache.maven.plugin.logging.Log;

import java.io.IOException;

/**
 * Check that the ynh binary meets the minimum version required.
 * Prevents cryptic DisallowUnknownFields errors when the binary
 * is too old to understand focus/profiles fields.
 */
public final class VersionCheck {

    static final String MIN_VERSION = "0.1.0";

    private VersionCheck() {}

    public static void check(String binaryPath, Log log) throws MojoExecutionException {
        String version;
        try {
            ProcessBuilder pb = new ProcessBuilder(binaryPath, "version");
            pb.redirectErrorStream(true);
            Process p = pb.start();
            version = new String(p.getInputStream().readAllBytes()).trim();
            if (p.waitFor() != 0) {
                log.warn("Could not determine ynh version — proceeding anyway");
                return;
            }
        } catch (IOException | InterruptedException e) {
            log.warn("Could not determine ynh version: " + e.getMessage());
            return;
        }

        // Dev builds: "dev-branch-sha" — skip check
        if (version.startsWith("dev-")) {
            log.debug("Dev build detected: " + version);
            return;
        }

        // Release builds: "0.1.0" or "v0.1.0"
        String clean = version.startsWith("v") ? version.substring(1) : version;
        if (!meetsMinimum(clean, MIN_VERSION)) {
            throw new MojoExecutionException(
                "ynh-maven-plugin requires ynh >= " + MIN_VERSION +
                ", but found " + version + ". Update with: brew upgrade ynh"
            );
        }

        log.debug("ynh version " + version + " meets minimum " + MIN_VERSION);
    }

    public static boolean meetsMinimum(String version, String minimum) {
        String[] v = version.split("\\.");
        String[] m = minimum.split("\\.");
        for (int i = 0; i < 3; i++) {
            int vi = i < v.length ? parseIntSafe(v[i]) : 0;
            int mi = i < m.length ? parseIntSafe(m[i]) : 0;
            if (vi > mi) return true;
            if (vi < mi) return false;
        }
        return true; // equal
    }

    private static int parseIntSafe(String s) {
        try {
            return Integer.parseInt(s);
        } catch (NumberFormatException e) {
            return 0;
        }
    }
}
