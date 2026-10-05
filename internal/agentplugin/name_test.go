package agentplugin

import "testing"

func TestNormalizeName(t *testing.T) {
	for in, want := range map[string]struct {
		out string
		ok  bool
	}{
		"my-plugin":      {"my-plugin", true},
		"acme.tools":     {"acme.tools", true},
		"My_Harness":     {"my-harness", false},
		"ynh-dev":        {"ynh-dev", true},
		"Has__Double":    {"has-double", false},
		"dots..twice":    {"dots.twice", false},
		"-lead-trail-":   {"lead-trail", false},
		"UPPER":          {"upper", false},
		"weird$$chars":   {"weirdchars", false},
		"___":            {"", false},
		"mixed-_.sep":    {"mixed-sep", false},
		"ends.with.dot.": {"ends.with.dot", false},
	} {
		t.Run(in, func(t *testing.T) {
			out, ok := NormalizeName(in)
			if out != want.out || ok != want.ok {
				t.Errorf("NormalizeName(%q) = %q, %v; want %q, %v", in, out, ok, want.out, want.ok)
			}
			if out != "" && !ValidName(out) {
				t.Errorf("normalised %q is not itself valid", out)
			}
		})
	}
}
