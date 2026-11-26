package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shadowFixture makes a scratch home, a PATH directory holding an executable named "git", and a
// bare harness directory named "git" to install.
func shadowFixture(t *testing.T) (home, harnessDir string) {
	t.Helper()
	home = filepath.Join(t.TempDir(), ".ynh")
	t.Setenv("YNH_HOME", home)

	pathDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(pathDir, "git"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", pathDir+string(os.PathListSeparator)+filepath.Join(home, "bin"))

	harnessDir = filepath.Join(t.TempDir(), "git")
	if err := os.MkdirAll(harnessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(harnessDir, "AGENTS.md"), []byte("# git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return home, harnessDir
}

func TestCmdInstall_RefusesLauncherShadowingPathCommand(t *testing.T) {
	home, dir := shadowFixture(t)

	var out bytes.Buffer
	var err error
	captureStdout(t, &out, func() { err = cmdInstall([]string{dir}) })
	if err == nil {
		t.Fatal("install of a harness named git succeeded; want a refusal")
	}
	for _, want := range []string{"would shadow the command", "git", "--allow-shadow"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if _, statErr := os.Stat(filepath.Join(home, "bin", "git")); !os.IsNotExist(statErr) {
		t.Errorf("a launcher was written despite the refusal (stat err: %v)", statErr)
	}
}

func TestCmdInstall_AllowShadowInstallsTheLauncher(t *testing.T) {
	home, dir := shadowFixture(t)

	var out bytes.Buffer
	var err error
	captureStdout(t, &out, func() { err = cmdInstall([]string{dir, "--allow-shadow"}) })
	if err != nil {
		t.Fatalf("cmdInstall --allow-shadow: %v\n%s", err, out.String())
	}
	if _, statErr := os.Stat(filepath.Join(home, "bin", "git")); statErr != nil {
		t.Errorf("launcher not written with --allow-shadow: %v", statErr)
	}
}

func TestFindShadowedCommand_IgnoresTheLauncherDirectory(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "mine"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if got := findShadowedCommand("mine", bin); got != "" {
		t.Errorf("the launcher directory's own entry counts as shadowed: %q", got)
	}
}
