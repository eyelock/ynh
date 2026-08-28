package clischema

import (
	"encoding/json"
	"regexp"
	"testing"
)

// TestEveryPatternCompilesInGo guards a class of bug rather than an instance.
//
// JSON Schema specifies ECMA-262 regexes, which support lookaround; Go's RE2
// does not. A pattern using lookahead therefore validates fine in a
// browser-based schema tool and fails to compile in every Go validator ynh
// ships — so the constraint reads as enforced while being enforced nowhere.
//
// That is not hypothetical: plugin.schema.json and marketplace.schema.json
// carried five `^(?!\.\./...)` path-traversal guards that no ynh code path
// could ever apply, while the copy ynd actually enforced silently omitted
// them. Traversal is enforced by pathutil.CheckSubpath instead, where it can
// be done correctly.
func TestEveryPatternCompilesInGo(t *testing.T) {
	all, err := AllRaw()
	if err != nil {
		t.Fatalf("AllRaw: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("no schemas found")
	}

	for name, data := range all {
		var doc any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Errorf("%s: invalid JSON: %v", name, err)
			continue
		}
		for path, pat := range collectPatterns(doc, "") {
			if _, err := regexp.Compile(pat); err != nil {
				t.Errorf("%s%s: pattern %q does not compile in Go: %v\n"+
					"Go uses RE2 — no lookahead/lookbehind. Express the constraint "+
					"in RE2, or enforce it in code and document it in the description.",
					name, path, pat, err)
			}
		}
	}
}

// collectPatterns returns every "pattern" string in the document, keyed by
// its JSON pointer, so a failure names the exact field.
func collectPatterns(node any, path string) map[string]string {
	out := map[string]string{}
	switch v := node.(type) {
	case map[string]any:
		if p, ok := v["pattern"].(string); ok {
			out[path+"/pattern"] = p
		}
		for k, child := range v {
			for cp, cv := range collectPatterns(child, path+"/"+k) {
				out[cp] = cv
			}
		}
	case []any:
		for i, child := range v {
			for cp, cv := range collectPatterns(child, path+"/"+itoa(i)) {
				out[cp] = cv
			}
		}
	}
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
