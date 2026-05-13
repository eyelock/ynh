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

func TestInteger(t *testing.T) {
	s := compileOne(t, `{"type": "integer"}`)
	if err := s.Validate(parse(t, `1`)); err != nil {
		t.Errorf("1 should be integer: %v", err)
	}
	if err := s.Validate(parse(t, `1.5`)); err == nil {
		t.Errorf("1.5 should not match integer")
	}
}
