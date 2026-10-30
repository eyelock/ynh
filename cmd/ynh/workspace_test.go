package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/workspace"
)

// wsWorld is a fake machine under t.TempDir(): a home with a harness, a project
// and sibling folders, YNH_HOME beside it, and the workspace environment
// cleared. Nothing here touches the real home.
type wsWorld struct {
	root, home, harnessDir, project, sibling, other, ynhHome string
}

func newWSWorld(t *testing.T) wsWorld {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := wsWorld{root: root, home: filepath.Join(root, "home")}
	w.harnessDir = filepath.Join(w.home, "work", "seat")
	w.project = filepath.Join(w.home, "work", "project")
	w.sibling = filepath.Join(w.home, "work", "ynh")
	w.other = filepath.Join(root, "elsewhere")
	w.ynhHome = filepath.Join(root, "ynhhome")
	for _, d := range []string{w.harnessDir, w.project, w.sibling, w.other, w.ynhHome} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", w.home)
	t.Setenv("YNH_HOME", w.ynhHome)
	t.Setenv(workspace.EnvRoots, "")
	return w
}

func (w wsWorld) harness(entries ...string) *harness.Harness {
	return &harness.Harness{Name: "seat", Dir: w.harnessDir, Workspace: entries}
}

func (w wsWorld) writeConfig(t *testing.T, roots ...string) {
	t.Helper()
	data, _ := json.Marshal(map[string]any{"workspace_roots": roots})
	if err := os.WriteFile(filepath.Join(w.ynhHome, "config.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// accept records p as accepted under id, the way `ynh trust accept` does.
func accept(t *testing.T, id string, p *harness.Harness) {
	t.Helper()
	store, err := loadTrustStore()
	if err != nil {
		t.Fatal(err)
	}
	set := harnessExecutables(p)
	store.Harnesses[id] = trustRecord{Digest: executablesDigest(set), Commands: set.count(), Scheme: trustScheme}
	if err := saveTrustStore(store); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceGrant(t *testing.T) {
	const id = "local/seat"
	tests := []struct {
		name        string
		setup       func(t *testing.T, w wsWorld, p *harness.Harness)
		vendor      string
		noWorkspace bool
		id          string
		prov        *harness.Provenance
		want        func(w wsWorld) []string
		stderr      []string // substrings that must appear
		absent      []string // substrings that must not
	}{
		{
			name:   "no allow-list, never accepted: skipped with the trust hint",
			want:   func(w wsWorld) []string { return nil },
			stderr: []string{`"../ynh" not granted`, "ynh trust accept local/seat"},
		},
		{
			name:   "no allow-list, accepted: granted",
			setup:  func(t *testing.T, w wsWorld, p *harness.Harness) { accept(t, id, p) },
			want:   func(w wsWorld) []string { return []string{w.sibling} },
			stderr: []string{"workspace: granted " + "%SIB%", "trust-accepted"},
		},
		{
			name: "accepted, then the entries changed: skipped",
			setup: func(t *testing.T, w wsWorld, p *harness.Harness) {
				accept(t, id, p)
				p.Workspace = append(p.Workspace, w.other)
			},
			want:   func(w wsWorld) []string { return nil },
			stderr: []string{"not granted", "ynh trust accept"},
		},
		{
			name: "accepted without workspace, then one added: skipped",
			setup: func(t *testing.T, w wsWorld, p *harness.Harness) {
				was := p.Workspace
				p.Workspace = nil
				accept(t, id, p)
				p.Workspace = was
			},
			want:   func(w wsWorld) []string { return nil },
			stderr: []string{"not granted"},
		},
		{
			name:   "run from a path has no trust record",
			id:     "",
			setup:  func(t *testing.T, w wsWorld, p *harness.Harness) { accept(t, id, p) },
			want:   func(w wsWorld) []string { return nil },
			stderr: []string{"run from a path has no trust record"},
		},
		{
			name:   "config allow-list covers: granted without trust",
			setup:  func(t *testing.T, w wsWorld, p *harness.Harness) { w.writeConfig(t, "~/work") },
			want:   func(w wsWorld) []string { return []string{w.sibling} },
			stderr: []string{"granted", "workspace_roots allow-list"},
		},
		{
			name:   "config allow-list does not cover: refused, trust cannot override",
			setup:  func(t *testing.T, w wsWorld, p *harness.Harness) { w.writeConfig(t, w.other); accept(t, id, p) },
			want:   func(w wsWorld) []string { return nil },
			stderr: []string{"outside the workspace_roots allow-list"},
		},
		{
			name: "env replaces config",
			setup: func(t *testing.T, w wsWorld, p *harness.Harness) {
				w.writeConfig(t, w.other)
				t.Setenv(workspace.EnvRoots, filepath.Join(w.home, "work"))
			},
			want:   func(w wsWorld) []string { return []string{w.sibling} },
			stderr: []string{"YNH_WORKSPACE_ROOTS allow-list"},
		},
		{
			name: "env replaces a config that would have covered it",
			setup: func(t *testing.T, w wsWorld, p *harness.Harness) {
				w.writeConfig(t, filepath.Join(w.home, "work"))
				t.Setenv(workspace.EnvRoots, w.other)
			},
			want:   func(w wsWorld) []string { return nil },
			stderr: []string{"outside the YNH_WORKSPACE_ROOTS allow-list"},
		},
		{
			name:   "remote install refused even with an allow-list and trust",
			setup:  func(t *testing.T, w wsWorld, p *harness.Harness) { w.writeConfig(t, w.home); accept(t, id, p) },
			prov:   &harness.Provenance{SourceType: "git", Source: "github.com/o/r"},
			want:   func(w wsWorld) []string { return nil },
			stderr: []string{"remote source"},
		},
		{
			name:   "registry install is remote too",
			setup:  func(t *testing.T, w wsWorld, p *harness.Harness) { w.writeConfig(t, w.home) },
			prov:   &harness.Provenance{SourceType: "registry"},
			want:   func(w wsWorld) []string { return nil },
			stderr: []string{"remote source"},
		},
		{
			name:   "a local install is not remote",
			setup:  func(t *testing.T, w wsWorld, p *harness.Harness) { w.writeConfig(t, w.home) },
			prov:   &harness.Provenance{SourceType: "local"},
			want:   func(w wsWorld) []string { return []string{w.sibling} },
			stderr: []string{"granted"},
		},
		{
			name:        "--no-workspace grants none and says so",
			setup:       func(t *testing.T, w wsWorld, p *harness.Harness) { w.writeConfig(t, w.home) },
			noWorkspace: true,
			want:        func(w wsWorld) []string { return nil },
			stderr:      []string{"--no-workspace", "1 requested folder(s) not granted"},
		},
		{
			name:   "unsupported vendor warns naming vendor and folder",
			setup:  func(t *testing.T, w wsWorld, p *harness.Harness) { w.writeConfig(t, w.home) },
			vendor: "cursor",
			want:   func(w wsWorld) []string { return nil },
			stderr: []string{"%SIB% not granted: cursor:", "no flag that adds a directory"},
			absent: []string{"workspace: granted"},
		},
		{
			name: "hard refusals ignore an allow-list that covers them",
			setup: func(t *testing.T, w wsWorld, p *harness.Harness) {
				w.writeConfig(t, "/")
				p.Workspace = []string{"/", "~", "..", "../missing"}
			},
			want:   func(w wsWorld) []string { return nil },
			stderr: []string{"filesystem root", "home directory", "contains the harness directory", "does not exist"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := newWSWorld(t)
			p := w.harness("../ynh")
			p.InstalledFrom = tc.prov
			hid := id
			if strings.Contains(tc.name, "from a path") {
				hid = tc.id
			}
			if tc.setup != nil {
				tc.setup(t, w, p)
			}
			vendorName := tc.vendor
			if vendorName == "" {
				vendorName = "claude"
			}
			var stderr bytes.Buffer
			got := workspaceGrant(p, hid, vendorName, w.project, tc.noWorkspace, &stderr)
			if want := tc.want(w); !reflect.DeepEqual(got, want) {
				t.Errorf("granted = %v, want %v\n%s", got, want, stderr.String())
			}
			out := strings.ReplaceAll(stderr.String(), w.sibling, "%SIB%")
			for _, s := range tc.stderr {
				if !strings.Contains(out, s) {
					t.Errorf("stderr lacks %q:\n%s", s, out)
				}
			}
			for _, s := range tc.absent {
				if strings.Contains(out, s) {
					t.Errorf("stderr has %q:\n%s", s, out)
				}
			}
		})
	}
}

func TestWorkspaceGrantSilentWithoutRequest(t *testing.T) {
	w := newWSWorld(t)
	var stderr bytes.Buffer
	if got := workspaceGrant(w.harness(), "x", "claude", w.project, false, &stderr); got != nil || stderr.Len() != 0 {
		t.Fatalf("got %v, stderr %q", got, stderr.String())
	}
	// Not even --no-workspace says anything when nothing was requested.
	if got := workspaceGrant(w.harness(), "x", "claude", w.project, true, &stderr); got != nil || stderr.Len() != 0 {
		t.Fatalf("got %v, stderr %q", got, stderr.String())
	}
}

func TestWorkspaceLaunchArgsPerVendor(t *testing.T) {
	w := newWSWorld(t)
	w.writeConfig(t, w.home)
	t.Chdir(w.project)
	for _, v := range []string{"claude", "codex", "copilot"} {
		var stderr bytes.Buffer
		args, err := workspaceLaunchArgs(w.harness("../ynh"), "local/seat", v, false, &stderr)
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"--add-dir", w.sibling}; !reflect.DeepEqual(args, want) {
			t.Errorf("%s: args = %v, want %v", v, args, want)
		}
	}
	var stderr bytes.Buffer
	args, err := workspaceLaunchArgs(w.harness("../ynh"), "local/seat", "cursor", false, &stderr)
	if err != nil || len(args) != 0 || !strings.Contains(stderr.String(), "cursor") {
		t.Errorf("cursor: args %v, err %v, stderr %q", args, err, stderr.String())
	}
}

// Old acceptances: a harness with no workspace entries keeps the digest it was
// accepted under. The expected value is rebuilt from the digest's own format
// here, independently of executablesDigest, so adding workspace to the digest
// cannot quietly move it.
func TestDigestStableWithoutWorkspace(t *testing.T) {
	p := harnessWithSensors(t, map[string]plugin.Sensor{"lint": cmdSensor("make lint", "make --version")})
	h := sha256.New()
	f := func(s string) { _, _ = fmt.Fprintf(h, "%d:%s\x00", len(s), s) }
	f("ynh-trust/2")
	for _, s := range []string{"sensor", "", "", "lint", "command", "make lint"} {
		f(s)
	}
	for _, s := range []string{"sensor", "", "", "lint", "version_command", "make --version"} {
		f(s)
	}
	want := "sha256:" + hex.EncodeToString(h.Sum(nil))[:16]
	if got := executablesDigest(harnessExecutables(p)); got != want {
		t.Fatalf("digest moved for a harness without workspace: %s, want %s", got, want)
	}
	p.Workspace = []string{}
	if got := executablesDigest(harnessExecutables(p)); got != want {
		t.Fatalf("an empty workspace array moved the digest: %s", got)
	}
}

func TestDigestMovesWithWorkspace(t *testing.T) {
	digest := func(ws ...string) string {
		p := harnessWithSensors(t, nil)
		p.Workspace = ws
		return executablesDigest(harnessExecutables(p))
	}
	none := digest()
	one := digest("../ynh")
	if none == one {
		t.Error("adding an entry did not move the digest")
	}
	if one == digest("../ynm") {
		t.Error("changing an entry did not move the digest")
	}
	if digest("../ynh", "../ynm") == one {
		t.Error("adding a second entry did not move the digest")
	}
	if digest("../ynh", "../ynm") != digest("../ynm", "../ynh") {
		t.Error("reordering entries moved the digest")
	}
}

func TestTrustStateWithWorkspace(t *testing.T) {
	p := harnessWithSensors(t, nil)
	if st := trustStatusFor("id", p, trustStore{Harnesses: map[string]trustRecord{}}, nil); st.State != trustNoCommands {
		t.Fatalf("state = %s, want no-commands", st.State)
	}
	p.Workspace = []string{"../ynh"}
	store := trustStore{Harnesses: map[string]trustRecord{}}
	if st := trustStatusFor("id", p, store, nil); st.State != trustUnreviewed {
		t.Fatalf("a workspace-only harness is reviewable: state = %s", st.State)
	}
	store.Harnesses["id"] = trustRecord{Digest: executablesDigest(harnessExecutables(p)), Scheme: trustScheme}
	if st := trustStatusFor("id", p, store, nil); st.State != trustAccepted {
		t.Fatalf("state = %s, want accepted", st.State)
	}
	p.Workspace = []string{"../other"}
	if st := trustStatusFor("id", p, store, nil); st.State != trustChanged {
		t.Fatalf("state = %s, want changed", st.State)
	}
}

func TestTrustShowWorkspaceSection(t *testing.T) {
	w := newWSWorld(t)
	t.Chdir(w.project)
	p := w.harness("../ynh", "/", "../nope")
	st := trustStatusFor("local/seat", p, trustStore{Harnesses: map[string]trustRecord{}}, nil)

	t.Run("no allow-list", func(t *testing.T) {
		st.Workspace = workspaceReportFor(p)
		out := showText(t, st)
		for _, want := range []string{"Workspace folders", "../ynh", "resolves to: " + w.sibling, "never granted: ", "filesystem root", "does not exist", "No allow-list is set"} {
			if !strings.Contains(out, want) {
				t.Errorf("missing %q in:\n%s", want, out)
			}
		}
	})
	t.Run("allow-list covers", func(t *testing.T) {
		w.writeConfig(t, "~/work")
		st.Workspace = workspaceReportFor(p)
		out := showText(t, st)
		if !strings.Contains(out, "the workspace_roots allow-list covers it") {
			t.Errorf("allow-list coverage not reported:\n%s", out)
		}
		if !strings.Contains(out, "Trust cannot grant one it does not cover") {
			t.Errorf("trust note missing:\n%s", out)
		}
	})
	t.Run("json", func(t *testing.T) {
		st.Workspace = workspaceReportFor(p)
		data, err := json.Marshal(st)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		_ = json.Unmarshal(data, &m)
		ws, ok := m["workspace"].(map[string]any)
		if !ok {
			t.Fatalf("no workspace in %s", data)
		}
		if ws["allow_list"] != "config" {
			t.Errorf("allow_list = %v", ws["allow_list"])
		}
		if n := len(ws["entries"].([]any)); n != 3 {
			t.Errorf("entries = %d", n)
		}
	})
	t.Run("omitted when none requested", func(t *testing.T) {
		st := trustStatusFor("x", harnessWithSensors(t, nil), trustStore{Harnesses: map[string]trustRecord{}}, nil)
		st.Workspace = workspaceReportFor(harnessWithSensors(t, nil))
		data, _ := json.Marshal(st)
		if strings.Contains(string(data), "workspace") {
			t.Errorf("workspace present: %s", data)
		}
	})
	t.Run("remote install is reported", func(t *testing.T) {
		rp := w.harness("../ynh")
		rp.InstalledFrom = &harness.Provenance{SourceType: "git"}
		r := workspaceReportFor(rp)
		if !r.RemoteInstall {
			t.Fatal("remote_install not set")
		}
		var buf bytes.Buffer
		printWorkspaceSection(&buf, r)
		if !strings.Contains(buf.String(), "never granted") && !strings.Contains(buf.String(), "none of these is ever granted") {
			t.Errorf("remote note missing:\n%s", buf.String())
		}
	})
}

func TestParseRunArgsNoWorkspace(t *testing.T) {
	ra := parseRunArgs([]string{"seat", "--no-workspace", "--model", "opus"})
	if !ra.NoWorkspace {
		t.Fatal("--no-workspace not parsed")
	}
	for _, a := range ra.VendorArgs {
		if a == "--no-workspace" {
			t.Fatal("--no-workspace was forwarded to the vendor")
		}
	}
	if ra.HarnessName != "seat" {
		t.Errorf("harness = %q", ra.HarnessName)
	}
}
