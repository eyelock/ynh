package io.ynh.maven.util;

import org.apache.maven.plugin.MojoExecutionException;
import org.apache.maven.plugin.logging.Log;

import java.io.File;
import java.io.IOException;
import java.util.List;

/**
 * Execute the ynh binary, streaming stdout/stderr to Maven's logger.
 */
public final class ProcessRunner {

    private ProcessRunner() {}

    /**
     * Run ynh with the given arguments from the specified working directory.
     *
     * @param binary  path to the ynh binary
     * @param args    arguments to pass (e.g. ["run", "--harness-file", "/tmp/..."])
     * @param workDir working directory (typically project.getBasedir())
     * @param log     Maven logger for output
     * @return exit code from the process
     */
    public static int run(String binary, List<String> args, File workDir, Log log)
            throws MojoExecutionException {
        List<String> command = new java.util.ArrayList<>();
        command.add(binary);
        command.addAll(args);

        log.info("Executing: " + String.join(" ", command));

        try {
            ProcessBuilder pb = new ProcessBuilder(command);
            pb.directory(workDir);
            pb.inheritIO();

            Process process = pb.start();
            int exitCode = process.waitFor();

            if (exitCode != 0) {
                log.warn("ynh exited with code " + exitCode);
            }

            return exitCode;
        } catch (IOException e) {
            throw new MojoExecutionException("Failed to execute ynh: " + e.getMessage(), e);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new MojoExecutionException("ynh execution interrupted", e);
        }
    }
}
