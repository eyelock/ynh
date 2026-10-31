// Package weaver reads ynh's telemetry registry (telemetry/registry), an
// OpenTelemetry Weaver registry, and writes the Go constants generated from
// it. Weaver is not needed to build ynh: this reads the subset of its format
// the registry uses, and CI checks the registry itself with Weaver's own
// `registry check`.
//
// The JSON shape of Registry is the one `ynf telemetry registry --format json`
// prints (github.com/eyelock/ynf, internal/telemetry/registry), which ynr
// reads from every tool. Keep the two in step.
package weaver

import (
	"fmt"
	"io/fs"
	"path"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Registry is a tool's telemetry names, as `ynh telemetry registry --format json` prints them.
type Registry struct {
	Tool    string `json:"tool"`
	Version string `json:"version"`
	// Semconv is the upstream semantic-conventions release the registry is pinned to.
	Semconv Semconv `json:"semantic_conventions"`
	// Attributes are the attributes ynh defines, under its own prefix.
	Attributes []Attribute `json:"attributes"`
	// Standard are the OpenTelemetry attributes ynh uses by name, defined upstream.
	Standard []string `json:"standard_attributes"`
	Spans    []Span   `json:"spans"`
	Events   []Event  `json:"events"`
	Metrics  []Metric `json:"metrics"`
}

// Semconv names the upstream conventions a registry follows.
type Semconv struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	SchemaURL string `json:"schema_url"`
}

// Attribute is an attribute ynh defines.
type Attribute struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	Members     []Member       `json:"members,omitempty"`
	Brief       string         `json:"brief"`
	Stability   string         `json:"stability"`
	Examples    []any          `json:"examples,omitempty"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

// Member is one value of an enumerated attribute.
type Member struct {
	ID    string `json:"id"`
	Value string `json:"value"`
	Brief string `json:"brief"`
}

// Use is an attribute a signal carries.
type Use struct {
	Name             string `json:"name"`
	RequirementLevel string `json:"requirement_level"`
	// Cardinality is the most distinct values the attribute may take on a metric (the
	// ynr.cardinality annotation), where the registry declares one.
	Cardinality int `json:"cardinality,omitempty"`
}

// Span is a kind of span.
type Span struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Brief      string `json:"brief"`
	Attributes []Use  `json:"attributes"`
}

// Event is a kind of event: a log record with an event name.
type Event struct {
	Name       string `json:"name"`
	Brief      string `json:"brief"`
	Attributes []Use  `json:"attributes"`
}

// Metric is a metric the tool records. ynh records none yet.
type Metric struct {
	Name       string `json:"name"`
	Instrument string `json:"instrument"`
	Unit       string `json:"unit"`
	Brief      string `json:"brief"`
	Attributes []Use  `json:"attributes"`
}

type manifest struct {
	Name         string `yaml:"name"`
	SchemaURL    string `yaml:"schema_url"`
	Dependencies []struct {
		Name         string `yaml:"name"`
		SchemaURL    string `yaml:"schema_url"`
		RegistryPath string `yaml:"registry_path"`
	} `yaml:"dependencies"`
}

type memberDoc struct {
	ID    string `yaml:"id"`
	Value string `yaml:"value"`
	Brief string `yaml:"brief"`
}

type attrDoc struct {
	ID          string         `yaml:"id"`
	Ref         string         `yaml:"ref"`
	Type        yaml.Node      `yaml:"type"`
	Brief       string         `yaml:"brief"`
	Stability   string         `yaml:"stability"`
	Examples    []any          `yaml:"examples"`
	Annotations map[string]any `yaml:"annotations"`
	Requirement string         `yaml:"requirement_level"`
}

type groupDoc struct {
	ID         string    `yaml:"id"`
	Type       string    `yaml:"type"`
	Brief      string    `yaml:"brief"`
	Name       string    `yaml:"name"`
	SpanKind   string    `yaml:"span_kind"`
	MetricName string    `yaml:"metric_name"`
	Instrument string    `yaml:"instrument"`
	Unit       string    `yaml:"unit"`
	Attributes []attrDoc `yaml:"attributes"`
}

// Load reads the registry in fsys: manifest.yaml and every other .yaml file, each a list of
// groups. version is the tool's version, which with its name identifies the registry.
func Load(fsys fs.FS, version string) (*Registry, error) {
	mb, err := fs.ReadFile(fsys, "manifest.yaml")
	if err != nil {
		return nil, err
	}
	var m manifest
	if err := yaml.Unmarshal(mb, &m); err != nil {
		return nil, fmt.Errorf("manifest.yaml: %w", err)
	}
	r := &Registry{Tool: m.Name, Version: version}
	for _, d := range m.Dependencies {
		r.Semconv = Semconv{Name: d.Name, SchemaURL: d.SchemaURL, Version: path.Base(d.SchemaURL)}
	}
	files, err := fs.Glob(fsys, "*.yaml")
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var groups []groupDoc
	for _, f := range files {
		if f == "manifest.yaml" {
			continue
		}
		b, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, err
		}
		var doc struct {
			Groups []groupDoc `yaml:"groups"`
		}
		if err := yaml.Unmarshal(b, &doc); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		groups = append(groups, doc.Groups...)
	}

	own := map[string]Attribute{}
	standard := map[string]bool{}
	for _, g := range groups {
		if g.Type != "attribute_group" {
			continue
		}
		for _, a := range g.Attributes {
			if a.ID == "" {
				return nil, fmt.Errorf("group %s: an attribute has no id", g.ID)
			}
			at, err := attribute(a)
			if err != nil {
				return nil, err
			}
			own[a.ID] = at
			r.Attributes = append(r.Attributes, at)
		}
	}
	limit := func(a attrDoc) int {
		if n, ok := a.Annotations["ynr.cardinality"].(int); ok {
			return n
		}
		if n, ok := own[a.Ref].Annotations["ynr.cardinality"].(int); ok {
			return n
		}
		return 0
	}
	uses := func(g groupDoc) ([]Use, error) {
		var out []Use
		for _, a := range g.Attributes {
			if a.Ref == "" {
				return nil, fmt.Errorf("group %s: an attribute is defined here, not referenced", g.ID)
			}
			if _, ok := own[a.Ref]; !ok {
				standard[a.Ref] = true
			}
			out = append(out, Use{Name: a.Ref, RequirementLevel: a.Requirement, Cardinality: limit(a)})
		}
		return out, nil
	}
	for _, g := range groups {
		u, err := uses(g)
		if g.Type != "attribute_group" && err != nil {
			return nil, err
		}
		switch g.Type {
		case "span":
			r.Spans = append(r.Spans, Span{Name: strings.TrimPrefix(g.ID, "span."), Kind: g.SpanKind, Brief: g.Brief, Attributes: u})
		case "event":
			r.Events = append(r.Events, Event{Name: g.Name, Brief: g.Brief, Attributes: u})
		case "metric":
			r.Metrics = append(r.Metrics, Metric{Name: g.MetricName, Instrument: g.Instrument, Unit: g.Unit, Brief: g.Brief, Attributes: u})
		case "attribute_group":
		default:
			return nil, fmt.Errorf("group %s: type %q is not one ynh's registry uses", g.ID, g.Type)
		}
	}
	r.Standard = []string{}
	for id := range standard {
		r.Standard = append(r.Standard, id)
	}
	slices.Sort(r.Standard)
	// Empty lists print as [], not null, so a reader can range over each.
	if r.Attributes == nil {
		r.Attributes = []Attribute{}
	}
	if r.Spans == nil {
		r.Spans = []Span{}
	}
	if r.Events == nil {
		r.Events = []Event{}
	}
	if r.Metrics == nil {
		r.Metrics = []Metric{}
	}
	return r, nil
}

func attribute(a attrDoc) (Attribute, error) {
	at := Attribute{ID: a.ID, Brief: strings.TrimSpace(a.Brief), Stability: a.Stability, Examples: a.Examples, Annotations: a.Annotations}
	switch a.Type.Kind {
	case yaml.ScalarNode:
		at.Type = a.Type.Value
	case yaml.MappingNode:
		var t struct {
			Members []memberDoc `yaml:"members"`
		}
		if err := a.Type.Decode(&t); err != nil {
			return at, fmt.Errorf("attribute %s: %w", a.ID, err)
		}
		at.Type = "enum"
		for _, m := range t.Members {
			at.Members = append(at.Members, Member(m))
		}
	default:
		return at, fmt.Errorf("attribute %s: no type", a.ID)
	}
	return at, nil
}
