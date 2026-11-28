package permission

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The schema enums of auto_approve_from repeat the Source words; they must
// stay the same set (ynh #1058).
func TestSchemaEnumsMatchSources(t *testing.T) {
	var want []string
	for _, s := range Sources {
		want = append(want, string(s))
	}
	for _, f := range []string{"shared/harness.schema.json", "cli/trust-show.schema.json"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "schema", f))
		if err != nil {
			t.Fatal(err)
		}
		var enums [][]string
		var walk func(v any)
		walk = func(v any) {
			switch x := v.(type) {
			case map[string]any:
				if p, ok := x["auto_approve_from"].(map[string]any); ok {
					var e []string
					for _, w := range p["enum"].([]any) {
						e = append(e, w.(string))
					}
					enums = append(enums, e)
				}
				for _, c := range x {
					walk(c)
				}
			case []any:
				for _, c := range x {
					walk(c)
				}
			}
		}
		var doc any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		walk(doc)
		if len(enums) == 0 {
			t.Fatalf("%s has no auto_approve_from", f)
		}
		for _, e := range enums {
			if !reflect.DeepEqual(e, want) {
				t.Errorf("%s: enum %s, want %s", f, strings.Join(e, ","), strings.Join(want, ","))
			}
		}
	}
}
