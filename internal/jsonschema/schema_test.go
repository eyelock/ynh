package jsonschema

import (
	"encoding/json"
	"strings"
	"testing"
)

func compileOne(t *testing.T, src string) *Schema {
	t.Helper()
	c := NewCompiler()
	if err := c.Add("test://main", []byte(src)); err != nil {
		t.Fatalf("Add: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(src), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	id, _ := raw["$id"].(string)
	if id == "" {
		id = "test://main"
	}
	s, err := c.Compile(id)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return s
}

func parse(t *testing.T, src string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(src), &v); err != nil {
		t.Fatalf("parse: %v", err)
	}
	return v
}

func TestType(t *testing.T) {
	s := compileOne(t, `{"type": "object"}`)
	if err := s.Validate(parse(t, `{}`)); err != nil {
		t.Errorf("expected ok, got %v", err)
	}
	if err := s.Validate(parse(t, `[]`)); err == nil {
		t.Errorf("expected type error")
	}
}

func TestRequiredAndAdditionalProperties(t *testing.T) {
	s := compileOne(t, `{
		"type": "object",
		"properties": {"name": {"type": "string"}},
		"required": ["name"],
		"additionalProperties": false
	}`)
	if err := s.Validate(parse(t, `{"name": "x"}`)); err != nil {
		t.Errorf("expected ok, got %v", err)
	}
	if err := s.Validate(parse(t, `{}`)); err == nil {
		t.Errorf("expected missing-required error")
	}
	if err := s.Validate(parse(t, `{"name": "x", "extra": 1}`)); err == nil {
		t.Errorf("expected additional-property error")
	}
}

func TestEnum(t *testing.T) {
	s := compileOne(t, `{"type": "string", "enum": ["a", "b"]}`)
	if err := s.Validate("a"); err != nil {
		t.Errorf("a should be ok: %v", err)
	}
	if err := s.Validate("c"); err == nil {
		t.Errorf("c should fail")
	}
}

func TestOneOf(t *testing.T) {
	s := compileOne(t, `{
		"oneOf": [
			{"type": "object", "properties": {"ok": {"type": "boolean"}}, "required": ["ok"]},
			{"type": "object", "properties": {"err": {"type": "string"}}, "required": ["err"]}
		]
	}`)
	if err := s.Validate(parse(t, `{"ok": true}`)); err != nil {
		t.Errorf("ok branch should match: %v", err)
	}
	if err := s.Validate(parse(t, `{"err": "x"}`)); err != nil {
		t.Errorf("err branch should match: %v", err)
	}
	if err := s.Validate(parse(t, `{}`)); err == nil {
		t.Errorf("empty should match neither")
	}
}

func TestLocalRef(t *testing.T) {
	src := `{
		"$id": "test://main",
		"$defs": {
			"Item": {"type": "string", "minLength": 1}
		},
		"type": "array",
		"items": {"$ref": "#/$defs/Item"}
	}`
	s := compileOne(t, src)
	if err := s.Validate(parse(t, `["a", "bb"]`)); err != nil {
		t.Errorf("expected ok, got %v", err)
	}
	if err := s.Validate(parse(t, `[""]`)); err == nil {
		t.Errorf("expected minLength error")
	}
}

func TestExternalRef(t *testing.T) {
	c := NewCompiler()
	if err := c.Add("test://shared", []byte(`{
		"$id": "test://shared",
		"$defs": {
			"Name": {"type": "string", "pattern": "^[a-z]+$"}
		}
	}`)); err != nil {
		t.Fatalf("Add shared: %v", err)
	}
	if err := c.Add("test://main", []byte(`{
		"$id": "test://main",
		"type": "object",
		"properties": {"name": {"$ref": "test://shared#/$defs/Name"}},
		"required": ["name"]
	}`)); err != nil {
		t.Fatalf("Add main: %v", err)
	}
	s, err := c.Compile("test://main")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if err := s.Validate(parse(t, `{"name": "abc"}`)); err != nil {
		t.Errorf("expected ok, got %v", err)
	}
	if err := s.Validate(parse(t, `{"name": "ABC"}`)); err == nil {
		t.Errorf("expected pattern error")
	}
}

func TestUnsupportedKeywordRejected(t *testing.T) {
	c := NewCompiler()
	if err := c.Add("test://main", []byte(`{"if": {"type": "string"}}`)); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := c.Compile("test://main"); err == nil {
		t.Errorf("expected unsupported-keyword error")
	} else if !strings.Contains(err.Error(), "unsupported keyword") {
		t.Errorf("expected unsupported keyword error, got %v", err)
	}
}

func TestAnnotationsIgnored(t *testing.T) {
	s := compileOne(t, `{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$comment": "ignored",
		"title": "ignored",
		"description": "ignored",
		"x-capabilities": "0.4",
		"type": "string"
	}`)
	if err := s.Validate("hello"); err != nil {
		t.Errorf("expected ok, got %v", err)
	}
}

func TestDeepEqualNested(t *testing.T) {
	// const with object value — deepEqual recurses through map.
	s := compileOne(t, `{
		"const": {"a": [1, 2], "b": {"c": "d"}}
	}`)
	if err := s.Validate(parse(t, `{"a": [1, 2], "b": {"c": "d"}}`)); err != nil {
		t.Errorf("matching nested const should pass: %v", err)
	}
	if err := s.Validate(parse(t, `{"a": [1, 3], "b": {"c": "d"}}`)); err == nil {
		t.Errorf("differing nested array should fail")
	}
	if err := s.Validate(parse(t, `{"a": [1, 2]}`)); err == nil {
		t.Errorf("missing key should fail")
	}
}

func TestTypePrimitives(t *testing.T) {
	cases := []struct {
		typ     string
		valid   string
		invalid string
	}{
		{"boolean", `true`, `"true"`},
		{"null", `null`, `"x"`},
		{"number", `3.14`, `"x"`},
	}
	for _, tc := range cases {
		t.Run(tc.typ, func(t *testing.T) {
			s := compileOne(t, `{"type": "`+tc.typ+`"}`)
			if err := s.Validate(parse(t, tc.valid)); err != nil {
				t.Errorf("valid %s should pass: %v", tc.typ, err)
			}
			if err := s.Validate(parse(t, tc.invalid)); err == nil {
				t.Errorf("invalid %s should fail", tc.typ)
			}
		})
	}
}

func TestTypeArray(t *testing.T) {
	// "type": ["string", "null"] permits either.
	s := compileOne(t, `{"type": ["string", "null"]}`)
	if err := s.Validate("x"); err != nil {
		t.Errorf("string should pass: %v", err)
	}
	if err := s.Validate(nil); err != nil {
		t.Errorf("null should pass: %v", err)
	}
	if err := s.Validate(parse(t, `1`)); err == nil {
		t.Errorf("number should fail")
	}
}

func TestMinMax(t *testing.T) {
	s := compileOne(t, `{"type": "number", "minimum": 0, "maximum": 10}`)
	if err := s.Validate(parse(t, `5`)); err != nil {
		t.Errorf("5 in range: %v", err)
	}
	if err := s.Validate(parse(t, `-1`)); err == nil {
		t.Errorf("-1 should fail minimum")
	}
	if err := s.Validate(parse(t, `11`)); err == nil {
		t.Errorf("11 should fail maximum")
	}
}

func TestAllOfFails(t *testing.T) {
	s := compileOne(t, `{
		"allOf": [
			{"type": "string"},
			{"minLength": 3}
		]
	}`)
	if err := s.Validate("abc"); err != nil {
		t.Errorf("abc should pass: %v", err)
	}
	if err := s.Validate("a"); err == nil {
		t.Errorf("a should fail allOf")
	}
}

func TestUnknownSchemaCompile(t *testing.T) {
	c := NewCompiler()
	if _, err := c.Compile("not-registered"); err == nil {
		t.Errorf("expected error for unregistered schema")
	}
}

func TestRefToMissingTarget(t *testing.T) {
	c := NewCompiler()
	if err := c.Add("test://main", []byte(`{
		"$id": "test://main",
		"$ref": "#/$defs/Missing"
	}`)); err != nil {
		t.Fatalf("Add: %v", err)
	}
	s, err := c.Compile("test://main")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if err := s.Validate("x"); err == nil {
		t.Errorf("expected ref-not-found error at validate time")
	}
}

// TestCompileMalformedKeywords exercises the validation paths in compile()
// that reject malformed schema documents — these are explicit error returns,
// not just unsupported-keyword rejections.
func TestCompileMalformedKeywords(t *testing.T) {
	cases := map[string]string{
		"bad type":                    `{"type": 1}`,
		"type array entry not string": `{"type": [1]}`,
		"required not string":         `{"required": [1]}`,
		"items not object":            `{"items": "x"}`,
		"properties value not object": `{"properties": {"x": "y"}}`,
		"additionalProperties bad":    `{"additionalProperties": 1}`,
		"oneOf entry not object":      `{"oneOf": ["x"]}`,
		"bad pattern":                 `{"pattern": "([unclosed"}`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			c := NewCompiler()
			if err := c.Add("test://main", []byte(src)); err != nil {
				t.Fatalf("Add: %v", err)
			}
			if _, err := c.Compile("test://main"); err == nil {
				t.Errorf("expected compile error for %s", name)
			}
		})
	}
}

func TestAddMalformedJSON(t *testing.T) {
	c := NewCompiler()
	if err := c.Add("test://main", []byte(`not json`)); err == nil {
		t.Error("expected parse error")
	}
}

func TestExternalRefUnregistered(t *testing.T) {
	c := NewCompiler()
	if err := c.Add("test://main", []byte(`{
		"$id": "test://main",
		"$ref": "test://missing#/$defs/X"
	}`)); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile("test://main")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate("x"); err == nil {
		t.Error("expected unregistered-external error")
	}
}

func TestStringBoundsAndPattern(t *testing.T) {
	s := compileOne(t, `{"type": "string", "minLength": 2, "maxLength": 5, "pattern": "^[a-z]+$"}`)
	if err := s.Validate("abc"); err != nil {
		t.Errorf("ok: %v", err)
	}
	if err := s.Validate("a"); err == nil {
		t.Error("minLength")
	}
	if err := s.Validate("abcdef"); err == nil {
		t.Error("maxLength")
	}
	if err := s.Validate("ABC"); err == nil {
		t.Error("pattern")
	}
}

func TestAdditionalPropertiesSchema(t *testing.T) {
	// additionalProperties: { schema } — every unknown property validates
	// against the schema.
	s := compileOne(t, `{
		"type": "object",
		"properties": {"name": {"type": "string"}},
		"additionalProperties": {"type": "integer"}
	}`)
	if err := s.Validate(parse(t, `{"name": "x", "count": 3}`)); err != nil {
		t.Errorf("ok: %v", err)
	}
	if err := s.Validate(parse(t, `{"name": "x", "count": "bad"}`)); err == nil {
		t.Error("additional should fail integer check")
	}
}

func TestRefExternalRootDocument(t *testing.T) {
	// Ref to the root of an external schema (no fragment).
	c := NewCompiler()
	if err := c.Add("test://shared", []byte(`{"$id": "test://shared", "type": "string"}`)); err != nil {
		t.Fatal(err)
	}
	if err := c.Add("test://main", []byte(`{"$id": "test://main", "$ref": "test://shared"}`)); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile("test://main")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate("x"); err != nil {
		t.Errorf("ok: %v", err)
	}
	if err := s.Validate(parse(t, `1`)); err == nil {
		t.Error("integer should fail string ref")
	}
}

func TestInteger(t *testing.T) {
	s := compileOne(t, `{"type": "integer"}`)
	if err := s.Validate(parse(t, `1`)); err != nil {
		t.Errorf("1 should be integer: %v", err)
	}
	if err := s.Validate(parse(t, `1.5`)); err == nil {
		t.Errorf("1.5 should not match integer")
	}
}
