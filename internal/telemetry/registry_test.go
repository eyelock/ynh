package telemetry

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"

	"github.com/eyelock/ynh/internal/telemetry/weaver"
	"github.com/eyelock/ynh/telemetry/registry"
)

func loadRegistry(t *testing.T) *weaver.Registry {
	t.Helper()
	r, err := weaver.Load(registry.FS, "test")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// names_gen.go is exactly what the registry generates, so a name added to the
// registry without `go generate ./...`, or a constant edited by hand, fails.
func TestConstantsMatchTheRegistry(t *testing.T) {
	want, err := weaver.Generate(loadRegistry(t), "telemetry")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("names_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("internal/telemetry/names_gen.go is not what telemetry/registry generates: run `go generate ./internal/telemetry`")
	}
}

// Every attribute a signal uses is ynh's own or an upstream name; ynh's own
// are under its prefix; the pinned conventions are the ones the Go code
// imports.
func TestTheRegistryIsConsistent(t *testing.T) {
	r := loadRegistry(t)
	if r.Tool != "ynh" {
		t.Fatalf("tool %q", r.Tool)
	}
	if r.Semconv.Version != SemconvVersion {
		t.Errorf("the registry pins semantic conventions %s, the code %s", r.Semconv.Version, SemconvVersion)
	}
	known := map[string]bool{}
	for _, a := range r.Attributes {
		known[a.ID] = true
		if !strings.HasPrefix(a.ID, "ynh.") {
			t.Errorf("%s is not under ynh's prefix", a.ID)
		}
	}
	for _, id := range r.Standard {
		known[id] = true
		if strings.HasPrefix(id, "ynh.") {
			t.Errorf("%s is ynh's and must be defined in the registry", id)
		}
	}
	check := func(what string, uses []weaver.Use) {
		for _, u := range uses {
			if !known[u.Name] {
				t.Errorf("%s uses unknown attribute %s", what, u.Name)
			}
		}
	}
	for _, s := range r.Spans {
		check(s.Name, s.Attributes)
	}
	for _, e := range r.Events {
		check(e.Name, e.Attributes)
	}
	for _, m := range r.Metrics {
		check(m.Name, m.Attributes)
	}
}

// The standard names the registry lists are the ones the pinned semconv Go
// package spells, so the registry and the SDK's constants cannot disagree.
func TestStandardNamesMatchSemconv(t *testing.T) {
	for got, want := range map[string]string{
		string(AttrProcessExitCode):                    string(semconv.ProcessExitCodeKey),
		string(AttrGenAIRequestModel):                  string(semconv.GenAIRequestModelKey),
		string(AttrGenAIResponseModel):                 string(semconv.GenAIResponseModelKey),
		string(AttrGenAIUsageInputTokens):              string(semconv.GenAIUsageInputTokensKey),
		string(AttrGenAIUsageOutputTokens):             string(semconv.GenAIUsageOutputTokensKey),
		string(AttrGenAIUsageCacheReadInputTokens):     string(semconv.GenAIUsageCacheReadInputTokensKey),
		string(AttrGenAIUsageCacheCreationInputTokens): string(semconv.GenAIUsageCacheCreationInputTokensKey),
	} {
		if got != want {
			t.Errorf("registry %q, semconv %q", got, want)
		}
	}
}

// No non-test source outside the generated file spells a registered name as
// a string: code uses the constants.
func TestCodeUsesOnlyTheConstants(t *testing.T) {
	r := loadRegistry(t)
	var names []string
	for _, a := range r.Attributes {
		names = append(names, a.ID)
	}
	for _, s := range r.Spans {
		names = append(names, s.Name)
	}
	for _, e := range r.Events {
		names = append(names, e.Name)
	}
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "testdata") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || filepath.Base(p) == "names_gen.go" {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, n := range names {
			if strings.Contains(string(b), `"`+n+`"`) {
				t.Errorf("%s spells the registered name %q: use the generated constant", p, n)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
