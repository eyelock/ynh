package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
)

// gitRepo makes a real git repo in dir whose manifest declares one sensor.
func gitRepo(t *testing.T, dir, sensor string) {
	t.Helper()
	hj := map[string]any{
		"name": "inc", "version": "1.0.0",
		"sensors": map[string]any{sensor: map[string]any{
			"category":  "maintainability",
			"tolerance": "blocking",
			"source":    map[string]any{"command": "true"},
			"output":    map[string]any{"format": "text", "channel": "stdout+exit"},
		}},
	}
	data, err := json.Marshal(hj)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePluginJSONFile(dir, data); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"add", "-A"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// TestBakeIncludeCache builds the include cache the way `ynh image` does, lays
// the build context out as the Dockerfile does, removes every source and then
// reads the sensors the way `ynh check` does inside the container.
func TestBakeIncludeCache(t *testing.T) {
	optOut := false
	tests := []struct {
		name string
		// setup returns the includes and the harness dir the build loads from.
		setup func(t *testing.T, root string) (harnessDir string, incs []harness.Include, sources []string)
		// profileInc, when set, is declared by a profile rather than the base.
		profileOnly bool
		wantSensors []string
	}{
		{
			name: "remote url (file://)",
			setup: func(t *testing.T, root string) (string, []harness.Include, []string) {
				src := filepath.Join(root, "remote")
				gitRepo(t, src, "remote-sensor")
				hd := filepath.Join(root, "work", "h")
				mustMkdir(t, hd)
				return hd, []harness.Include{{GitSource: harness.GitSource{Git: "file://" + src}}}, []string{src}
			},
			wantSensors: []string{"remote-sensor"},
		},
		{
			name: "relative to the harness (../..)",
			setup: func(t *testing.T, root string) (string, []harness.Include, []string) {
				repo := filepath.Join(root, "repo")
				gitRepo(t, repo, "rel-sensor")
				hd := filepath.Join(repo, "harnesses", "h")
				mustMkdir(t, hd)
				return hd, []harness.Include{{GitSource: harness.GitSource{Git: "../.."}}}, []string{repo}
			},
			wantSensors: []string{"rel-sensor"},
		},
		{
			name: "relative to the harness (.)",
			setup: func(t *testing.T, root string) (string, []harness.Include, []string) {
				repo := filepath.Join(root, "repo")
				gitRepo(t, repo, "dot-sensor")
				return repo, []harness.Include{{GitSource: harness.GitSource{Git: "."}}}, []string{repo}
			},
			wantSensors: []string{"dot-sensor"},
		},
		{
			name: "include with sensors false is still baked",
			setup: func(t *testing.T, root string) (string, []harness.Include, []string) {
				src := filepath.Join(root, "remote")
				gitRepo(t, src, "ignored")
				hd := filepath.Join(root, "work", "h")
				mustMkdir(t, hd)
				return hd, []harness.Include{{GitSource: harness.GitSource{Git: "file://" + src}, Sensors: &optOut}}, []string{src}
			},
			wantSensors: nil,
		},
		{
			name: "include a profile adds",
			setup: func(t *testing.T, root string) (string, []harness.Include, []string) {
				src := filepath.Join(root, "remote")
				gitRepo(t, src, "profile-sensor")
				hd := filepath.Join(root, "work", "h")
				mustMkdir(t, hd)
				return hd, []harness.Include{{GitSource: harness.GitSource{Git: "file://" + src}}}, []string{src}
			},
			profileOnly: true,
			wantSensors: []string{"profile-sensor"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("YNH_HOME", filepath.Join(root, "buildhome"))
			harnessDir, incs, sources := tt.setup(t, root)

			p := &harness.Harness{Name: "h", Dir: harnessDir}
			if tt.profileOnly {
				meta := make([]plugin.IncludeMeta, len(incs))
				for i, inc := range incs {
					meta[i] = plugin.IncludeMeta{Git: inc.Git, Ref: inc.Ref, Sensors: inc.Sensors}
				}
				p.Profiles = map[string]plugin.Profile{"ci": {Includes: meta}}
			} else {
				p.Includes = incs
			}

			// Build time: what cmdImageTo does before baking.
			if _, err := resolver.Resolve(&harness.Harness{Dir: harnessDir, Includes: incs}, nil); err != nil {
				t.Fatalf("resolve: %v", err)
			}
			all := p.Includes
			if tt.profileOnly {
				var err error
				if all, err = profileOnlyIncludes(p); err != nil {
					t.Fatal(err)
				}
			}

			// The image's layout, rooted in the test's temp dir. runtimeDir is
			// where the Dockerfile puts the harness, so it is what a relative
			// include resolves against at runtime.
			imgHome := filepath.Join(root, "image", ".ynh")
			runtimeDir := filepath.Join(imgHome, "harnesses", "local--h")
			ctxHome := filepath.Join(root, "ctx", "home")
			mustMkdir(t, ctxHome) // WriteSchemaVersion makes it in the real build
			if err := bakeIncludeCache(p, all, ctxHome, runtimeDir); err != nil {
				t.Fatalf("bake: %v", err)
			}

			// The entry is under the runtime key, with a .git, and it is the
			// only entry.
			for _, inc := range all {
				key := resolver.CacheEntryName(resolver.GitSourceURL(inc.Git, runtimeDir), inc.Ref)
				if _, err := os.Stat(filepath.Join(ctxHome, "cache", key, ".git")); err != nil {
					t.Errorf("no valid cache entry for %s under runtime key %s: %v", inc.Git, key, err)
				}
			}
			entries, _ := os.ReadDir(filepath.Join(ctxHome, "cache"))
			if len(entries) != len(all) {
				t.Errorf("cache has %d entries, want %d", len(entries), len(all))
			}

			// Runtime: COPY home/ and harness/ where the Dockerfile puts them,
			// then take every source away so nothing can be fetched.
			if err := copyTreeWithGit(ctxHome, imgHome); err != nil {
				t.Fatal(err)
			}
			if err := assembler.CopyDir(harnessDir, runtimeDir); err != nil {
				t.Fatal(err)
			}
			for _, s := range sources {
				mustUnder(t, root, s)
				if err := os.RemoveAll(s); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("YNH_HOME", imgHome)

			rt := &harness.Harness{Name: "h", Dir: runtimeDir, Includes: all}
			if resolver.IsOwnRepoSource(all[0].GitSource, runtimeDir) {
				t.Error("the harness copy in the image must not count as the include's own repo")
			}
			merged, err := resolver.WithIncludedSensors(rt)
			if err != nil {
				t.Fatalf("sensor merge inside the image: %v", err)
			}
			for _, name := range tt.wantSensors {
				if _, ok := merged.Sensors[name]; !ok {
					t.Errorf("sensor %q not found in the image; have %v", name, merged.Sensors)
				}
			}
			if len(tt.wantSensors) == 0 && len(merged.Sensors) != 0 {
				t.Errorf("want no sensors, got %v", merged.Sensors)
			}

			// What `ynh agent run` does first: must hit the cache, not fetch.
			res, err := resolver.ResolveFromCache(rt, nil)
			if err != nil {
				t.Fatalf("resolve from cache inside the image: %v", err)
			}
			for _, r := range res {
				if r.Cloned {
					t.Errorf("%s was fetched at runtime, want a cache hit", r.Source)
				}
			}
		})
	}
}

func TestBakeIncludeCache_SkipsLocalAndRefusesCollision(t *testing.T) {
	root := t.TempDir()
	t.Setenv("YNH_HOME", filepath.Join(root, "home"))
	p := &harness.Harness{Name: "h", Dir: filepath.Join(root, "h")}
	local := []harness.Include{{GitSource: harness.GitSource{Local: "vendored/x"}}}
	home := filepath.Join(root, "ctx")
	if err := bakeIncludeCache(p, local, home, "/home/ynh/.ynh/harnesses/local--h"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "cache")); !os.IsNotExist(err) {
		t.Errorf("a local include must not create a cache: %v", err)
	}
	// Never resolved, so not in the cache: an error, not a silent skip.
	remote := []harness.Include{{GitSource: harness.GitSource{Git: "file://" + filepath.Join(root, "nope")}}}
	if err := bakeIncludeCache(p, remote, home, "/home/ynh/.ynh/harnesses/local--h"); err == nil {
		t.Error("want an error for an include that is not cached")
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

// mustUnder hard-asserts that a path about to be deleted is inside the test's
// temp directory.
func mustUnder(t *testing.T, root, path string) {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || len(rel) >= 2 && rel[:2] == ".." {
		t.Fatalf("refusing to delete %s: not under %s", path, root)
	}
}
