package io.ynh.maven;

import io.ynh.maven.util.ProcessRunner;
import org.apache.maven.plugin.MojoExecutionException;
import org.apache.maven.plugins.annotations.LifecyclePhase;
import org.apache.maven.plugins.annotations.Mojo;
import org.apache.maven.plugins.annotations.Parameter;

import java.util.List;

/**
 * Non-interactive execution with a focus or prompt. Designed for CI.
 *
 * Usage:
 *   mvn ynh:execute -Dynh.focus=review
 *   mvn ynh:execute -Dynh.prompt="Review staged changes"
 *
 * Or bound to a lifecycle phase:
 *   <execution>
 *     <phase>verify</phase>
 *     <goals><goal>execute</goal></goals>
 *     <configuration>
 *       <focus>security</focus>
 *     </configuration>
 *   </execution>
 */
@Mojo(name = "execute", defaultPhase = LifecyclePhase.NONE, requiresProject = true)
public class YnhExecuteMojo extends AbstractYnhMojo {

    /** Named focus entry from harness config. */
    @Parameter(property = "ynh.focus")
    private String focus;

    /** Direct prompt (alternative to focus). */
    @Parameter(property = "ynh.prompt")
    private String prompt;

    /** Skip execution. Useful for conditional CI: -DskipAiReview=true */
    @Parameter(property = "ynh.skip", defaultValue = "false")
    private boolean skip;

    @Override
    public void execute() throws MojoExecutionException {
        if (skip) {
            getLog().info("ynh:execute skipped");
            return;
        }

        if (focus != null && prompt != null) {
            throw new MojoExecutionException(
                "Cannot specify both <focus> and <prompt> — focus already includes a prompt"
            );
        }

        List<String> args = buildArgs();

        if (focus != null) {
            args.add("--focus");
            args.add(focus);
        }

        if (prompt != null) {
            args.add("--");
            args.add(prompt);
        }

        String binary = findYnhBinary();
        int exitCode = ProcessRunner.run(binary, args, project.getBasedir(), getLog());

        if (exitCode != 0) {
            throw new MojoExecutionException("ynh exited with code " + exitCode);
        }
    }
}
