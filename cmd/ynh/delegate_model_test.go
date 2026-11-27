package main

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/clischema"
	"github.com/eyelock/ynh/internal/harness"
)

// modelFixture installs three delegates and a parent that reaches each: one
// whose entry names a model and effort, one whose harness has its own default,
// and one with neither. Returns the parent's id.
func modelFixture(t *testing.T) (root string) {
	t.Helper()
	t.Setenv("YNH_HOME", t.TempDir())
	root = t.TempDir()
	writeIncludedHarness(t, root, "withentry", mcpFields("a")+`,"default_model":"dm-a","default_effort":"low"`)
	writeIncludedHarness(t, root, "withdefault", mcpFields("b")+`,"default_model":"dm-b","default_effort":"high"`)
	writeIncludedHarness(t, root, "plain", mcpFields("c"))
	for _, n := range []string{"withentry", "withdefault", "plain"} {
		installLocalHarness(t, filepath.Join(root, n), n)
	}
	writeIncludedHarness(t, root, "parent", `,"delegates_to":[
		{"git":"`+filepath.Join(root, "withentry")+`","model":"entry-m","effort":"medium"},
		{"git":"`+filepath.Join(root, "withdefault")+`"},
		{"git":"`+filepath.Join(root, "plain")+`"},
		{"git":"github.com/eyelock/not-installed"}
	]`)
	installLocalHarness(t, filepath.Join(root, "parent"), "parent")
	return root
}

// ynh info shows the model and effort each delegate opens with when a call
// names none, and where each came from.
func TestInfoShowsEachDelegatesModelAndEffortAndWhereTheyCameFrom(t *testing.T) {
	modelFixture(t)

	var out bytes.Buffer
	if err := cmdInfoTo([]string{"local/parent"}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	lines := infoSection(out.String(), "Delegates to")
	for i, want := range []string{
		"  model=entry-m (entry)  effort=medium (entry)",
		"  model=dm-b (delegate default)  effort=high (delegate default)",
		"  model=(vendor default)  effort=(vendor default)",
	} {
		if !strings.HasSuffix(lines[i], want) {
			t.Errorf("line %d = %q, want it to end %q", i, lines[i], want)
		}
	}
	if strings.Contains(lines[3], "model=") || strings.Contains(lines[3], "effort=") {
		t.Errorf("a delegate ynh cannot read must show nothing, not a guess: %q", lines[3])
	}

	out.Reset()
	if err := cmdInfoTo([]string{"local/parent", "--format", "json"}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	schema, err := clischema.Get("info")
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(any(v)); err != nil {
		t.Errorf("does not validate: %v\n%s", err, out.String())
	}
	dels := v["harness"].(map[string]any)["delegates_to"].([]any)
	type want struct{ model, modelFrom, effort, effortFrom string }
	for i, w := range []want{
		{"entry-m", "entry", "medium", "entry"},
		{"dm-b", "delegate_default", "high", "delegate_default"},
		{"", "vendor_default", "", "vendor_default"},
		{"", "", "", ""},
	} {
		d := dels[i].(map[string]any)
		get := func(k string) string { s, _ := d[k].(string); return s }
		if get("model") != w.model || get("model_from") != w.modelFrom || get("effort") != w.effort || get("effort_from") != w.effortFrom {
			t.Errorf("delegate %d = %v, want %+v", i, d, w)
		}
	}
}

// The endpoint a run opens for an entry carries what the layers need: the
// entry's own model and effort, the delegate harness's default and the vendor.
func TestEndpointCarriesTheEntrysAndTheDelegatesModelAndEffort(t *testing.T) {
	root := modelFixture(t)
	parent, err := harness.LoadByID("local/parent")
	if err != nil {
		t.Fatal(err)
	}
	parent.DelegatesTo = parent.DelegatesTo[:2]
	store := accepted(t, "local/withentry")
	for id, rec := range accepted(t, "local/withdefault").Harnesses {
		store.Harnesses[id] = rec
	}
	t.Setenv("YNH_VENDOR", "codex")
	o, err := openEndpointsWith(parent, "local/parent", t.TempDir(), &store, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer o.close()
	if len(o.eps) != 2 {
		t.Fatalf("endpoints = %d", len(o.eps))
	}
	a, b := o.eps[0].Opts, o.eps[1].Opts
	if a.Model != "entry-m" || a.Effort != "medium" || a.DefaultModel != "dm-a" || a.DefaultEffort != "low" || a.Vendor != "codex" {
		t.Errorf("first endpoint: %+v", a)
	}
	if b.Model != "" || b.Effort != "" || b.DefaultModel != "dm-b" || b.DefaultEffort != "high" {
		t.Errorf("second endpoint: %+v", b)
	}
	_ = root
}
