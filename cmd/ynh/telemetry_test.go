package main

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/agent"
	"github.com/eyelock/ynh/internal/clischema"
	"github.com/eyelock/ynh/internal/telemetry"
)

// ynrRegistry is the shape ynr reads from `<tool> telemetry registry --format
// json` (github.com/eyelock/ynr, internal/registry.Registry, which ynf
// defined), copied here and decoded strictly, so ynh's output cannot grow,
// rename or drop a field without this test noticing.
type ynrRegistry struct {
	Tool    string `json:"tool"`
	Version string `json:"version"`
	Semconv struct {
		Name      string `json:"name"`
		Version   string `json:"version"`
		SchemaURL string `json:"schema_url"`
	} `json:"semantic_conventions"`
	Attributes []struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Members []struct {
			ID    string `json:"id"`
			Value string `json:"value"`
			Brief string `json:"brief"`
		} `json:"members,omitempty"`
		Brief       string         `json:"brief"`
		Stability   string         `json:"stability"`
		Examples    []any          `json:"examples,omitempty"`
		Annotations map[string]any `json:"annotations,omitempty"`
	} `json:"attributes"`
	Standard []string    `json:"standard_attributes"`
	Spans    []ynrSignal `json:"spans"`
	Events   []ynrSignal `json:"events"`
	Metrics  []ynrSignal `json:"metrics"`
}

type ynrSignal struct {
	Name       string `json:"name"`
	Kind       string `json:"kind,omitempty"`
	Instrument string `json:"instrument,omitempty"`
	Unit       string `json:"unit,omitempty"`
	Brief      string `json:"brief"`
	Attributes []struct {
		Name             string `json:"name"`
		RequirementLevel string `json:"requirement_level"`
		Cardinality      int    `json:"cardinality,omitempty"`
	} `json:"attributes"`
}

func TestTelemetryRegistryJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := cmdTelemetryTo([]string{"registry", "--format", "json"}, &stdout, &stderr); err != nil {
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	var v any
	if err := json.Unmarshal(stdout.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	schema, err := clischema.Get("telemetry-registry")
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(v); err != nil {
		t.Errorf("does not validate: %v\n%s", err, stdout.String())
	}

	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	dec.DisallowUnknownFields()
	var r ynrRegistry
	if err := dec.Decode(&r); err != nil {
		t.Fatalf("not the shape ynr reads: %v", err)
	}
	if r.Tool != "ynh" || r.Semconv.Version != telemetry.SemconvVersion || r.Metrics == nil {
		t.Errorf("tool %q, semconv %q, metrics %v", r.Tool, r.Semconv.Version, r.Metrics)
	}
	var spans, events []string
	for _, s := range r.Spans {
		spans = append(spans, s.Name)
	}
	for _, e := range r.Events {
		events = append(events, e.Name)
	}
	if !slices.Equal(spans, []string{telemetry.SpanRun, telemetry.SpanCheck}) || !slices.Equal(events, []string{telemetry.EventRunStarted, telemetry.EventRunFinished}) {
		t.Errorf("spans %v, events %v", spans, events)
	}
}

func TestTelemetryRegistryText(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := cmdTelemetryTo([]string{"registry"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"following otel " + telemetry.SemconvVersion, "  ynh.run.outcome (enum)", "  ynh.run (internal)", "  ynh.check (client)", "  ynh.run.started", "metrics\n  none"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("text lacks %q:\n%s", want, stdout.String())
		}
	}
}

func TestTelemetryRegistryRefusals(t *testing.T) {
	for _, args := range [][]string{nil, {"spool"}, {"registry", "--format", "yaml"}, {"registry", "--nope"}, {"registry", "extra"}} {
		var stdout, stderr bytes.Buffer
		if err := cmdTelemetryTo(args, &stdout, &stderr); err == nil {
			t.Errorf("%v: no error", args)
		}
	}
}

// The outcomes and bounds the run reports are members of the registry's
// enums, so a new exit code or budget cannot reach telemetry unregistered.
func TestRunVocabularyIsRegistered(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := cmdTelemetryTo([]string{"registry", "--format", "json"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	var r ynrRegistry
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	members := map[string][]string{}
	for _, a := range r.Attributes {
		for _, m := range a.Members {
			members[a.ID] = append(members[a.ID], m.Value)
		}
	}
	for code, word := range runOutcomes {
		if !slices.Contains(members[string(telemetry.AttrRunOutcome)], word) {
			t.Errorf("exit %d's outcome %q is not a member of ynh.run.outcome", code, word)
		}
	}
	for _, b := range []agent.BudgetType{agent.BudgetTurns, agent.BudgetTokens, agent.BudgetWallClock} {
		if !slices.Contains(members[string(telemetry.AttrRunBoundBy)], string(b)) {
			t.Errorf("budget %q is not a member of ynh.run.bound_by", b)
		}
	}
}
