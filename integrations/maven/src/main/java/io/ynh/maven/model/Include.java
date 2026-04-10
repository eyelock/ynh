package io.ynh.maven.model;

import java.util.List;

public class Include {

    private String git;
    private String ref;
    private String path;
    private List<String> pick;

    public String getGit() { return git; }
    public void setGit(String git) { this.git = git; }

    public String getRef() { return ref; }
    public void setRef(String ref) { this.ref = ref; }

    public String getPath() { return path; }
    public void setPath(String path) { this.path = path; }

    public List<String> getPick() { return pick; }
    public void setPick(List<String> pick) { this.pick = pick; }
}
