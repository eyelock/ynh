package io.ynh.maven;

import io.ynh.maven.model.HarnessConfig;
import io.ynh.maven.util.BinaryLocator;
import io.ynh.maven.util.HarnessWriter;
import io.ynh.maven.util.VersionCheck;
import org.apache.maven.plugin.AbstractMojo;
import org.apache.maven.plugin.MojoExecutionException;
import org.apache.maven.plugins.annotations.Parameter;
import org.apache.maven.project.MavenProject;

import java.io.IOException;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;

/**
 * Base class for ynh Mojos. Handles binary location, config writing,
 * and common argument building.
 */
public abstract class AbstractYnhMojo extends AbstractMojo {

    @Parameter(defaultValue = "${project}", readonly = true, required = true)
    protected MavenProject project;

    /**
     * Vendor name (claude, codex, cursor). Translated to default_vendor in
     * the generated .harness.json.
     */
    @Parameter(property = "ynh.vendor")
    protected String vendor;

    /**
     * Inline harness configuration. Maps to the same schema as .harness.json
     * but expressed in XML. Written to a temp file and passed via --harness-file.
     */
    @Parameter
    protected HarnessConfig inlineConfig;

    /**
     * Output directory for the generated .harness.json.
     * Defaults to ${project.build.directory}/ynh (target/ynh/).
     */
    @Parameter(property = "ynh.outputDir", defaultValue = "${project.build.directory}/ynh")
    protected String outputDir;

    /**
     * Find the ynh binary.
     */
    protected String findYnhBinary() throws MojoExecutionException {
        try {
            String binary = BinaryLocator.find();
            getLog().debug("Using ynh binary: " + binary);
            VersionCheck.check(binary, getLog());
            return binary;
        } catch (IOException e) {
            throw new MojoExecutionException(e.getMessage(), e);
        }
    }

    /**
     * Write inline config to a .harness.json file and return the path.
     * Returns null if no inline config is set.
     */
    protected Path writeHarnessFile() throws MojoExecutionException {
        if (inlineConfig == null) {
            return null;
        }

        try {
            Path outDir = Path.of(outputDir);
            return HarnessWriter.write(inlineConfig, vendor, outDir);
        } catch (IOException e) {
            throw new MojoExecutionException("Failed to write .harness.json: " + e.getMessage(), e);
        }
    }

    /**
     * Build the base argument list for ynh run.
     */
    protected List<String> buildArgs() throws MojoExecutionException {
        List<String> args = new ArrayList<>();
        args.add("run");

        Path harnessFile = writeHarnessFile();
        if (harnessFile != null) {
            args.add("--harness-file");
            args.add(harnessFile.toString());
        }

        if (vendor != null && harnessFile == null) {
            args.add("-v");
            args.add(vendor);
        }

        return args;
    }
}
