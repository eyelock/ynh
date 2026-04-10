package io.ynh.maven;

import io.ynh.maven.util.ProcessRunner;
import org.apache.maven.plugin.MojoExecutionException;
import org.apache.maven.plugins.annotations.Mojo;
import org.apache.maven.plugins.annotations.Parameter;

import java.util.List;

/**
 * Interactive ynh session. Launches the vendor CLI for interactive use.
 *
 * Usage: mvn ynh:run
 */
@Mojo(name = "run", requiresProject = true)
public class YnhRunMojo extends AbstractYnhMojo {

    /** Named profile to activate. */
    @Parameter(property = "ynh.profile")
    private String profile;

    @Override
    public void execute() throws MojoExecutionException {
        List<String> args = buildArgs();

        if (profile != null) {
            args.add("--profile");
            args.add(profile);
        }

        String binary = findYnhBinary();
        ProcessRunner.run(binary, args, project.getBasedir(), getLog());
    }
}
