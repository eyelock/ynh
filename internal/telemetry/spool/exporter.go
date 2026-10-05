package spool

import (
	"context"
	"encoding/json"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// TraceExporter writes spans to a Writer. It never returns an error.
type TraceExporter struct {
	w       *Writer
	stopped atomic.Bool
}

var _ sdktrace.SpanExporter = (*TraceExporter)(nil)

// NewTraceExporter returns a span exporter writing to w.
func NewTraceExporter(w *Writer) *TraceExporter { return &TraceExporter{w: w} }

// ExportSpans writes spans as one ExportTraceServiceRequest line.
func (e *TraceExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	if len(spans) == 0 {
		return nil
	}
	if e.stopped.Load() {
		e.w.dropped.Add(int64(len(spans)))
		return nil
	}
	e.w.writeEncoded(traceRequest(spans), len(spans))
	return nil
}

// Shutdown stops the exporter. It leaves the Writer open, since the log
// exporter may share it; close the Writer once both have shut down.
func (e *TraceExporter) Shutdown(context.Context) error {
	e.stopped.Store(true)
	return nil
}

// LogExporter writes log records to a Writer. It never returns an error.
type LogExporter struct {
	w       *Writer
	stopped atomic.Bool
}

var _ sdklog.Exporter = (*LogExporter)(nil)

// NewLogExporter returns a log record exporter writing to w.
func NewLogExporter(w *Writer) *LogExporter { return &LogExporter{w: w} }

// Export writes records as one ExportLogsServiceRequest line.
func (e *LogExporter) Export(_ context.Context, records []sdklog.Record) error {
	if len(records) == 0 {
		return nil
	}
	if e.stopped.Load() {
		e.w.dropped.Add(int64(len(records)))
		return nil
	}
	e.w.writeEncoded(logsRequest(records), len(records))
	return nil
}

// ForceFlush flushes the Writer's open file to disk, bounded by its
// SyncTimeout.
func (e *LogExporter) ForceFlush(context.Context) error {
	e.w.Sync()
	return nil
}

// Shutdown stops the exporter. It leaves the Writer open; see
// TraceExporter.Shutdown.
func (e *LogExporter) Shutdown(context.Context) error {
	e.stopped.Store(true)
	return nil
}

// writeEncoded marshals one export request and writes it as a line.
func (w *Writer) writeEncoded(req any, records int) {
	data, err := json.Marshal(req)
	if err != nil {
		w.errors.Add(1)
		w.dropped.Add(int64(records))
		return
	}
	w.writeLine(append(data, '\n'), records)
}

// Span flags carry the trace flags in the low byte and, in two further bits,
// whether the parent (or linked) span context is remote.
const (
	flagHasIsRemote = 0x100
	flagIsRemote    = 0x200
)

func spanFlags(sc trace.SpanContext) uint32 {
	f := uint32(sc.TraceFlags()) | flagHasIsRemote
	if sc.IsRemote() {
		f |= flagIsRemote
	}
	return f
}

type jsonTraceRequest struct {
	ResourceSpans []jsonResourceSpans `json:"resourceSpans"`
}

type jsonResourceSpans struct {
	Resource   jsonResource     `json:"resource"`
	ScopeSpans []jsonScopeSpans `json:"scopeSpans"`
	SchemaURL  string           `json:"schemaUrl,omitempty"`
}

type jsonScopeSpans struct {
	Scope     jsonScope  `json:"scope"`
	Spans     []jsonSpan `json:"spans"`
	SchemaURL string     `json:"schemaUrl,omitempty"`
}

type jsonSpan struct {
	TraceID                string         `json:"traceId"`
	SpanID                 string         `json:"spanId"`
	TraceState             string         `json:"traceState,omitempty"`
	ParentSpanID           string         `json:"parentSpanId,omitempty"`
	Flags                  uint32         `json:"flags,omitempty"`
	Name                   string         `json:"name"`
	Kind                   int            `json:"kind,omitempty"`
	StartTimeUnixNano      string         `json:"startTimeUnixNano,omitempty"`
	EndTimeUnixNano        string         `json:"endTimeUnixNano,omitempty"`
	Attributes             []jsonKeyValue `json:"attributes,omitempty"`
	DroppedAttributesCount int            `json:"droppedAttributesCount,omitempty"`
	Events                 []jsonEvent    `json:"events,omitempty"`
	DroppedEventsCount     int            `json:"droppedEventsCount,omitempty"`
	Links                  []jsonLink     `json:"links,omitempty"`
	DroppedLinksCount      int            `json:"droppedLinksCount,omitempty"`
	Status                 *jsonStatus    `json:"status,omitempty"`
}

type jsonEvent struct {
	TimeUnixNano           string         `json:"timeUnixNano,omitempty"`
	Name                   string         `json:"name"`
	Attributes             []jsonKeyValue `json:"attributes,omitempty"`
	DroppedAttributesCount int            `json:"droppedAttributesCount,omitempty"`
}

type jsonLink struct {
	TraceID                string         `json:"traceId"`
	SpanID                 string         `json:"spanId"`
	TraceState             string         `json:"traceState,omitempty"`
	Attributes             []jsonKeyValue `json:"attributes,omitempty"`
	DroppedAttributesCount int            `json:"droppedAttributesCount,omitempty"`
	Flags                  uint32         `json:"flags,omitempty"`
}

// jsonStatus codes follow OTLP (unset 0, ok 1, error 2), which is not the
// order of the Go API's codes.
type jsonStatus struct {
	Message string `json:"message,omitempty"`
	Code    int    `json:"code,omitempty"`
}

func statusOf(s sdktrace.Status) *jsonStatus {
	switch s.Code {
	case codes.Ok:
		return &jsonStatus{Code: 1}
	case codes.Error:
		return &jsonStatus{Code: 2, Message: s.Description}
	default:
		return nil
	}
}

func traceRequest(spans []sdktrace.ReadOnlySpan) jsonTraceRequest {
	var req jsonTraceRequest
	resIndex := map[attribute.Distinct]int{}
	scopeIndex := map[attribute.Distinct]map[scopeKey]int{}
	for _, s := range spans {
		rk := resourceKey(s.Resource())
		ri, ok := resIndex[rk]
		if !ok {
			ri = len(req.ResourceSpans)
			resIndex[rk] = ri
			scopeIndex[rk] = map[scopeKey]int{}
			req.ResourceSpans = append(req.ResourceSpans, jsonResourceSpans{
				Resource:  encodeResource(s.Resource()),
				SchemaURL: schemaURL(s.Resource()),
			})
		}
		rs := &req.ResourceSpans[ri]
		sk := keyOf(s.InstrumentationScope())
		si, ok := scopeIndex[rk][sk]
		if !ok {
			si = len(rs.ScopeSpans)
			scopeIndex[rk][sk] = si
			rs.ScopeSpans = append(rs.ScopeSpans, jsonScopeSpans{
				Scope:     encodeScope(s.InstrumentationScope()),
				SchemaURL: s.InstrumentationScope().SchemaURL,
			})
		}
		rs.ScopeSpans[si].Spans = append(rs.ScopeSpans[si].Spans, encodeSpan(s))
	}
	return req
}

func encodeSpan(s sdktrace.ReadOnlySpan) jsonSpan {
	sc := s.SpanContext()
	js := jsonSpan{
		TraceID:                traceIDHex(sc.TraceID()),
		SpanID:                 spanIDHex(sc.SpanID()),
		TraceState:             sc.TraceState().String(),
		ParentSpanID:           spanIDHex(s.Parent().SpanID()),
		Flags:                  spanFlags(s.Parent()) &^ 0xff,
		Name:                   s.Name(),
		Kind:                   int(s.SpanKind()),
		StartTimeUnixNano:      unixNano(s.StartTime()),
		EndTimeUnixNano:        unixNano(s.EndTime()),
		Attributes:             encodeAttrs(s.Attributes()),
		DroppedAttributesCount: s.DroppedAttributes(),
		DroppedEventsCount:     s.DroppedEvents(),
		DroppedLinksCount:      s.DroppedLinks(),
		Status:                 statusOf(s.Status()),
	}
	js.Flags |= uint32(sc.TraceFlags())
	for _, ev := range s.Events() {
		js.Events = append(js.Events, jsonEvent{
			TimeUnixNano:           unixNano(ev.Time),
			Name:                   ev.Name,
			Attributes:             encodeAttrs(ev.Attributes),
			DroppedAttributesCount: ev.DroppedAttributeCount,
		})
	}
	for _, l := range s.Links() {
		js.Links = append(js.Links, jsonLink{
			TraceID:                traceIDHex(l.SpanContext.TraceID()),
			SpanID:                 spanIDHex(l.SpanContext.SpanID()),
			TraceState:             l.SpanContext.TraceState().String(),
			Attributes:             encodeAttrs(l.Attributes),
			DroppedAttributesCount: l.DroppedAttributeCount,
			Flags:                  spanFlags(l.SpanContext),
		})
	}
	return js
}

func resourceKey(r *resource.Resource) attribute.Distinct {
	if r == nil {
		return attribute.EmptySet().Equivalent()
	}
	return r.Equivalent()
}

func schemaURL(r *resource.Resource) string {
	if r == nil {
		return ""
	}
	return r.SchemaURL()
}

type jsonLogsRequest struct {
	ResourceLogs []jsonResourceLogs `json:"resourceLogs"`
}

type jsonResourceLogs struct {
	Resource  jsonResource    `json:"resource"`
	ScopeLogs []jsonScopeLogs `json:"scopeLogs"`
	SchemaURL string          `json:"schemaUrl,omitempty"`
}

type jsonScopeLogs struct {
	Scope      jsonScope       `json:"scope"`
	LogRecords []jsonLogRecord `json:"logRecords"`
	SchemaURL  string          `json:"schemaUrl,omitempty"`
}

type jsonLogRecord struct {
	TimeUnixNano           string         `json:"timeUnixNano,omitempty"`
	ObservedTimeUnixNano   string         `json:"observedTimeUnixNano,omitempty"`
	SeverityNumber         int            `json:"severityNumber,omitempty"`
	SeverityText           string         `json:"severityText,omitempty"`
	Body                   *jsonValue     `json:"body,omitempty"`
	Attributes             []jsonKeyValue `json:"attributes,omitempty"`
	DroppedAttributesCount int            `json:"droppedAttributesCount,omitempty"`
	Flags                  uint32         `json:"flags,omitempty"`
	TraceID                string         `json:"traceId,omitempty"`
	SpanID                 string         `json:"spanId,omitempty"`
	EventName              string         `json:"eventName,omitempty"`
}

func logsRequest(records []sdklog.Record) jsonLogsRequest {
	var req jsonLogsRequest
	resIndex := map[attribute.Distinct]int{}
	scopeIndex := map[attribute.Distinct]map[scopeKey]int{}
	for i := range records {
		r := &records[i]
		rk := resourceKey(r.Resource())
		ri, ok := resIndex[rk]
		if !ok {
			ri = len(req.ResourceLogs)
			resIndex[rk] = ri
			scopeIndex[rk] = map[scopeKey]int{}
			req.ResourceLogs = append(req.ResourceLogs, jsonResourceLogs{
				Resource:  encodeResource(r.Resource()),
				SchemaURL: schemaURL(r.Resource()),
			})
		}
		rl := &req.ResourceLogs[ri]
		sk := keyOf(r.InstrumentationScope())
		si, ok := scopeIndex[rk][sk]
		if !ok {
			si = len(rl.ScopeLogs)
			scopeIndex[rk][sk] = si
			rl.ScopeLogs = append(rl.ScopeLogs, jsonScopeLogs{
				Scope:     encodeScope(r.InstrumentationScope()),
				SchemaURL: r.InstrumentationScope().SchemaURL,
			})
		}
		rl.ScopeLogs[si].LogRecords = append(rl.ScopeLogs[si].LogRecords, encodeLogRecord(r))
	}
	return req
}

func encodeLogRecord(r *sdklog.Record) jsonLogRecord {
	jr := jsonLogRecord{
		TimeUnixNano:           unixNano(r.Timestamp()),
		ObservedTimeUnixNano:   unixNano(r.ObservedTimestamp()),
		SeverityNumber:         int(r.Severity()),
		SeverityText:           r.SeverityText(),
		DroppedAttributesCount: r.DroppedAttributes(),
		Flags:                  uint32(r.TraceFlags()),
		TraceID:                traceIDHex(r.TraceID()),
		SpanID:                 spanIDHex(r.SpanID()),
		EventName:              r.EventName(),
	}
	if body := r.Body(); body.Type() != attribute.EMPTY {
		v := encodeValue(body)
		jr.Body = &v
	}
	r.WalkAttributes(func(kv attribute.KeyValue) bool {
		jr.Attributes = append(jr.Attributes, jsonKeyValue{Key: string(kv.Key), Value: encodeValue(kv.Value)})
		return true
	})
	return jr
}
