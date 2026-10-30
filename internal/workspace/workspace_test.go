package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tree makes a fake machine under t.TempDir(): a home, a harness and a project
// beneath it, and siblings. Every path is symlink-resolved so comparisons hold
// on macOS, where the temp dir is itself behind a symlink.
type tree struct {
	root, home, harness, project, sibling, outside string
}

func newTree(t *testing.T) tree {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tr := tree{root: root}
	tr.home = filepath.Join(root, "home")
	tr.harness = filepath.Join(tr.home, "work", "seat")
	tr.project = filepath.Join(tr.home, "work", "project")
	tr.sibling = filepath.Join(tr.home, "work", "ynh")
	tr.outside = filepath.Join(root, "elsewhere")
	for _, d := range []string{tr.harness, tr.project, tr.sibling, tr.outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return tr
}

func TestRefusal(t *testing.T) {
	tr := newTree(t)
	file := filepath.Join(tr.sibling, "a.txt")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	linkToHome := filepath.Join(tr.outside, "tohome")
	linkToProject := filepath.Join(tr.outside, "toproject")
	linkToSibling := filepath.Join(tr.outside, "tosibling")
	for link, target := range map[string]string{linkToHome: tr.home, linkToProject: tr.project, linkToSibling: tr.sibling} {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name string
		path string
		want string // substring of the reason; "" means allowed
	}{
		{"sibling folder is fine", tr.sibling, ""},
		{"folder under a sibling is fine", tr.outside, ""},
		{"missing", filepath.Join(tr.root, "nope"), "does not exist"},
		{"a file", file, "not a directory"},
		{"filesystem root", "/", "filesystem root"},
		{"home itself", tr.home, "home directory"},
		{"parent of harness and project", filepath.Join(tr.home, "work"), "contains the harness directory"},
		{"the harness directory", tr.harness, "contains the harness directory"},
		{"the project directory", tr.project, "contains the project directory"},
		{"symlink to home", linkToHome, "home directory"},
		{"symlink to the project", linkToProject, "contains the project directory"},
		{"symlink to a fine folder", linkToSibling, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Refusal(tc.path, tr.harness, tr.project, tr.home)
			if tc.want == "" {
				if got != "" {
					t.Fatalf("want allowed, refused: %s", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("reason %q lacks %q", got, tc.want)
			}
		})
	}
}

// TestRefusalHomeInput feeds the strings the issue names: "/" and $HOME as the
// real home. The predicate only reads, so this is safe.
func TestRefusalHomeInput(t *testing.T) {
	tr := newTree(t)
	t.Setenv("HOME", tr.home)
	h, _ := os.UserHomeDir()
	if got := Refusal(h, tr.harness, tr.project, h); !strings.Contains(got, "home directory") {
		t.Fatalf("$HOME: %q", got)
	}
	if got := Refusal("/", tr.harness, tr.project, h); !strings.Contains(got, "root") {
		t.Fatalf("/: %q", got)
	}
}

func TestExpand(t *testing.T) {
	tests := []struct{ entry, want string }{
		{"../ynh", "/h/ynh"},
		{"sub/x", "/h/seat/sub/x"},
		{"/abs/p", "/abs/p"},
		{"~/code", "/home/u/code"},
		{"~", "/home/u"},
	}
	for _, tc := range tests {
		if got := Expand(tc.entry, "/h/seat", "/home/u"); got != tc.want {
			t.Errorf("Expand(%q) = %q, want %q", tc.entry, got, tc.want)
		}
	}
}

func TestLoadAllowlist(t *testing.T) {
	tr := newTree(t)
	cfg := []string{filepath.Join(tr.home, "work")}
	envVal := tr.outside + string(os.PathListSeparator) + "relative/dir" + string(os.PathListSeparator) + filepath.Join(tr.root, "missing")

	t.Run("none", func(t *testing.T) {
		if a := LoadAllowlist(nil, "", false, tr.home); a.Set {
			t.Fatal("want no allow-list")
		}
	})
	t.Run("config", func(t *testing.T) {
		a := LoadAllowlist(cfg, "", false, tr.home)
		if !a.Set || a.Source != "config" || len(a.Roots) != 1 {
			t.Fatalf("%+v", a)
		}
	})
	t.Run("env replaces config", func(t *testing.T) {
		a := LoadAllowlist(cfg, envVal, true, tr.home)
		if a.Source != "env" || len(a.Roots) != 1 || a.Roots[0] != tr.outside {
			t.Fatalf("%+v", a)
		}
		if len(a.Notes) != 2 {
			t.Fatalf("want 2 notes (relative, missing): %v", a.Notes)
		}
		if a.Covers(tr.sibling) {
			t.Fatal("config root must not survive an env list")
		}
	})
	t.Run("empty env does not replace config", func(t *testing.T) {
		if a := LoadAllowlist(cfg, "", true, tr.home); a.Source != "config" {
			t.Fatalf("%+v", a)
		}
	})
	t.Run("tilde", func(t *testing.T) {
		a := LoadAllowlist([]string{"~/work"}, "", false, tr.home)
		if len(a.Roots) != 1 || a.Roots[0] != filepath.Join(tr.home, "work") {
			t.Fatalf("%+v", a)
		}
	})
	t.Run("covers is by path component", func(t *testing.T) {
		a := Allowlist{Set: true, Roots: []string{"/a/b"}}
		if !a.Covers("/a/b") || !a.Covers("/a/b/c") || a.Covers("/a/bc") || a.Covers("/a") {
			t.Fatal("prefix matching must respect path components")
		}
	})
}

func TestDecide(t *testing.T) {
	tr := newTree(t)
	base := Request{
		Entries:    []string{"../ynh"},
		HarnessDir: tr.harness, ProjectDir: tr.project, Home: tr.home,
		TrustHint: "run 'ynh trust accept seat'",
	}
	allow := func(roots ...string) Allowlist {
		return Allowlist{Set: true, Source: "env", Roots: roots}
	}
	tests := []struct {
		name    string
		mod     func(*Request)
		granted bool
		reason  string
	}{
		{"allow-list covers", func(r *Request) { r.Allow = allow(filepath.Join(tr.home, "work")) }, true, "allow-list"},
		{"allow-list does not cover", func(r *Request) { r.Allow = allow(tr.outside) }, false, "outside the YNH_WORKSPACE_ROOTS allow-list"},
		{"trust cannot override the allow-list", func(r *Request) { r.Allow = allow(tr.outside); r.Trusted = true }, false, "outside"},
		{"no allow-list, untrusted", func(r *Request) {}, false, "ynh trust accept"},
		{"no allow-list, trusted", func(r *Request) { r.Trusted = true }, true, "trust-accepted"},
		{"remote install refused even when allowed and trusted", func(r *Request) {
			r.Remote, r.Trusted, r.Allow = true, true, allow(tr.home)
		}, false, "remote source"},
		{"hard refusal beats the allow-list", func(r *Request) {
			r.Entries = []string{"/"}
			r.Allow = allow("/")
		}, false, "filesystem root"},
		{"home beats trust", func(r *Request) { r.Entries = []string{"~"}; r.Trusted = true }, false, "home directory"},
		{"missing folder", func(r *Request) { r.Entries = []string{"../nope"}; r.Trusted = true }, false, "does not exist"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.mod(&r)
			got := Decide(r)
			if len(got) != 1 {
				t.Fatalf("got %d entries", len(got))
			}
			if got[0].Granted != tc.granted {
				t.Fatalf("granted = %v (%s), want %v", got[0].Granted, got[0].Reason, tc.granted)
			}
			if !strings.Contains(got[0].Reason, tc.reason) {
				t.Fatalf("reason %q lacks %q", got[0].Reason, tc.reason)
			}
			if tc.granted && got[0].Resolved != tr.sibling {
				t.Fatalf("resolved = %q, want %q", got[0].Resolved, tr.sibling)
			}
		})
	}
}
