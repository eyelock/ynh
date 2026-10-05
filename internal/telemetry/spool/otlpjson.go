package spool

import (
	"encoding/base64"
	"math"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/trace"
)

// The types below are the OTLP/JSON encoding of the OTLP protobuf messages
// this package writes, following the protobuf JSON mapping with the OTLP
// exceptions: ids are hex, not base64, and field names are lowerCamelCase.
// Fields at their default value are omitted, as the mapping allows.

type jsonKeyValue struct {
	Key   string    `json:"key"`
	Value jsonValue `json:"value"`
}

// jsonValue is an AnyValue. Exactly one field is set; an empty value has none.
type jsonValue struct {
	StringValue *string        `json:"stringValue,omitempty"`
	BoolValue   *bool          `json:"boolValue,omitempty"`
	IntValue    *string        `json:"intValue,omitempty"`
	DoubleValue any            `json:"doubleValue,omitempty"`
	ArrayValue  *jsonArray     `json:"arrayValue,omitempty"`
	KvlistValue *jsonKeyValues `json:"kvlistValue,omitempty"`
	BytesValue  *string        `json:"bytesValue,omitempty"`
}

type jsonArray struct {
	Values []jsonValue `json:"values"`
}

type jsonKeyValues struct {
	Values []jsonKeyValue `json:"values"`
}

type jsonResource struct {
	Attributes []jsonKeyValue `json:"attributes,omitempty"`
}

type jsonScope struct {
	Name       string         `json:"name,omitempty"`
	Version    string         `json:"version,omitempty"`
	Attributes []jsonKeyValue `json:"attributes,omitempty"`
}

func encodeAttrs(kvs []attribute.KeyValue) []jsonKeyValue {
	if len(kvs) == 0 {
		return nil
	}
	out := make([]jsonKeyValue, 0, len(kvs))
	for _, kv := range kvs {
		out = append(out, jsonKeyValue{Key: string(kv.Key), Value: encodeValue(kv.Value)})
	}
	return out
}

func encodeValue(v attribute.Value) jsonValue {
	switch v.Type() {
	case attribute.BOOL:
		b := v.AsBool()
		return jsonValue{BoolValue: &b}
	case attribute.INT64:
		return intValue(v.AsInt64())
	case attribute.FLOAT64:
		return doubleValue(v.AsFloat64())
	case attribute.STRING:
		s := v.AsString()
		return jsonValue{StringValue: &s}
	case attribute.BYTESLICE:
		s := base64.StdEncoding.EncodeToString(v.AsByteSlice())
		return jsonValue{BytesValue: &s}
	case attribute.BOOLSLICE:
		bs := v.AsBoolSlice()
		vals := make([]jsonValue, len(bs))
		for i := range bs {
			vals[i] = jsonValue{BoolValue: &bs[i]}
		}
		return jsonValue{ArrayValue: &jsonArray{Values: vals}}
	case attribute.INT64SLICE:
		is := v.AsInt64Slice()
		vals := make([]jsonValue, len(is))
		for i, n := range is {
			vals[i] = intValue(n)
		}
		return jsonValue{ArrayValue: &jsonArray{Values: vals}}
	case attribute.FLOAT64SLICE:
		fs := v.AsFloat64Slice()
		vals := make([]jsonValue, len(fs))
		for i, f := range fs {
			vals[i] = doubleValue(f)
		}
		return jsonValue{ArrayValue: &jsonArray{Values: vals}}
	case attribute.STRINGSLICE:
		ss := v.AsStringSlice()
		vals := make([]jsonValue, len(ss))
		for i := range ss {
			vals[i] = jsonValue{StringValue: &ss[i]}
		}
		return jsonValue{ArrayValue: &jsonArray{Values: vals}}
	case attribute.SLICE:
		items := v.AsSlice()
		vals := make([]jsonValue, len(items))
		for i, item := range items {
			vals[i] = encodeValue(item)
		}
		return jsonValue{ArrayValue: &jsonArray{Values: vals}}
	case attribute.MAP:
		return jsonValue{KvlistValue: &jsonKeyValues{Values: encodeAttrs(v.AsMap())}}
	default:
		return jsonValue{}
	}
}

func intValue(n int64) jsonValue {
	s := strconv.FormatInt(n, 10)
	return jsonValue{IntValue: &s}
}

// doubleValue writes NaN and the infinities as the strings the protobuf JSON
// mapping uses, which encoding/json cannot write as numbers.
func doubleValue(f float64) jsonValue {
	switch {
	case math.IsNaN(f):
		return jsonValue{DoubleValue: "NaN"}
	case math.IsInf(f, 1):
		return jsonValue{DoubleValue: "Infinity"}
	case math.IsInf(f, -1):
		return jsonValue{DoubleValue: "-Infinity"}
	}
	// A pointer, so omitempty never drops a real zero.
	return jsonValue{DoubleValue: &f}
}

func encodeResource(r *resource.Resource) jsonResource {
	if r == nil {
		return jsonResource{}
	}
	return jsonResource{Attributes: encodeAttrs(r.Attributes())}
}

func encodeScope(s instrumentation.Scope) jsonScope {
	return jsonScope{Name: s.Name, Version: s.Version, Attributes: encodeAttrs(s.Attributes.ToSlice())}
}

// unixNano writes a timestamp as a decimal string, and the zero time as
// absent, which OTLP reads as unknown.
func unixNano(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return strconv.FormatInt(t.UnixNano(), 10)
}

func traceIDHex(id trace.TraceID) string {
	if !id.IsValid() {
		return ""
	}
	return id.String()
}

func spanIDHex(id trace.SpanID) string {
	if !id.IsValid() {
		return ""
	}
	return id.String()
}

// scopeKey groups records by instrumentation scope.
type scopeKey struct {
	name, version, schemaURL string
	attrs                    attribute.Distinct
}

func keyOf(s instrumentation.Scope) scopeKey {
	return scopeKey{name: s.Name, version: s.Version, schemaURL: s.SchemaURL, attrs: s.Attributes.Equivalent()}
}
