package io.ynh.maven.model;

/**
 * A named combination of profile + prompt for repeatable AI execution.
 */
public class Focus {

    private String profile;
    private String prompt;

    public String getProfile() { return profile; }
    public void setProfile(String profile) { this.profile = profile; }

    public String getPrompt() { return prompt; }
    public void setPrompt(String prompt) { this.prompt = prompt; }
}
