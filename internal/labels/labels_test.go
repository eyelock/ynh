package labels

import (
	"fmt"
	"strings"
	"testing"
)

func TestResolve_MergeOrder(t *testing.T) {
	src := Sources{
		Config:   Set{"a": "config", "b": "config", "c": "config", "d": "config"},
		Manifest: Set{"b": "manifest", "c": "manifest", "d": "manifest"},
		Env:      "c:env,d:env",
		Flags:    []string{"d:flag"},
	}
	got, err := Resolve(src, "")
	if err != nil {
		t.Fatal(err)
	}
	want := Set{"a": "config", "b": "manifest", "c": "env", "d": "flag"}
	if Encode(got) != Encode(want) {
		t.Errorf("merged = %s, want %s", Encode(got), Encode(want))
	}
}

func TestResolve_RemovalForm(t *testing.T) {
	src := Sources{
		Config:   Set{"a": "config", "b": "config"},
		Manifest: Set{"c": "manifest"},
		Env:      "d:env",
		// Removing a key drops what any earlier source set; a later flag
		// can set it again.
		Flags: []string{"a:", "c:", "d:", "e:", "b:", "b:again"},
	}
	got, err := Resolve(src, "")
	if err != nil {
		t.Fatal(err)
	}
	if Encode(got) != "b:again" {
		t.Errorf("merged = %s, want only b:again", Encode(got))
	}
}

func TestResolve_RemovalCanClearTheSet(t *testing.T) {
	got, err := Resolve(Sources{Config: Set{"a": "x"}, Flags: []string{"a:"}}, "")
	if err != nil || len(got) != 0 || EnvEntry(got) != "" {
		t.Errorf("got %v, %v, %q; want an empty set and no env entry", got, err, EnvEntry(got))
	}
}

func TestResolve_Refusals(t *testing.T) {
	many := Set{}
	for i := 0; i < Max+1; i++ {
		many[fmt.Sprintf("k%d", i)] = "v"
	}
	tests := []struct {
		name string
		src  Sources
		home string
		want string // a fragment of the refusal
	}{
		{"key starts with a digit", Sources{Flags: []string{"1a:v"}}, "", "label key"},
		{"key has an upper-case letter", Sources{Flags: []string{"Wheelhouse:ynw"}}, "", "label key"},
		{"key is too long", Sources{Flags: []string{strings.Repeat("a", 33) + ":v"}}, "", "label key"},
		{"key has a space", Sources{Flags: []string{"a b:v"}}, "", "label key"},
		{"bad key in config.json", Sources{Config: Set{"Bad": "v"}}, "", "config.json"},
		{"bad key in the manifest, removed later", Sources{Manifest: Set{"Bad": "v"}, Flags: []string{"Bad:"}}, "", "harness manifest"},
		{"value has a space", Sources{Flags: []string{"a:b c"}}, "", "no spaces"},
		{"value is an email", Sources{Flags: []string{"owner:me@example.com"}}, "", "no spaces, no @"},
		{"value starts with a slash", Sources{Flags: []string{"dir:/etc"}}, "", "start with / or ~"},
		{"value starts with a tilde", Sources{Manifest: Set{"dir": "~x"}}, "", "start with / or ~"},
		{"value holds the home folder", Sources{Flags: []string{"dir:x-/Users/me-y"}}, "/Users/me", "home folder"},
		{"value is too long", Sources{Flags: []string{"a:" + strings.Repeat("v", 65)}}, "", "must match"},
		{"value has a colon", Sources{Flags: []string{"a:b:c"}}, "", "must match"},
		{"too many labels", Sources{Config: many}, "", "at most 16"},
		{"flag without a colon", Sources{Flags: []string{"wheelhouse"}}, "", "not key:value"},
		{"flag without a key", Sources{Flags: []string{":ynw"}}, "", "not key:value"},
		{"env entry without a value", Sources{Env: "a:"}, "", "YNH_LABELS"},
		{"env entry without a colon", Sources{Env: "a"}, "", "YNH_LABELS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Resolve(tt.src, tt.home)
			if err == nil {
				t.Fatal("want a refusal")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q lacks %q", err, tt.want)
			}
		})
	}
}

func TestResolve_Limits(t *testing.T) {
	sixteen := Set{}
	for i := 0; i < Max; i++ {
		sixteen[fmt.Sprintf("k%d", i)] = "v"
	}
	for name, src := range map[string]Sources{
		"sixteen labels":   {Config: sixteen},
		"longest key":      {Flags: []string{"a" + strings.Repeat("b", 31) + ":v"}},
		"longest value":    {Flags: []string{"a:" + strings.Repeat("v", 64)}},
		"every value char": {Flags: []string{"a:Az09._/#-"}},
		"example":          {Flags: []string{"wheelhouse:ynw"}},
	} {
		if _, err := Resolve(src, "/Users/me"); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Seventeen keys in all, spread over sources, is still too many.
	if _, err := Resolve(Sources{Config: sixteen, Flags: []string{"extra:v"}}, ""); err == nil {
		t.Error("17 labels across sources must be refused")
	}
	// A removal brings it back under.
	if _, err := Resolve(Sources{Config: sixteen, Flags: []string{"extra:v", "k0:"}}, ""); err != nil {
		t.Errorf("16 after a removal: %v", err)
	}
}

func TestRefusalNeverEchoesTheValue(t *testing.T) {
	_, err := Resolve(Sources{Flags: []string{"token:/secret/path"}}, "")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("error %v must name the key, not echo the value", err)
	}
}

func TestEncodeAndEnvEntry(t *testing.T) {
	s := Set{"b": "2", "a": "1"}
	if got := Encode(s); got != "a:1,b:2" {
		t.Errorf("Encode = %q", got)
	}
	if got := EnvEntry(s); got != "YNH_LABELS=a:1,b:2" {
		t.Errorf("EnvEntry = %q", got)
	}
	back, err := ParseList(Encode(s))
	if err != nil || Encode(back) != Encode(s) {
		t.Errorf("round trip = %v, %v", back, err)
	}
}

func TestMatches(t *testing.T) {
	s := Set{"a": "1", "b": "2"}
	if !s.Matches(Set{"a": "1"}) || !s.Matches(Set{}) || !s.Matches(Set{"a": "1", "b": "2"}) {
		t.Error("a subset must match")
	}
	if s.Matches(Set{"a": "2"}) || s.Matches(Set{"c": "1"}) {
		t.Error("a different value or a missing key must not match")
	}
}
