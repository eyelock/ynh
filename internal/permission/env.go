package permission

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ConfigContentEnv is OpenCode's inline config variable. MCP isolation sets
// it too, to turn off the servers a harness does not compose, so a permission
// block must be merged into it, never set beside or over it.
const ConfigContentEnv = "OPENCODE_CONFIG_CONTENT"

// MergeEnv returns env with add applied. A variable in add replaces the same
// variable in env, except ConfigContentEnv: its effective value (the last one
// in env, which is the one the child sees) is kept and add's content is merged
// into it, object by object, add winning on its own keys. Earlier duplicates of
// a variable that add sets are dropped, so the child sees exactly one.
func MergeEnv(env, add []string) ([]string, error) {
	out := append([]string{}, env...)
	for _, kv := range add {
		name, value, _ := strings.Cut(kv, "=")
		last := -1
		for i, e := range out {
			if strings.HasPrefix(e, name+"=") {
				last = i
			}
		}
		if name == ConfigContentEnv && last >= 0 {
			merged, err := MergeJSON(strings.TrimPrefix(out[last], name+"="), value)
			if err != nil {
				return nil, fmt.Errorf("your %s is not a JSON object: %w", name, err)
			}
			value = merged
		}
		kept := out[:0]
		for _, e := range out {
			if !strings.HasPrefix(e, name+"=") {
				kept = append(kept, e)
			}
		}
		out = append(kept, name+"="+value)
	}
	return out, nil
}

// MergeJSON returns the JSON object existing with add merged into it:
// objects merge key by key all the way down, anything else in add replaces
// what is in existing. An empty existing is an empty object.
func MergeJSON(existing, add string) (string, error) {
	base := map[string]any{}
	if strings.TrimSpace(existing) != "" {
		if err := json.Unmarshal([]byte(existing), &base); err != nil {
			return "", err
		}
	}
	over := map[string]any{}
	if err := json.Unmarshal([]byte(add), &over); err != nil {
		return "", err
	}
	b, err := json.Marshal(mergeObjects(base, over))
	return string(b), err
}

func mergeObjects(base, over map[string]any) map[string]any {
	for k, v := range over {
		ov, ok := v.(map[string]any)
		bv, bok := base[k].(map[string]any)
		if ok && bok {
			base[k] = mergeObjects(bv, ov)
			continue
		}
		base[k] = v
	}
	return base
}
