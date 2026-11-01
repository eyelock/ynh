package harness

import (
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

func TestDelegate_CheckDeclared(t *testing.T) {
	dh := &Harness{
		Name:     "dlg",
		Profiles: map[string]plugin.Profile{"strict": {}},
		Focuses:  map[string]plugin.Focus{"triage": {Profile: "strict"}, "orphan": {Profile: "gone"}},
	}
	cases := []struct {
		name    string
		d       Delegate
		wantErr string
	}{
		{"neither", Delegate{}, ""},
		{"profile ok", Delegate{Profile: "strict"}, ""},
		{"profile missing", Delegate{Profile: "lax"}, `profile "lax", which delegate dlg does not define (it declares: strict)`},
		{"focus ok", Delegate{Focus: "triage"}, ""},
		{"focus missing", Delegate{Focus: "nope"}, `focus "nope", which delegate dlg does not define (it declares: orphan, triage)`},
		{"focus with undeclared profile", Delegate{Focus: "orphan"}, `names profile "gone"`},
	}
	for _, tc := range cases {
		err := tc.d.CheckDeclared(dh)
		if tc.wantErr == "" && err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
		if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
			t.Errorf("%s: got %v, want %q", tc.name, err, tc.wantErr)
		}
	}
	if err := (Delegate{}).CheckDeclared(&Harness{Name: "e"}); err != nil {
		t.Error(err)
	}
	err := (Delegate{Profile: "p"}).CheckDeclared(&Harness{Name: "e"})
	if err == nil || !strings.Contains(err.Error(), "declares no profiles") {
		t.Errorf("got %v", err)
	}
}
