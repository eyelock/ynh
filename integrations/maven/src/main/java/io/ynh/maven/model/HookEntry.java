package io.ynh.maven.model;

public class HookEntry {

    private String matcher;
    private String command;

    public String getMatcher() { return matcher; }
    public void setMatcher(String matcher) { this.matcher = matcher; }

    public String getCommand() { return command; }
    public void setCommand(String command) { this.command = command; }
}
