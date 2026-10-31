package resolver

import (
	"path/filepath"
	"slices"
	"testing"
)

// An intermediate harness's null removes a server only from what reaches the
// root through it. A harness reached by two routes loses the server only when
// both routes remove it, so a direct include by the root wins, and the result
// is the same in either include order.
func TestResolve_IntermediateNullInADiamondIsOrderIndependent(t *testing.T) {
	cases := []struct {
		name string
		// build writes the harnesses under base and returns the root's two
		// includes, which the test lists in both orders.
		build func(base string) (first, second map[string]any)
		want  []string
	}{
		{
			name: "the root includes the shared harness directly",
			build: func(base string) (map[string]any, map[string]any) {
				b := filepath.Join(base, "b")
				writeNS(t, b, map[string]any{"name": "b", "mcp_servers": map[string]any{"x": srv("x"), "y": srv("y")}})
				writeNS(t, filepath.Join(base, "a"), map[string]any{"name": "a", "includes": []map[string]any{{"local": b}}, "mcp_servers": map[string]any{"x": nil}})
				return map[string]any{"local": filepath.Join(base, "a")}, map[string]any{"local": b}
			},
			want: []string{"x", "y"},
		},
		{
			name: "another intermediate keeps the server",
			build: func(base string) (map[string]any, map[string]any) {
				b := filepath.Join(base, "b")
				writeNS(t, b, map[string]any{"name": "b", "mcp_servers": map[string]any{"x": srv("x"), "y": srv("y")}})
				writeNS(t, filepath.Join(base, "a"), map[string]any{"name": "a", "includes": []map[string]any{{"local": b}}, "mcp_servers": map[string]any{"x": nil}})
				writeNS(t, filepath.Join(base, "c"), map[string]any{"name": "c", "includes": []map[string]any{{"local": b}}})
				return map[string]any{"local": filepath.Join(base, "a")}, map[string]any{"local": filepath.Join(base, "c")}
			},
			want: []string{"x", "y"},
		},
		{
			name: "every intermediate removes the server",
			build: func(base string) (map[string]any, map[string]any) {
				b := filepath.Join(base, "b")
				writeNS(t, b, map[string]any{"name": "b", "mcp_servers": map[string]any{"x": srv("x"), "y": srv("y")}})
				writeNS(t, filepath.Join(base, "a"), map[string]any{"name": "a", "includes": []map[string]any{{"local": b}}, "mcp_servers": map[string]any{"x": nil}})
				writeNS(t, filepath.Join(base, "c"), map[string]any{"name": "c", "includes": []map[string]any{{"local": b}}, "mcp_servers": map[string]any{"x": nil}})
				return map[string]any{"local": filepath.Join(base, "a")}, map[string]any{"local": filepath.Join(base, "c")}
			},
			want: []string{"y"},
		},
		{
			name: "the server comes from the shared harness's own dependency",
			build: func(base string) (map[string]any, map[string]any) {
				d := filepath.Join(base, "d")
				b := filepath.Join(base, "b")
				writeNS(t, d, map[string]any{"name": "d", "mcp_servers": map[string]any{"x": srv("x"), "y": srv("y")}})
				writeNS(t, b, map[string]any{"name": "b", "includes": []map[string]any{{"local": d}}})
				writeNS(t, filepath.Join(base, "a"), map[string]any{"name": "a", "includes": []map[string]any{{"local": b}}, "mcp_servers": map[string]any{"x": nil}})
				return map[string]any{"local": filepath.Join(base, "a")}, map[string]any{"local": b}
			},
			want: []string{"x", "y"},
		},
		{
			name: "the shared harness is picked on both routes",
			build: func(base string) (map[string]any, map[string]any) {
				b := filepath.Join(base, "b")
				writeNS(t, b, map[string]any{"name": "b", "mcp_servers": map[string]any{"x": srv("x"), "y": srv("y")}})
				writeNS(t, filepath.Join(base, "a"), map[string]any{"name": "a", "includes": []map[string]any{{"local": b, "pick": []string{"skills"}}}, "mcp_servers": map[string]any{"x": nil}})
				return map[string]any{"local": filepath.Join(base, "a")}, map[string]any{"local": b, "pick": []string{"skills"}}
			},
			want: []string{"x", "y"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, order := range []string{"intermediate first", "intermediate last"} {
				base := t.TempDir()
				first, second := tc.build(base)
				if order == "intermediate last" {
					first, second = second, first
				}
				p := namespaceRoot(t, filepath.Join(base, "root"), first, second)
				results, err := Resolve(p, nil)
				if err != nil {
					t.Fatalf("%s: %v", order, err)
				}
				if got := serverNames(t, p, results); !slices.Equal(got, tc.want) {
					t.Errorf("%s: servers = %v, want %v", order, got, tc.want)
				}
			}
		})
	}
}

// With a single route, an intermediate harness's null still removes the
// server from its dependency.
func TestResolve_IntermediateNullOnTheOnlyRouteRemoves(t *testing.T) {
	base := t.TempDir()
	b := filepath.Join(base, "b")
	writeNS(t, b, map[string]any{"name": "b", "mcp_servers": map[string]any{"x": srv("x"), "y": srv("y")}})
	writeNS(t, filepath.Join(base, "a"), map[string]any{"name": "a", "includes": []map[string]any{{"local": b}}, "mcp_servers": map[string]any{"x": nil}})
	p := namespaceRoot(t, filepath.Join(base, "root"), map[string]any{"local": filepath.Join(base, "a")})

	results, err := Resolve(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := serverNames(t, p, results), []string{"y"}; !slices.Equal(got, want) {
		t.Errorf("servers = %v, want %v", got, want)
	}
}
