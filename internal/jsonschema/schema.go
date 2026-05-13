// Package jsonschema is a deliberately narrow JSON Schema (draft 2020-12)
// validator scoped to the subset used by ynh's published CLI-output schemas.
//
// It is NOT a general-purpose JSON Schema implementation. Unsupported
// keywords are rejected at Compile time rather than silently passed, so
// schema authors cannot accidentally write contracts that the validator
// does not actually check.
//
// Supported keywords (everything else is rejected at Compile):
//
//	$id, $ref, $defs, $schema, $comment, description, title, x-* (annotation)
//	type, enum, const
//	properties, required, additionalProperties (bool or schema)
//	items
//	pattern, minLength, maxLength
//	minimum, maximum
//	oneOf, allOf, anyOf
//
// Resolution semantics:
//
//   - $ref values are resolved relative to the current schema's $id.
//   - Local refs (starting with "#/$defs/...") resolve inside the current
//     document.
//   - External refs (absolute URLs) resolve against schemas registered with
//     Compiler.Add prior to compilation.
//
// This validator is consumer-internal: it is used by `ynd validate-output`
// and the CI golden round-trip. Downstream consumers pick their own
// validator at their own layer (see plan: consumer boundary).
package jsonschema

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// Compiler holds the set of registered schema resources and produces
// compiled, validation-ready *Schema values.
type Compiler struct {
	resources map[string]rawSchema // keyed by $id
}

// NewCompiler returns an empty compiler.
func NewCompiler() *Compiler {
	return &Compiler{resources: map[string]rawSchema{}}
}

// Add registers a schema document under its $id. The caller passes the raw
// JSON bytes. If the schema lacks an $id, the supplied url is used.
func (c *Compiler) Add(url string, data []byte) error {
	var raw rawSchema
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parsing schema %s: %w", url, err)
	}
	id := raw["$id"]
	if s, ok := id.(string); ok && s != "" {
		c.resources[s] = raw
		return nil
	}
	c.resources[url] = raw
	return nil
}

// Compile produces a validator for the schema registered under id.
func (c *Compiler) Compile(id string) (*Schema, error) {
	raw, ok := c.resources[id]
	if !ok {
		return nil, fmt.Errorf("schema not registered: %s", id)
	}
	s := &Schema{compiler: c, id: id}
	if err := s.compile(raw); err != nil {
		return nil, fmt.Errorf("compiling %s: %w", id, err)
	}
	return s, nil
}

// rawSchema is the lightly-typed intermediate parse of a schema document.
type rawSchema map[string]any

// Schema is a compiled JSON Schema validator.
type Schema struct {
	compiler *Compiler
	id       string

	// Effective constraints. A nil pointer means "not constrained".
	types        []string // permitted "type" values, empty = any
	enumValues   []any
	hasConst     bool
	constValue   any
	properties   map[string]*Schema
	required     []string
	addlProps    *Schema // nil → allow; &Schema{} (no constraints, allow=false) → reject
	addlAllowed  bool    // when addlProps is nil-because-true
	addlExplicit bool    // whether additionalProperties was set
	items        *Schema
	pattern      *regexp.Regexp
	minLength    *int
	maxLength    *int
	minimum      *float64
	maximum      *float64
	oneOf        []*Schema
	allOf        []*Schema
	anyOf        []*Schema
	ref          string // unresolved at compile, resolved at validate
}

var knownKeywords = map[string]bool{
	"$id": true, "$ref": true, "$defs": true, "$schema": true,
	"$comment": true, "description": true, "title": true,
	"type": true, "enum": true, "const": true,
	"properties": true, "required": true, "additionalProperties": true,
	"items":   true,
	"pattern": true, "minLength": true, "maxLength": true,
	"minimum": true, "maximum": true,
	"oneOf": true, "allOf": true, "anyOf": true,
}

func (s *Schema) compile(raw rawSchema) error {
	for k := range raw {
		if knownKeywords[k] || strings.HasPrefix(k, "x-") {
			continue
		}
		return fmt.Errorf("unsupported keyword %q (subset validator)", k)
	}

	if r, ok := raw["$ref"].(string); ok {
		s.ref = r
		return nil
	}

	if t, ok := raw["type"]; ok {
		switch v := t.(type) {
		case string:
			s.types = []string{v}
		case []any:
			for _, e := range v {
				if str, ok := e.(string); ok {
					s.types = append(s.types, str)
				} else {
					return fmt.Errorf(`"type" array entries must be strings`)
				}
			}
		default:
			return fmt.Errorf(`"type" must be string or array of strings`)
		}
	}

	if e, ok := raw["enum"].([]any); ok {
		s.enumValues = e
	}
	if c, ok := raw["const"]; ok {
		s.hasConst = true
		s.constValue = c
	}

	if p, ok := raw["properties"].(map[string]any); ok {
		s.properties = map[string]*Schema{}
		for name, sub := range p {
			subRaw, ok := sub.(map[string]any)
			if !ok {
				return fmt.Errorf("properties.%s: expected object", name)
			}
			child := &Schema{compiler: s.compiler, id: s.id}
			if err := child.compile(subRaw); err != nil {
				return fmt.Errorf("properties.%s: %w", name, err)
			}
			s.properties[name] = child
		}
	}

	if r, ok := raw["required"].([]any); ok {
		for _, e := range r {
			if str, ok := e.(string); ok {
				s.required = append(s.required, str)
			} else {
				return fmt.Errorf(`"required" entries must be strings`)
			}
		}
	}

	if ap, ok := raw["additionalProperties"]; ok {
		s.addlExplicit = true
		switch v := ap.(type) {
		case bool:
			s.addlAllowed = v
			if v {
				s.addlProps = nil
			} else {
				s.addlProps = &Schema{}
			}
		case map[string]any:
			child := &Schema{compiler: s.compiler, id: s.id}
			if err := child.compile(v); err != nil {
				return fmt.Errorf("additionalProperties: %w", err)
			}
			s.addlProps = child
			s.addlAllowed = true
		default:
			return fmt.Errorf(`"additionalProperties" must be bool or schema`)
		}
	} else {
		s.addlAllowed = true
	}

	if it, ok := raw["items"]; ok {
		sub, ok := it.(map[string]any)
		if !ok {
			return fmt.Errorf(`"items" must be a schema object`)
		}
		child := &Schema{compiler: s.compiler, id: s.id}
		if err := child.compile(sub); err != nil {
			return fmt.Errorf("items: %w", err)
		}
		s.items = child
	}

	if p, ok := raw["pattern"].(string); ok {
		re, err := regexp.Compile(p)
		if err != nil {
			return fmt.Errorf(`"pattern" invalid: %w`, err)
		}
		s.pattern = re
	}
	if v, ok := readInt(raw, "minLength"); ok {
		s.minLength = &v
	}
	if v, ok := readInt(raw, "maxLength"); ok {
		s.maxLength = &v
	}
	if v, ok := readFloat(raw, "minimum"); ok {
		s.minimum = &v
	}
	if v, ok := readFloat(raw, "maximum"); ok {
		s.maximum = &v
	}

	for _, kw := range []string{"oneOf", "allOf", "anyOf"} {
		arr, ok := raw[kw].([]any)
		if !ok {
			continue
		}
		var out []*Schema
		for i, e := range arr {
			sub, ok := e.(map[string]any)
			if !ok {
				return fmt.Errorf("%s[%d]: expected object", kw, i)
			}
			child := &Schema{compiler: s.compiler, id: s.id}
			if err := child.compile(sub); err != nil {
				return fmt.Errorf("%s[%d]: %w", kw, i, err)
			}
			out = append(out, child)
		}
		switch kw {
		case "oneOf":
			s.oneOf = out
		case "allOf":
			s.allOf = out
		case "anyOf":
			s.anyOf = out
		}
	}

	return nil
}

// Validate checks value against the schema and returns the first error
// encountered, or nil on success.
func (s *Schema) Validate(value any) error {
	return s.validate(value, "")
}

func (s *Schema) validate(value any, path string) error {
	if s.ref != "" {
		target, err := s.resolveRef(s.ref)
		if err != nil {
			return err
		}
		return target.validate(value, path)
	}

	if len(s.types) > 0 {
		if !matchesAnyType(value, s.types) {
			return fmt.Errorf("%s: expected type %v, got %T", pathOrRoot(path), s.types, value)
		}
	}

	if s.hasConst {
		if !deepEqual(value, s.constValue) {
			return fmt.Errorf("%s: const mismatch", pathOrRoot(path))
		}
	}
	if len(s.enumValues) > 0 {
		ok := false
		for _, e := range s.enumValues {
			if deepEqual(value, e) {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("%s: not in enum", pathOrRoot(path))
		}
	}

	switch v := value.(type) {
	case map[string]any:
		if s.properties != nil || len(s.required) > 0 || s.addlExplicit {
			for _, name := range s.required {
				if _, ok := v[name]; !ok {
					return fmt.Errorf("%s: missing required property %q", pathOrRoot(path), name)
				}
			}
			// Stable iteration for deterministic errors.
			keys := make([]string, 0, len(v))
			for k := range v {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				child, ok := s.properties[k]
				if ok {
					if err := child.validate(v[k], path+"/"+k); err != nil {
						return err
					}
					continue
				}
				if !s.addlAllowed {
					return fmt.Errorf("%s: additional property %q not allowed", pathOrRoot(path), k)
				}
				if s.addlProps != nil {
					if err := s.addlProps.validate(v[k], path+"/"+k); err != nil {
						return err
					}
				}
			}
		}
	case []any:
		if s.items != nil {
			for i, e := range v {
				if err := s.items.validate(e, fmt.Sprintf("%s/%d", path, i)); err != nil {
					return err
				}
			}
		}
	case string:
		if s.pattern != nil && !s.pattern.MatchString(v) {
			return fmt.Errorf("%s: does not match pattern", pathOrRoot(path))
		}
		if s.minLength != nil && len(v) < *s.minLength {
			return fmt.Errorf("%s: minLength %d", pathOrRoot(path), *s.minLength)
		}
		if s.maxLength != nil && len(v) > *s.maxLength {
			return fmt.Errorf("%s: maxLength %d", pathOrRoot(path), *s.maxLength)
		}
	case float64:
		if s.minimum != nil && v < *s.minimum {
			return fmt.Errorf("%s: minimum %v", pathOrRoot(path), *s.minimum)
		}
		if s.maximum != nil && v > *s.maximum {
			return fmt.Errorf("%s: maximum %v", pathOrRoot(path), *s.maximum)
		}
	}

	for i, sub := range s.allOf {
		if err := sub.validate(value, path); err != nil {
			return fmt.Errorf("allOf[%d]: %w", i, err)
		}
	}
	if len(s.anyOf) > 0 {
		any := false
		var lastErr error
		for _, sub := range s.anyOf {
			if err := sub.validate(value, path); err == nil {
				any = true
				break
			} else {
				lastErr = err
			}
		}
		if !any {
			return fmt.Errorf("anyOf: no branch matched (last: %v)", lastErr)
		}
	}
	if len(s.oneOf) > 0 {
		matched := -1
		var firstErr error
		for i, sub := range s.oneOf {
			if err := sub.validate(value, path); err == nil {
				if matched >= 0 {
					return fmt.Errorf("oneOf: matched branches %d and %d", matched, i)
				}
				matched = i
			} else if firstErr == nil {
				firstErr = err
			}
		}
		if matched < 0 {
			return fmt.Errorf("oneOf: no branch matched (first: %v)", firstErr)
		}
	}

	return nil
}

func (s *Schema) resolveRef(ref string) (*Schema, error) {
	if strings.HasPrefix(ref, "#/$defs/") {
		root, ok := s.compiler.resources[s.id]
		if !ok {
			return nil, fmt.Errorf("ref base %s not registered", s.id)
		}
		defs, ok := root["$defs"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("ref %s: no $defs in %s", ref, s.id)
		}
		name := strings.TrimPrefix(ref, "#/$defs/")
		raw, ok := defs[name].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("ref %s: not found in $defs", ref)
		}
		child := &Schema{compiler: s.compiler, id: s.id}
		if err := child.compile(raw); err != nil {
			return nil, fmt.Errorf("ref %s: %w", ref, err)
		}
		return child, nil
	}
	// External ref: may include #/$defs/X suffix.
	base := ref
	fragment := ""
	if idx := strings.Index(ref, "#"); idx >= 0 {
		base = ref[:idx]
		fragment = ref[idx:]
	}
	raw, ok := s.compiler.resources[base]
	if !ok {
		return nil, fmt.Errorf("external ref %s: schema not registered", base)
	}
	if fragment == "" || fragment == "#" {
		child := &Schema{compiler: s.compiler, id: base}
		if err := child.compile(raw); err != nil {
			return nil, fmt.Errorf("ref %s: %w", ref, err)
		}
		return child, nil
	}
	if !strings.HasPrefix(fragment, "#/$defs/") {
		return nil, fmt.Errorf("ref %s: only #/$defs/<name> fragments supported", ref)
	}
	defs, ok := raw["$defs"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("ref %s: no $defs in %s", ref, base)
	}
	name := strings.TrimPrefix(fragment, "#/$defs/")
	subRaw, ok := defs[name].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("ref %s: not found in $defs", ref)
	}
	child := &Schema{compiler: s.compiler, id: base}
	if err := child.compile(subRaw); err != nil {
		return nil, fmt.Errorf("ref %s: %w", ref, err)
	}
	return child, nil
}

func matchesAnyType(value any, types []string) bool {
	for _, t := range types {
		if matchesType(value, t) {
			return true
		}
	}
	return false
}

func matchesType(value any, t string) bool {
	switch t {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	case "number":
		_, ok := value.(float64)
		return ok
	case "integer":
		f, ok := value.(float64)
		if !ok {
			return false
		}
		return f == math.Trunc(f) && !math.IsInf(f, 0)
	}
	return false
}

func deepEqual(a, b any) bool {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			bvv, exists := bv[k]
			if !exists || !deepEqual(v, bvv) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !deepEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}

func readInt(raw rawSchema, key string) (int, bool) {
	v, ok := raw[key]
	if !ok {
		return 0, false
	}
	f, ok := v.(float64)
	if !ok {
		return 0, false
	}
	return int(f), true
}

func readFloat(raw rawSchema, key string) (float64, bool) {
	v, ok := raw[key]
	if !ok {
		return 0, false
	}
	f, ok := v.(float64)
	return f, ok
}

func pathOrRoot(p string) string {
	if p == "" {
		return "<root>"
	}
	return p
}
