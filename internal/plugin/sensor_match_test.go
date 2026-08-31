package plugin

import (
	"strings"
	"testing"
)

func TestOutputMatcher(t *testing.T) {
	t.Run("absent means no filter", func(t *testing.T) {
		re, err := Sensor{}.OutputMatcher()
		if err != nil {
			t.Fatalf("an absent match is not an error: %v", err)
		}
		if re != nil {
			t.Error("an absent match must compile to nil, which means every line counts")
		}
	})

	t.Run("valid pattern compiles", func(t *testing.T) {
		s := Sensor{Output: SensorOutput{Format: "text", Match: `^[^ ]+\.go:[0-9]+:`}}
		re, err := s.OutputMatcher()
		if err != nil {
			t.Fatalf("OutputMatcher: %v", err)
		}
		if re == nil {
			t.Fatal("expected a compiled pattern")
		}
		if !re.MatchString("main.go:10:2: something (errcheck)") {
			t.Error("pattern should match a finding line")
		}
		if re.MatchString("3 issues:") {
			t.Error("pattern should not match a summary line")
		}
	})

	t.Run("invalid pattern is an error naming itself", func(t *testing.T) {
		s := Sensor{Output: SensorOutput{Format: "text", Match: `([unclosed`}}
		_, err := s.OutputMatcher()
		if err == nil {
			t.Fatal("a pattern that does not compile must be an error")
		}
		if !strings.Contains(err.Error(), "output.match") {
			t.Errorf("error should name the field, got: %v", err)
		}
	})
}

// A match that does not compile would silently select nothing, turning the
// sensor's whole output into accepted debt on the next baseline write. It is
// caught at validation instead.
func TestValidateSensors_RejectsAnUncompilableMatch(t *testing.T) {
	issues := ValidateSensors(map[string]Sensor{
		"lint": {
			Source: SensorSource{Command: "true"},
			Output: SensorOutput{Format: "text", Match: `([unclosed`},
		},
	}, nil, nil)
	joined := strings.Join(issues, "\n")
	if !strings.Contains(joined, "output.match") {
		t.Errorf("an uncompilable match must be reported, got: %v", issues)
	}
}

func TestValidateSensors_AcceptsAValidMatch(t *testing.T) {
	issues := ValidateSensors(map[string]Sensor{
		"lint": {
			Source: SensorSource{Command: "true"},
			Output: SensorOutput{Format: "text", Match: `^[^ ]+\.go:[0-9]+:[0-9]+:`},
		},
	}, nil, nil)
	for _, i := range issues {
		if strings.Contains(i, "output.match") {
			t.Errorf("a valid match must not be reported: %s", i)
		}
	}
}
