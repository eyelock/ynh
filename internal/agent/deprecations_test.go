package agent

import (
	"reflect"
	"testing"

	"github.com/eyelock/ynh/internal/config"
)

// deprecatedUntilBump lists fields kept only to avoid removing them from a
// structured response, which would bump CapabilitiesVersion. Each must go in
// the next release that bumps it for any reason. This test fails as soon as the
// version moves past the one the field was deprecated at, so the removal cannot
// be forgotten: do it, or move the entry to the new version on purpose.
var deprecatedUntilBump = []struct {
	typ     reflect.Type
	field   string
	jsonKey string
	since   string
	issue   string
}{
	{reflect.TypeFor[SessionStartData](), "Model", "session_start.model", "0.9.0", "https://github.com/eyelock/ynh/issues/443"},
}

func TestDeprecatedFieldsGoAtTheNextCapabilityBump(t *testing.T) {
	for _, d := range deprecatedUntilBump {
		if _, still := d.typ.FieldByName(d.field); !still {
			t.Errorf("%s is gone: delete its entry from deprecatedUntilBump", d.jsonKey)
			continue
		}
		if config.CapabilitiesVersion != d.since {
			t.Errorf("CapabilitiesVersion is %s, but %s (deprecated at %s) is still written. "+
				"Remove it in this bump (schema, goldens, docs), or move its entry to %s if it must wait. See %s",
				config.CapabilitiesVersion, d.jsonKey, d.since, config.CapabilitiesVersion, d.issue)
		}
	}
}
