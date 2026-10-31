package harness

import (
	"reflect"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

func lookupFrom(env map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := env[k]; return v, ok }
}

func included(source, dir string, env []string, servers map[string]plugin.MCPServer) IncludedHarness {
	return IncludedHarness{
		Source:  source,
		Harness: &Harness{Name: "inc", Dir: dir, EnvPassthrough: env, MCPServers: servers},
	}
}

func provenanceMap(p []MCPProvenance) map[string]string {
	m := map[string]string{}
	for _, e := range p {
		m[e.Server] = e.Source
	}
	return m
}

func TestComposeMCPServers_ExpandsInEachIncludesOwnContext(t *testing.T) {
	root := &Harness{Name: "root", Dir: "/root", EnvPassthrough: []string{"ROOT_TOKEN"},
		MCPServers: map[string]plugin.MCPServer{
			"own": {Command: "./bin/own", Env: map[string]string{"T": "${ROOT_TOKEN}"}},
		}}
	inc := included("eyelock/a", "/inc", []string{"INC_TOKEN"}, map[string]plugin.MCPServer{
		"theirs": {
			Command: "./bin/theirs",
			Args:    []string{"${PLUGIN_ROOT}/x", "${PLUGIN_DATA}/y"},
			Env:     map[string]string{"T": "${INC_TOKEN}"},
		},
	})
	servers, prov, err := ComposeMCPServers(root, []IncludedHarness{inc}, "/data",
		lookupFrom(map[string]string{"ROOT_TOKEN": "r", "INC_TOKEN": "i"}))
	if err != nil {
		t.Fatal(err)
	}
	theirs := servers["theirs"]
	if theirs.Command != "/inc/bin/theirs" || theirs.Args[0] != "/inc/x" || theirs.Args[1] != "/data/y" || theirs.Env["T"] != "i" {
		t.Errorf("included server expanded in the wrong context: %+v", theirs)
	}
	own := servers["own"]
	if own.Command != "/root/bin/own" || own.Env["T"] != "r" {
		t.Errorf("root server: %+v", own)
	}
	want := map[string]string{"own": "root", "theirs": "eyelock/a"}
	if got := provenanceMap(prov); !reflect.DeepEqual(got, want) {
		t.Errorf("provenance = %v, want %v", got, want)
	}
}

func TestComposeMCPServers_RootEnvPassthroughIsNotWidenedByIncludes(t *testing.T) {
	// The include declares INC_TOKEN, the root does not. The include's server
	// may use it; a root server may not.
	inc := included("a", "/inc", []string{"INC_TOKEN"}, map[string]plugin.MCPServer{
		"theirs": {Command: "c", Env: map[string]string{"T": "${INC_TOKEN}"}},
	})
	lookup := lookupFrom(map[string]string{"INC_TOKEN": "i"})

	root := &Harness{Name: "root", Dir: "/root"}
	if _, _, err := ComposeMCPServers(root, []IncludedHarness{inc}, "/d", lookup); err != nil {
		t.Fatalf("include's own variable should expand: %v", err)
	}

	root.MCPServers = map[string]plugin.MCPServer{"own": {Command: "c", Env: map[string]string{"T": "${INC_TOKEN}"}}}
	_, _, err := ComposeMCPServers(root, []IncludedHarness{inc}, "/d", lookup)
	if err == nil || !strings.Contains(err.Error(), "INC_TOKEN") {
		t.Fatalf("root must not see the include's env_passthrough, got %v", err)
	}
}

func TestComposeMCPServers_UnsetIncludeVariableFails(t *testing.T) {
	inc := included("eyelock/a", "/inc", []string{"INC_TOKEN"}, map[string]plugin.MCPServer{
		"theirs": {Command: "c", Env: map[string]string{"T": "${INC_TOKEN}"}},
	})
	_, _, err := ComposeMCPServers(&Harness{Name: "root", Dir: "/r"}, []IncludedHarness{inc}, "/d", lookupFrom(nil))
	if err == nil || !strings.Contains(err.Error(), "INC_TOKEN") || !strings.Contains(err.Error(), "eyelock/a") {
		t.Fatalf("error = %v, want one naming INC_TOKEN and the include", err)
	}
}

func TestComposeMCPServers_ExpandsIncludeVariableNotInItsAllowlist(t *testing.T) {
	inc := included("a", "/inc", nil, map[string]plugin.MCPServer{
		"theirs": {Command: "c", Env: map[string]string{"T": "${ROOT_TOKEN}"}},
	})
	root := &Harness{Name: "root", Dir: "/r", EnvPassthrough: []string{"ROOT_TOKEN"}}
	_, _, err := ComposeMCPServers(root, []IncludedHarness{inc}, "/d", lookupFrom(map[string]string{"ROOT_TOKEN": "x"}))
	if err == nil {
		t.Fatal("an include's server must not read a variable only the root declared")
	}
}

func TestComposeMCPServers_Merge(t *testing.T) {
	srv := func(cmd string) map[string]plugin.MCPServer { return map[string]plugin.MCPServer{"db": {Command: cmd}} }
	tests := []struct {
		name     string
		root     map[string]plugin.MCPServer
		removals []string
		incs     []IncludedHarness
		want     map[string]string // server -> command
		wantSrc  map[string]string
		wantErr  []string
	}{
		{
			name: "root replaces an included server entirely",
			root: map[string]plugin.MCPServer{"db": {URL: "https://x"}},
			incs: []IncludedHarness{included("a", "/a", nil, srv("inc"))},
			want: map[string]string{"db": ""}, wantSrc: map[string]string{"db": "root"},
		},
		{
			name: "identical duplicate keeps one",
			incs: []IncludedHarness{included("a", "/a", nil, srv("same")), included("b", "/b", nil, srv("same"))},
			want: map[string]string{"db": "same"}, wantSrc: map[string]string{"db": "a"},
		},
		{
			name:    "conflicting duplicate names both sources",
			incs:    []IncludedHarness{included("eyelock/a", "/a", nil, srv("one")), included("eyelock/b", "/b", nil, srv("two"))},
			wantErr: []string{`"db"`, "eyelock/a", "eyelock/b"},
		},
		{
			name:     "root null removes an included server",
			removals: []string{"db"},
			incs:     []IncludedHarness{included("a", "/a", nil, srv("inc"))},
			want:     map[string]string{},
		},
		{
			name:     "removing a name that exists nowhere is not an error",
			removals: []string{"ghost"},
			incs:     []IncludedHarness{included("a", "/a", nil, srv("inc"))},
			want:     map[string]string{"db": "inc"}, wantSrc: map[string]string{"db": "a"},
		},
		{
			name: "included servers are carried alongside root's",
			root: map[string]plugin.MCPServer{"mine": {Command: "m"}},
			incs: []IncludedHarness{included("a", "/a", nil, srv("inc"))},
			want: map[string]string{"db": "inc", "mine": "m"}, wantSrc: map[string]string{"db": "a", "mine": "root"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := &Harness{Name: "root", Dir: "/root", MCPServers: tt.root, MCPRemovals: tt.removals}
			servers, prov, err := ComposeMCPServers(root, tt.incs, "/d", lookupFrom(nil))
			if len(tt.wantErr) > 0 {
				if err == nil {
					t.Fatal("expected an error")
				}
				for _, w := range tt.wantErr {
					if !strings.Contains(err.Error(), w) {
						t.Errorf("error %q does not contain %q", err, w)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]string{}
			for n, s := range servers {
				got[n] = s.Command
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("servers = %v, want %v", got, tt.want)
			}
			if tt.wantSrc != nil && !reflect.DeepEqual(provenanceMap(prov), tt.wantSrc) {
				t.Errorf("provenance = %v, want %v", provenanceMap(prov), tt.wantSrc)
			}
		})
	}
}

func TestComposeMCPServers_ProfileNullRemovesAnIncludedServer(t *testing.T) {
	base := &Harness{
		Name: "root", Dir: "/root",
		Profiles: map[string]plugin.Profile{
			"lean": {MCPServers: map[string]*plugin.MCPServer{"db": nil}},
		},
	}
	resolved, err := ResolveProfile(base, "lean")
	if err != nil {
		t.Fatal(err)
	}
	inc := included("a", "/a", nil, map[string]plugin.MCPServer{"db": {Command: "inc"}, "keep": {Command: "k"}})

	servers, _, err := ComposeMCPServers(resolved, []IncludedHarness{inc}, "/d", lookupFrom(nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := servers["db"]; ok {
		t.Error("profile null should remove the included server")
	}
	if _, ok := servers["keep"]; !ok {
		t.Error("unrelated included server was lost")
	}
	// Without the profile the server is carried.
	servers, _, err = ComposeMCPServers(base, []IncludedHarness{inc}, "/d", lookupFrom(nil))
	if err != nil || len(servers) != 2 {
		t.Errorf("base harness: servers=%v err=%v, want both", servers, err)
	}
}

func TestComposeMCPServersForExport(t *testing.T) {
	tests := []struct {
		name    string
		server  plugin.MCPServer
		wantErr bool
	}{
		{"npx server exports", plugin.MCPServer{Command: "npx", Args: []string{"-y", "pkg"}, Env: map[string]string{"T": "${TOKEN}"}}, false},
		{"remote server exports", plugin.MCPServer{URL: "https://x", Headers: map[string]string{"A": "${TOKEN}"}}, false},
		{"relative command", plugin.MCPServer{Command: "./bin/s"}, true},
		{"PLUGIN_ROOT in args", plugin.MCPServer{Command: "node", Args: []string{"${PLUGIN_ROOT}/s.js"}}, true},
		{"PLUGIN_ROOT in env", plugin.MCPServer{Command: "node", Env: map[string]string{"P": "${PLUGIN_ROOT}"}}, true},
		{"relative cwd", plugin.MCPServer{Command: "node", Cwd: "./srv"}, true},
		{"PLUGIN_DATA alone is fine", plugin.MCPServer{Command: "node", Args: []string{"${PLUGIN_DATA}/x"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inc := included("eyelock/a", "/a", nil, map[string]plugin.MCPServer{"s": tt.server})
			servers, err := ComposeMCPServersForExport(&Harness{Name: "root", Dir: "/r"}, []IncludedHarness{inc})
			if tt.wantErr {
				want := `included MCP server "s" from eyelock/a uses a path inside the include and cannot be exported; declare it in the root harness`
				if err == nil || err.Error() != want {
					t.Fatalf("error = %v, want %q", err, want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(servers["s"], tt.server) {
				t.Errorf("server was altered: %+v", servers["s"])
			}
		})
	}
}

func TestComposeMCPServersForExport_RootOverrideAndRemovalSkipTheCheck(t *testing.T) {
	inc := included("a", "/a", nil, map[string]plugin.MCPServer{"s": {Command: "./bin/s"}})
	override := &Harness{Name: "root", Dir: "/r", MCPServers: map[string]plugin.MCPServer{"s": {Command: "npx"}}}
	if servers, err := ComposeMCPServersForExport(override, []IncludedHarness{inc}); err != nil || servers["s"].Command != "npx" {
		t.Errorf("override: %v %v", servers, err)
	}
	removed := &Harness{Name: "root", Dir: "/r", MCPRemovals: []string{"s"}}
	if servers, err := ComposeMCPServersForExport(removed, []IncludedHarness{inc}); err != nil || len(servers) != 0 {
		t.Errorf("removal: %v %v", servers, err)
	}
	// A path in the root's own server is the root's business.
	own := &Harness{Name: "root", Dir: "/r", MCPServers: map[string]plugin.MCPServer{"s": {Command: "./bin/s"}}}
	if _, err := ComposeMCPServersForExport(own, nil); err != nil {
		t.Errorf("root path: %v", err)
	}
}

func TestResolveProfile_DoesNotMutateTheBaseHarness(t *testing.T) {
	base := &Harness{
		Name: "root",
		MCPServers: map[string]plugin.MCPServer{
			"gh": {Command: "gh", Args: []string{"a"}, Env: map[string]string{"TOKEN": "base"}, Headers: map[string]string{"H": "base"}},
		},
		Profiles: map[string]plugin.Profile{
			"p": {MCPServers: map[string]*plugin.MCPServer{
				"gh": {Env: map[string]string{"TOKEN": "profile", "EXTRA": "1"}, Headers: map[string]string{"H": "profile"}},
			}},
		},
	}
	resolved, err := ResolveProfile(base, "p")
	if err != nil {
		t.Fatal(err)
	}
	if got := resolved.MCPServers["gh"].Env; got["TOKEN"] != "profile" || got["EXTRA"] != "1" {
		t.Errorf("profile env not merged: %v", got)
	}
	if got := resolved.MCPServers["gh"].Headers["H"]; got != "profile" {
		t.Errorf("profile headers not applied: %v", got)
	}
	baseGH := base.MCPServers["gh"]
	if baseGH.Env["TOKEN"] != "base" || len(baseGH.Env) != 1 || baseGH.Headers["H"] != "base" {
		t.Errorf("resolving a profile changed the base harness: %+v", baseGH)
	}
}

func TestResolveProfile_DoesNotShareMapsForUntouchedServers(t *testing.T) {
	base := &Harness{
		Name:       "root",
		MCPServers: map[string]plugin.MCPServer{"gh": {Command: "gh", Env: map[string]string{"A": "1"}}},
		Profiles: map[string]plugin.Profile{
			"p": {MCPServers: map[string]*plugin.MCPServer{"other": {Command: "o"}}},
		},
	}
	resolved, err := ResolveProfile(base, "p")
	if err != nil {
		t.Fatal(err)
	}
	resolved.MCPServers["gh"].Env["A"] = "changed"
	if base.MCPServers["gh"].Env["A"] != "1" {
		t.Error("resolved harness shares an env map with the base")
	}
}

func TestResolveProfile_MergesType(t *testing.T) {
	base := &Harness{
		Name:       "root",
		MCPServers: map[string]plugin.MCPServer{"remote": {URL: "https://x", Type: plugin.MCPTypeStreamableHTTP}},
		Profiles: map[string]plugin.Profile{
			"sse":   {MCPServers: map[string]*plugin.MCPServer{"remote": {Type: plugin.MCPTypeSSE}}},
			"unset": {MCPServers: map[string]*plugin.MCPServer{"remote": {URL: "https://y"}}},
		},
	}
	got, err := ResolveProfile(base, "sse")
	if err != nil {
		t.Fatal(err)
	}
	if got.MCPServers["remote"].Type != plugin.MCPTypeSSE {
		t.Errorf("Type = %q, want sse", got.MCPServers["remote"].Type)
	}
	got, err = ResolveProfile(base, "unset")
	if err != nil {
		t.Fatal(err)
	}
	if got.MCPServers["remote"].Type != plugin.MCPTypeStreamableHTTP || got.MCPServers["remote"].URL != "https://y" {
		t.Errorf("a profile that does not set Type must keep it: %+v", got.MCPServers["remote"])
	}
}

func TestResolveProfile_ProfileServerOverridesRootRemoval(t *testing.T) {
	base := &Harness{
		Name:        "root",
		MCPRemovals: []string{"db"},
		Profiles: map[string]plugin.Profile{
			"p": {MCPServers: map[string]*plugin.MCPServer{"db": {Command: "mine"}}},
		},
	}
	got, err := ResolveProfile(base, "p")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.MCPRemovals) != 0 {
		t.Errorf("MCPRemovals = %v, want none", got.MCPRemovals)
	}
	if len(base.MCPRemovals) != 1 {
		t.Error("base removals were altered")
	}
}

func TestLoadDir_NullMCPServerIsARemoval(t *testing.T) {
	dir := t.TempDir()
	if err := writePluginJSONFile(dir, []byte(`{"name":"h","version":"1.0.0","mcp_servers":{"gone":null,"mine":{"command":"c"}}}`)); err != nil {
		t.Fatal(err)
	}
	h, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(h.MCPRemovals, []string{"gone"}) {
		t.Errorf("MCPRemovals = %v", h.MCPRemovals)
	}
	if _, ok := h.MCPServers["gone"]; ok || len(h.MCPServers) != 1 {
		t.Errorf("MCPServers = %v, want only mine", h.MCPServers)
	}
}

func TestComposeMCPServers_RootDecisionsPrecedeIncludeExpansionAndConflicts(t *testing.T) {
	conflict := func() []IncludedHarness {
		return []IncludedHarness{
			included("a", "/a", nil, map[string]plugin.MCPServer{"db": {Command: "one"}}),
			included("b", "/b", nil, map[string]plugin.MCPServer{"db": {Command: "two"}}),
		}
	}
	needsToken := func() []IncludedHarness {
		return []IncludedHarness{included("a", "/a", []string{"TOKEN"}, map[string]plugin.MCPServer{
			"db": {Command: "c", Env: map[string]string{"T": "${TOKEN}"}},
		})}
	}
	tests := []struct {
		name    string
		root    *Harness
		incs    []IncludedHarness
		wantCmd string // "" means the server must be absent
	}{
		{"conflict resolved by a root declaration", &Harness{Name: "r", Dir: "/r",
			MCPServers: map[string]plugin.MCPServer{"db": {Command: "mine"}}}, conflict(), "mine"},
		{"conflict resolved by a root null", &Harness{Name: "r", Dir: "/r", MCPRemovals: []string{"db"}}, conflict(), ""},
		{"unset variable in a server the root nulls", &Harness{Name: "r", Dir: "/r", MCPRemovals: []string{"db"}}, needsToken(), ""},
		{"unset variable in a server the root overrides", &Harness{Name: "r", Dir: "/r",
			MCPServers: map[string]plugin.MCPServer{"db": {Command: "mine"}}}, needsToken(), "mine"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			servers, _, err := ComposeMCPServers(tt.root, tt.incs, "/d", lookupFrom(nil))
			if err != nil {
				t.Fatal(err)
			}
			got, ok := servers["db"]
			if tt.wantCmd == "" && ok {
				t.Errorf("db should be absent, got %+v", got)
			}
			if tt.wantCmd != "" && got.Command != tt.wantCmd {
				t.Errorf("db = %+v, want command %q", got, tt.wantCmd)
			}
			for _, inc := range tt.incs {
				if len(inc.Harness.MCPServers) == 0 {
					t.Error("the loaded include was mutated")
				}
			}
		})
	}
}
