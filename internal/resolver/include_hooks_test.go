package resolver

import (
	"path/filepath"
	"testing"
)

// Hook consent for a harness reached by more than one route does not depend
// on the order of the includes. Hooks are off unless consented to, so every
// route must say "hooks": true at every link, except that the root's own
// direct include of the harness decides, whichever way, whatever other routes
// say. Consent is keyed by the harness, so a picked include of it and a whole
// one agree.
func TestResolve_IncludeHooksConsentInADiamondIsOrderIndependent(t *testing.T) {
	type inc = map[string]any
	cases := []struct {
		name string
		// build writes the harnesses under base and returns the root's two
		// includes, which the test lists in both orders, and the directory
		// of the shared harness whose consent is checked.
		build func(base string) (first, second inc, shared string)
		want  bool
	}{
		{
			name: "the root's direct include without hooks wins over a route that consents",
			build: func(base string) (inc, inc, string) {
				b := filepath.Join(base, "b")
				writeNS(t, b, inc{"name": "b"})
				writeNS(t, filepath.Join(base, "a"), inc{"name": "a", "includes": []inc{{"local": b, "hooks": true}}})
				return inc{"local": filepath.Join(base, "a"), "hooks": true}, inc{"local": b}, b
			},
			want: false,
		},
		{
			name: "the root's direct include with hooks wins over a route that does not consent",
			build: func(base string) (inc, inc, string) {
				b := filepath.Join(base, "b")
				writeNS(t, b, inc{"name": "b"})
				writeNS(t, filepath.Join(base, "a"), inc{"name": "a", "includes": []inc{{"local": b}}})
				return inc{"local": filepath.Join(base, "a"), "hooks": true}, inc{"local": b, "hooks": true}, b
			},
			want: true,
		},
		{
			name: "the root's direct pick with hooks decides for the harness",
			build: func(base string) (inc, inc, string) {
				b := filepath.Join(base, "b")
				writeNS(t, b, inc{"name": "b"})
				writeNS(t, filepath.Join(base, "a"), inc{"name": "a", "includes": []inc{{"local": b}}})
				return inc{"local": filepath.Join(base, "a"), "hooks": true}, inc{"local": b, "pick": []string{"skills"}, "hooks": true}, b
			},
			want: true,
		},
		{
			name: "one intermediate route without consent turns the hooks off",
			build: func(base string) (inc, inc, string) {
				b := filepath.Join(base, "b")
				writeNS(t, b, inc{"name": "b"})
				writeNS(t, filepath.Join(base, "a"), inc{"name": "a", "includes": []inc{{"local": b, "hooks": true}}})
				writeNS(t, filepath.Join(base, "c"), inc{"name": "c", "includes": []inc{{"local": b}}})
				return inc{"local": filepath.Join(base, "a"), "hooks": true}, inc{"local": filepath.Join(base, "c"), "hooks": true}, b
			},
			want: false,
		},
		{
			name: "a route missing consent at its first link turns the hooks off",
			build: func(base string) (inc, inc, string) {
				b := filepath.Join(base, "b")
				writeNS(t, b, inc{"name": "b"})
				writeNS(t, filepath.Join(base, "a"), inc{"name": "a", "includes": []inc{{"local": b, "hooks": true}}})
				writeNS(t, filepath.Join(base, "c"), inc{"name": "c", "includes": []inc{{"local": b, "hooks": true}}})
				return inc{"local": filepath.Join(base, "a"), "hooks": true}, inc{"local": filepath.Join(base, "c")}, b
			},
			want: false,
		},
		{
			name: "every route consents at every link",
			build: func(base string) (inc, inc, string) {
				b := filepath.Join(base, "b")
				writeNS(t, b, inc{"name": "b"})
				writeNS(t, filepath.Join(base, "a"), inc{"name": "a", "includes": []inc{{"local": b, "hooks": true}}})
				writeNS(t, filepath.Join(base, "c"), inc{"name": "c", "includes": []inc{{"local": b, "hooks": true}}})
				return inc{"local": filepath.Join(base, "a"), "hooks": true}, inc{"local": filepath.Join(base, "c"), "hooks": true}, b
			},
			want: true,
		},
		{
			name: "the root's direct include of an intermediate decides for the routes through it",
			build: func(base string) (inc, inc, string) {
				d := filepath.Join(base, "d")
				b := filepath.Join(base, "b")
				writeNS(t, d, inc{"name": "d"})
				writeNS(t, b, inc{"name": "b", "includes": []inc{{"local": d, "hooks": true}}})
				writeNS(t, filepath.Join(base, "a"), inc{"name": "a", "includes": []inc{{"local": b}}})
				return inc{"local": filepath.Join(base, "a"), "hooks": true}, inc{"local": b, "hooks": true}, d
			},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, order := range []string{"intermediate first", "intermediate last"} {
				base := t.TempDir()
				first, second, shared := tc.build(base)
				if order == "intermediate last" {
					first, second = second, first
				}
				p := namespaceRoot(t, filepath.Join(base, "root"), first, second)
				results, err := Resolve(p, nil)
				if err != nil {
					t.Fatalf("%s: %v", order, err)
				}
				found := 0
				for _, r := range results {
					if r.Content.BasePath != shared {
						continue
					}
					found++
					if r.HooksActive != tc.want {
						t.Errorf("%s: HooksActive for %s (%s) = %v, want %v", order, r.Chain, r.Content.Paths, r.HooksActive, tc.want)
					}
				}
				if found == 0 {
					t.Fatalf("%s: shared harness %s not in the results", order, shared)
				}
			}
		})
	}
}

// With a single route, consent must hold at every link of it.
func TestResolve_IncludeHooksConsentOnTheOnlyRoute(t *testing.T) {
	for _, tc := range []struct {
		name         string
		rootHooks    bool
		innerHooks   bool
		wantA, wantB bool
	}{
		{"both links consent", true, true, true, true},
		{"only the root's link consents", true, false, true, false},
		{"only the inner link consents", false, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			a, b := filepath.Join(base, "a"), filepath.Join(base, "b")
			writeNS(t, b, map[string]any{"name": "b"})
			writeNS(t, a, map[string]any{"name": "a", "includes": []map[string]any{{"local": b, "hooks": tc.innerHooks}}})
			p := namespaceRoot(t, filepath.Join(base, "root"), map[string]any{"local": a, "hooks": tc.rootHooks})
			results, err := Resolve(p, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range results {
				switch r.Content.BasePath {
				case a:
					if r.HooksActive != tc.wantA {
						t.Errorf("a: HooksActive = %v, want %v", r.HooksActive, tc.wantA)
					}
				case b:
					if r.HooksActive != tc.wantB {
						t.Errorf("b: HooksActive = %v, want %v", r.HooksActive, tc.wantB)
					}
				}
			}
		})
	}
}
