package weaver_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/eyelock/ynh/internal/telemetry/weaver"
)

const manifest = `name: t
schema_url: https://example.test/t/1
dependencies:
  - name: otel
    schema_url: https://opentelemetry.io/schemas/1.41.0
    registry_path: https://example.test/x.zip[model]
`

func load(t *testing.T, groups string) (*weaver.Registry, error) {
	t.Helper()
	return weaver.Load(fstest.MapFS{
		"manifest.yaml": {Data: []byte(manifest)},
		"g.yaml":        {Data: []byte(groups)},
	}, "1.2.3")
}

func TestLoadAndGenerate(t *testing.T) {
	r, err := load(t, `groups:
  - id: registry.t
    type: attribute_group
    attributes:
      - {id: t.thing.id, type: string, brief: A thing., stability: development}
      - id: t.kind
        type:
          members:
            - {id: big, value: big, brief: Big.}
        brief: A kind.
        annotations: {ynr.cardinality: 4}
      - {id: t.cost_usd, type: double, brief: Cost., stability: development}
  - id: span.t.work
    type: span
    span_kind: internal
    brief: Work.
    attributes:
      - {ref: t.thing.id, requirement_level: required}
      - {ref: gen_ai.request.model, requirement_level: recommended}
  - id: event.t.began
    type: event
    name: t.began
    brief: Began.
    attributes: [{ref: t.thing.id}]
  - id: metric.t.n
    type: metric
    metric_name: t.n
    instrument: counter
    unit: "{n}"
    brief: Count.
    attributes:
      - {ref: t.kind, requirement_level: required}
`)
	if err != nil {
		t.Fatal(err)
	}
	if r.Tool != "t" || r.Version != "1.2.3" || r.Semconv.Version != "1.41.0" || len(r.Standard) != 1 || r.Standard[0] != "gen_ai.request.model" {
		t.Errorf("%+v", r)
	}
	if r.Metrics[0].Attributes[0].Cardinality != 4 || r.Attributes[1].Type != "enum" || r.Spans[0].Name != "t.work" {
		t.Errorf("%+v", r)
	}
	src, err := weaver.Generate(r, "p")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`AttrThingID attribute.Key = "t.thing.id"`,
		`AttrCostUSD attribute.Key = "t.cost_usd"`,
		`AttrGenAIRequestModel attribute.Key = "gen_ai.request.model"`,
		`SpanWork = "t.work"`,
		`EventBegan = "t.began"`,
		`MetricN = "t.n"`,
		`KindBig = "big"`,
		`"t.kind": 4`,
	} {
		// gofmt aligns the "=" of neighbouring constants, so compare with
		// runs of spaces collapsed.
		if !strings.Contains(strings.Join(strings.Fields(string(src)), " "), want) {
			t.Errorf("generated code lacks %s:\n%s", want, src)
		}
	}
}

// A registry with no metrics or standard attributes prints empty lists, not
// null, so a reader can range over each.
func TestLoadEmptyListsAreNotNil(t *testing.T) {
	r, err := load(t, "groups:\n  - {id: registry.t, type: attribute_group, attributes: [{id: t.a, type: string, brief: A.}]}\n")
	if err != nil {
		t.Fatal(err)
	}
	if r.Metrics == nil || r.Spans == nil || r.Events == nil || r.Standard == nil {
		t.Errorf("nil list in %+v", r)
	}
}

func TestLoadRefusals(t *testing.T) {
	for name, groups := range map[string]string{
		"not yaml":       "groups: [",
		"no id":          "groups:\n  - {id: g, type: attribute_group, attributes: [{type: string}]}",
		"no type":        "groups:\n  - {id: g, type: attribute_group, attributes: [{id: a.b}]}",
		"unknown group":  "groups:\n  - {id: g, type: entity}",
		"defined in use": "groups:\n  - {id: s, type: span, attributes: [{id: a.b, type: string}]}",
	} {
		if _, err := load(t, groups); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
	if _, err := weaver.Load(fstest.MapFS{}, ""); err == nil {
		t.Error("no manifest")
	}
	if _, err := weaver.Load(fstest.MapFS{"manifest.yaml": {Data: []byte("name: [")}}, ""); err == nil {
		t.Error("bad manifest")
	}
}
